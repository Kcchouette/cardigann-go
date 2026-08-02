// Package template provides the Cardigann template rendering engine
// (`{{ .Keywords }}`, `{{ if .Config.xxx }}...{{ end }}`, etc.), which is
// natively compatible with Go text/template syntax.
package template

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"text/template"
	"time"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/template/filters"
)

// ErrUnresolved marks a rendered template that still contains unresolved
// placeholders (a missing value rendered as `<no value>` or a raw `{{`).
// Callers defer to a later extraction pass instead of failing hard.
var ErrUnresolved = errors.New("unresolved template")

// RenderFilterSteps renders every filter argument as a template with the
// given context, returning the ready-to-run filter steps. Corpus filter args
// frequently reference .Result / .Config (e.g. `args: "-{{ .Result.year }}"`).
func RenderFilterSteps(steps []cmodel.FilterStep, rctx Context) ([]filters.Step, error) {
	out := make([]filters.Step, len(steps))
	for i, s := range steps {
		args := make([]string, len(s.Args))
		for j, a := range s.Args {
			if !strings.Contains(a, "{{") {
				args[j] = a
				continue
			}
			rendered, err := Render(a, rctx)
			if err != nil || strings.Contains(rendered, "<no value>") || strings.Contains(rendered, "{{") {
				return nil, ErrUnresolved
			}
			args[j] = rendered
		}
		out[i] = filters.Step{Name: s.Name, Args: args}
	}
	return out, nil
}

// Context is the rendering context exposed to Cardigann templates.
type Context struct {
	Config      map[string]string // user configuration values (settings)
	Keywords    string            // raw search terms
	Categories  []string          // native category IDs mapped from Newznab IDs
	Query       map[string]string // additional query parameters (categories, season, etc.)
	Result      map[string]string // values already extracted for the current result row
	DownloadUri *url.URL          // resolved download URL (usable in templates)
}

// True renders the constant "true" (corpus idiom: `{{ .True }}`).
func (Context) True() string { return "true" }

// False renders the empty string, not "false": the engine populates every
// Query key with "" when the parameter is absent and excludes "false" config
// defaults, so the corpus idioms `eq .Query.IMDBID .False` and
// `if .Result._internal` (which receives a rendered .False) require the empty
// string to be falsy.
func (Context) False() string { return "" }

// Today returns the current time, usable as `{{ .Today.Year }}` (corpus idiom
// in empty-keywords/browse branches).
func (Context) Today() time.Time { return time.Now() }

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
// than on every Render call. It exposes the named helpers (ToLower/ToUpper),
// `join`, and every registered field filter as a template function (piped
// value arrives as the last argument; see filterAsFunc). `andmatch` is row
// level and needs keywords — not exposed.
var funcMap = buildFuncMap()

func buildFuncMap() template.FuncMap {
	m := template.FuncMap{
		"ToLower": strings.ToLower,
		"ToUpper": strings.ToUpper,
		"join":    joinFunc,
	}
	for name, fn := range filters.Registry {
		if name == "andmatch" {
			continue
		}
		m[name] = filterAsFunc(name, fn)
	}
	return m
}

// filterAsFunc exposes a field filter (value, args...) as a template
// function. Go template pipes pass the previous value as the LAST argument,
// so the wrapper pops the last variadic argument as the value and forwards
// the rest as the filter's positional args.
func filterAsFunc(name string, fn filters.FilterFunc) func(args ...string) (string, error) {
	return func(args ...string) (string, error) {
		if len(args) == 0 {
			return "", fmt.Errorf("%s: missing value", name)
		}
		value := args[len(args)-1]
		return fn(value, args[:len(args)-1])
	}
}

// joinFunc implements `{{ join .Categories "," }}` (the corpus form) and the
// pipe form `{{ .Cats | join "," }}`. It collects the []string element and
// the string separator regardless of argument order.
func joinFunc(parts ...any) string {
	var elems []string
	sep := ""
	for _, p := range parts {
		switch v := p.(type) {
		case []string:
			elems = v
		case string:
			if sep == "" {
				sep = v
			} else {
				elems = append(elems, v)
			}
		}
	}
	return strings.Join(elems, sep)
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
