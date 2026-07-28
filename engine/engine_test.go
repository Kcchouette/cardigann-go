package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
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
