// Package engine implémente le moteur d'exécution des définitions Cardigann :
// rendu des templates de recherche, appel HTTP, extraction des lignes/champs
// via les sélecteurs, application des filtres de post-traitement, et
// construction de releases normalisées.
package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/httpclient"
	"github.com/Kcchouette/cardigann-go/login"
	"github.com/Kcchouette/cardigann-go/release"
	"github.com/Kcchouette/cardigann-go/selector"
	tmpl "github.com/Kcchouette/cardigann-go/template"
	"github.com/Kcchouette/cardigann-go/template/filters"
	"golang.org/x/time/rate"
)

// Query représente une recherche Cardigann générique.
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

// Provider exécute une IndexerDefinition Cardigann comme fournisseur de recherche.
type Provider struct {
	Def        cmodel.IndexerDefinition
	BaseURL    string
	Config     map[string]string
	HTTPClient *http.Client

	// Client, si renseigné, est utilisé à la place de HTTPClient : il fournit
	// cookies persistés (SQLite), rate limiting et retries (Slice C). Le login
	// (si def.Login.Method != "") est déclenché une seule fois, avant la
	// première requête de recherche.
	Client *httpclient.IndexerClient

	loginOnce    sync.Once
	loginErr     error
	lastKeywords string
	limiter      *rate.Limiter
}

// NewProvider construit un Provider Cardigann prêt à l'emploi, sans gestion
// de session/login (utile pour les indexeurs publics ou les tests).
// Si def.RequestDelay > 0, un rate limiter est créé pour espacer les requêtes.
func NewProvider(def cmodel.IndexerDefinition, baseURL string, config map[string]string) *Provider {
	p := &Provider{
		Def:        def,
		BaseURL:    baseURL,
		Config:     config,
		HTTPClient: &http.Client{Timeout: 20 * time.Second},
	}
	if def.RequestDelay > 0 {
		p.limiter = rate.NewLimiter(rate.Every(time.Duration(def.RequestDelay*float64(time.Second))), 1)
	}
	return p
}

// NewAuthenticatedProvider construit un Provider Cardigann relié à un
// httpclient.IndexerClient (cookies persistés + rate limit + retries) et
// effectuant le login défini par def.Login avant la première recherche.
func NewAuthenticatedProvider(def cmodel.IndexerDefinition, baseURL string, config map[string]string, client *httpclient.IndexerClient) *Provider {
	return &Provider{
		Def:     def,
		BaseURL: baseURL,
		Config:  config,
		Client:  client,
	}
}

func (p *Provider) ID() string   { return p.Def.ID }
func (p *Provider) Name() string { return p.Def.Name }

func (p *Provider) Protocol() release.Protocol {
	if p.Def.IsUsenet() {
		return release.ProtocolUsenet
	}
	return release.ProtocolTorrent
}

// httpClient retourne le client HTTP effectif (IndexerClient prioritaire),
// après avoir garanti que le login (le cas échéant) a été effectué une fois.
// Si un rate limiter est configuré (via RequestDelay), la requête est espacée.
func (p *Provider) httpClient(ctx context.Context) (interface {
	Do(*http.Request) (*http.Response, error)
}, error) {
	if p.Client == nil {
		if p.limiter != nil {
			if err := p.limiter.Wait(ctx); err != nil {
				return nil, fmt.Errorf("rate limiter: %w", err)
			}
		}
		return p.HTTPClient, nil
	}
	p.loginOnce.Do(func() {
		if p.Def.Login.Method == "" || p.Def.Login.Method == "none" {
			return
		}
		p.loginErr = login.Login(ctx, p.Client.HTTPClient(), p.Def, p.BaseURL, p.Config)
	})
	if p.loginErr != nil {
		return nil, fmt.Errorf("login: %w", p.loginErr)
	}
	return p.Client, nil
}

// Search exécute la recherche pour tous les search.paths de la définition.
func (p *Provider) Search(ctx context.Context, q Query) ([]release.Release, error) {
	keywords := q.Keywords
	for _, step := range p.Def.Search.KeywordsFilters {
		var err error
		keywords, err = filters.Apply(keywords, []filters.Step{{Name: step.Name, Args: step.Args}})
		if err != nil {
			return nil, fmt.Errorf("keywordsfilters: %w", err)
		}
	}
	p.lastKeywords = keywords

	// Map Newznab category IDs to native tracker category IDs.
	mappedCategories := mapCategories(q.Categories, p.Def.Caps.CategoryMappings)

	rctx := tmpl.Context{
		Config:     p.Config,
		Keywords:   keywords,
		Categories: mappedCategories,
		Query: map[string]string{
			"Keywords":    keywords,
			"q":           keywords,
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
	if len(mappedCategories) > 0 {
		rctx.Query["categories"] = strings.Join(mappedCategories, ",")
	}

	var all []release.Release
	for _, path := range p.Def.Search.Paths {
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
// using the definition's category mappings.
func mapCategories(newznabIDs []int, mappings []cmodel.CategoryMapping) []string {
	if len(newznabIDs) == 0 || len(mappings) == 0 {
		return nil
	}
	// Build a set of Newznab IDs for fast lookup.
	nzSet := make(map[int]struct{}, len(newznabIDs))
	for _, id := range newznabIDs {
		nzSet[id] = struct{}{}
	}
	var result []string
	for _, m := range mappings {
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

	fullURL := strings.TrimRight(p.BaseURL, "/") + "/" + strings.TrimLeft(renderedPath, "/")

	method := strings.ToUpper(path.Method)
	if method == "" {
		method = http.MethodGet
	}

	var req *http.Request
	if method == http.MethodPost {
		values := url.Values{}
		for k, v := range path.Inputs {
			rendered, err := tmpl.Render(v, rctx)
			if err != nil {
				return nil, fmt.Errorf("rendering input %q: %w", k, err)
			}
			values.Set(k, rendered)
		}
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
		values := u.Query()
		for k, v := range path.Inputs {
			rendered, err := tmpl.Render(v, rctx)
			if err != nil {
				return nil, fmt.Errorf("rendering input %q: %w", k, err)
			}
			values.Set(k, rendered)
		}
		u.RawQuery = values.Encode()
		req, err = http.NewRequestWithContext(ctx, method, u.String(), nil)
		if err != nil {
			return nil, err
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
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16 MiB cap
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}

	if err := selector.CheckErrors(string(body), p.Def.Search.Error); err != nil {
		return nil, fmt.Errorf("search error: %w", err)
	}

	return p.parseResults(ctx, string(body), path.Response.Type, rctx)
}

func (p *Provider) parseResults(ctx context.Context, body string, responseType string, rctx tmpl.Context) ([]release.Release, error) {
	var doc selector.Document
	var err error
	switch responseType {
	case "json":
		doc, err = selector.NewJSONDocument(body)
	case "xml":
		doc, err = selector.NewXMLDocument(body)
	default:
		doc, err = selector.NewHTMLDocument(body)
	}
	if err != nil {
		return nil, fmt.Errorf("parsing response body: %w", err)
	}

	rows, err := doc.Find(p.Def.Search.Rows.Selector)
	if err != nil {
		return nil, fmt.Errorf("selecting rows: %w", err)
	}
	if p.Def.Search.Rows.After > 0 && p.Def.Search.Rows.After < len(rows) {
		rows = rows[p.Def.Search.Rows.After:]
	}

	// Apply row-level filters (e.g. andmatch) before field extraction.
	if len(p.Def.Search.Rows.Filters) > 0 {
		keywords := p.lastKeywords
		filtered := rows[:0]
		for _, row := range rows {
			text := row.Text()
			passed := true
			for _, f := range p.Def.Search.Rows.Filters {
				if f.Name == "andmatch" {
					if !filters.ApplyAndMatch(text, keywords) {
						passed = false
						break
					}
					continue
				}
				result, err := filters.Apply(text, []filters.Step{{Name: f.Name, Args: f.Args}})
				if err != nil || result == "" {
					passed = false
					break
				}
				text = result
			}
			if passed {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}

	releases := make([]release.Release, 0, len(rows))
	var extractErr error
	for _, row := range rows {
		release, err := p.extractRelease(ctx, row, rctx)
		if err != nil {
			if extractErr == nil {
				extractErr = err
			}
			continue
		}
		release.IndexerID = p.Def.ID
		release.IndexerName = p.Def.Name
		release.Protocol = p.Protocol()
		releases = append(releases, release)
	}
	if len(releases) == 0 && extractErr != nil {
		return nil, fmt.Errorf("no releases extracted (last error: %w)", extractErr)
	}
	return releases, nil
}

func (p *Provider) extractRelease(ctx context.Context, row selector.Node, rctx tmpl.Context) (release.Release, error) {
	values := make(map[string]string, len(p.Def.Search.Fields))

	// Implémentation minimale (Slice B) : les sélecteurs de champs sont
	// évalués relativement au texte/attributs du noeud de ligne lui-même.
	// Une évaluation "sous-document" par ligne (ex: re-sélection CSS
	// enfant) sera ajoutée si le corpus réel de définitions l'exige.
	for name, field := range p.Def.Search.Fields {
		var raw string
		var ok bool
		target := row

		if field.Selector != "" {
			if sub, isSub := row.(selector.SubSelectable); isSub {
				nodes, err := sub.Find(field.Selector)
				if err != nil || len(nodes) == 0 {
					if field.Optional {
						continue
					}
					return release.Release{}, fmt.Errorf("required field %q: selector %q matched nothing", name, field.Selector)
				}
				target = nodes[0]
			}
		}

		if field.Text != "" {
			raw = field.Text
			ok = true
		} else if field.Attribute != "" {
			raw, ok = target.Attr(field.Attribute)
		} else {
			raw = target.Text()
			ok = raw != ""
		}

		if !ok {
			if field.Optional {
				continue
			}
			return release.Release{}, fmt.Errorf("required field %q missing", name)
		}

		filtered, err := filters.Apply(raw, toFilterSteps(field.Filters))
		if err != nil {
			if field.Optional {
				continue
			}
			return release.Release{}, fmt.Errorf("field %q: %w", name, err)
		}
		values[name] = filtered
	}

	rel, err := fieldsToRelease(values)
	if err != nil {
		return release.Release{}, err
	}

	// Resolve download link via download block if defined
	if p.Def.Download != nil {
		client, clientErr := p.httpClient(ctx)
		if clientErr == nil {
			dlResult, dlErr := p.resolveDownload(ctx, client, row, p.BaseURL, rctx)
			if dlErr == nil && dlResult != nil {
				if dlResult.URL != "" {
					rel.DownloadLink = dlResult.URL
				}
				if dlResult.InfoHash != "" {
					rel.InfoHash = dlResult.InfoHash
				}
				if dlResult.Title != "" {
					rel.Title = dlResult.Title
				}
			}
		}
		// If resolveDownload fails, fall back to the raw field value already in rel.DownloadLink
	}

	return rel, nil
}

func toFilterSteps(steps []cmodel.FilterStep) []filters.Step {
	out := make([]filters.Step, len(steps))
	for i, s := range steps {
		out[i] = filters.Step{Name: s.Name, Args: s.Args}
	}
	return out
}

func fieldsToRelease(values map[string]string) (release.Release, error) {
	release := release.Release{
		Title:        values["title"],
		Details:      values["details"],
		DownloadLink: values["download"],
		InfoHash:     values["infohash"],
	}
	if release.Title == "" {
		return release, fmt.Errorf("title is required")
	}
	if v, ok := values["size"]; ok {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			release.Size = parsed
		}
	}
	if v, ok := values["seeders"]; ok {
		if parsed, err := strconv.Atoi(v); err == nil {
			release.Seeders = parsed
		}
	}
	if v, ok := values["peers"]; ok {
		if parsed, err := strconv.Atoi(v); err == nil {
			release.Peers = parsed
		}
	}
	if v, ok := values["date"]; ok {
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			release.PublishDate = parsed
		}
	}
	return release, nil
}

// downloadResult holds the resolved download information.
type downloadResult struct {
	URL      string
	InfoHash string
	Title    string
}

// resolveDownload résout le lien réel de téléchargement à partir de la définition
// download de la ligne. Si aucun download n'est défini, retourne nil.
func (p *Provider) resolveDownload(ctx context.Context, client interface {
	Do(*http.Request) (*http.Response, error)
}, row selector.Node, baseURL string, rctx tmpl.Context) (*downloadResult, error) {
	dl := p.Def.Download
	if dl == nil {
		return nil, nil
	}

	var beforeResponse string
	var downloadURL string

	if dl.Before != nil {
		var beforeURL string
		if dl.Before.PathSelector != nil {
			extracted, err := extractSelectorField(row, dl.Before.PathSelector)
			if err != nil {
				return nil, fmt.Errorf("before pathselector: %w", err)
			}
			if strings.HasPrefix(extracted, "http://") || strings.HasPrefix(extracted, "https://") {
				beforeURL = extracted
			} else {
				beforeURL = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(extracted, "/")
			}
		} else if dl.Before.Path != "" {
			rendered, err := tmpl.Render(dl.Before.Path, rctx)
			if err != nil {
				return nil, fmt.Errorf("rendering before path: %w", err)
			}
			if strings.HasPrefix(rendered, "http://") || strings.HasPrefix(rendered, "https://") {
				beforeURL = rendered
			} else {
				beforeURL = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(rendered, "/")
			}
		}

		if beforeURL != "" {
			method := strings.ToUpper(dl.Before.Method)
			if method == "" {
				method = http.MethodGet
			}

			var req *http.Request
			if method == http.MethodPost {
				values := url.Values{}
				for k, v := range dl.Before.Inputs {
					rendered, err := tmpl.Render(v, rctx)
					if err != nil {
						return nil, fmt.Errorf("rendering before input %q: %w", k, err)
					}
					values.Set(k, rendered)
				}
				var err error
				req, err = http.NewRequestWithContext(ctx, method, beforeURL, strings.NewReader(values.Encode()))
				if err != nil {
					return nil, fmt.Errorf("creating before request: %w", err)
				}
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			} else {
				u, err := url.Parse(beforeURL)
				if err != nil {
					return nil, fmt.Errorf("invalid before url %q: %w", beforeURL, err)
				}
				values := u.Query()
				for k, v := range dl.Before.Inputs {
					rendered, err := tmpl.Render(v, rctx)
					if err != nil {
						return nil, fmt.Errorf("rendering before input %q: %w", k, err)
					}
					values.Set(k, rendered)
				}
				u.RawQuery = values.Encode()
				req, err = http.NewRequestWithContext(ctx, method, u.String(), nil)
				if err != nil {
					return nil, fmt.Errorf("creating before request: %w", err)
				}
			}

			if dl.Headers != nil {
				for header, vals := range dl.Headers {
					for _, v := range vals {
						req.Header.Add(header, v)
					}
				}
			}

			resp, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("before request: %w", err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
			if err != nil {
				return nil, fmt.Errorf("reading before response: %w", err)
			}
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("before request status %d", resp.StatusCode)
			}
			beforeResponse = string(body)
		}
	}

	// Extract download URL from selectors
	if len(dl.Selectors) > 0 {
		for _, sel := range dl.Selectors {
			if sel.UseBeforeResponse && beforeResponse != "" {
				doc, err := selector.NewHTMLDocument(beforeResponse)
				if err != nil {
					return nil, fmt.Errorf("parsing before response: %w", err)
				}
				nodes, err := doc.Find(sel.Selector)
				if err != nil || len(nodes) == 0 {
					return nil, fmt.Errorf("before response selector %q matched nothing", sel.Selector)
				}
				target := nodes[0]
				if sel.Attribute != "" {
					raw, ok := target.Attr(sel.Attribute)
					if !ok {
						return nil, fmt.Errorf("before response attribute %q not found", sel.Attribute)
					}
					downloadURL = raw
				} else {
					downloadURL = target.Text()
				}
			} else {
				extracted, err := extractSelectorFromNode(row, sel.Selector, sel.Attribute)
				if err != nil {
					return nil, fmt.Errorf("selector %q: %w", sel.Selector, err)
				}
				downloadURL = extracted
			}

			if len(sel.Filters) > 0 {
				filtered, err := filters.Apply(downloadURL, toFilterSteps(sel.Filters))
				if err != nil {
					return nil, fmt.Errorf("applying filters to selector: %w", err)
				}
				downloadURL = filtered
			}

			if downloadURL != "" {
				break
			}
		}
	} else if beforeResponse != "" {
		doc, err := selector.NewHTMLDocument(beforeResponse)
		if err == nil {
			for _, sel := range []string{"a[href*='download']", "a[href*='torrent']", "a[href*='.torrent']"} {
				nodes, err := doc.Find(sel)
				if err == nil && len(nodes) > 0 {
					if href, ok := nodes[0].Attr("href"); ok {
						downloadURL = href
						break
					}
				}
			}
		}
	}

	result := &downloadResult{URL: downloadURL}

	// Handle infohash extraction
	if dl.Infohash != nil {
		var infohashSource selector.Node
		if dl.Infohash.UseBeforeResponse && beforeResponse != "" {
			doc, err := selector.NewHTMLDocument(beforeResponse)
			if err != nil {
				return nil, fmt.Errorf("parsing before response for infohash: %w", err)
			}
			nodes, err := doc.Find(dl.Infohash.Hash.Selector)
			if err != nil || len(nodes) == 0 {
				return nil, fmt.Errorf("infohash hash selector %q matched nothing", dl.Infohash.Hash.Selector)
			}
			infohashSource = nodes[0]
		} else {
			infohashSource = row
		}

		hash, err := extractSelectorField(infohashSource, &dl.Infohash.Hash)
		if err != nil {
			return nil, fmt.Errorf("infohash hash: %w", err)
		}
		result.InfoHash = hash

		if dl.Infohash.Title.Selector != "" {
			title, err := extractSelectorField(infohashSource, &dl.Infohash.Title)
			if err == nil && title != "" {
				result.Title = title
			}
		}
	}

	// If no URL found, check if the before-response is a redirect URL
	if downloadURL == "" && beforeResponse != "" {
		trimmed := strings.TrimSpace(beforeResponse)
		if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
			result.URL = trimmed
		}
	}

	return result, nil
}

// extractSelectorField extrait une valeur d'un noeud en utilisant un SelectorField.
func extractSelectorField(node selector.Node, sf *cmodel.SelectorField) (string, error) {
	if sf == nil {
		return "", nil
	}

	if sf.Text != "" {
		raw := sf.Text
		filtered, err := filters.Apply(raw, toFilterSteps(sf.Filters))
		if err != nil {
			return "", fmt.Errorf("applying filters: %w", err)
		}
		return filtered, nil
	}

	if sf.Selector == "" {
		return "", nil
	}

	sub, isSub := node.(selector.SubSelectable)
	if !isSub {
		return "", fmt.Errorf("node does not support Find")
	}

	nodes, err := sub.Find(sf.Selector)
	if err != nil {
		return "", fmt.Errorf("selector %q: %w", sf.Selector, err)
	}
	if len(nodes) == 0 {
		if sf.Optional {
			return "", nil
		}
		return "", fmt.Errorf("selector %q matched nothing", sf.Selector)
	}

	target := nodes[0]
	var raw string
	var ok bool
	if sf.Attribute != "" {
		raw, ok = target.Attr(sf.Attribute)
		if !ok {
			if sf.Optional {
				return "", nil
			}
			return "", fmt.Errorf("attribute %q not found", sf.Attribute)
		}
	} else {
		raw = target.Text()
	}

	filtered, err := filters.Apply(raw, toFilterSteps(sf.Filters))
	if err != nil {
		return "", fmt.Errorf("applying filters: %w", err)
	}
	return filtered, nil
}

// extractSelectorFromNode extrait une valeur d'un noeud en utilisant un sélecteur et un attribut.
func extractSelectorFromNode(node selector.Node, selectorStr, attribute string) (string, error) {
	sub, isSub := node.(selector.SubSelectable)
	if !isSub {
		return "", fmt.Errorf("node does not support Find")
	}

	nodes, err := sub.Find(selectorStr)
	if err != nil {
		return "", fmt.Errorf("selector %q: %w", selectorStr, err)
	}
	if len(nodes) == 0 {
		return "", fmt.Errorf("selector %q matched nothing", selectorStr)
	}

	target := nodes[0]
	if attribute != "" {
		raw, ok := target.Attr(attribute)
		if !ok {
			return "", fmt.Errorf("attribute %q not found", attribute)
		}
		return raw, nil
	}
	return target.Text(), nil
}
