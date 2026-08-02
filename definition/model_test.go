package definition

import "testing"

const fixtureDefinitionYAML = `
id: examplepublic
name: Example Public Tracker
description: Fixture inventée pour les tests unitaires
language: en-US
type: public
links:
  - https://example.invalid/
caps:
  categorymappings:
    - {id: "1", cat: Movies, default: true}
settings:
  - name: freeleech
    type: checkbox
    label: Freeleech only
login:
  method: form
  path: /login.php
  inputs:
    username: "{{ .Config.username }}"
    password: "{{ .Config.password }}"
search:
  paths:
    - path: /browse.php
      response: {type: html}
  rows:
    selector: table.results tr
  fields:
    title:
      selector: a.title
`

func TestParse_FullDefinition(t *testing.T) {
	def, err := Parse([]byte(fixtureDefinitionYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if def.ID != "examplepublic" {
		t.Errorf("unexpected id: %q", def.ID)
	}
	if def.Type != "public" {
		t.Errorf("unexpected type: %q", def.Type)
	}
	if def.IsUsenet() {
		t.Error("expected IsUsenet() == false for type:public")
	}
	if len(def.Links) != 1 || def.Links[0] != "https://example.invalid/" {
		t.Errorf("unexpected links: %v", def.Links)
	}
	if def.Login.Method != "form" {
		t.Errorf("unexpected login method: %q", def.Login.Method)
	}
	if def.Login.Inputs["username"] != "{{ .Config.username }}" {
		t.Errorf("unexpected login input template: %q", def.Login.Inputs["username"])
	}
	if len(def.Search.Paths) != 1 || def.Search.Paths[0].Path != "/browse.php" {
		t.Errorf("unexpected search paths: %+v", def.Search.Paths)
	}
	if def.Search.Rows.Selector != "table.results tr" {
		t.Errorf("unexpected rows selector: %q", def.Search.Rows.Selector)
	}
	if len(def.Settings) != 1 || def.Settings[0].Name != "freeleech" {
		t.Errorf("unexpected settings: %+v", def.Settings)
	}
}

func TestParse_UsenetType(t *testing.T) {
	def, err := Parse([]byte("id: usenetidx\nname: Usenet Idx\ntype: usenet\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !def.IsUsenet() {
		t.Error("expected IsUsenet() == true for type:usenet")
	}
}

func TestParse_UsenetCategories(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		wantBool bool
	}{
		{
			name: "all categories in Newznab range",
			yaml: `id: test
name: Test
type: public
caps:
  categorymappings:
    - {id: "2000", cat: "2000", desc: "Movies"}
    - {id: "2010", cat: "2010", desc: "Movies HD"}`,
			wantBool: true,
		},
		{
			name: "category with usenet keyword in description",
			yaml: `id: test
name: Test
type: private
caps:
  categorymappings:
    - {id: "1", cat: "1", desc: "Books/Usenet"}`,
			wantBool: true,
		},
		{
			name: "category with NZB keyword",
			yaml: `id: test
name: Test
type: public
caps:
  categorymappings:
    - {id: "5000", cat: "5000", desc: "NZB General"}`,
			wantBool: true,
		},
		{
			name: "mixed categories (some outside range, no keyword)",
			yaml: `id: test
name: Test
type: public
caps:
  categorymappings:
    - {id: "1", cat: "1", desc: "Movies"}
    - {id: "2000", cat: "2000", desc: "Linux Distros"}`,
			wantBool: false,
		},
		{
			name: "torrent tracker (linuxtracker-like, categories in range but no keyword)",
			yaml: `id: linuxtracker
name: Linux Tracker
type: public
caps:
  categorymappings:
    - {id: "2002", cat: "2002", desc: "4MLinux"}
    - {id: "2289", cat: "2289", desc: "APODIO"}
    - {id: "1", cat: "1", desc: "Other"}`,
			wantBool: false,
		},
		{
			name: "no category mappings",
			yaml: `id: test
name: Test
type: public`,
			wantBool: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def, err := Parse([]byte(tt.yaml))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := def.IsUsenet(); got != tt.wantBool {
				t.Errorf("IsUsenet() = %v, want %v", got, tt.wantBool)
			}
		})
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	_, err := Parse([]byte("id: [unclosed"))
	if err == nil {
		t.Fatal("expected error for invalid yaml")
	}
}

func TestFilterStep_StringArgs(t *testing.T) {
	yaml := `
id: test
name: Test
links:
  - https://example.invalid/
search:
  paths:
    - path: /search
      response: {type: html}
  rows:
    selector: tr
  fields:
    title:
      selector: td
      filters:
        - name: replace
          args: "old new"
`
	def, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	filters := def.Search.Fields["title"].Filters
	if len(filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(filters))
	}
	if filters[0].Name != "replace" {
		t.Errorf("filter name = %q, want %q", filters[0].Name, "replace")
	}
	// String args are kept as a single element array
	if len(filters[0].Args) != 1 || filters[0].Args[0] != "old new" {
		t.Errorf("filter args = %v, want [old new]", filters[0].Args)
	}
}

func TestFilterStep_ArrayArgs(t *testing.T) {
	yaml := `
id: test
name: Test
links:
  - https://example.invalid/
search:
  paths:
    - path: /search
      response: {type: html}
  rows:
    selector: tr
  fields:
    title:
      selector: td
      filters:
        - name: re_replace
          args: ["\\.", " "]
`
	def, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	filters := def.Search.Fields["title"].Filters
	if len(filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(filters))
	}
	if len(filters[0].Args) != 2 || filters[0].Args[0] != "\\." || filters[0].Args[1] != " " {
		t.Errorf("filter args = %v, want [\\.  ]", filters[0].Args)
	}
}

func TestSearchInputs(t *testing.T) {
	yaml := `
id: test
name: Test
links:
  - https://example.invalid/
search:
  paths:
    - path: /search
      response: {type: html}
  inputs:
    name: "{{ .Keywords }}"
    resultByPage: 100
  rows:
    selector: tr
  fields:
    title:
      selector: td
`
	def, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(def.Search.Inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(def.Search.Inputs))
	}
	if def.Search.Inputs["name"] != "{{ .Keywords }}" {
		t.Errorf("input name = %q, want %q", def.Search.Inputs["name"], "{{ .Keywords }}")
	}
	if def.Search.Inputs["resultByPage"] != "100" {
		t.Errorf("input resultByPage = %q, want %q", def.Search.Inputs["resultByPage"], "100")
	}
}

func TestSearchHeaders(t *testing.T) {
	yaml := `
id: test
name: Test
links:
  - https://example.invalid/
search:
  paths:
    - path: /search
      response: {type: html}
  headers:
    Authorization: ["Bearer {{ .Config.apikey }}"]
  rows:
    selector: tr
  fields:
    title:
      selector: td
`
	def, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(def.Search.Headers) != 1 {
		t.Fatalf("expected 1 header, got %d", len(def.Search.Headers))
	}
	vals, ok := def.Search.Headers["Authorization"]
	if !ok || len(vals) != 1 || vals[0] != "Bearer {{ .Config.apikey }}" {
		t.Errorf("header Authorization = %v, want [Bearer {{ .Config.apikey }}]", vals)
	}
}

func TestSearchPathInputs(t *testing.T) {
	yaml := `
id: test
name: Test
links:
  - https://example.invalid/
search:
  paths:
    - path: /search
      response: {type: html}
      inputs:
        q: "{{ .Keywords }}"
  rows:
    selector: tr
  fields:
    title:
      selector: td
`
	def, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(def.Search.Paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(def.Search.Paths))
	}
	if len(def.Search.Paths[0].Inputs) != 1 {
		t.Fatalf("expected 1 path input, got %d", len(def.Search.Paths[0].Inputs))
	}
	if def.Search.Paths[0].Inputs["q"] != "{{ .Keywords }}" {
		t.Errorf("path input q = %q, want %q", def.Search.Paths[0].Inputs["q"], "{{ .Keywords }}")
	}
}

func TestSettingsOptions(t *testing.T) {
	yaml := `
id: opts
name: Opts
type: public
caps:
  modes:
    search: [q]
settings:
  - name: sort
    type: select
    default: desc
    options:
      desc: Descending
      asc: Ascending
      1: "Numeric key"
search:
  paths:
    - path: /s
  rows:
    selector: x
  fields:
    title:
      selector: y
`
	def, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(def.Settings) != 1 {
		t.Fatalf("expected 1 setting, got %d", len(def.Settings))
	}
	opts := def.Settings[0].Options
	if opts.Len() != 3 {
		t.Fatalf("expected 3 options, got %d", opts.Len())
	}
	if got := opts.Value("desc"); got != "Descending" {
		t.Errorf("options.desc = %q, want Descending", got)
	}
	if got := opts.Value("1"); got != "Numeric key" {
		t.Errorf("numeric option key = %q, want 'Numeric key'", got)
	}
	keys := opts.Keys()
	if len(keys) != 3 || keys[0] != "desc" || keys[1] != "asc" || keys[2] != "1" {
		t.Errorf("document order not preserved: %v", keys)
	}
}

func TestCapsCategories(t *testing.T) {
	yaml := `
id: cats
name: Cats
type: public
caps:
  categories:
    XXX: XXX
    1: TV
  modes:
    search: [q]
search:
  paths:
    - path: /s
  rows:
    selector: x
  fields:
    title:
      selector: y
`
	def, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cats := def.Caps.Categories
	if cats.Len() != 2 {
		t.Fatalf("expected 2 categories, got %d", cats.Len())
	}
	if got := cats.Value("1"); got != "TV" {
		t.Errorf("categories.1 = %q, want TV", got)
	}
	keys := cats.Keys()
	if len(keys) != 2 || keys[0] != "XXX" || keys[1] != "1" {
		t.Errorf("document order not preserved: %v", keys)
	}
}

func TestSearchPathSingular(t *testing.T) {
	yaml := `
id: singular
name: Singular
type: public
caps:
  modes:
    search: [q]
search:
  path: /browse.php
  rows:
    selector: x
  fields:
    title:
      selector: y
`
	def, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(def.Search.Paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(def.Search.Paths))
	}
	if def.Search.Paths[0].Path != "/browse.php" {
		t.Errorf("path = %q, want /browse.php", def.Search.Paths[0].Path)
	}
	// The plural form must still work and take precedence when both are set.
	yaml2 := `
id: plural
name: Plural
type: public
caps:
  modes:
    search: [q]
search:
  path: /singular.php
  paths:
    - path: /plural.php
  rows:
    selector: x
  fields:
    title:
      selector: y
`
	def2, err := Parse([]byte(yaml2))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(def2.Search.Paths) != 1 || def2.Search.Paths[0].Path != "/plural.php" {
		t.Errorf("expected paths to win, got %+v", def2.Search.Paths)
	}
}

func TestRowsAttributeParse(t *testing.T) {
	yaml := `
id: rowsattr
name: RowsAttr
type: public
caps:
  modes:
    search: [q]
search:
  path: /api
  response:
    type: json
  rows:
    selector: data
    attribute: attributes
  fields:
    title:
      selector: title
`
	def, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if def.Search.Rows.Attribute != "attributes" {
		t.Errorf("rows.attribute = %q, want attributes (parse-only)", def.Search.Rows.Attribute)
	}
}
