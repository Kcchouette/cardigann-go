// Package selector fournit une abstraction commune aux différents modes
// d'extraction Cardigann (CSS pour HTML, JSON path pour JSON). Le support
// XPath/XML est repoussé au post-MVP (Slice E), le RSS Newznab utilisant
// `encoding/xml` directement (voir internal/newznab).
package selector

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/tidwall/gjson"
)

// Document est l'abstraction d'un contenu de réponse parsé, quel que soit son format.
type Document interface {
	// Find retourne les noeuds correspondant au sélecteur donné.
	Find(selector string) ([]Node, error)
}

// Node représente un noeud/élément extrait, avec accès au texte et aux attributs.
type Node interface {
	Text() string
	Attr(name string) (string, bool)
}

// SubSelectable est implémenté par les noeuds capables de ré-appliquer un
// sélecteur relativement à eux-mêmes (ex: sélectionner un champ à l'intérieur
// d'une ligne de résultat déjà sélectionnée).
type SubSelectable interface {
	Find(selector string) ([]Node, error)
}

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

// NewJSONDocument prépare un contenu JSON pour des sélecteurs de type gjson path.
//
// Limitation documentée (cf. revue architecture) : gjson n'est pas un moteur
// JSONPath complet équivalent à ce qu'utilisent certaines définitions
// Cardigann ; les expressions les plus avancées (filtres `?()`, unions
// complexes) ne sont pas garanties de fonctionner à l'identique.
func NewJSONDocument(json string) (Document, error) {
	if !gjson.Valid(json) {
		return nil, fmt.Errorf("invalid json document")
	}
	return &jsonDocument{raw: json}, nil
}

type jsonDocument struct {
	raw string
}

func (d *jsonDocument) Find(path string) ([]Node, error) {
	result := gjson.Get(d.raw, path)
	if !result.Exists() {
		return nil, nil
	}
	if result.IsArray() {
		nodes := make([]Node, 0)
		result.ForEach(func(_, value gjson.Result) bool {
			nodes = append(nodes, &jsonNode{result: value})
			return true
		})
		return nodes, nil
	}
	return []Node{&jsonNode{result: result}}, nil
}

type jsonNode struct {
	result gjson.Result
}

func (n *jsonNode) Text() string {
	return n.result.String()
}

func (n *jsonNode) Attr(name string) (string, bool) {
	sub := n.result.Get(name)
	if !sub.Exists() {
		return "", false
	}
	return sub.String(), true
}
