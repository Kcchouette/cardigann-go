// Package engine implements the Cardigann definition execution engine:
// search template rendering, HTTP calls, row/field extraction via selectors,
// post-processing filter application, and normalized release construction.
package engine

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/httpclient"
	"github.com/Kcchouette/cardigann-go/internal/httputil"
	"github.com/Kcchouette/cardigann-go/login"
	"github.com/Kcchouette/cardigann-go/release"
	"github.com/Kcchouette/cardigann-go/selector"
	tmpl "github.com/Kcchouette/cardigann-go/template"
	"github.com/Kcchouette/cardigann-go/template/filters"
	"golang.org/x/time/rate"
)

// HTTPDoer is the abstraction for HTTP clients.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Query represents a generic Cardigann search.
type Query struct {
	Keywords    string
	Categories  []int
	IMDBID      string
	IMDBIDShort string
	TMDBID      string
	TVDBID      string
	DoubanID    string
	Season      string
	Ep          string
	Year        string
	Genre       string
}

// Provider executes a Cardigann IndexerDefinition as a search provider.
type Provider struct {
	def     cmodel.IndexerDefinition
	baseURL string
	config  map[string]string
	httpCl  *http.Client

	// client, if set, is used instead of httpCl: it provides persisted cookies
	// (SQLite), rate limiting, and retries (Slice C). Login (if
	// def.Login.Method != "") is triggered once, before the first search request.
	client *httpclient.IndexerClient

	loginOnce  sync.Once
	loginErr   error
	configOnce sync.Once
	limiter    *rate.Limiter
}

// Def returns a copy of the indexer definition.
func (p *Provider) Def() cmodel.IndexerDefinition { return p.def }

// NewProvider builds a ready-to-use Cardigann Provider without session/login
// management (useful for public indexers or tests). If def.RequestDelay > 0,
// a rate limiter is created to space out requests.
func NewProvider(def cmodel.IndexerDefinition, baseURL string, config map[string]string) *Provider {
	p := &Provider{
		def:     def,
		baseURL: baseURL,
		config:  config,
		httpCl:  &http.Client{Timeout: 20 * time.Second},
	}
	if def.RequestDelay > 0 {
		p.limiter = rate.NewLimiter(rate.Every(time.Duration(def.RequestDelay*float64(time.Second))), 1)
	}
	logWarnings(def)
	return p
}

// NewAuthenticatedProvider builds a Cardigann Provider connected to an
// httpclient.IndexerClient (persisted cookies + rate limit + retries) that
// performs the login defined by def.Login before the first search.
func NewAuthenticatedProvider(def cmodel.IndexerDefinition, baseURL string, config map[string]string, client *httpclient.IndexerClient) *Provider {
	logWarnings(def)
	return &Provider{
		def:     def,
		baseURL: baseURL,
		config:  config,
		client:  client,
	}
}

// logWarnings emits the non-fatal protocol validation warnings of a definition.
func logWarnings(def cmodel.IndexerDefinition) {
	for _, w := range def.Validate() {
		slog.Warn("definition validation", "indexer", def.ID, "warning", w)
	}
}

func (p *Provider) ID() string   { return p.def.ID }
func (p *Provider) Name() string { return p.def.Name }

func (p *Provider) Protocol() release.Protocol {
	switch p.def.Type {
	case "usenet":
		return release.ProtocolUsenet
	case "ddl":
		return release.ProtocolDDL
	case "streaming":
		return release.ProtocolStreaming
	default:
		// Heuristic fallback for definitions without an explicit protocol type.
		if p.def.IsUsenet() {
			return release.ProtocolUsenet
		}
		return release.ProtocolTorrent
	}
}

// httpClient returns the effective HTTP client (IndexerClient preferred),
// after ensuring that login (if applicable) has been performed once.
// If a rate limiter is configured (via RequestDelay), the request is spaced out.
func (p *Provider) httpClient(ctx context.Context) (HTTPDoer, error) {
	if p.client == nil {
		if p.limiter != nil {
			if err := p.limiter.Wait(ctx); err != nil {
				return nil, fmt.Errorf("rate limiter: %w", err)
			}
		}
		return p.httpCl, nil
	}
	p.loginOnce.Do(func() {
		if p.def.Login.Method == "" || p.def.Login.Method == "none" {
			return
		}
		p.loginErr = login.Login(ctx, p.client.HTTPClient(), p.def, p.baseURL, p.config)
	})
	if p.loginErr != nil {
		return nil, fmt.Errorf("login: %w", p.loginErr)
	}
	return p.client, nil
}

// Search executes the search for all search.paths in the definition.
//
// Safe for concurrent calls on the same *Provider instance: no shared mutable
// state is modified after the first search (default config values are applied
// only once via configOnce, and filtered keywords remain local to the call).
func (p *Provider) Search(ctx context.Context, q Query) ([]release.Release, error) {
	// Populate config with default values from definition if not already set
	// Note: "false" is truthy in Go templates, so we exclude it from Config
	// so that {{ if .Config.freeleech }} works correctly.
	// Done only once (configOnce): p.config is then treated as read-only,
	// making it safe to share between concurrent searches.
	p.configOnce.Do(func() {
		if p.config == nil {
			p.config = make(map[string]string)
		}
		for _, setting := range p.def.Settings {
			if _, exists := p.config[setting.Name]; !exists && setting.Default != "" && setting.Default != "false" {
				p.config[setting.Name] = setting.Default
			}
		}
	})

	// The render context must exist before the keyword filters run: their
	// args reference .Config (e.g. torrentleech's exclude_archives).
	rctx := tmpl.Context{
		Config:   p.config,
		Keywords: q.Keywords,
		Query: map[string]string{
			"Keywords":    q.Keywords,
			"q":           q.Keywords,
			"IMDBID":      q.IMDBID,
			"IMDBIDShort": q.IMDBIDShort,
			"TMDBID":      q.TMDBID,
			"TVDBID":      q.TVDBID,
			"DoubanID":    q.DoubanID,
			"Season":      q.Season,
			"Ep":          q.Ep,
			"Year":        q.Year,
			"Genre":       q.Genre,
		},
	}

	keywords := q.Keywords
	for _, step := range p.def.Search.KeywordsFilters {
		steps, err := tmpl.RenderFilterSteps([]cmodel.FilterStep{step}, rctx)
		if err != nil {
			return nil, fmt.Errorf("keywordsfilters: %w", err)
		}
		keywords, err = filters.Apply(keywords, steps)
		if err != nil {
			return nil, fmt.Errorf("keywordsfilters: %w", err)
		}
	}
	rctx.Keywords = keywords
	rctx.Query["Keywords"] = keywords
	rctx.Query["q"] = keywords

	// Map Newznab category IDs to native tracker category IDs.
	mappedCategories := mapCategories(q.Categories, p.def.Caps)
	rctx.Categories = mappedCategories
	if len(mappedCategories) > 0 {
		rctx.Query["categories"] = strings.Join(mappedCategories, ",")
	}

	var all []release.Release
	for _, path := range p.def.Search.Paths {
		// Skip paths whose Categories list has no intersection with requested categories.
		if len(path.Categories) > 0 && !categoriesIntersect(path.Categories, q.Categories) {
			continue
		}
		releases, err := p.searchPath(ctx, path, rctx)
		if err != nil {
			return nil, fmt.Errorf("search path %q: %w", path.Path, err)
		}
		all = append(all, releases...)
	}
	return all, nil
}

// mapCategories converts Newznab category IDs to native tracker category IDs
// using the definition's category mappings, falling back to caps.categories
// (native -> newznab representation) when no mappings are declared.
func mapCategories(newznabIDs []int, caps cmodel.Caps) []string {
	if len(newznabIDs) == 0 {
		return nil
	}
	if len(caps.CategoryMappings) == 0 {
		return mapCategoriesFromMap(newznabIDs, caps.Categories)
	}
	// Build a set of Newznab IDs for fast lookup.
	nzSet := make(map[int]struct{}, len(newznabIDs))
	for _, id := range newznabIDs {
		nzSet[id] = struct{}{}
	}
	var result []string
	for _, m := range caps.CategoryMappings {
		catNum := 0
		for _, r := range m.Cat {
			if r >= '0' && r <= '9' {
				catNum = catNum*10 + int(r-'0')
			} else {
				catNum = 0
				break
			}
		}
		if catNum == 0 {
			continue
		}
		if _, ok := nzSet[catNum]; ok {
			result = append(result, m.ID)
		}
	}
	return result
}

// mapCategoriesFromMap converts via caps.categories (native key -> newznab
// representation). Only numeric newznab representations can be matched
// against the requested numeric IDs; name-based values (e.g. {1: TV}) are
// left unmatched without a name->ID table (known limitation).
func mapCategoriesFromMap(newznabIDs []int, categories cmodel.StringMap) []string {
	nzSet := make(map[string]struct{}, len(newznabIDs))
	for _, id := range newznabIDs {
		nzSet[strconv.Itoa(id)] = struct{}{}
	}
	var result []string
	for _, key := range categories.Keys() {
		if v, ok := categories.Lookup(key); ok {
			if _, hit := nzSet[v]; hit {
				result = append(result, key)
			}
		}
	}
	return result
}

// categoriesIntersect returns true if any native ID from the path is in the
// requested Newznab IDs (both as ints).
func categoriesIntersect(pathCategories, requested []int) bool {
	if len(requested) == 0 {
		return true
	}
	set := make(map[int]struct{}, len(requested))
	for _, id := range requested {
		set[id] = struct{}{}
	}
	for _, id := range pathCategories {
		if _, ok := set[id]; ok {
			return true
		}
	}
	return false
}

func (p *Provider) searchPath(ctx context.Context, path cmodel.SearchPath, rctx tmpl.Context) ([]release.Release, error) {
	renderedPath, err := tmpl.Render(path.Path, rctx)
	if err != nil {
		return nil, fmt.Errorf("rendering path: %w", err)
	}

	fullURL := httputil.ResolveURL(p.baseURL, renderedPath)

	method := strings.ToUpper(path.Method)
	if method == "" {
		method = http.MethodGet
	}

	// Use path inputs, or fall back to search inputs
	inputs := path.Inputs
	if len(inputs) == 0 {
		inputs = p.def.Search.Inputs
	}

	// Render inputs and separate $raw
	rendered, rawParams, err := renderInputs(inputs, rctx)
	if err != nil {
		return nil, err
	}

	// Build request with empty/"false" value filtering (allowEmptyInputs
	// overrides the empty-value filter entirely)
	values := filterInputs(rendered, p.def.Search.AllowEmptyInputs)
	appendRawParams(values, rawParams)

	var req *http.Request
	if method == http.MethodPost {
		req, err = http.NewRequestWithContext(ctx, method, fullURL, strings.NewReader(values.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		u, err := url.Parse(fullURL)
		if err != nil {
			return nil, fmt.Errorf("invalid url %q: %w", fullURL, err)
		}
		// Merge existing query params from the URL with new values
		existingValues := u.Query()
		for k, v := range values {
			existingValues[k] = v
		}
		u.RawQuery = existingValues.Encode()
		req, err = http.NewRequestWithContext(ctx, method, u.String(), nil)
		if err != nil {
			return nil, err
		}
	}

	// Apply definition headers (e.g. Authorization: Bearer {{ .Config.apikey }})
	for header, vals := range p.def.Search.Headers {
		for _, v := range vals {
			rendered, err := tmpl.Render(v, rctx)
			if err != nil {
				return nil, fmt.Errorf("rendering header %q: %w", header, err)
			}
			req.Header.Add(header, rendered)
		}
	}

	client, err := p.httpClient(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16 MiB cap
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}

	if err := selector.CheckErrors(string(body), p.def.Search.Error, rctx); err != nil {
		return nil, fmt.Errorf("search error: %w", err)
	}

	return p.parseResults(ctx, string(body), path.Response.Type, rctx, rctx.Keywords)
}
