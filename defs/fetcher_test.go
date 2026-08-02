package defs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// buildFixtureTarball builds an in-memory .tar.gz tarball mimicking the
// Prowlarr/Indexers repository structure (single root directory + definitions
// under definitions/<version>/), with entirely fabricated YAML content
// for testing (no real definitions, no network).
func buildFixtureTarball(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, content := range entries {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("writing tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("writing tar content: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("closing gzip writer: %v", err)
	}
	return buf.Bytes()
}

func TestFetcher_FetchVersion(t *testing.T) {
	tarball := buildFixtureTarball(t, map[string]string{
		"Indexers-abc123/definitions/v11/testtracker.yml": "id: testtracker\nname: TestTracker\n",
		"Indexers-abc123/definitions/v11/other.yaml":      "id: other\nname: Other\n",
		"Indexers-abc123/definitions/v10/legacy.yml":      "id: legacy\nname: Legacy\n", // different version, should be ignored
		"Indexers-abc123/README.md":                       "# not a definition",
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tarball)
	}))
	defer server.Close()

	f := NewFetcher()
	f.URLOverride = server.URL

	results, err := f.FetchVersion(context.Background(), "v11")
	if err != nil {
		t.Fatalf("FetchVersion: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 definitions, got %d: %+v", len(results), results)
	}

	byID := make(map[string]RawDefinition)
	for _, r := range results {
		byID[r.ID] = r
	}

	if _, ok := byID["testtracker"]; !ok {
		t.Errorf("expected 'testtracker' definition, got ids: %v", keys(byID))
	}
	if _, ok := byID["other"]; !ok {
		t.Errorf("expected 'other' definition, got ids: %v", keys(byID))
	}
	if byID["testtracker"].SHA == "" {
		t.Error("expected non-empty SHA")
	}
}

func keys(m map[string]RawDefinition) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestFetcher_FetchAll(t *testing.T) {
	tarball := buildFixtureTarball(t, map[string]string{
		"Indexers-abc123/definitions/v11/testtracker.yml": "id: testtracker\nname: TestTracker\n",
		"Indexers-abc123/definitions/v11/other.yaml":      "id: other\nname: Other\n",
		"Indexers-abc123/definitions/v10/legacy.yml":      "id: legacy\nname: Legacy\n",
		"Indexers-abc123/definitions/v9/old.yml":          "id: old\nname: Old\n",
		"Indexers-abc123/definitions/test/test.yml":       "id: test\nname: Test\n", // not a version dir, should be ignored
		"Indexers-abc123/definitions/v11/deep/nested.yml": "id: deep\nname: Deep\n", // too deep, should be ignored
		"Indexers-abc123/README.md":                       "# not a definition",
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tarball)
	}))
	defer server.Close()

	f := NewFetcher()
	f.URLOverride = server.URL

	results, err := f.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}

	if len(results) != 4 {
		t.Fatalf("expected 4 definitions, got %d: %+v", len(results), results)
	}

	// Check that all expected definitions are present with correct versions
	byKey := make(map[string]RawDefinition)
	for _, r := range results {
		byKey[r.Version+"/"+r.ID] = r
	}

	tests := []struct {
		key     string
		wantSHA bool
	}{
		{"v11/testtracker", true},
		{"v11/other", true},
		{"v10/legacy", true},
		{"v9/old", true},
	}
	for _, tt := range tests {
		r, ok := byKey[tt.key]
		if !ok {
			t.Errorf("expected definition %q, got keys: %v", tt.key, keysBy(byKey))
			continue
		}
		if tt.wantSHA && r.SHA == "" {
			t.Errorf("definition %q: expected non-empty SHA", tt.key)
		}
	}

	// Verify non-version dirs and deep files were skipped
	if _, ok := byKey["test/test"]; ok {
		t.Error("expected 'test/test' to be skipped (not a version directory)")
	}
	if _, ok := byKey["v11/deep"]; ok {
		t.Error("expected 'v11/deep' to be skipped (too deep)")
	}
}

func keysBy(m map[string]RawDefinition) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestFetcher_ETagRoundtrip(t *testing.T) {
	tarball := buildFixtureTarball(t, map[string]string{
		"Indexers-abc123/definitions/v11/testtracker.yml": "id: testtracker\nname: TestTracker\n",
	})

	const etag = `"abc123-def"`
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		_, _ = w.Write(tarball)
	}))
	defer server.Close()

	f := NewFetcher()
	f.URLOverride = server.URL

	// First fetch: 200 + ETag captured.
	results, err := f.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("first FetchAll: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(results))
	}
	if f.LastETag() != etag {
		t.Errorf("expected LastETag %q, got %q", etag, f.LastETag())
	}
	if f.NotModified() {
		t.Error("expected NotModified()==false after a full fetch")
	}

	// Second fetch with the stored ETag: 304, no results, NotModified.
	f.SetIfNoneMatch(f.LastETag())
	results, err = f.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("second FetchAll: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on 304, got %d", len(results))
	}
	if !f.NotModified() {
		t.Error("expected NotModified()==true after 304")
	}

	// FetchVersion honors the same ETag.
	f.SetIfNoneMatch(etag)
	results, err = f.FetchVersion(context.Background(), "v11")
	if err != nil {
		t.Fatalf("FetchVersion after ETag: %v", err)
	}
	if len(results) != 0 || !f.NotModified() {
		t.Errorf("FetchVersion: expected 304 (0 results), got %d results, NotModified=%v", len(results), f.NotModified())
	}

	if requests != 3 {
		t.Errorf("expected 3 requests total, got %d", requests)
	}
}
