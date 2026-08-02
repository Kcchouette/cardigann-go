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
	"regexp"
	"strings"
	"time"
)

var versionPattern = regexp.MustCompile(`^v\d+$`)

// RawDefinition is an unparsed Cardigann definition, as retrieved from the
// repository tarball.
type RawDefinition struct {
	ID      string // derived from the filename, e.g. "1337x" for "1337x.yml"
	Version string // e.g. "v11", extracted from path
	YAML    string
	SHA     string // hex-encoded sha256 of the content, for change detection/cache
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

	// ifNoneMatch is the last ETag seen on a previous download; when set, it
	// is sent as If-None-Match so an unchanged tarball answers 304 without
	// re-downloading it. lastETag/notModified are populated by each fetch.
	ifNoneMatch string
	lastETag    string
	notModified bool
}

// NewFetcher constructs a Fetcher with the real repository defaults.
func NewFetcher() *Fetcher {
	return &Fetcher{
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		Repo:       "Prowlarr/Indexers",
		Ref:        "master",
	}
}

// SetIfNoneMatch configures the If-None-Match header for the next fetch
// (the ETag previously returned by LastETag, persisted by the caller).
func (f *Fetcher) SetIfNoneMatch(etag string) { f.ifNoneMatch = etag }

// LastETag returns the ETag header of the last completed fetch ("" if none).
func (f *Fetcher) LastETag() string { return f.lastETag }

// NotModified reports whether the last fetch answered 304 Not Modified
// (the tarball is unchanged; no results were produced).
func (f *Fetcher) NotModified() bool { return f.notModified }

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
	if f.ifNoneMatch != "" {
		req.Header.Set("If-None-Match", f.ifNoneMatch)
	}

	resp, err := f.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading tarball: %w", err)
	}
	defer resp.Body.Close()

	f.lastETag = resp.Header.Get("ETag")
	if resp.StatusCode == http.StatusNotModified {
		f.notModified = true
		return nil, nil // unchanged tarball, nothing to re-extract
	}
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

// FetchAll downloads the full tarball and extracts ALL YAML definitions
// from every version directory under `definitions/`. The version is extracted
// from the path and returned in RawDefinition.Version.
//
// Only directories matching the pattern "v<digits>" (e.g. v1, v10, v11)
// are treated as version directories. Files directly under definitions/
// or in non-matching subdirectories are skipped.
func (f *Fetcher) FetchAll(ctx context.Context) ([]RawDefinition, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.tarballURL(), nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if f.ifNoneMatch != "" {
		req.Header.Set("If-None-Match", f.ifNoneMatch)
	}

	resp, err := f.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading tarball: %w", err)
	}
	defer resp.Body.Close()

	f.lastETag = resp.Header.Get("ETag")
	if resp.StatusCode == http.StatusNotModified {
		f.notModified = true
		return nil, nil // unchanged tarball, nothing to re-extract
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected http status %d while downloading tarball", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("opening gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	defsPrefix := "definitions/"

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
		idx := strings.Index(normalized, defsPrefix)
		if idx < 0 {
			continue
		}
		rel := normalized[idx+len(defsPrefix):]

		// Split on "/" — expect exactly ["<version>", "<file>"]
		parts := strings.SplitN(rel, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			continue // file directly under definitions/ or too deep
		}

		version := parts[0]
		if !versionPattern.MatchString(version) {
			continue // not a version directory (e.g. "test", "examples")
		}

		filename := parts[1]
		if strings.Contains(filename, "/") {
			continue // deeper than definitions/<version>/<file>
		}

		ext := path.Ext(filename)
		if ext != ".yml" && ext != ".yaml" {
			continue
		}

		content, err := io.ReadAll(io.LimitReader(tr, 4<<20)) // 4 MiB per definition, safeguard.
		if err != nil {
			return nil, fmt.Errorf("reading definition %q: %w", filename, err)
		}

		sum := sha256.Sum256(content)
		results = append(results, RawDefinition{
			ID:      strings.TrimSuffix(filename, ext),
			Version: version,
			YAML:    string(content),
			SHA:     hex.EncodeToString(sum[:]),
		})
	}

	return results, nil
}
