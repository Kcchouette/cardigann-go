package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/httpclient"
	"github.com/Kcchouette/cardigann-go/release"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// yamlFixture is a minimal Cardigann v11 definition invented for testing (not a
// real definition from the Prowlarr/Indexers repo, so it depends on no external
// content or network).
const yamlFixture = `
id: testtracker
name: TestTracker
description: Fixture locale pour tests unitaires
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    details:
      selector: a.title
      attribute: href
    download:
      selector: a.dl
      attribute: href
    size:
      selector: td.size
    seeders:
      selector: td.seeders
`

const htmlFixture = `
<html><body>
<table class="results">
  <tr>
    <td><a class="title" href="/details/1">Ubuntu.24.04.ISO</a></td>
    <td class="size">1048576</td>
    <td class="seeders">42</td>
    <td><a class="dl" href="/download/1.torrent">DL</a></td>
  </tr>
  <tr>
    <td><a class="title" href="/details/2">Debian.12.ISO</a></td>
    <td class="size">2097152</td>
    <td class="seeders">7</td>
    <td><a class="dl" href="/download/2.torrent">DL</a></td>
  </tr>
</table>
</body></html>
`

func TestProviderSearch_FullPipeline(t *testing.T) {
	def, err := cmodel.Parse([]byte(yamlFixture))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(htmlFixture))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	releases, err := provider.Search(context.Background(), Query{Keywords: "ubuntu"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(releases))
	}

	first := releases[0]
	if first.Title != "Ubuntu.24.04.ISO" {
		t.Errorf("expected title 'Ubuntu.24.04.ISO', got %q", first.Title)
	}
	if first.Details != "/details/1" {
		t.Errorf("expected details '/details/1', got %q", first.Details)
	}
	if first.DownloadLink != "/download/1.torrent" {
		t.Errorf("expected download link '/download/1.torrent', got %q", first.DownloadLink)
	}
	if first.Size != 1048576 {
		t.Errorf("expected size 1048576, got %d", first.Size)
	}
	if first.Seeders != 42 {
		t.Errorf("expected seeders 42, got %d", first.Seeders)
	}
	if first.Protocol != release.ProtocolTorrent {
		t.Errorf("expected protocol torrent, got %q", first.Protocol)
	}
	if first.IndexerID != "testtracker" {
		t.Errorf("expected indexer id 'testtracker', got %q", first.IndexerID)
	}

	second := releases[1]
	if second.Title != "Debian.12.ISO" || second.Seeders != 7 {
		t.Errorf("unexpected second release: %+v", second)
	}
}

// --- T8: Category Mapping Tests ---

func TestMapCategories(t *testing.T) {
	mappings := []cmodel.CategoryMapping{
		{ID: "100", Cat: "2000", Desc: "Movies"},
		{ID: "200", Cat: "2010", Desc: "Movies/Foreign"},
		{ID: "300", Cat: "5000", Desc: "Audio"},
	}

	tests := []struct {
		name     string
		input    []int
		expected []string
	}{
		{"single match", []int{2000}, []string{"100"}},
		{"multiple matches", []int{2000, 5000}, []string{"100", "300"}},
		{"no match", []int{9999}, nil},
		{"empty input", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapCategories(tt.input, cmodel.Caps{CategoryMappings: mappings})
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d results, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("expected[%d] = %q, got %q", i, tt.expected[i], v)
				}
			}
		})
	}
}

func TestMapCategories_EmptyMappings(t *testing.T) {
	result := mapCategories([]int{2000}, cmodel.Caps{})
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

// TestMapCategories_FromMap covers the caps.categories fallback: native key
// -> newznab representation, matched against the requested numeric IDs.
func TestMapCategories_FromMap(t *testing.T) {
	caps := cmodel.Caps{Categories: cmodel.StringMapFrom("2000", "2000", "1", "TV", "XXX", "XXX")}
	result := mapCategories([]int{2000}, caps)
	if len(result) != 1 || result[0] != "2000" {
		t.Errorf("expected [2000], got %v", result)
	}
	// Name-based representations cannot be matched without a name table.
	if result := mapCategories([]int{2000}, cmodel.Caps{Categories: cmodel.StringMapFrom("1", "TV")}); result != nil {
		t.Errorf("expected no match for name-based values, got %v", result)
	}
}

func TestCategoriesIntersect(t *testing.T) {
	tests := []struct {
		name      string
		path      []int
		requested []int
		expected  bool
	}{
		{"overlap", []int{100, 200}, []int{200, 300}, true},
		{"no overlap", []int{100, 200}, []int{300, 400}, false},
		{"empty requested", []int{100}, nil, true},
		{"empty path", nil, []int{100}, false},
		{"both empty", nil, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := categoriesIntersect(tt.path, tt.requested)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestProviderSearch_PathFilteringByCategory(t *testing.T) {
	yamlDef := `
id: testtracker
name: TestTracker
description: Category filtering test
language: en-US
type: public
links:
  - http://example.invalid/
caps:
  categorymappings:
    - id: "100"
      cat: "2000"
      desc: Movies
    - id: "200"
      cat: "5000"
      desc: Audio
search:
  paths:
    - path: /movies
      categories: [2000]
      response:
        type: html
    - path: /audio
      categories: [5000]
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    details:
      selector: a.title
      attribute: href
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(htmlFixture))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	// Request only audio category — should only hit /audio path.
	_, err = provider.Search(context.Background(), Query{Keywords: "test", Categories: []int{5000}})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if requestedPath != "/audio" {
		t.Errorf("expected path /audio, got %q", requestedPath)
	}

	// Request only movies category — should only hit /movies path.
	_, err = provider.Search(context.Background(), Query{Keywords: "test", Categories: []int{2000}})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if requestedPath != "/movies" {
		t.Errorf("expected path /movies, got %q", requestedPath)
	}
}

func TestProviderSearch_CategoriesInContext(t *testing.T) {
	yamlDef := `
id: testtracker
name: TestTracker
description: Category context test
language: en-US
type: public
links:
  - http://example.invalid/
caps:
  categorymappings:
    - id: "100"
      cat: "2000"
      desc: Movies
search:
  paths:
    - path: /search?cat={{index .Categories 0}}
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    details:
      selector: a.title
      attribute: href
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	var requestedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedURL = r.URL.String()
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(htmlFixture))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	_, err = provider.Search(context.Background(), Query{Keywords: "test", Categories: []int{2000}})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if !strings.Contains(requestedURL, "cat=100") {
		t.Errorf("expected URL to contain 'cat=100' (native ID), got %q", requestedURL)
	}
}

// --- T10: Search Error Detection Tests ---

func TestProviderSearch_ErrorDetection(t *testing.T) {
	yamlDef := `
id: testtracker
name: TestTracker
description: Error detection test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    details:
      selector: a.title
      attribute: href
    download:
      selector: a.dl
      attribute: href
  error:
    - selector: div.error-box
      message:
        selector: span.error-msg
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	errorHTML := `<html><body>
		<div class="error-box"><span class="error-msg">Your account has been disabled</span></div>
	</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(errorHTML))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	_, err = provider.Search(context.Background(), Query{Keywords: "test"})
	if err == nil {
		t.Fatal("expected search error, got nil")
	}
	if !strings.Contains(err.Error(), "search error") {
		t.Errorf("expected 'search error' prefix, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "Your account has been disabled") {
		t.Errorf("expected error message 'Your account has been disabled', got %q", err.Error())
	}
}

func TestProviderSearch_NoErrorSelectors(t *testing.T) {
	yamlDef := `
id: testtracker
name: TestTracker
description: No error selectors test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    details:
      selector: a.title
      attribute: href
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(htmlFixture))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(releases))
	}
}

// --- Integration tests for engine package ---

// Test 1: XML response parsing (Torznab format)
func TestSearch_XMLTorznab(t *testing.T) {
	yamlDef := `
id: xmltracker
name: XMLTracker
description: XML Torznab test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /rss
      response:
        type: xml
  rows:
    selector: rss > channel > item
  fields:
    title:
      selector: title
    download:
      selector: enclosure
      attribute: url
    size:
      selector: "[name=size]"
      attribute: value
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	torznabXML := `<?xml version="1.0" encoding="UTF-8"?>
<rss xmlns:torznab="http://torznab.github.io/schemas/2015/feed">
  <channel>
    <item>
      <title>Ubuntu 24.04 LTS</title>
      <enclosure url="http://example.invalid/download/1.torrent" length="1048576" type="application/x-bittorrent"/>
      <torznab:attr name="size" value="1048576"/>
      <torznab:attr name="seeders" value="42"/>
    </item>
    <item>
      <title>Debian 12 Netinst</title>
      <enclosure url="http://example.invalid/download/2.torrent" length="2097152" type="application/x-bittorrent"/>
      <torznab:attr name="size" value="2097152"/>
      <torznab:attr name="seeders" value="7"/>
    </item>
  </channel>
</rss>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(torznabXML))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "linux"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(releases))
	}

	if releases[0].Title != "Ubuntu 24.04 LTS" {
		t.Errorf("expected title 'Ubuntu 24.04 LTS', got %q", releases[0].Title)
	}
	if releases[0].DownloadLink != "http://example.invalid/download/1.torrent" {
		t.Errorf("expected download 'http://example.invalid/download/1.torrent', got %q", releases[0].DownloadLink)
	}
	if releases[0].Size != 1048576 {
		t.Errorf("expected size 1048576, got %d", releases[0].Size)
	}
	if releases[1].Title != "Debian 12 Netinst" {
		t.Errorf("expected title 'Debian 12 Netinst', got %q", releases[1].Title)
	}
	if releases[1].Size != 2097152 {
		t.Errorf("expected size 2097152, got %d", releases[1].Size)
	}
}

// Test 2: JSON response parsing (gjson paths)
func TestSearch_JSONResponse(t *testing.T) {
	yamlDef := `
id: jsontracker
name: JSONTracker
description: JSON response test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /api/search
      response:
        type: json
  rows:
    selector: items
  fields:
    title:
      attribute: title
    download:
      attribute: link
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	jsonBody := `{"items":[{"title":"TestItem1","link":"http://example.invalid/dl/1.torrent"},{"title":"TestItem2","link":"http://example.invalid/dl/2.torrent"}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jsonBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(releases))
	}
	if releases[0].Title != "TestItem1" {
		t.Errorf("expected title 'TestItem1', got %q", releases[0].Title)
	}
	if releases[0].DownloadLink != "http://example.invalid/dl/1.torrent" {
		t.Errorf("expected download link, got %q", releases[0].DownloadLink)
	}
	if releases[1].Title != "TestItem2" {
		t.Errorf("expected title 'TestItem2', got %q", releases[1].Title)
	}
}

// Test 3: POST method with form inputs
func TestSearch_PostMethodWithInputs(t *testing.T) {
	yamlDef := `
id: posttracker
name: PostTracker
description: POST method test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      method: post
      inputs:
        q: "{{ .Keywords }}"
        cat: "1"
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	var receivedMethod string
	var receivedBody string
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedContentType = r.Header.Get("Content-Type")
		if r.Method == http.MethodPost {
			buf := make([]byte, 4096)
			n, _ := r.Body.Read(buf)
			receivedBody = string(buf[:n])
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>
<table class="results">
  <tr><td><a class="title" href="/d1">Result1</a></td><td><a class="dl" href="/dl/1">DL</a></td></tr>
</table>
</body></html>`))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test query"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if receivedMethod != http.MethodPost {
		t.Errorf("expected POST method, got %q", receivedMethod)
	}
	if receivedContentType != "application/x-www-form-urlencoded" {
		t.Errorf("expected content-type 'application/x-www-form-urlencoded', got %q", receivedContentType)
	}
	if !strings.Contains(receivedBody, "q=test+query") || !strings.Contains(receivedBody, "cat=1") {
		t.Errorf("expected body to contain 'q=test+query&cat=1', got %q", receivedBody)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	if releases[0].Title != "Result1" {
		t.Errorf("expected title 'Result1', got %q", releases[0].Title)
	}
}

// Test 4: GET method with query input parameters
func TestSearch_GetWithInputs(t *testing.T) {
	yamlDef := `
id: gettracker
name: GetTracker
description: GET with inputs test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      inputs:
        q: "{{ .Keywords }}"
        limit: "100"
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	var receivedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>
<table class="results">
  <tr><td><a class="title" href="/d1">Result1</a></td><td><a class="dl" href="/dl/1">DL</a></td></tr>
</table>
</body></html>`))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "my+keywords"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if !strings.Contains(receivedQuery, "q=my%2Bkeywords") && !strings.Contains(receivedQuery, "q=my+keywords") {
		t.Errorf("expected query to contain 'q=my+keywords', got %q", receivedQuery)
	}
	if !strings.Contains(receivedQuery, "limit=100") {
		t.Errorf("expected query to contain 'limit=100', got %q", receivedQuery)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
}

// Test 5: Row-level andmatch filter
func TestSearch_RowFiltersAndmatch(t *testing.T) {
	yamlDef := `
id: andmatchtracker
name: AndmatchTracker
description: andmatch filter test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
    filters:
      - name: andmatch
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	htmlBody := `<html><body>
<table class="results">
  <tr><td><a class="title" href="/d1">The Matrix Reloaded</a></td><td><a class="dl" href="/dl/1">DL</a></td></tr>
  <tr><td><a class="title" href="/d2">Inception Movie</a></td><td><a class="dl" href="/dl/2">DL</a></td></tr>
  <tr><td><a class="title" href="/d3">The Matrix Revolutions</a></td><td><a class="dl" href="/dl/3">DL</a></td></tr>
  <tr><td><a class="title" href="/d4">Toy Story 4</a></td><td><a class="dl" href="/dl/4">DL</a></td></tr>
  <tr><td><a class="title" href="/d5">The Matrix Resurrections</a></td><td><a class="dl" href="/dl/5">DL</a></td></tr>
</table>
</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(htmlBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "matrix"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(releases) != 3 {
		t.Fatalf("expected 3 releases matching 'matrix', got %d", len(releases))
	}
	for i, r := range releases {
		if !strings.Contains(strings.ToLower(r.Title), "matrix") {
			t.Errorf("release %d title %q does not contain 'matrix'", i, r.Title)
		}
	}
}

// TestSearch_ConcurrentDistinctKeywords tests concurrent Search() calls on the
// same *Provider with different keywords. Verifies no race (via `go test -race`)
// AND no cross-contamination of results between goroutines (each search must
// filter by its own keywords, not those of another concurrent search that may
// have overwritten shared state).
func TestSearch_ConcurrentDistinctKeywords(t *testing.T) {
	yamlDef := `
id: concurrenttracker
name: ConcurrentTracker
description: concurrent Search() test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
    filters:
      - name: andmatch
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	htmlBody := `<html><body>
<table class="results">
  <tr><td><a class="title" href="/d1">The Matrix Reloaded</a></td><td><a class="dl" href="/dl/1">DL</a></td></tr>
  <tr><td><a class="title" href="/d2">Inception Movie</a></td><td><a class="dl" href="/dl/2">DL</a></td></tr>
  <tr><td><a class="title" href="/d3">Toy Story 4</a></td><td><a class="dl" href="/dl/4">DL</a></td></tr>
</table>
</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(htmlBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	cases := []struct {
		keywords string
		want     string
	}{
		{"matrix", "matrix"},
		{"inception", "inception"},
		{"toy", "toy"},
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(cases)*20)
	for i := 0; i < 20; i++ {
		for _, tc := range cases {
			wg.Add(1)
			go func(keywords, want string) {
				defer wg.Done()
				releases, err := provider.Search(context.Background(), Query{Keywords: keywords})
				if err != nil {
					errs <- fmt.Errorf("Search(%q): %w", keywords, err)
					return
				}
				if len(releases) != 1 {
					errs <- fmt.Errorf("Search(%q): expected 1 release, got %d", keywords, len(releases))
					return
				}
				if !strings.Contains(strings.ToLower(releases[0].Title), want) {
					errs <- fmt.Errorf("Search(%q): title %q does not contain %q (cross-contamination?)", keywords, releases[0].Title, want)
				}
			}(tc.keywords, tc.want)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// Test 6: Optional text fields
func TestSearch_WithOptionalTextFields(t *testing.T) {
	yamlDef := `
id: optiontracker
name: OptionTracker
description: optional fields test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
    details:
      text: mysite
    category:
      selector: td.nonexistent
      optional: true
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>
<table class="results">
  <tr><td><a class="title" href="/d1">SomeTitle</a></td><td><a class="dl" href="/dl/1">DL</a></td></tr>
</table>
</body></html>`))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	if releases[0].Details != "mysite" {
		t.Errorf("expected details 'mysite' from text field, got %q", releases[0].Details)
	}
}

// Test 7: Infohash extraction with before-response
func TestSearch_InfohashWithBeforeResponse(t *testing.T) {
	yamlDef := `
id: infohashtracker
name: InfohashTracker
description: infohash before-response test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
download:
  before:
    path: /api/info
  infohash:
    usebeforeresponse: true
    hash:
      selector: "[name=hash]"
      filters:
        - name: regexp
          args: ["([a-f0-9]{40})"]
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	var beforeFetched bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body>
<table class="results">
  <tr><td><a class="title" href="/d1">TestTorrent</a></td><td><a class="dl" href="/dl/1">DL</a></td></tr>
</table>
</body></html>`))
		case "/api/info":
			beforeFetched = true
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body><div><item name="hash"><item name="hash">a0b1c2d3e4f5a0b1c2d3e4f5a0b1c2d3e4f5a0b1</item></item></div></body></html>`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if !beforeFetched {
		t.Error("expected /api/info to be fetched before extraction")
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	if releases[0].InfoHash != "a0b1c2d3e4f5a0b1c2d3e4f5a0b1c2d3e4f5a0b1" {
		t.Errorf("expected infohash 'a0b1c2d3e4f5a0b1c2d3e4f5a0b1c2d3e4f5a0b1', got %q", releases[0].InfoHash)
	}
}

// TestSearch_ParallelDownloadResolution verifies that download link resolution
// (before-request) for multiple rows is performed in parallel (bounded by
// maxParallelExtract), not sequentially row by row. An artificially slow
// endpoint ensures the total time for an N-row search is well below
// N × before-request latency, and that multiple /api/info requests are in
// flight simultaneously.
func TestSearch_ParallelDownloadResolution(t *testing.T) {
	yamlDef := `
id: paralleltracker
name: ParallelTracker
description: parallel before-request resolution test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
download:
  before:
    path: /api/info
  selectors:
    - selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	const numRows = 6
	const perRequestDelay = 100 * time.Millisecond

	var mu sync.Mutex
	inFlight := 0
	maxInFlight := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			var sb strings.Builder
			sb.WriteString(`<html><body><table class="results">`)
			for i := 0; i < numRows; i++ {
				fmt.Fprintf(&sb, `<tr><td><a class="title" href="/d%d">Release %d</a></td><td><a class="dl" href="/dl/%d">DL</a></td></tr>`, i, i, i)
			}
			sb.WriteString(`</table></body></html>`)
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(sb.String()))
		case "/api/info":
			mu.Lock()
			inFlight++
			if inFlight > maxInFlight {
				maxInFlight = inFlight
			}
			mu.Unlock()

			time.Sleep(perRequestDelay)

			mu.Lock()
			inFlight--
			mu.Unlock()

			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	start := time.Now()
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != numRows {
		t.Fatalf("expected %d releases, got %d", numRows, len(releases))
	}
	for i, r := range releases {
		want := fmt.Sprintf("Release %d", i)
		if r.Title != want {
			t.Errorf("release %d: expected title %q (order preserved), got %q", i, want, r.Title)
		}
	}

	sequentialWorstCase := numRows * perRequestDelay
	if elapsed >= sequentialWorstCase {
		t.Errorf("expected parallel resolution to be faster than sequential worst case (%v), took %v", sequentialWorstCase, elapsed)
	}

	mu.Lock()
	got := maxInFlight
	mu.Unlock()
	if got < 2 {
		t.Errorf("expected at least 2 concurrent /api/info requests in flight, observed max %d", got)
	}
}

// Test 8: Login with CSRF token extraction
func TestSearch_LoginCSRF(t *testing.T) {
	yamlDef := `
id: loginctracker
name: LoginTracker
description: login CSRF test
language: en-US
type: public
links:
  - http://example.invalid/
login:
  method: form
  path: /login
  inputs:
    username: testuser
    password: testpass
  selectorinputs:
    csrf_token:
      selector: input[name=csrf_token]
      attribute: value
  test:
    path: /check
    selector: div.logged-in
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	var requestLog []string
	var receivedCSRF string
	var receivedUsername string
	var loggedIn bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			switch r.Method {
			case http.MethodGet:
				requestLog = append(requestLog, "GET /login")
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(`<html><body>
<form method="post" action="/login">
  <input type="hidden" name="csrf_token" value="abc123csrf"/>
  <input type="text" name="username"/>
  <input type="password" name="password"/>
</form>
</body></html>`))
			case http.MethodPost:
				requestLog = append(requestLog, "POST /login")
				loggedIn = true
				buf := make([]byte, 4096)
				n, _ := r.Body.Read(buf)
				body := string(buf[:n])
				receivedCSRF = ""
				for _, part := range strings.Split(body, "&") {
					if strings.HasPrefix(part, "csrf_token=") {
						receivedCSRF = strings.TrimPrefix(part, "csrf_token=")
					}
					if strings.HasPrefix(part, "username=") {
						receivedUsername = strings.TrimPrefix(part, "username=")
					}
				}
				w.WriteHeader(http.StatusOK)
			}
		case "/check":
			requestLog = append(requestLog, "GET /check")
			w.Header().Set("Content-Type", "text/html")
			if !loggedIn {
				// Session not authenticated yet: the login pre-check fails
				// and the form login flow runs.
				_, _ = w.Write([]byte(`<html><body><div class="not-logged">Login required</div></body></html>`))
				return
			}
			_, _ = w.Write([]byte(`<html><body><div class="logged-in">Welcome</div></body></html>`))
		case "/search":
			requestLog = append(requestLog, "GET /search")
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body>
<table class="results">
  <tr><td><a class="title" href="/d1">LoggedInResult</a></td><td><a class="dl" href="/dl/1">DL</a></td></tr>
</table>
</body></html>`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := httpclient.New(httpclient.Options{
		IndexerID: "loginctracker",
	})
	if err != nil {
		t.Fatalf("creating httpclient: %v", err)
	}

	provider := NewAuthenticatedProvider(def, server.URL, nil, client)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	// Verify login flow: session pre-check (GET /check fails) → GET /login →
	// POST /login → GET /check (verify) → GET /search
	if len(requestLog) < 5 {
		t.Fatalf("expected at least 5 requests, got %d: %v", len(requestLog), requestLog)
	}
	if requestLog[0] != "GET /check" {
		t.Errorf("expected first request 'GET /check' (session pre-check), got %q", requestLog[0])
	}
	if requestLog[1] != "GET /login" {
		t.Errorf("expected second request 'GET /login', got %q", requestLog[1])
	}
	if requestLog[2] != "POST /login" {
		t.Errorf("expected third request 'POST /login', got %q", requestLog[2])
	}
	if requestLog[3] != "GET /check" {
		t.Errorf("expected fourth request 'GET /check' (verify), got %q", requestLog[3])
	}

	if receivedCSRF != "abc123csrf" {
		t.Errorf("expected CSRF token 'abc123csrf', got %q", receivedCSRF)
	}
	if receivedUsername != "testuser" {
		t.Errorf("expected username 'testuser', got %q", receivedUsername)
	}

	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	if releases[0].Title != "LoggedInResult" {
		t.Errorf("expected title 'LoggedInResult', got %q", releases[0].Title)
	}
}

// --- Tests for gowlarr compatibility fixes ---

func TestProviderSearch_FullURLPath(t *testing.T) {
	// Test that full URLs in search paths skip BaseURL prefix
	def, err := cmodel.Parse([]byte(`
id: test
name: Test
links:
  - http://base.invalid/
search:
  paths:
    - path: "/api/search"
      response:
        type: html
  inputs:
    q: "{{ .Keywords }}"
  rows:
    selector: tr
  fields:
    title:
      selector: td
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><table><tr><td>Result</td></tr></table></body></html>`))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	_, err = provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if receivedPath != "/api/search" {
		t.Errorf("expected path '/api/search', got %q", receivedPath)
	}
}

func TestProviderSearch_RawParams(t *testing.T) {
	// Test $raw parameter support
	def, err := cmodel.Parse([]byte(`
id: test
name: Test
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  inputs:
    $raw: "categoryId=2000&categoryId=3000"
    q: "{{ .Keywords }}"
  rows:
    selector: tr
  fields:
    title:
      selector: td
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var receivedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><table><tr><td>Result</td></tr></table></body></html>`))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	_, err = provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if !strings.Contains(receivedQuery, "categoryId=2000") || !strings.Contains(receivedQuery, "categoryId=3000") {
		t.Errorf("expected categoryId params in query, got %q", receivedQuery)
	}
}

func TestProviderSearch_SkipEmptyInputs(t *testing.T) {
	// Test that empty and "false" input values are skipped
	def, err := cmodel.Parse([]byte(`
id: test
name: Test
links:
  - http://example.invalid/
settings:
  - name: freeleech
    type: checkbox
    default: "false"
search:
  paths:
    - path: /search
      response:
        type: html
  inputs:
    q: "{{ .Keywords }}"
    freeleech: "{{ if .Config.freeleech }}true{{ else }}{{ end }}"
  rows:
    selector: tr
  fields:
    title:
      selector: td
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var receivedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><table><tr><td>Result</td></tr></table></body></html>`))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	_, err = provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// freeleech should not be in the query since it's "false"
	if strings.Contains(receivedQuery, "freeleech") {
		t.Errorf("freeleech should not be in query when config is false, got %q", receivedQuery)
	}
}

func TestProviderSearch_SearchHeaders(t *testing.T) {
	// Test search-level headers
	def, err := cmodel.Parse([]byte(`
id: test
name: Test
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  headers:
    Authorization: ["Bearer {{ .Config.apikey }}"]
  inputs:
    q: "{{ .Keywords }}"
  rows:
    selector: tr
  fields:
    title:
      selector: td
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><table><tr><td>Result</td></tr></table></body></html>`))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, map[string]string{
		"apikey": "secret123",
	})

	_, err = provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if receivedAuth != "Bearer secret123" {
		t.Errorf("expected Authorization header 'Bearer secret123', got %q", receivedAuth)
	}
}

func TestProviderSearch_ConfigDefaults(t *testing.T) {
	// Test that config defaults from definition are applied
	def, err := cmodel.Parse([]byte(`
id: test
name: Test
links:
  - http://example.invalid/
settings:
  - name: sort
    type: select
    default: created
  - name: type
    type: select
    default: desc
search:
  paths:
    - path: /search
      response:
        type: html
  inputs:
    q: "{{ .Keywords }}"
    sort: "{{ .Config.sort }}"
    order: "{{ .Config.type }}"
  rows:
    selector: tr
  fields:
    title:
      selector: td
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var receivedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><table><tr><td>Result</td></tr></table></body></html>`))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	_, err = provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if !strings.Contains(receivedQuery, "sort=created") {
		t.Errorf("expected sort=created in query, got %q", receivedQuery)
	}
	if !strings.Contains(receivedQuery, "order=desc") {
		t.Errorf("expected order=desc in query, got %q", receivedQuery)
	}
}

func TestProviderSearch_JSONDollarSelector(t *testing.T) {
	// Test $ selector in JSON (returns all array items)
	def, err := cmodel.Parse([]byte(`
id: test
name: Test
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: json
  inputs:
    q: "{{ .Keywords }}"
  rows:
    selector: $
  fields:
    title:
      selector: name
    size:
      selector: size
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	jsonResponse := `[{"name":"First","size":100},{"name":"Second","size":200}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jsonResponse))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(releases))
	}
	if releases[0].Title != "First" || releases[0].Size != 100 {
		t.Errorf("first release = %+v, want First/100", releases[0])
	}
	if releases[1].Title != "Second" || releases[1].Size != 200 {
		t.Errorf("second release = %+v, want Second/200", releases[1])
	}
}

// TestSearch_FieldCase_HTML covers field-level `case` on HTML rows: keys are
// CSS selectors matched against the row, "*" is only an explicit fallback
// (it must NOT shadow a specific matching key even though it sorts first).
func TestSearch_FieldCase_HTML(t *testing.T) {
	yamlDef := `
id: casehtml
name: CaseHTML
description: field case HTML test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    downloadvolumefactor:
      case:
        img[src$="free.gif"]: 0
        "*": 1
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	htmlBody := `<table class="results"><tr>
	  <td><img src="/free.gif"><a class="title">Free Item</a></td>
	</tr><tr>
	  <td><a class="title">Normal Item</a></td>
	</tr></table>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(htmlBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(releases))
	}
	if releases[0].Title != "Free Item" || releases[1].Title != "Normal Item" {
		t.Errorf("unexpected titles: %q, %q", releases[0].Title, releases[1].Title)
	}
}

// TestSearch_FieldCase_JSON covers field-level `case` on JSON rows: keys are
// literal value strings, values may be templates (.True/.False).
func TestSearch_FieldCase_JSON(t *testing.T) {
	yamlDef := `
id: casejson
name: CaseJSON
description: field case JSON test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /api/search
      response:
        type: json
  rows:
    selector: items
  fields:
    _internal:
      selector: internal
      case:
        False: "{{ .False }}"
        True: "{{ .True }}"
    title:
      text: "{{ if .Result._internal }}Internal: {{ .Result._internal }}{{ else }}Public{{ end }}"
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	jsonBody := `{"items":[
	  {"internal":"False","title":"A"},
	  {"internal":"True","title":"B"}
	]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jsonBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(releases))
	}
	if releases[0].Title != "Public" {
		t.Errorf("False case: expected Public (falsy .Result._internal), got %q", releases[0].Title)
	}
	if releases[1].Title != "Internal: true" {
		t.Errorf("True case: expected 'Internal: true', got %q", releases[1].Title)
	}
}

// TestSearch_FieldRemove covers the field-level `remove` feature: matching
// descendant subtrees are stripped from the text without mutating the shared
// document (verified under -race).
func TestSearch_FieldRemove(t *testing.T) {
	yamlDef := `
id: remover
name: Remover
description: field remove test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: td.t
      remove: "span.remove-me, a"
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	htmlBody := `<table class="results"><tr>
	  <td class="t">Keep <span class="remove-me">DROP</span> this <a href="/x">link</a> text</td>
	  <td><a class="dl" href="/dl/1"></a></td>
	</tr></table>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(htmlBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	if strings.Contains(releases[0].Title, "DROP") || strings.Contains(releases[0].Title, "link") {
		t.Errorf("title still contains removed content: %q", releases[0].Title)
	}
	if !strings.Contains(releases[0].Title, "Keep") || !strings.Contains(releases[0].Title, "text") {
		t.Errorf("title lost kept content: %q", releases[0].Title)
	}
}

// TestSearch_FieldDefault covers the field-level `default` feature (v11
// requires optional: true): the rendered default fills an empty extraction.
func TestSearch_FieldDefault(t *testing.T) {
	yamlDef := `
id: defaulted
name: Defaulted
description: field default test
language: en-US
type: public
links:
  - http://example.invalid/
settings:
  - name: fallback_size
    type: text
    label: Fallback size
    default: "4242"
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    size:
      selector: a.size
      optional: true
      default: "{{ .Config.fallback_size }}"
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	htmlBody := `<table class="results"><tr>
	  <td><a class="title">Item</a><a class="dl" href="/dl/1"></a></td>
	</tr></table>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(htmlBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	if releases[0].Size != 4242 {
		t.Errorf("size = %d, want 4242 (rendered config default)", releases[0].Size)
	}
}

// TestSearch_FilterArgsRendered covers filter args rendered as templates:
// a field filter arg referencing .Result.year must resolve on a later pass.
func TestSearch_FilterArgsRendered(t *testing.T) {
	yamlDef := `
id: filterargs
name: FilterArgs
description: filter arg rendering test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    year:
      selector: span.year
    title:
      selector: a.title
      filters:
        - name: append
          args: "-{{ .Result.year }}"
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	htmlBody := `<table class="results"><tr>
	  <td><span class="year">2026</span><a class="title">Foo</a><a class="dl" href="/dl/1"></a></td>
	</tr></table>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(htmlBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	want := "Foo-2026"
	if releases[0].Title != want {
		t.Errorf("title = %q, want %q (resolved .Result.year filter arg)", releases[0].Title, want)
	}
}

// TestSearch_KeywordsFilterArgs covers keywordsfilters args rendered with the
// config context (they run after configOnce now, not before).
func TestSearch_KeywordsFilterArgs(t *testing.T) {
	yamlDef := `
id: kwargs
name: KWArgs
description: keywords filter arg rendering test
language: en-US
type: public
links:
  - http://example.invalid/
settings:
  - name: suffix
    type: text
    label: Suffix
    default: "suffix"
search:
  keywordsfilters:
    - name: append
      args: "-{{ .Config.suffix }}"
  paths:
    - path: /search
      inputs:
        q: "{{ .Keywords }}"
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		_, _ = w.Write([]byte(`<table class="results"><tr><td><a class="title">Item</a><a class="dl" href="/dl/1"></a></td></tr></table>`))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	want := "test-suffix"
	if gotQuery != want {
		t.Errorf("keywordsfilters arg rendered q = %q, want %q", gotQuery, want)
	}
}

// TestSearch_AllowEmptyInputs covers search.allowEmptyInputs: empty and
// "false" values are dropped by default, kept when the flag is set.
func TestSearch_AllowEmptyInputs(t *testing.T) {
	yamlDef := `
id: emptyinputs
name: EmptyInputs
description: allowEmptyInputs test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  allowEmptyInputs: true
  paths:
    - path: /search
      inputs:
        foo: ""
        bar: "false"
        q: "{{ .Keywords }}"
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte(`<table class="results"><tr><td><a class="title">Item</a><a class="dl" href="/dl/1"></a></td></tr></table>`))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	if _, err := provider.Search(context.Background(), Query{Keywords: "test"}); err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if !query.Has("foo") || query.Get("foo") != "" {
		t.Errorf("allowEmptyInputs: expected foo= to be sent, got %v", query.Get("foo"))
	}
	if !query.Has("bar") || query.Get("bar") != "false" {
		t.Errorf("allowEmptyInputs: expected bar=false to be sent, got %v", query.Get("bar"))
	}
	if query.Get("q") != "test" {
		t.Errorf("q = %q, want test", query.Get("q"))
	}
}

// TestSearch_RowsCount covers rows.count: the selector (JSON $.numFound) caps
// the number of extracted rows.
func TestSearch_RowsCount(t *testing.T) {
	yamlDef := `
id: rowcount
name: RowCount
description: rows.count test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /api/search
      response:
        type: json
  rows:
    selector: items
    count:
      selector: $.numFound
  fields:
    title:
      selector: title
    download:
      selector: link
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	jsonBody := `{"numFound": 2, "items":[
	  {"title":"A","link":"/dl/1"},
	  {"title":"B","link":"/dl/2"},
	  {"title":"C","link":"/dl/3"},
	  {"title":"D","link":"/dl/4"}
	]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jsonBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("expected 2 releases (numFound cap), got %d", len(releases))
	}
	if releases[0].Title != "A" || releases[1].Title != "B" {
		t.Errorf("unexpected titles: %q, %q", releases[0].Title, releases[1].Title)
	}
}

// TestSearch_EncodingWindows1251 covers the v11 encoding: field: a
// windows-1251 response body is decoded before HTML parsing.
func TestSearch_EncodingWindows1251(t *testing.T) {
	yamlDef := `
id: cp1251
name: CP1251
description: encoding test
language: ru-RU
type: public
encoding: windows-1251
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	html := `<table class="results"><tr><td><a class="title">Фильм Название</a><a class="dl" href="/dl/1"></a></td></tr></table>`
	encoded, _, err := transform.Bytes(charmap.Windows1251.NewEncoder(), []byte(html))
	if err != nil {
		t.Fatalf("encoding fixture: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(encoded)
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	if want := "Фильм Название"; releases[0].Title != want {
		t.Errorf("title = %q, want %q (decoded from windows-1251)", releases[0].Title, want)
	}
}

// TestSearch_EncodingUTF8Passthrough covers the default UTF-8 case: non-ASCII
// characters round-trip unchanged (no decoding applied).
func TestSearch_EncodingUTF8Passthrough(t *testing.T) {
	yamlDef := `
id: utf8
name: UTF8
description: encoding passthrough test
language: fr-FR
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	html := `<table class="results"><tr><td><a class="title">Étrange été à Paris</a><a class="dl" href="/dl/1"></a></td></tr></table>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	if want := "Étrange été à Paris"; releases[0].Title != want {
		t.Errorf("title = %q, want %q", releases[0].Title, want)
	}
}

// TestSearch_EncodingKinozalFixture runs the committed kinozal-magnet fixture
// (windows-1251) end-to-end: the search page is served as windows-1251 bytes
// and the extracted title must decode cleanly (no U+FFFD replacement chars).
// The before-chain infohash is asserted once DownloadUri seeding lands (T2);
// here only the row fields are checked.
func TestSearch_EncodingKinozalFixture(t *testing.T) {
	raw, err := os.ReadFile("../internal/defstest/testdata/kinozal-magnet.yml")
	if err != nil {
		t.Fatalf("reading kinozal fixture: %v", err)
	}
	def, err := cmodel.Parse(raw)
	if err != nil {
		t.Fatalf("parsing kinozal fixture: %v", err)
	}

	html := `<table><tbody><tr>
	  <td class="bt"><img onclick="cat 12"></td>
	  <td class="nam"><a href="/details.php?id=1">Фильм Название 2024</a></td>
	  <td>1.5 ГБ</td><td>10</td><td>5</td><td>сегодня 10:00</td>
	</tr></tbody></table>`
	encoded, _, err := transform.Bytes(charmap.Windows1251.NewEncoder(), []byte(html))
	if err != nil {
		t.Fatalf("encoding fixture: %v", err)
	}
	// The before-chain response is also served as windows-1251.
	beforeHTML := `<ul><li>abcdef0123456789abcdef0123456789abcdef01</li></ul><div class="b">Фильм Название 2024</div>`
	encodedBefore, _, err := transform.Bytes(charmap.Windows1251.NewEncoder(), []byte(beforeHTML))
	if err != nil {
		t.Fatalf("encoding fixture: %v", err)
	}

	var beforeID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/browse.php":
			_, _ = w.Write(encoded)
		case "/get_srv_details.php":
			beforeID = r.URL.Query().Get("id")
			_, _ = w.Write(encodedBefore)
		default:
			_, _ = w.Write([]byte("<html><body></body></html>"))
		}
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	if strings.Contains(releases[0].Title, "\uFFFD") {
		t.Errorf("title still contains U+FFFD (not decoded): %q", releases[0].Title)
	}
	if !strings.Contains(releases[0].Title, "2024") {
		t.Errorf("title lost the year: %q", releases[0].Title)
	}
	if want := "/download.php?id=1"; releases[0].DownloadLink != want {
		t.Errorf("download link = %q, want %q", releases[0].DownloadLink, want)
	}
	// The before chain ran with DownloadUri seeded from the row's download
	// link: step 0 renders `id: {{ .DownloadUri.Query.id }}` → id=1.
	if beforeID != "1" {
		t.Errorf("before chain id = %q, want %q (DownloadUri seed)", beforeID, "1")
	}
}

// TestSearch_JSONAttribute covers rows.attribute (UNIT3D shape): the named
// key replaces each matched row for field extraction.
func TestSearch_JSONAttribute(t *testing.T) {
	yamlDef := `
id: attrjson
name: AttrJSON
description: rows.attribute test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /api/search
      response:
        type: json
  rows:
    selector: data
    attribute: attributes
  fields:
    title:
      selector: name
    download:
      selector: download_link
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	jsonBody := `{"data":[{"attributes":{"name":"Movie A","download_link":"/dl/1"}},{"attributes":{"name":"Movie B","download_link":"/dl/2"}}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jsonBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(releases))
	}
	if releases[0].Title != "Movie A" || releases[1].Title != "Movie B" {
		t.Errorf("unexpected titles: %q, %q", releases[0].Title, releases[1].Title)
	}
	if releases[0].DownloadLink != "/dl/1" {
		t.Errorf("download = %q, want /dl/1", releases[0].DownloadLink)
	}
}

// TestSearch_JSONAttributeMultiple covers rows.multiple: each element of the
// attribute array becomes its own release row.
func TestSearch_JSONAttributeMultiple(t *testing.T) {
	yamlDef := `
id: multjson
name: MultJSON
description: rows.multiple test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /api/search
      response:
        type: json
  rows:
    selector: data
    attribute: torrents
    multiple: true
  fields:
    title:
      selector: name
    download:
      selector: url
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	jsonBody := `{"data":[
	  {"torrents":[{"name":"A-1","url":"/t1"},{"name":"A-2","url":"/t2"}]},
	  {"torrents":[{"name":"B-1","url":"/t3"}]}
	]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jsonBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 3 {
		t.Fatalf("expected 3 releases (2+1 expanded), got %d", len(releases))
	}
	if releases[0].DownloadLink != "/t1" || releases[2].DownloadLink != "/t3" {
		t.Errorf("unexpected download links: %q, %q", releases[0].DownloadLink, releases[2].DownloadLink)
	}
}

// TestSearch_JSONRecursiveDescent covers `..`-prefixed field selectors: they
// evaluate against the ORIGINAL row (the parent object), not the attribute
// value. gjson has no `..` — this guards the document-order walker.
func TestSearch_JSONRecursiveDescent(t *testing.T) {
	yamlDef := `
id: descent
name: Descent
description: recursive descent test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /api/search
      response:
        type: json
  rows:
    selector: data
    attribute: torrents
    multiple: true
  fields:
    title:
      selector: ..name
    download:
      selector: url
    category:
      selector: ..categoryID
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	jsonBody := `{"data":[
	  {"name":"A","categoryID":"7","torrents":[{"url":"/t1"},{"url":"/t2"}]}
	]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jsonBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(releases))
	}
	if releases[0].Title != "A" {
		t.Errorf("title = %q, want A (from parent via ..name)", releases[0].Title)
	}
}

// TestSearch_JSONAttributeMissing covers attribute-less rows: they are
// skipped whether or not missingAttributeEqualsNoResults is set (lenient).
func TestSearch_JSONAttributeMissing(t *testing.T) {
	yamlDef := `
id: missjson
name: MissJSON
description: missing attribute test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /api/search
      response:
        type: json
  rows:
    selector: data
    attribute: torrents
    multiple: true
    missingAttributeEqualsNoResults: true
  fields:
    title:
      selector: name
    download:
      selector: url
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}
	if !def.Search.Rows.MissingAttributeEqualsNoResults {
		t.Fatal("camelCase tag missingAttributeEqualsNoResults not parsed")
	}

	jsonBody := `{"data":[
	  {"torrents":[{"name":"WithTorrent","url":"/t1"}]},
	  {"torrents":[]},
	  {}
	]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jsonBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	// WithTorrent expands to 1; the empty array and the missing attribute
	// yield nothing.
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	if releases[0].Title != "WithTorrent" {
		t.Errorf("title = %q, want WithTorrent", releases[0].Title)
	}
}

// TestSearch_JSONAttributeCountOrder pins the ordering: rows.count caps the
// TOP-LEVEL selection (movies) BEFORE the attribute/multiple expansion (yts
// shape), not the expanded torrent rows.
func TestSearch_JSONAttributeCountOrder(t *testing.T) {
	yamlDef := `
id: ytsshape
name: YTSShape
description: count before expansion test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /api/search
      response:
        type: json
  rows:
    selector: data.movies
    attribute: torrents
    multiple: true
    count:
      selector: data.movie_count
  fields:
    title:
      selector: ..title
    download:
      selector: url
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	jsonBody := `{"data":{"movie_count":2,"movies":[
	  {"title":"M1","torrents":[{"url":"/m1-1"},{"url":"/m1-2"}]},
	  {"title":"M2","torrents":[{"url":"/m2-1"}]},
	  {"title":"M3","torrents":[{"url":"/m3-1"}]}
	]}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jsonBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	// movie_count=2 caps the movies first: M1+M2 expand to 3 torrent rows.
	if len(releases) != 3 {
		t.Fatalf("expected 3 releases (2 movies capped before expansion), got %d", len(releases))
	}
	if releases[0].Title != "M1" || releases[1].Title != "M1" || releases[2].Title != "M2" {
		t.Errorf("unexpected titles: %q, %q, %q", releases[0].Title, releases[1].Title, releases[2].Title)
	}
}

// TestSearch_JSONAttributeAitherFixture runs the committed aither-api.yml
// fixture end-to-end (rows.attribute + fields sub-selecting the attributes
// object).
func TestSearch_JSONAttributeAitherFixture(t *testing.T) {
	raw, err := os.ReadFile("../internal/defstest/testdata/aither-api.yml")
	if err != nil {
		t.Fatalf("reading aither fixture: %v", err)
	}
	def, err := cmodel.Parse(raw)
	if err != nil {
		t.Fatalf("parsing aither fixture: %v", err)
	}

	jsonBody := `{"data":[{"attributes":{"name":"Movie A","download_link":"/dl/1","category_id":5}}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jsonBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "test"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	if releases[0].Title != "Movie A" {
		t.Errorf("title = %q, want Movie A (extracted from attributes object)", releases[0].Title)
	}
}

// TestSearch_AllowRawSearch_Gated covers the allowrawsearch gate: an empty
// keyword-less query on an indexer without the flag fires no HTTP request.
func TestSearch_AllowRawSearch_Gated(t *testing.T) {
	yamlDef := `
id: nogate
name: NoGate
description: allowrawsearch gate test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      inputs:
        q: "{{ .Keywords }}"
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected HTTP request for gated empty search: %s", r.URL.String())
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if releases != nil {
		t.Errorf("expected no releases, got %d", len(releases))
	}
}

// TestSearch_AllowRawSearch_Flagged covers an indexer with the flag: the
// empty-keyword browse path runs.
func TestSearch_AllowRawSearch_Flagged(t *testing.T) {
	yamlDef := `
id: rawok
name: RawOK
description: allowrawsearch flagged test
language: en-US
type: public
links:
  - http://example.invalid/
caps:
  allowrawsearch: true
search:
  paths:
    - path: "{{ if .Keywords }}search{{ else }}browse{{ end }}"
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`<table class="results"><tr><td><a class="title">Browse Item</a><a class="dl" href="/dl/1"></a></td></tr></table>`))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	if gotPath != "/browse" {
		t.Errorf("browse path = %q, want /browse", gotPath)
	}
}

// TestSearch_AllowRawSearch_IDBypass covers media-ID queries: they bypass
// the gate even without keywords or the flag.
func TestSearch_AllowRawSearch_IDBypass(t *testing.T) {
	yamlDef := `
id: idpass
name: IDPass
description: allowrawsearch ID bypass test
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      inputs:
        q: "{{ .Keywords }}"
        imdb: "{{ .Query.IMDBID }}"
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.dl
      attribute: href
`
	def, err := cmodel.Parse([]byte(yamlDef))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<table class="results"><tr><td><a class="title">ID Item</a><a class="dl" href="/dl/1"></a></td></tr></table>`))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{IMDBID: "tt1234567"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release (ID bypasses the gate), got %d", len(releases))
	}
}
