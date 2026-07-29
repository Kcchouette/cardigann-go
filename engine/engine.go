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

// HTTPDoer est l'abstraction pour les clients HTTP.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

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
	def     cmodel.IndexerDefinition
	baseURL string
	config  map[string]string
	httpCl  *http.Client

	// client, si renseigné, est utilisé à la place de httpCl : il fournit
	// cookies persistés (SQLite), rate limiting et retries (Slice C). Le login
	// (si def.Login.Method != "") est déclenché une seule fois, avant la
	// première requête de recherche.
	client *httpclient.IndexerClient

	loginOnce  sync.Once
	loginErr   error
	configOnce sync.Once
	limiter    *rate.Limiter
}

// Def retourne une copie de la définition de l'indexeur.
func (p *Provider) Def() cmodel.IndexerDefinition { return p.def }

// NewProvider construit un Provider Cardigann prêt à l'emploi, sans gestion
// de session/login (utile pour les indexeurs publics ou les tests).
// Si def.RequestDelay > 0, un rate limiter est créé pour espacer les requêtes.
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
	return p
}

// NewAuthenticatedProvider construit un Provider Cardigann relié à un
// httpclient.IndexerClient (cookies persistés + rate limit + retries) et
// effectuant le login défini par def.Login avant la première recherche.
func NewAuthenticatedProvider(def cmodel.IndexerDefinition, baseURL string, config map[string]string, client *httpclient.IndexerClient) *Provider {
	return &Provider{
		def:     def,
		baseURL: baseURL,
		config:  config,
		client:  client,
	}
}

func (p *Provider) ID() string   { return p.def.ID }
func (p *Provider) Name() string { return p.def.Name }

func (p *Provider) Protocol() release.Protocol {
	if p.def.IsUsenet() {
		return release.ProtocolUsenet
	}
	return release.ProtocolTorrent
}

// httpClient retourne le client HTTP effectif (IndexerClient prioritaire),
// après avoir garanti que le login (le cas échéant) a été effectué une fois.
// Si un rate limiter est configuré (via RequestDelay), la requête est espacée.
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

// Search exécute la recherche pour tous les search.paths de la définition.
//
// Sûr pour des appels concurrents sur la même instance de *Provider :
// aucun état mutable partagé n'est modifié après la première recherche
// (les valeurs de configuration par défaut ne sont appliquées qu'une seule
// fois via configOnce, et les mots-clés filtrés restent locaux à l'appel).
func (p *Provider) Search(ctx context.Context, q Query) ([]release.Release, error) {
	keywords := q.Keywords
	for _, step := range p.def.Search.KeywordsFilters {
		var err error
		keywords, err = filters.Apply(keywords, []filters.Step{{Name: step.Name, Args: step.Args}})
		if err != nil {
			return nil, fmt.Errorf("keywordsfilters: %w", err)
		}
	}

	// Map Newznab category IDs to native tracker category IDs.
	mappedCategories := mapCategories(q.Categories, p.def.Caps.CategoryMappings)

	// Populate config with default values from definition if not already set
	// Note: "false" est truthy dans les templates Go, donc on l'exclut du Config
	// pour que {{ if .Config.freeleech }} fonctionne correctement.
	// N'est fait qu'une seule fois (configOnce) : p.config est ensuite traité
	// comme une valeur en lecture seule, ce qui le rend sûr à partager entre
	// des recherches concurrentes.
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

	rctx := tmpl.Context{
		Config:     p.config,
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

	fullURL := httputil.ResolveURL(p.baseURL, renderedPath)

	method := strings.ToUpper(path.Method)
	if method == "" {
		method = http.MethodGet
	}

	// Utiliser les inputs du path, ou fallback sur les inputs du search
	inputs := path.Inputs
	if len(inputs) == 0 {
		inputs = p.def.Search.Inputs
	}

	// Rendre les inputs et séparer $raw
	rendered, rawParams, err := renderInputs(inputs, rctx)
	if err != nil {
		return nil, err
	}

	// Construire la requête avec filtrage des valeurs vides/"false"
	values := filterInputs(rendered)
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

	// Appliquer les headers de la définition (ex: Authorization: Bearer {{ .Config.apikey }})
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
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16 MiB cap
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}

	if err := selector.CheckErrors(string(body), p.def.Search.Error); err != nil {
		return nil, fmt.Errorf("search error: %w", err)
	}

	return p.parseResults(ctx, string(body), path.Response.Type, rctx, rctx.Keywords)
}
