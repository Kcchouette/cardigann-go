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
	// root is the original row the node was expanded from (rows.attribute);
	// `..`-prefixed field selectors evaluate against it (Prowlarr's
	// recursive-descent escape hatch). Zero value = unset.
	root gjson.Result
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

// ExpandAttribute applies rows.attribute semantics to a JSON row: the named
// key's value becomes the row (single object/value, or each element of its
// array when multiple is true); each expanded node links back to the
// original row for `..` selectors. ok=false for non-JSON nodes (HTML/XML
// ignore attribute) or an absent attribute.
func ExpandAttribute(node Node, attribute string, multiple bool) ([]Node, bool) {
	jn, ok := node.(*jsonNode)
	if !ok {
		return nil, false
	}
	sub := jn.result.Get(attribute)
	if !sub.Exists() {
		return nil, false
	}
	if multiple && sub.IsArray() {
		out := make([]Node, 0)
		sub.ForEach(func(_, v gjson.Result) bool {
			out = append(out, &jsonNode{result: v, root: jn.result})
			return true
		})
		return out, true
	}
	return []Node{&jsonNode{result: sub, root: jn.result}}, true
}

// descentKey returns the first value whose object key matches key, walking
// the gjson tree in document order (mimics Json.NET's `..key` recursive
// descent over the original row). gjson itself has no `..` support (the
// prefix is JSON-Lines-only), so the walk is implemented here. Only bare
// keys are supported (`..a.b` is not).
func descentKey(r gjson.Result, key string) (gjson.Result, bool) {
	var found gjson.Result
	if r.IsObject() {
		r.ForEach(func(k, v gjson.Result) bool {
			if k.String() == key {
				found = v
				return false
			}
			if vv, ok := descentKey(v, key); ok {
				found = vv
				return false
			}
			return true
		})
	} else if r.IsArray() {
		r.ForEach(func(_, v gjson.Result) bool {
			if vv, ok := descentKey(v, key); ok {
				found = vv
				return false
			}
			return true
		})
	}
	return found, found.Exists()
}

// Find re-applies a gjson selector relative to this node (sub-selecting
// a field inside a JSON result row). Selectors starting with `..` walk the
// original row (root) in document order instead.
func (n *jsonNode) Find(selector string) ([]Node, error) {
	if strings.HasPrefix(selector, "..") {
		base := n.result
		if n.root.Exists() {
			base = n.root
		}
		if v, ok := descentKey(base, strings.TrimPrefix(selector, "..")); ok {
			return []Node{&jsonNode{result: v, root: n.root}}, nil
		}
		return nil, nil
	}
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
