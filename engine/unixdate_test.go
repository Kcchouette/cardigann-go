package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
)

// unixDateYAML is an invented JSON indexer fixture exercising the unixdate
// filter on the date field (no real site involved).
const unixDateYAML = `
id: epochtracker
name: EpochTracker
description: unixdate filter fixture
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
        type: json
  rows:
    selector: "$.items"
  fields:
    title:
      selector: name
    download:
      selector: url
    date:
      selector: ts
      filters:
        - name: unixdate
`

const unixDateBody = `{"items":[{"name":"Example.Release.S01E01","url":"http://example.invalid/1.nzb","ts":1792340016}]}`

// TestSearch_UnixDateField verifies end-to-end that an epoch-seconds JSON
// date survives extraction as a real PublishDate (RFC3339 inside,
// time.Unix outside).
func TestSearch_UnixDateField(t *testing.T) {
	def, err := cmodel.Parse([]byte(unixDateYAML))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(unixDateBody))
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "example"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	want := time.Unix(1792340016, 0).UTC()
	if !releases[0].PublishDate.Equal(want) {
		t.Errorf("PublishDate = %v, want %v", releases[0].PublishDate, want)
	}
}
