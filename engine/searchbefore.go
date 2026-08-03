package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/internal/httputil"
	"github.com/Kcchouette/cardigann-go/selector"
	tmpl "github.com/Kcchouette/cardigann-go/template"
	"github.com/Kcchouette/cardigann-go/template/filters"
)

// newDocument builds the selector document for the response type declared by
// a search path or a search.before step (html by default, json, xml).
func newDocument(responseType, body string) (selector.Document, error) {
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
	return doc, nil
}

// extractFromDocument extracts a SelectorField value from a whole response
// document (JSON via gjson, HTML/XML via CSS). A missing match is a soft
// result (empty string), mirroring extractFromResponse for chain steps.
func extractFromDocument(doc selector.Document, sf cmodel.SelectorField) (string, error) {
	if sf.Text != "" {
		if len(sf.Filters) > 0 {
			return filters.Apply(sf.Text, toFilterSteps(sf.Filters))
		}
		return sf.Text, nil
	}
	nodes, err := doc.Find(sf.Selector)
	if err != nil {
		return "", fmt.Errorf("selector %q: %w", sf.Selector, err)
	}
	if len(nodes) == 0 {
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

// runSearchBefore executes one search.before step: it performs the request
// (with the rendered inputs and the definition search headers) and stores
// every named selector's extracted value into rctx.Before[step.Name].
//
// captured=false reports that the request succeeded but a required selector
// matched nothing (e.g. a keyword search without results): a legitimate
// "no results" outcome, so the search returns empty without firing the
// search paths. An HTTP, auth or parsing failure returns a *searchError so
// the whole search aborts visibly (a wrong/expired token must not look like
// an empty result).
func (p *Provider) runSearchBefore(ctx context.Context, rctx *tmpl.Context, step cmodel.SearchBefore) (captured bool, err error) {
	renderedPath, err := tmpl.Render(step.Path, *rctx)
	if err != nil {
		return false, fmt.Errorf("rendering before path: %w", err)
	}
	fullURL := httputil.ResolveURL(p.baseURL, renderedPath)

	method := strings.ToUpper(step.Method)
	if method == "" {
		method = http.MethodGet
	}

	rendered, rawParams, err := renderInputs(step.Inputs, *rctx)
	if err != nil {
		return false, fmt.Errorf("rendering before inputs: %w", err)
	}
	values := filterInputs(rendered, p.def.Search.AllowEmptyInputs)
	appendRawParams(values, rawParams)

	var req *http.Request
	if method == http.MethodPost {
		req, err = http.NewRequestWithContext(ctx, method, fullURL, strings.NewReader(values.Encode()))
		if err != nil {
			return false, fmt.Errorf("building before request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		u, err := url.Parse(fullURL)
		if err != nil {
			return false, fmt.Errorf("invalid before url %q: %w", fullURL, err)
		}
		existingValues := u.Query()
		for k, v := range values {
			existingValues[k] = v
		}
		u.RawQuery = existingValues.Encode()
		req, err = http.NewRequestWithContext(ctx, method, u.String(), nil)
		if err != nil {
			return false, fmt.Errorf("building before request: %w", err)
		}
	}

	if err := applySearchHeaders(req, p.def.Search.Headers, *rctx); err != nil {
		return false, fmt.Errorf("rendering before headers: %w", err)
	}

	client, err := p.httpClient(ctx)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, &searchError{err: fmt.Errorf("before request: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, &searchError{err: fmt.Errorf("before request %s returned status %d", req.URL.String(), resp.StatusCode)}
	}
	body, err := httputil.ReadBodyLimited(resp, p.def.Encoding, 16<<20)
	if err != nil {
		return false, &searchError{err: fmt.Errorf("reading before response: %w", err)}
	}

	doc, err := newDocument(step.Response.Type, string(body))
	if err != nil {
		return false, &searchError{err: fmt.Errorf("parsing before response: %w", err)}
	}

	if rctx.Before == nil {
		rctx.Before = map[string]map[string]string{}
	}
	capturedValues := make(map[string]string, len(step.Selectors))
	for name, sf := range step.Selectors {
		v, err := extractFromDocument(doc, sf)
		if err != nil {
			return false, &searchError{err: fmt.Errorf("before selector %q: %w", name, err)}
		}
		if v == "" && !sf.Optional {
			return false, nil
		}
		capturedValues[name] = v
	}
	rctx.Before[step.Name] = capturedValues
	return true, nil
}
