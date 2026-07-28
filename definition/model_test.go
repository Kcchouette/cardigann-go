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

func TestParse_InvalidYAML(t *testing.T) {
	_, err := Parse([]byte("id: [unclosed"))
	if err == nil {
		t.Fatal("expected error for invalid yaml")
	}
}
