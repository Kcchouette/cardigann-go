package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/release"
)

// yamlFixture est une définition Cardigann v11 minimaliste inventée pour le
// test (pas une définition réelle du dépôt Prowlarr/Indexers, afin de ne
// dépendre d'aucun contenu externe ni réseau).
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
			result := mapCategories(tt.input, mappings)
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
	result := mapCategories([]int{2000}, nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
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
