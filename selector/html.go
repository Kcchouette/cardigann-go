package selector

import (
	"fmt"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"
)

// matcherCache caches compiled cascadia matchers, indexed by the raw selector
// string. goquery recompiles the matcher on every Find call; the engine calls
// Find once per row selector and once per field selector per row, so caching
// here removes the dominant selector-parsing cost.
//
// Sizing assumption: selectors come from loaded Cardigann definitions (YAML),
// so their cardinality is bounded by the definitions corpus.
var matcherCache sync.Map // string -> cascadia.Selector

// compileMatcher returns the compiled cascadia selector for the given
// selector string, reusing from matcherCache if already seen. cascadia.Selector
// satisfies goquery.Matcher (Match/MatchAll/Filter).
func compileMatcher(selector string) (cascadia.Selector, error) {
	if m, ok := matcherCache.Load(selector); ok {
		return m.(cascadia.Selector), nil
	}
	m, err := cascadia.Compile(selector)
	if err != nil {
		return nil, err
	}
	actual, _ := matcherCache.LoadOrStore(selector, m)
	return actual.(cascadia.Selector), nil
}

// NewHTMLDocument parses HTML content for CSS selectors (goquery).
func NewHTMLDocument(html string) (Document, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parsing html: %w", err)
	}
	return &htmlDocument{doc: doc}, nil
}

type htmlDocument struct {
	doc *goquery.Document
}

func (d *htmlDocument) Find(selector string) ([]Node, error) {
	m, err := compileMatcher(selector)
	if err != nil {
		// Parity with goquery v1.12's compileMatcher, which returns an
		// invalidMatcher (matches nothing) instead of an error.
		return nil, nil
	}
	sel := d.doc.FindMatcher(m)
	nodes := make([]Node, 0, sel.Length())
	sel.Each(func(_ int, s *goquery.Selection) {
		nodes = append(nodes, &htmlNode{sel: s})
	})
	return nodes, nil
}

type htmlNode struct {
	sel *goquery.Selection
}

func (n *htmlNode) Text() string {
	return strings.TrimSpace(n.sel.Text())
}

func (n *htmlNode) Attr(name string) (string, bool) {
	return n.sel.Attr(name)
}

// Find re-applies a CSS selector relative to this node (sub-selecting
// a field inside a result row).
func (n *htmlNode) Find(selector string) ([]Node, error) {
	m, err := compileMatcher(selector)
	if err != nil {
		return nil, nil
	}
	sub := n.sel.FindMatcher(m)
	nodes := make([]Node, 0, sub.Length())
	sub.Each(func(_ int, s *goquery.Selection) {
		nodes = append(nodes, &htmlNode{sel: s})
	})
	return nodes, nil
}
