package template

import (
	"fmt"
	"sync"
	"testing"
)

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

func TestRender_CachedTemplateConcurrent(t *testing.T) {
	const tmplStr = `{{ .Keywords }}-{{ ToUpper .Keywords }}`

	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			got, err := Render(tmplStr, Context{Keywords: "kw"})
			if err != nil {
				errs <- err
				return
			}
			if got != "kw-KW" {
				errs <- fmt.Errorf("goroutine %d: got %q, want %q", n, got, "kw-KW")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestRender_InvalidTemplateNotCached(t *testing.T) {
	// Un template invalide échoue à chaque appel (pas de cache d'erreur).
	for i := 0; i < 3; i++ {
		if _, err := Render(`{{ .Unclosed`, Context{}); err == nil {
			t.Fatalf("call %d: expected parse error", i)
		}
	}
}

func BenchmarkRender_CachedTemplate(b *testing.B) {
	tmplStr := `search?q={{ .Keywords }}&cat={{ index .Query "categories" }}`
	ctx := Context{Keywords: "ubuntu", Query: map[string]string{"categories": "1,2,3"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Render(tmplStr, ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRender_QueryFields(t *testing.T) {
	tests := []struct {
		name     string
		template string
		query    map[string]string
		expected string
	}{
		{"TVDBID", `{{ index .Query "TVDBID" }}`, map[string]string{"TVDBID": "12345"}, "12345"},
		{"Year", `{{ index .Query "Year" }}`, map[string]string{"Year": "2024"}, "2024"},
		{"Genre", `{{ index .Query "Genre" }}`, map[string]string{"Genre": "action"}, "action"},
		{"DoubanID", `{{ index .Query "DoubanID" }}`, map[string]string{"DoubanID": "3691"}, "3691"},
		{"IMDBIDShort", `{{ index .Query "IMDBIDShort" }}`, map[string]string{"IMDBIDShort": "1234567"}, "1234567"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.template, Context{Query: tt.query})
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}
