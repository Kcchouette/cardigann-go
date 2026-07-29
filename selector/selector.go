// Package selector provides a common abstraction for the different Cardigann
// extraction modes (CSS for HTML, JSON path for JSON, XML via etree).
package selector

// Document is the abstraction for parsed response content, regardless of format.
type Document interface {
	// Find returns the nodes matching the given selector.
	Find(selector string) ([]Node, error)
}

// Node represents an extracted node/element, with access to text and attributes.
type Node interface {
	Text() string
	Attr(name string) (string, bool)
}

// SubSelectable is implemented by nodes capable of re-applying a selector
// relative to themselves (e.g. selecting a field inside an already-selected
// result row).
type SubSelectable interface {
	Find(selector string) ([]Node, error)
}
