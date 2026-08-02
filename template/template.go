// Package template provides the Cardigann template rendering engine
// (`{{ .Keywords }}`, `{{ if .Config.xxx }}...{{ end }}`, etc.), which is
// natively compatible with Go text/template syntax.
package template

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"text/template"
)

// Context is the rendering context exposed to Cardigann templates.
type Context struct {
	Config      map[string]string // user configuration values (settings)
	Keywords    string            // raw search terms
	Categories  []string          // native category IDs mapped from Newznab IDs
	Query       map[string]string // additional query parameters (categories, season, etc.)
	Result      map[string]string // values already extracted for the current result row
	DownloadUri *url.URL          // resolved download URL (usable in templates)
}

// cachedTemplate is the cache value: the compiled template and its parse
// error (nil when parsing succeeded). Errors are cached too so that an
// invalid template is not re-parsed on every call (no negative-cache
// absence); the error is still returned to the caller each time.
type cachedTemplate struct {
	tmpl *template.Template
	err  error
}

// templateCache caches compiled *template.Template values (and parse
// failures), indexed by their raw source, to avoid re-parsing the same
// Cardigann template on every Render call (called per result row x field x
// extraction pass).
//
// Sizing assumption: template strings come from Cardigann definitions (YAML)
// loaded once, so their cardinality is bounded by the number of
// fields/paths/headers in the definitions corpus. Since this function is
// exported, a caller that generated templates dynamically from unbounded
// input (e.g. user values) would grow this cache indefinitely; that is not
// the intended use.
var templateCache sync.Map // string -> cachedTemplate

// funcMap is static: FuncMap() depends on no state, so build it once rather
// than on every Render call.
var funcMap = template.FuncMap{
	"ToLower": strings.ToLower,
	"ToUpper": strings.ToUpper,
}

// bufPool reuses the output buffers across Render calls. A pooled buffer
// retains its grown capacity, avoiding the per-call grow allocation; it is
// safe to reuse because bytes.Buffer.String() returns a copy.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Render executes tmplStr as a Cardigann template with the given context and
// returns the result as a string.
func Render(tmplStr string, ctx Context) (string, error) {
	cached, err := compile(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing template %q: %w", tmplStr, err)
	}
	if cached.err != nil {
		return "", fmt.Errorf("parsing template %q: %w", tmplStr, cached.err)
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)
	if err := cached.tmpl.Execute(buf, ctx); err != nil {
		return "", fmt.Errorf("executing template %q: %w", tmplStr, err)
	}
	return buf.String(), nil
}

// compile returns the compiled template for tmplStr, reusing from
// templateCache if already seen (successes and failures). A parsed
// *template.Template is safe for concurrent Execute calls, making this shared
// cache usable by multiple goroutines.
func compile(tmplStr string) (cachedTemplate, error) {
	if cached, ok := templateCache.Load(tmplStr); ok {
		return cached.(cachedTemplate), nil
	}
	tmpl, err := template.New("cardigann").Funcs(funcMap).Parse(tmplStr)
	// LoadOrStore to avoid overwriting a template already stored by another
	// goroutine with the same tmplStr (both are equivalent, but we keep a
	// single canonical instance).
	actual, _ := templateCache.LoadOrStore(tmplStr, cachedTemplate{tmpl: tmpl, err: err})
	return actual.(cachedTemplate), nil
}

// FuncMap registers the custom functions available in Cardigann templates
// (distinct from field *filters*, see package filters).
func FuncMap() template.FuncMap {
	return funcMap
}
