package template

import "testing"

// BenchmarkRender_FieldTemplate measures a typical per-row field render
// (fresh bytes.Buffer per call, template cache hit).
func BenchmarkRender_FieldTemplate(b *testing.B) {
	src := `{{ if .Config.freeleech }}{{ .Result.title }}{{ end }}`
	ctx := Context{
		Config: map[string]string{"freeleech": "true"},
		Result: map[string]string{"title": "Ubuntu 24.04 ISO"},
	}
	// Warm-up: populate the one-shot template cache outside timing.
	if _, err := Render(src, ctx); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Render(src, ctx); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRender_InvalidTemplate measures the no-negative-cache path: an
// invalid template is re-parsed (and fails) on every Render call.
func BenchmarkRender_InvalidTemplate(b *testing.B) {
	src := `{{ .Unclosed`
	ctx := Context{Result: map[string]string{"title": "x"}}
	if _, err := Render(src, ctx); err == nil {
		b.Fatal("expected parse error")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Render(src, ctx); err == nil {
			b.Fatal("expected parse error")
		}
	}
}
