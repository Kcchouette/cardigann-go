package definition

import (
	"strings"
	"testing"
)

func TestParse_SearchBeforeListForm(t *testing.T) {
	const yaml = `
id: beforelist
name: Before List
description: fixture
language: en-US
type: ddl
links:
  - https://example.invalid/
search:
  before:
    - name: title
      path: titles
      inputs:
        query: "{{ .Keywords }}"
      response: {type: json}
      selectors:
        id: {selector: "$.pagination.data.0.id"}
    - name: extra
      path: other
      selectors:
        v: {selector: "#x"}
  paths:
    - path: "items/{{ .Before.title.id }}"
  rows:
    selector: ".row"
  fields:
    title:
      selector: a
`
	def, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(def.Search.Before) != 2 {
		t.Fatalf("expected 2 before steps, got %d", len(def.Search.Before))
	}
	first := def.Search.Before[0]
	if first.Name != "title" || first.Path != "titles" {
		t.Errorf("unexpected first step: %+v", first)
	}
	if first.Response.Type != "json" {
		t.Errorf("expected response type json, got %q", first.Response.Type)
	}
	if _, ok := first.Selectors["id"]; !ok || first.Selectors["id"].Selector != "$.pagination.data.0.id" {
		t.Errorf("unexpected selectors: %+v", first.Selectors)
	}
	second := def.Search.Before[1]
	if second.Method != "" || second.Response.Type != "" {
		t.Errorf("expected empty defaults, got method %q response %q", second.Method, second.Response.Type)
	}
	// The search path template must have survived the mirror unmarshal.
	if len(def.Search.Paths) != 1 || def.Search.Paths[0].Path != "items/{{ .Before.title.id }}" {
		t.Errorf("search paths not mirrored: %+v", def.Search.Paths)
	}
}

func TestParse_SearchBeforeSingleMappingForm(t *testing.T) {
	const yaml = `
id: beforesingle
name: Before Single
description: fixture
language: en-US
type: ddl
links:
  - https://example.invalid/
search:
  before:
    name: title
    path: titles
    selectors:
      id: {selector: "$.id"}
  paths:
    - path: x
  rows:
    selector: .row
  fields:
    title:
      selector: a
`
	def, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(def.Search.Before) != 1 || def.Search.Before[0].Name != "title" {
		t.Fatalf("single-mapping before not parsed: %+v", def.Search.Before)
	}
}

func TestValidate_SearchBeforeWarnings(t *testing.T) {
	const yaml = `
id: beforewarn
name: Before Warn
description: fixture
language: en-US
type: ddl
links:
  - https://example.invalid/
search:
  before:
    - name: bad-name
      selectors:
        id: {selector: "$.id"}
    - path: lonely
    - name: ok
      path: fine
  paths:
    - path: x
  rows:
    selector: .row
  fields:
    title:
      selector: a
`
	def, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	warns := def.Validate()
	joined := strings.Join(warns, "\n")
	for _, want := range []string{"invalid name", "no path", "captures no selectors"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected warning containing %q, got:\n%s", want, joined)
		}
	}
}
