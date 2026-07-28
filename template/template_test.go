package template

import "testing"

func TestRender_Keywords(t *testing.T) {
	got, err := Render("search?q={{ .Keywords }}", Context{Keywords: "ubuntu server"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "search?q=ubuntu server" {
		t.Errorf("unexpected render: %q", got)
	}
}

func TestRender_ConfigLookup(t *testing.T) {
	got, err := Render("user={{ .Config.username }}", Context{Config: map[string]string{"username": "alice"}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "user=alice" {
		t.Errorf("unexpected render: %q", got)
	}
}

func TestRender_Conditional(t *testing.T) {
	tmplStr := `{{ if .Config.freeleech }}fl=1{{ else }}fl=0{{ end }}`

	got, err := Render(tmplStr, Context{Config: map[string]string{"freeleech": "true"}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "fl=1" {
		t.Errorf("expected fl=1, got %q", got)
	}

	got, err = Render(tmplStr, Context{Config: map[string]string{}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "fl=0" {
		t.Errorf("expected fl=0, got %q", got)
	}
}

func TestRender_CustomFuncs(t *testing.T) {
	got, err := Render(`{{ ToUpper .Keywords }}`, Context{Keywords: "hello"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "HELLO" {
		t.Errorf("expected HELLO, got %q", got)
	}

	got, err = Render(`{{ ToLower .Keywords }}`, Context{Keywords: "WORLD"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "world" {
		t.Errorf("expected world, got %q", got)
	}
}

func TestRender_InvalidTemplate(t *testing.T) {
	_, err := Render(`{{ .Unclosed`, Context{})
	if err == nil {
		t.Fatal("expected parse error for invalid template")
	}
}
