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
  login: /login.php
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
