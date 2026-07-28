package selector

import (
	"fmt"
	"strings"

	"github.com/beevik/etree"
)

// selectorStep represents one part of a parsed CSS selector.
type selectorStep struct {
	tag         string // element name, "*" for wildcard
	attribute   string // attribute filter key (e.g. "name" for [name=X])
	attrValue   string // attribute filter value (e.g. "category" for [name=category])
	childOnly   bool   // true if this step requires direct child (>)
}

// parseCSSSelector parses a CSS selector string into a list of steps.
// Supports: element names, wildcard *, child combinator >, attribute selectors [key=val].
func parseCSSSelector(sel string) []selectorStep {
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return nil
	}

	var steps []selectorStep
	parts := strings.Split(sel, ">")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		step := selectorStep{childOnly: true}
		if idx := strings.Index(part, "["); idx != -1 {
			step.tag = strings.TrimSpace(part[:idx])
			attrPart := part[idx+1:]
			if eqIdx := strings.Index(attrPart, "="); eqIdx != -1 {
				step.attribute = strings.TrimSpace(attrPart[:eqIdx])
				step.attrValue = strings.TrimSpace(strings.TrimSuffix(attrPart[eqIdx+1:], "]"))
			}
		} else {
			step.tag = strings.TrimSpace(part)
		}
		steps = append(steps, step)
	}

	// First step should not require child combinator
	if len(steps) > 0 {
		steps[0].childOnly = false
	}
	return steps
}

// applySelector navigates the XML tree from root using parsed CSS selector steps.
func applySelector(root *etree.Element, steps []selectorStep) []*etree.Element {
	if len(steps) == 0 {
		return nil
	}

	// For the first step, check if the root itself matches
	var currents []*etree.Element
	first := steps[0]
	rootMatch := (first.tag == "" || first.tag == "*" || root.Tag == first.tag)
	if first.attribute != "" {
		rootMatch = rootMatch && matchAttribute(root, first.attribute, first.attrValue)
	}
	if rootMatch {
		currents = []*etree.Element{root}
	} else {
		// First step doesn't match root; search children of root
		currents = getChildren(root, first)
	}

	for _, step := range steps[1:] {
		var nexts []*etree.Element
		for _, cur := range currents {
			children := getChildren(cur, step)
			nexts = append(nexts, children...)
		}
		currents = nexts
	}

	return currents
}

// getChildren returns the matching children of an element for a given step.
func getChildren(parent *etree.Element, step selectorStep) []*etree.Element {
	var matches []*etree.Element
	for _, child := range parent.ChildElements() {
		tagMatch := step.tag == "" || step.tag == "*" || child.Tag == step.tag
		if !tagMatch {
			continue
		}
		if step.attribute != "" {
			if !matchAttribute(child, step.attribute, step.attrValue) {
				continue
			}
		}
		matches = append(matches, child)
	}
	return matches
}

// matchAttribute checks if an element has a child whose attr key/value match,
// OR if the element itself has the named attribute with that value.
// For XML/RSS [name=category] selectors, we search child elements that have
// an attribute with the given key/value.
func matchAttribute(el *etree.Element, key, value string) bool {
	// Check direct attribute on the element
	for _, attr := range el.Attr {
		if attr.Key == key && attr.Value == value {
			return true
		}
	}
	// Check child elements that have an attribute matching key=value
	// This handles <category>child</category> patterns where name attribute
	// on child elements is used for selection
	for _, child := range el.ChildElements() {
		for _, attr := range child.Attr {
			if attr.Key == key && attr.Value == value {
				return true
			}
		}
	}
	return false
}

// NewXMLDocument parses an XML string and returns a Document that supports
// CSS-like selectors for navigating the XML tree.
func NewXMLDocument(xml string) (Document, error) {
	xml = strings.TrimSpace(xml)
	if xml == "" {
		return nil, fmt.Errorf("parsing xml: empty document")
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromString(xml); err != nil {
		return nil, fmt.Errorf("parsing xml: %w", err)
	}
	if doc.Root() == nil {
		return nil, fmt.Errorf("parsing xml: no root element")
	}
	return &xmlDocument{doc: doc}, nil
}

type xmlDocument struct {
	doc *etree.Document
}

func (d *xmlDocument) Find(selector string) ([]Node, error) {
	steps := parseCSSSelector(selector)
	if len(steps) == 0 {
		return nil, nil
	}

	elements := applySelector(d.doc.Root(), steps)
	nodes := make([]Node, 0, len(elements))
	for _, el := range elements {
		nodes = append(nodes, &xmlNode{el: el})
	}
	return nodes, nil
}

type xmlNode struct {
	el *etree.Element
}

func (n *xmlNode) Text() string {
	return strings.TrimSpace(n.el.Text())
}

func (n *xmlNode) Attr(name string) (string, bool) {
	for _, attr := range n.el.Attr {
		if attr.Key == name {
			return attr.Value, true
		}
	}
	return "", false
}

func (n *xmlNode) Find(selector string) ([]Node, error) {
	steps := parseCSSSelector(selector)
	if len(steps) == 0 {
		return nil, nil
	}

	// For sub-selection, treat the first step as direct children of this node
	// regardless of child combinator
	var elements []*etree.Element
	if len(steps) == 1 {
		elements = getChildren(n.el, steps[0])
	} else {
		elements = applySelector(n.el, steps)
	}

	nodes := make([]Node, 0, len(elements))
	for _, el := range elements {
		nodes = append(nodes, &xmlNode{el: el})
	}
	return nodes, nil
}
