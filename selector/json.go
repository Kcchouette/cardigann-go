package selector

import (
	"fmt"

	"github.com/tidwall/gjson"
)

// NewJSONDocument prepares JSON content for gjson path selectors.
//
// Documented limitation (see architecture review): gjson is not a complete
// JSONPath engine equivalent to what some Cardigann definitions use; the
// most advanced expressions (`?()` filters, complex unions) are not
// guaranteed to work identically.
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
	// Special case: "$" means "all elements of the root array"
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
		// If not an array, return the root element
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

// Find re-applies a gjson selector relative to this node (sub-selecting
// a field inside a JSON result row).
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
