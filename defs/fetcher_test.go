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

// buildFixtureTarball construit en mémoire un tarball .tar.gz imitant la
// structure du dépôt Prowlarr/Indexers (dossier racine unique + définitions
// sous definitions/<version>/), avec du contenu YAML entièrement inventé
// pour le test (aucune définition réelle, aucun réseau).
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
		"Indexers-abc123/definitions/v10/legacy.yml":      "id: legacy\nname: Legacy\n", // autre version, doit être ignorée
		"Indexers-abc123/README.md":                       "# not a definition",
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarball)
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
