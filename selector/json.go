package selector

import (
	"fmt"
	"strings"

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
	// Parse once and keep the Result: gjson.Get operates on Result.Raw, so
	// Find("$") and Find(path) reuse this single parse instead of re-scanning
	// the whole document (gjson.Valid above is the second full pass, the last
	// one per document).
	return &jsonDocument{parsed: gjson.Parse(json)}, nil
}

type jsonDocument struct {
	parsed gjson.Result
}

func (d *jsonDocument) Find(path string) ([]Node, error) {
	// Special case: "$" means "all elements of the root array"
	if path == "$" {
		result := d.parsed
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

	// Normalize the JSONPath-style "$.foo.bar" prefix used by some corpus
	// selectors (e.g. rows.count) — gjson paths are unprefixed.
	path = strings.TrimPrefix(path, "$.")

	result := d.parsed.Get(path)
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

// jsonNodeMarker marks jsonNode as a JSONNode (see selector.JSONNode).
func (n *jsonNode) jsonNodeMarker() {}

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
