package selector

import (
	"fmt"
	"strings"
	"testing"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	tmpl "github.com/Kcchouette/cardigann-go/template"
)

// Fixtures are built once at package init (outside any timed loop) so the
// timed sections measure parsing/selection only, never fixture generation.

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

const (
	benchHTMLRows = 100
	benchJSONRows = 2000
	benchXMLRows  = 100
)

var (
	benchHTMLBody = benchHTMLTable(benchHTMLRows)
	benchJSONBody = benchJSONArray(benchJSONRows)
	benchXMLBody  = benchXMLFeed(benchXMLRows)
)

// BenchmarkHTMLFind_Rows100 measures document parse + one row selector Find.
// goquery recompiles the cascadia matcher on every Find call.
func BenchmarkHTMLFind_Rows100(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		doc, err := NewHTMLDocument(benchHTMLBody)
		if err != nil {
			b.Fatal(err)
		}
		nodes, err := doc.Find("tr.row")
		if err != nil {
			b.Fatal(err)
		}
		if len(nodes) != benchHTMLRows {
			b.Fatalf("expected %d rows, got %d", benchHTMLRows, len(nodes))
		}
	}
}

// BenchmarkHTMLSubSelect_Fields10 measures the per-row x per-field sub-select
// cost (a.title, td.size, ...) over all 100 rows — the shape of field
// extraction in the engine.
func BenchmarkHTMLSubSelect_Fields10(b *testing.B) {
	doc, err := NewHTMLDocument(benchHTMLBody)
	if err != nil {
		b.Fatal(err)
	}
	rows, err := doc.Find("tr.row")
	if err != nil {
		b.Fatal(err)
	}
	fieldSelectors := []string{"a.title", "td.size", "td.seeders", "td.date", "a.dl", "td.size", "a.title", "td.seeders", "td.date", "a.dl"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, row := range rows {
			sub, ok := row.(SubSelectable)
			if !ok {
				b.Fatal("row is not SubSelectable")
			}
			for _, sel := range fieldSelectors {
				if _, err := sub.Find(sel); err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}

// BenchmarkHTMLFind_Repeat isolates the selector-recompilation cost from the
// tree-walk cost: the same selector applied repeatedly to the same document.
func BenchmarkHTMLFind_Repeat(b *testing.B) {
	doc, err := NewHTMLDocument(benchHTMLBody)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < benchHTMLRows; j++ {
			if _, err := doc.Find("tr.row"); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkJSONFind_Items2000 measures JSON validation + the "$" row selector.
func BenchmarkJSONFind_Items2000(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		doc, err := NewJSONDocument(benchJSONBody)
		if err != nil {
			b.Fatal(err)
		}
		nodes, err := doc.Find("$")
		if err != nil {
			b.Fatal(err)
		}
		if len(nodes) != benchJSONRows {
			b.Fatalf("expected %d rows, got %d", benchJSONRows, len(nodes))
		}
	}
}

// BenchmarkJSONAttr_Scan2000 measures per-node Attr lookups, which re-scan the
// raw JSON string for every node (O(n*m) over the response).
func BenchmarkJSONAttr_Scan2000(b *testing.B) {
	doc, err := NewJSONDocument(benchJSONBody)
	if err != nil {
		b.Fatal(err)
	}
	nodes, err := doc.Find("$")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, node := range nodes {
			if _, ok := node.Attr("title"); !ok {
				b.Fatal("missing title attr")
			}
		}
	}
}

// BenchmarkXMLFind_Rows100 measures etree parse + row selector Find (which
// re-parses the CSS selector and re-walks the tree on every call).
func BenchmarkXMLFind_Rows100(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		doc, err := NewXMLDocument(benchXMLBody)
		if err != nil {
			b.Fatal(err)
		}
		nodes, err := doc.Find("rss > channel > item")
		if err != nil {
			b.Fatal(err)
		}
		if len(nodes) != benchXMLRows {
			b.Fatalf("expected %d rows, got %d", benchXMLRows, len(nodes))
		}
	}
}

// BenchmarkXMLSubSelect_Fields measures per-row sub-selection (title, size,
// seeders, date, enclosure) — each Find re-parses the CSS selector and walks
// the subtree.
func BenchmarkXMLSubSelect_Fields(b *testing.B) {
	doc, err := NewXMLDocument(benchXMLBody)
	if err != nil {
		b.Fatal(err)
	}
	rows, err := doc.Find("rss > channel > item")
	if err != nil {
		b.Fatal(err)
	}
	fieldSelectors := []string{"title", "[name=size]", "[name=seeders]", "[name=date]", "enclosure"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, row := range rows {
			sub, ok := row.(SubSelectable)
			if !ok {
				b.Fatal("row is not SubSelectable")
			}
			for _, sel := range fieldSelectors {
				if _, err := sub.Find(sel); err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}

// BenchmarkCheckErrors_ReParse measures the error-selector path, which
// re-parses the whole HTML body on every call (selector/errors.go).
func BenchmarkCheckErrors_ReParse(b *testing.B) {
	selectors := []cmodel.ErrorSelector{
		{Selector: "div.error-box", Message: cmodel.MessageSelector{Selector: "span.error-msg"}},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := CheckErrors(benchHTMLBody, selectors, tmpl.Context{}); err != nil {
			b.Fatal(err)
		}
	}
}
