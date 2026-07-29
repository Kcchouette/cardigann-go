// Package defs synchronizes Cardigann definitions from the GitHub repository
// Prowlarr/Indexers (never vendored or redistributed, see legal disclaimer —
// only cached locally for the end user).
package defs

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
)

// RawDefinition is an unparsed Cardigann definition, as retrieved from the
// repository tarball.
type RawDefinition struct {
	ID   string // derived from the filename, e.g. "1337x" for "1337x.yml"
	YAML string
	SHA  string // hex-encoded sha256 of the content, for change detection/cache
}

// Fetcher downloads Cardigann definitions for a given version
// (e.g. "v11") from a GitHub tarball (`codeload.github.com`), avoiding the
// strict rate limit of the GitHub Contents API (60 req/h unauthenticated).
type Fetcher struct {
	HTTPClient *http.Client
	Repo       string // "Prowlarr/Indexers"
	Ref        string // branche/tag, ex: "master"

	// URLOverride, if non-empty, replaces the computed tarball URL —
	// used by tests to point to a local httptest server
	// instead of the real GitHub.
	URLOverride string
}

// NewFetcher constructs a Fetcher with the real repository defaults.
func NewFetcher() *Fetcher {
	return &Fetcher{
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		Repo:       "Prowlarr/Indexers",
		Ref:        "master",
	}
}

func (f *Fetcher) tarballURL() string {
	if f.URLOverride != "" {
		return f.URLOverride
	}
	return fmt.Sprintf("https://codeload.github.com/%s/tar.gz/refs/heads/%s", f.Repo, f.Ref)
}

// FetchVersion downloads the full tarball and extracts only the
// YAML definitions located under `definitions/<version>/`.
func (f *Fetcher) FetchVersion(ctx context.Context, version string) ([]RawDefinition, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.tarballURL(), nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := f.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading tarball: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected http status %d while downloading tarball", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("opening gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	// GitHub tarballs have a single root directory of the form
	// "Indexers-<sha>/", so we match on the path suffix rather than
	// an absolute prefix.
	wantSuffix := path.Join("definitions", version) + "/"

	var results []RawDefinition
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar entry: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		normalized := strings.ReplaceAll(hdr.Name, "\\", "/")
		idx := strings.Index(normalized, wantSuffix)
		if idx < 0 {
			continue
		}
		rel := normalized[idx+len(wantSuffix):]
		if rel == "" || strings.Contains(rel, "/") {
			continue // unexpected subdirectory, skip
		}
		ext := path.Ext(rel)
		if ext != ".yml" && ext != ".yaml" {
			continue
		}

		content, err := io.ReadAll(io.LimitReader(tr, 4<<20)) // 4 MiB per definition, safeguard.
		if err != nil {
			return nil, fmt.Errorf("reading definition %q: %w", rel, err)
		}

		sum := sha256.Sum256(content)
		results = append(results, RawDefinition{
			ID:   strings.TrimSuffix(rel, ext),
			YAML: string(content),
			SHA:  hex.EncodeToString(sum[:]),
		})
	}

	return results, nil
}
