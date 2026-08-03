package selector

import (
	"fmt"
	stdhtml "html"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
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

// TextExcluding returns the node's trimmed text, skipping any descendant
// subtree matched by one of the given selectors. Read-only: the underlying
// document is never mutated, so it is safe under the parallel row extraction
// that shares a single document.
func (n *htmlNode) TextExcluding(selectors []string) string {
	matchers := make([]cascadia.Selector, 0, len(selectors))
	for _, s := range selectors {
		if s == "" {
			continue
		}
		if m, err := compileMatcher(s); err == nil {
			matchers = append(matchers, m)
		}
	}
	if len(matchers) == 0 || len(n.sel.Nodes) == 0 {
		return n.Text()
	}

	var b strings.Builder
	var walk func(node *html.Node)
	walk = func(node *html.Node) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			switch child.Type {
			case html.ElementNode:
				excluded := false
				for _, m := range matchers {
					if m.Match(child) {
						excluded = true
						break
					}
				}
				if excluded {
					continue
				}
				walk(child)
			case html.TextNode:
				b.WriteString(child.Data)
			}
		}
	}
	for _, root := range n.sel.Nodes {
		walk(root)
	}
	return strings.TrimSpace(b.String())
}

// Attr returns the node's attribute value with HTML character references
// decoded (x/net/html keeps attribute values raw, so e.g. a href containing
// &amp; must be unescaped before being used as a URL).
func (n *htmlNode) Attr(name string) (string, bool) {
	v, ok := n.sel.Attr(name)
	if !ok {
		return "", false
	}
	return stdhtml.UnescapeString(v), true
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
