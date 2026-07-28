package selector

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// NewHTMLDocument parse un contenu HTML pour des sélecteurs CSS (goquery).
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

// Find ré-applique un sélecteur CSS relativement à ce noeud (sous-sélection
// d'un champ à l'intérieur d'une ligne de résultat).
func (n *htmlNode) Find(selector string) ([]Node, error) {
	sub := n.sel.Find(selector)
	nodes := make([]Node, 0, sub.Length())
	sub.Each(func(_ int, s *goquery.Selection) {
		nodes = append(nodes, &htmlNode{sel: s})
	})
	return nodes, nil
}
