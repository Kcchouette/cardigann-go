package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Kcchouette/cardigann-go/internal/httputil"
	"github.com/Kcchouette/cardigann-go/selector"
	tmpl "github.com/Kcchouette/cardigann-go/template"
	"github.com/Kcchouette/cardigann-go/template/filters"
)

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
	dl := p.def.Download
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
			beforeURL = httputil.ResolveURL(baseURL, extracted)
		} else if dl.Before.Path != "" {
			rendered, err := tmpl.Render(dl.Before.Path, rctx)
			if err != nil {
				return nil, fmt.Errorf("rendering before path: %w", err)
			}
			beforeURL = httputil.ResolveURL(baseURL, rendered)
		}

		if beforeURL != "" {
			method := strings.ToUpper(dl.Before.Method)
			if method == "" {
				method = http.MethodGet
			}

			// Rendre les inputs (sans filtrage des valeurs vides/"false")
			renderedInputs, _, err := renderInputs(dl.Before.Inputs, rctx)
			if err != nil {
				return nil, err
			}

			var req *http.Request
			if method == http.MethodPost {
				values := url.Values{}
				for k, v := range renderedInputs {
					values.Set(k, v)
				}
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
				for k, v := range renderedInputs {
					values.Set(k, v)
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
