package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	tmpl "github.com/Kcchouette/cardigann-go/template"
)

// Fixtures and definitions are package-level so they are built once; the
// warm-up call before ResetTimer populates the one-shot template/regex caches.

const benchHTMLDefYAML = `
id: benchhtml
name: BenchHTML
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
    selector: table.results tr.row
  fields:
    title:
      selector: a.title
    details:
      selector: a.title
      attribute: href
    size:
      selector: td.size
    seeders:
      selector: td.seeders
    date:
      selector: td.date
    download:
      selector: a.dl
      attribute: href
`

const benchJSONDefYAML = `
id: benchjson
name: BenchJSON
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: json
  rows:
    selector: $
  fields:
    title:
      selector: title
    size:
      selector: size
    seeders:
      selector: seeders
    date:
      selector: date
    download:
      selector: link
`

const benchXMLDefYAML = `
id: benchxml
name: BenchXML
language: en-US
type: public
links:
  - http://example.invalid/
search:
  paths:
    - path: /search
      response:
        type: xml
  rows:
    selector: rss > channel > item
  fields:
    title:
      selector: title
    size:
      selector: "[name=size]"
      attribute: value
    seeders:
      selector: "[name=seeders]"
      attribute: value
    date:
      selector: "[name=date]"
      attribute: value
    download:
      selector: enclosure
      attribute: url
`

const (
	benchHTMLRows = 100
	benchJSONRows = 2000
	benchXMLRows  = 100
)

var (
	benchHTMLBody = benchHTMLTable(benchHTMLRows)
	benchJSONBody = benchJSONArray(benchJSONRows)
	benchXMLBody  = benchXMLFeed(benchXMLRows)

	benchHTMLDef = mustParseDef(benchHTMLDefYAML)
	benchJSONDef = mustParseDef(benchJSONDefYAML)
	benchXMLDef  = mustParseDef(benchXMLDefYAML)
)

func mustParseDef(yaml string) cmodel.IndexerDefinition {
	def, err := cmodel.Parse([]byte(yaml))
	if err != nil {
		panic(err)
	}
	return def
}

func benchHTMLTable(n int) string {
	var sb strings.Builder
	sb.WriteString(`<html><body><table class="results">`)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb,
			`<tr class="row"><td><a class="title" href="/details/%d">Release %d</a></td>`+
				`<td class="size">1048576</td><td class="seeders">42</td>`+
				`<td class="date">2024-01-02T15:04:05Z</td>`+
				`<td><a class="dl" href="/download/%d.torrent">DL</a></td></tr>`, i, i, i)
	}
	sb.WriteString(`</table></body></html>`)
	return sb.String()
}

func benchJSONArray(n int) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb,
			`{"title":"Item %d","size":1048576,"seeders":42,`+
				`"date":"2024-01-02T15:04:05Z","link":"http://example.invalid/dl/%d.torrent"}`, i, i)
	}
	sb.WriteByte(']')
	return sb.String()
}

func benchXMLFeed(n int) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?><rss><channel>`)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb,
			`<item><title>Item %d</title><attr name="size" value="1048576"/>`+
				`<attr name="seeders" value="42"/><attr name="date" value="2024-01-02T15:04:05Z"/>`+
				`<enclosure url="http://example.invalid/dl/%d.torrent"/></item>`, i, i)
	}
	sb.WriteString(`</channel></rss>`)
	return sb.String()
}

// benchmarkExtract measures parseResults end-to-end (document parse, row
// selector, bounded 8-goroutine extraction pool, 10-pass field loop, filters,
// per-field templates) with no HTTP (def.Download is nil).
func benchmarkExtract(b *testing.B, def cmodel.IndexerDefinition, body, responseType string, wantRows int) {
	p := NewProvider(def, "http://example.invalid/", map[string]string{"freeleech": "true"})
	rctx := tmpl.Context{Config: map[string]string{"freeleech": "true"}, Keywords: "ubuntu"}
	ctx := context.Background()

	// Warm-up: populate one-shot caches (template, regex) outside timing.
	if rels, err := p.parseResults(ctx, body, responseType, rctx, "ubuntu"); err != nil {
		b.Fatal(err)
	} else if len(rels) != wantRows {
		b.Fatalf("warm-up: expected %d releases, got %d", wantRows, len(rels))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rels, err := p.parseResults(ctx, body, responseType, rctx, "ubuntu")
		if err != nil {
			b.Fatal(err)
		}
		if len(rels) != wantRows {
			b.Fatalf("expected %d releases, got %d", wantRows, len(rels))
		}
	}
}

func BenchmarkExtract_HTML100(b *testing.B) {
	benchmarkExtract(b, benchHTMLDef, benchHTMLBody, "html", benchHTMLRows)
}

func BenchmarkExtract_JSON2000(b *testing.B) {
	benchmarkExtract(b, benchJSONDef, benchJSONBody, "json", benchJSONRows)
}

func BenchmarkExtract_XML100(b *testing.B) {
	benchmarkExtract(b, benchXMLDef, benchXMLBody, "xml", benchXMLRows)
}
