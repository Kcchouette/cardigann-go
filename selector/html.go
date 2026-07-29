package selector

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

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
	sel := d.doc.Find(selector)
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
	sub := n.sel.Find(selector)
	nodes := make([]Node, 0, sub.Length())
	sub.Each(func(_ int, s *goquery.Selection) {
		nodes = append(nodes, &htmlNode{sel: s})
	})
	return nodes, nil
}
