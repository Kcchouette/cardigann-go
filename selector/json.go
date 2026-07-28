package selector

import (
	"fmt"

	"github.com/tidwall/gjson"
)

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
	// Cas spécial : "$" signifie "tous les éléments du tableau racine"
	if path == "$" {
		result := gjson.Parse(d.raw)
		if result.Exists() && result.IsArray() {
			nodes := make([]Node, 0)
			result.ForEach(func(_, value gjson.Result) bool {
				nodes = append(nodes, &jsonNode{result: value})
				return true
			})
			return nodes, nil
		}
		// Si ce n'est pas un tableau, retourner l'élément racine
		return []Node{&jsonNode{result: result}}, nil
	}

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

// Find ré-applique un sélecteur gjson relativement à ce noeud (sous-sélection
// d'un champ à l'intérieur d'une ligne de résultat JSON).
func (n *jsonNode) Find(selector string) ([]Node, error) {
	result := n.result.Get(selector)
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
