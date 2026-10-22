package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
)

// queryTypeYAML renders the Newznab "t" parameter through one of the three
// template motifs found in the synced definition corpus. The definition and
// tracker are invented for testing (no real site involved).
const queryTypeYAML = `
id: typetracker
name: TypeTracker
description: Query.Type rendering fixture
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      inputs:
        t: %s
      response:
        type: html
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
`

const (
	motifVerbatim = `"{{ .Query.Type }}"`
	motifIfType   = `"{{ if .Query.Type }}{{ .Query.Type }}{{ else }}search{{ end }}"`
	motifIfWords  = `"{{ if .Keywords }}{{ .Query.Type }}{{ else }}search{{ end }}"`
)

// TestSearch_QueryType verifies how Query.Type reaches the "t" query
// parameter: the engine default ("search" when Type is empty) and the
// keyword-less branch of the if-.Keywords motif (Prowlarr parity: it falls
// back to "search" even for a tvsearch query).
func TestSearch_QueryType(t *testing.T) {
	cases := []struct {
		name      string
		motif     string
		query     Query
		wantT     string
		wantNoReq bool // empty-search gate: no HTTP request at all
	}{
		{
			name:  "verbatim motif defaults to search when Type empty",
			motif: motifVerbatim,
			query: Query{Keywords: "ubuntu"},
			wantT: "search",
		},
		{
			name:  "if-Type motif defaults to search when Type empty",
			motif: motifIfType,
			query: Query{Keywords: "ubuntu"},
			wantT: "search",
		},
		{
			name:  "if-Keywords motif forwards tvsearch with keywords",
			motif: motifIfWords,
			query: Query{Keywords: "ubuntu", Type: "tvsearch"},
			wantT: "tvsearch",
		},
		{
			name:  "if-Keywords motif without keywords keeps search (Prowlarr parity)",
			motif: motifIfWords,
			query: Query{Season: "2", Type: "tvsearch"},
			wantT: "search",
		},
		{
			name:  "season-only search passes the empty gate with default search",
			motif: motifVerbatim,
			query: Query{Season: "2"},
			wantT: "search",
		},
		{
			name:      "fully empty query is gated before any request",
			motif:     motifVerbatim,
			query:     Query{},
			wantNoReq: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def, err := cmodel.Parse([]byte(fmt.Sprintf(queryTypeYAML, tc.motif)))
			if err != nil {
				t.Fatalf("parsing fixture yaml: %v", err)
			}

			var gotT string
			var requests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				gotT = r.URL.Query().Get("t")
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(`<html><body></body></html>`))
			}))
			defer server.Close()

			provider := NewProvider(def, server.URL, nil)
			if _, err := provider.Search(context.Background(), tc.query); err != nil {
				t.Fatalf("Search returned error: %v", err)
			}

			if tc.wantNoReq {
				if requests != 0 {
					t.Fatalf("expected no request (empty-search gate), got %d", requests)
				}
				return
			}
			if requests != 1 {
				t.Fatalf("expected exactly 1 request, got %d", requests)
			}
			if gotT != tc.wantT {
				t.Errorf("t = %q, want %q", gotT, tc.wantT)
			}
		})
	}
}
