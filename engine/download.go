package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/internal/httputil"
	"github.com/Kcchouette/cardigann-go/release"
	"github.com/Kcchouette/cardigann-go/selector"
	tmpl "github.com/Kcchouette/cardigann-go/template"
	"github.com/Kcchouette/cardigann-go/template/filters"
)

// downloadResult holds the resolved download information.
type downloadResult struct {
	URL      string
	InfoHash string
	Title    string
	// Unlocked reports whether the link was resolved through a chained
	// download.before sequence (e.g. a "thanks" POST unlocking hidden content).
	Unlocked bool
}

// maxBeforeSteps caps the number of chained download.before steps.
const maxBeforeSteps = 5

// resolveDownload resolves the actual download link from the row's download
// definition. If no download is defined, returns nil.
//
// The before block is a chain of intermediate requests: step 0 resolves its
// pathselector against the row, later steps against the previous step's
// response body. The chain soft-fails without error: a failed or missing step
// stops the chain and extraction proceeds with the last available response,
// so the row always survives (e.g. a one-shot "thanks" token already consumed).
//
// startURL, when non-empty, seeds the chain's first request (step 0 is then
// skipped): used at download time, starting from a release's stored link.
// row may be nil in that case.
func (p *Provider) resolveDownload(ctx context.Context, client interface {
	Do(*http.Request) (*http.Response, error)
}, row selector.Node, baseURL string, rctx tmpl.Context, startURL string) (*downloadResult, error) {
	dl := p.def.Download
	if dl == nil {
		return nil, nil
	}

	var beforeResponse string
	var downloadURL string
	chain := dl.Before
	if len(chain) > maxBeforeSteps {
		chain = chain[:maxBeforeSteps]
	}

	// Seed DownloadUri with the row's original download link (Prowlarr
	// semantics): before-chain templates reference the row's download URL,
	// not an intermediate step's URL. All 12 corpus usages are at step 0.
	// At download time the stored release link seeds it instead.
	if startURL != "" {
		if u, err := url.Parse(httputil.ResolveURL(baseURL, startURL)); err == nil {
			rctx.DownloadUri = tmpl.NewDownloadURL(u)
		}
	} else if raw, ok := rctx.Result["download"]; ok && raw != "" {
		if u, err := url.Parse(httputil.ResolveURL(baseURL, raw)); err == nil {
			rctx.DownloadUri = tmpl.NewDownloadURL(u)
		}
	}

	for i, step := range chain {
		// onlyiflocked steps (e.g. posting a reply to unlock hidden content)
		// are skipped when the download selectors already match the previous
		// response: the link is already present, a side-effecting action must
		// not run again.
		if step.OnlyIfLocked && len(dl.Selectors) > 0 && beforeSelectorsMatch(dl.Selectors, beforeResponse) {
			break
		}
		var stepURL string
		if i == 0 && startURL != "" {
			stepURL = httputil.ResolveURL(baseURL, startURL)
		} else {
			var err error
			stepURL, err = resolveBeforeURL(row, baseURL, rctx, i, step, beforeResponse)
			if err != nil {
				return nil, fmt.Errorf("before step %d: %w", i, err)
			}
			if stepURL == "" {
				// Nothing to request (missing selector match): stop the chain.
				break
			}
		}
		// Space every chain request with the plain client (the authenticated
		// IndexerClient spaces requests internally via its RoundTripper).
		if p.client == nil && p.limiter != nil {
			if err := p.limiter.Wait(ctx); err != nil {
				break
			}
		}

		// Execute the step. onlyiflocked steps (e.g. posting an unlock reply)
		// are retried while the site rejects the POST (generic div.error "form
		// invalid") or fails transiently (5xx/network): a rejected POST is not
		// applied, so re-POSTing never duplicates the action. Any other
		// response (including a reveal) ends the retries.
		maxAttempts := 1
		if step.OnlyIfLocked {
			maxAttempts = 3
		}
		var body string
		var lastErr error
		for attempt := 0; attempt < maxAttempts; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(attempt) * 750 * time.Millisecond):
				}
			}
			body, lastErr = p.beforeRequest(ctx, client, step, stepURL, rctx, dl.Headers, beforeResponse)
			if lastErr == nil && !responseHasErrorDiv(body) {
				break
			}
		}
		if lastErr != nil {
			// Soft-fail: keep the last response and the row's raw values.
			break
		}
		beforeResponse = body
		// Expose the resolved step URL to templates (kept alongside the
		// initial seed: the fork's chained tests read .DownloadUri.Path of
		// the previous step).
		if u, parseErr := url.Parse(stepURL); parseErr == nil {
			rctx.DownloadUri = tmpl.NewDownloadURL(u)
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
			} else if row != nil {
				extracted, err := extractSelectorFromNode(row, sel.Selector, sel.Attribute)
				if err != nil {
					return nil, fmt.Errorf("selector %q: %w", sel.Selector, err)
				}
				downloadURL = extracted
			}
			// row == nil (download-time resolution) with a non-before
			// selector: nothing to extract here, try the next selector.

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
	// With a chained before sequence, the release counts as unlocked when the
	// link was resolved from the chain's last response.
	if len(chain) > 0 {
		result.Unlocked = downloadURL != "" && beforeResponse != ""
	}

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

// responseHasErrorDiv reports whether the response body contains a
// div.error element (phpBB renders "form invalid" rejections that way):
// used to retry an onlyiflocked unlock step whose POST was rejected.
func responseHasErrorDiv(body string) bool {
	doc, err := selector.NewHTMLDocument(body)
	if err != nil {
		return false
	}
	nodes, err := doc.Find("div.error")
	return err == nil && len(nodes) > 0
}

// beforeSelectorsMatch reports whether any usebeforeresponse download
// selector already matches the given response body: used to stop the before
// chain early when the link is already present (e.g. the hidden content was
// already revealed, so the side-effecting step must not run again).
func beforeSelectorsMatch(selectors []cmodel.DownloadSelector, body string) bool {
	doc, err := selector.NewHTMLDocument(body)
	if err != nil {
		return false
	}
	for _, sel := range selectors {
		if !sel.UseBeforeResponse {
			continue
		}
		if nodes, err := doc.Find(sel.Selector); err == nil && len(nodes) > 0 {
			return true
		}
	}
	return false
}

// ResolveDownloadLink resolves the download.before chain for a release,
// starting from the release's stored DownloadLink (the row's download field).
// It is the counterpart of SkipSearchDownload: for indexers that opt out of
// search-time resolution, the chain — which may perform a side-effecting
// action such as posting a "thanks" to unlock hidden content — runs only
// here, when the user explicitly requests the links.
func (p *Provider) ResolveDownloadLink(ctx context.Context, rel release.Release) (release.Release, error) {
	if p.def.Download == nil || rel.DownloadLink == "" {
		return rel, nil
	}
	client, err := p.httpClient(ctx)
	if err != nil {
		return rel, err
	}
	rctx := tmpl.Context{Config: p.config}
	res, err := p.resolveDownload(ctx, client, nil, p.baseURL, rctx, rel.DownloadLink)
	if err != nil {
		return rel, err
	}
	if res == nil {
		return rel, nil
	}
	if res.URL != "" {
		rel.DownloadLink = res.URL
	}
	if res.InfoHash != "" {
		rel.InfoHash = res.InfoHash
	}
	if res.Title != "" {
		rel.Title = res.Title
	}
	if res.Unlocked {
		rel.Unlocked = true
	}
	rel.Hosts = DetectHosts(rel.DownloadLink)
	return rel, nil
}

// resolveBeforeURL computes the URL of a chain step. Step 0 resolves its
// pathselector against the row; later steps against the previous response
// body (parsed as HTML). A literal path is rendered as a template with the
// row context. An empty result means "nothing to request": the chain stops.
func resolveBeforeURL(row selector.Node, baseURL string, rctx tmpl.Context, i int, step cmodel.DownloadBefore, beforeResponse string) (string, error) {
	if step.PathSelector != nil {
		var extracted string
		var err error
		if i == 0 {
			extracted, err = extractSelectorField(row, step.PathSelector)
			if err != nil {
				return "", nil
			}
		} else {
			extracted, err = extractFromResponse(beforeResponse, step.PathSelector)
			if err != nil {
				return "", err
			}
		}
		if extracted == "" {
			return "", nil
		}
		return httputil.ResolveURL(baseURL, extracted), nil
	}
	if step.Path != "" {
		rendered, err := tmpl.Render(step.Path, rctx)
		if err != nil {
			return "", fmt.Errorf("rendering before path: %w", err)
		}
		return httputil.ResolveURL(baseURL, rendered), nil
	}
	return "", nil
}

// extractFromResponse extracts a SelectorField value from a previous chain
// step's response body. A missing match is a soft result (empty string), so
// the chain can stop gracefully instead of failing the row.
func extractFromResponse(beforeResponse string, sf *cmodel.SelectorField) (string, error) {
	if sf == nil || sf.Selector == "" {
		return "", nil
	}
	if sf.Text != "" {
		if len(sf.Filters) > 0 {
			return filters.Apply(sf.Text, toFilterSteps(sf.Filters))
		}
		return sf.Text, nil
	}

	doc, err := selector.NewHTMLDocument(beforeResponse)
	if err != nil {
		return "", fmt.Errorf("parsing before response: %w", err)
	}
	nodes, err := doc.Find(sf.Selector)
	if err != nil || len(nodes) == 0 {
		return "", nil
	}

	target := nodes[0]
	var raw string
	var ok bool
	if sf.Attribute != "" {
		raw, ok = target.Attr(sf.Attribute)
		if !ok {
			return "", nil
		}
	} else {
		raw = target.Text()
	}
	if len(sf.Filters) > 0 {
		return filters.Apply(raw, toFilterSteps(sf.Filters))
	}
	return raw, nil
}

// beforeRequest performs one chain step: a GET or POST request to stepURL,
// applying the rendered inputs, the definition headers and a 16 MiB cap.
// beforeResponse is the previous step's body: step selectorinputs are
// extracted from it (e.g. CSRF tokens of a form to submit).
func (p *Provider) beforeRequest(ctx context.Context, client interface {
	Do(*http.Request) (*http.Response, error)
}, step cmodel.DownloadBefore, stepURL string, rctx tmpl.Context, headers map[string][]string, beforeResponse string) (string, error) {
	method := strings.ToUpper(step.Method)
	if method == "" {
		method = http.MethodGet
	}

	// Render inputs (without empty/"false" value filtering)
	renderedInputs, _, err := renderInputs(step.Inputs, rctx)
	if err != nil {
		return "", err
	}
	// Step selectorinputs: extract hidden field values from the previous
	// response and merge (they override static inputs on key collision).
	for name, sf := range step.SelectorInputs {
		if beforeResponse == "" {
			continue
		}
		v, err := extractFromResponse(beforeResponse, &sf)
		if err != nil || v == "" {
			continue
		}
		renderedInputs[name] = v
	}

	var req *http.Request
	if method == http.MethodPost {
		values := url.Values{}
		for k, v := range renderedInputs {
			values.Set(k, v)
		}
		req, err = http.NewRequestWithContext(ctx, method, stepURL, strings.NewReader(values.Encode()))
		if err != nil {
			return "", fmt.Errorf("creating before request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		u, err := url.Parse(stepURL)
		if err != nil {
			return "", fmt.Errorf("invalid before url %q: %w", stepURL, err)
		}
		values := u.Query()
		for k, v := range renderedInputs {
			values.Set(k, v)
		}
		u.RawQuery = values.Encode()
		req, err = http.NewRequestWithContext(ctx, method, u.String(), nil)
		if err != nil {
			return "", fmt.Errorf("creating before request: %w", err)
		}
	}

	for header, vals := range headers {
		for _, v := range vals {
			req.Header.Add(header, v)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("before request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := httputil.ReadBodyLimited(resp, p.def.Encoding, 16<<20)
	if err != nil {
		return "", fmt.Errorf("reading before response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("before request status %d", resp.StatusCode)
	}
	return string(body), nil
}
