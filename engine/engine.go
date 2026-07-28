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
)

// Query représente une recherche Cardigann générique.
type Query struct {
	Keywords   string
	Categories []int
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

	loginOnce   sync.Once
	loginErr    error
	lastKeywords string
}

// NewProvider construit un Provider Cardigann prêt à l'emploi, sans gestion
// de session/login (utile pour les indexeurs publics ou les tests).
func NewProvider(def cmodel.IndexerDefinition, baseURL string, config map[string]string) *Provider {
	return &Provider{
		Def:        def,
		BaseURL:    baseURL,
		Config:     config,
		HTTPClient: &http.Client{Timeout: 20 * time.Second},
	}
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
func (p *Provider) httpClient(ctx context.Context) (interface {
	Do(*http.Request) (*http.Response, error)
}, error) {
	if p.Client == nil {
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

	rctx := tmpl.Context{
		Config:   p.Config,
		Keywords: keywords,
		Query:    map[string]string{"Keywords": keywords},
	}

	var all []release.Release
	for _, path := range p.Def.Search.Paths {
		releases, err := p.searchPath(ctx, path, rctx)
		if err != nil {
			return nil, fmt.Errorf("search path %q: %w", path.Path, err)
		}
		all = append(all, releases...)
	}
	return all, nil
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

	return p.parseResults(string(body), path.Response.Type)
}

func (p *Provider) parseResults(body string, responseType string) ([]release.Release, error) {
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
	for _, row := range rows {
		release, err := p.extractRelease(row)
		if err != nil {
			// Un champ requis manquant/erreur d'extraction ne doit pas faire
			// échouer toute la recherche : la ligne est simplement ignorée.
			continue
		}
		release.IndexerID = p.Def.ID
		release.IndexerName = p.Def.Name
		release.Protocol = p.Protocol()
		releases = append(releases, release)
	}
	return releases, nil
}

func (p *Provider) extractRelease(row selector.Node) (release.Release, error) {
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

	return fieldsToRelease(values)
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
