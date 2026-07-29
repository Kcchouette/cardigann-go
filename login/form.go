package login

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/internal/httputil"
	"github.com/Kcchouette/cardigann-go/selector"
	tmpl "github.com/Kcchouette/cardigann-go/template"
	"github.com/Kcchouette/cardigann-go/template/filters"
)

// loginForm performs a login via HTML form: POST declared inputs to the login
// URL, then verify via login.error and/or login.test. If def.Login.Form is
// set, the targeted form is selected via this CSS selector (action URL and
// extraction of inputs scoped to that form).
func loginForm(ctx context.Context, client *http.Client, def cmodel.IndexerDefinition, baseURL string, config map[string]string) error {
	rctx := tmpl.Context{Config: config}

	loginPath, err := tmpl.Render(def.Login.Login, rctx)
	if err != nil {
		return fmt.Errorf("rendering login path: %w", err)
	}
	if def.Login.SubmitPath != "" {
		loginPath, err = tmpl.Render(def.Login.SubmitPath, rctx)
		if err != nil {
			return fmt.Errorf("rendering submit path: %w", err)
		}
	}
	loginURL := httputil.ResolveURL(baseURL, loginPath)

	if len(def.Login.Cookies) > 0 && client.Jar != nil {
		u, _ := url.Parse(baseURL)
		for name, value := range def.Login.Cookies {
			client.Jar.SetCookies(u, []*http.Cookie{{Name: name, Value: value}})
		}
	}

	// Extract dynamic values from the login page (CSRF tokens, etc.)
	extracted, err := extractSelectorInputs(ctx, client, def, baseURL, rctx, def.Login.Form)
	if err != nil {
		return fmt.Errorf("extracting selector inputs: %w", err)
	}

	// If Form selector found a form action, use it as POST target
	// (SubmitPath takes precedence if explicitly set)
	if def.Login.SubmitPath == "" && extracted.FormAction != "" {
		loginPath = extracted.FormAction
		loginURL = httputil.ResolveURL(baseURL, extracted.FormAction)
	}

	values := url.Values{}
	for name, valueTmpl := range def.Login.Inputs {
		rendered, err := tmpl.Render(valueTmpl, rctx)
		if err != nil {
			return fmt.Errorf("rendering login input %q: %w", name, err)
		}
		values.Set(name, rendered)
	}
	// Merge extracted values (they override static inputs if same key)
	for k, v := range extracted.Values {
		values.Set(k, v)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(values.Encode()))
	if err != nil {
		return fmt.Errorf("building login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range def.Login.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("reading login response: %w", err)
	}

	if err := checkLoginErrors(string(body), def.Login.Error); err != nil {
		return err
	}

	if def.Login.Test != nil && def.Login.Test.Path != "" {
		return verifyTest(ctx, client, baseURL, *def.Login.Test)
	}
	return nil
}

// extractSelectorInputsResult holds the extracted values and optional form action.
type extractSelectorInputsResult struct {
	Values     map[string]string
	FormAction string
}

// extractSelectorInputs fetches the login page via GET and extracts values
// from HTML using the configured selectorinputs (CSRF tokens, hidden fields, etc.).
// When formSelector is non-empty, the extraction is scoped to the matching <form>
// element and its action attribute is returned in FormAction.
func extractSelectorInputs(ctx context.Context, client *http.Client, def cmodel.IndexerDefinition, baseURL string, rctx tmpl.Context, formSelector string) (*extractSelectorInputsResult, error) {
	result := &extractSelectorInputsResult{Values: make(map[string]string)}

	if len(def.Login.SelectorInputs) == 0 && formSelector == "" {
		return result, nil
	}

	loginPath, err := tmpl.Render(def.Login.Login, rctx)
	if err != nil {
		return nil, fmt.Errorf("rendering login path: %w", err)
	}
	loginURL := httputil.ResolveURL(baseURL, loginPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching login page: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	doc, err := selector.NewHTMLDocument(string(body))
	if err != nil {
		if formSelector != "" {
			return nil, fmt.Errorf("login form selector requires HTML response")
		}
		return result, nil // non-HTML, skip (backward compat when no form selector)
	}

	// Scope to specific form if selector provided
	var scope selector.Node
	if formSelector != "" {
		formNodes, err := doc.Find(formSelector)
		if err != nil || len(formNodes) == 0 {
			return nil, fmt.Errorf("login form selector %q matched nothing", formSelector)
		}
		scope = formNodes[0]
		if action, ok := scope.Attr("action"); ok && action != "" {
			result.FormAction = action
		}
	}

	for name, sel := range def.Login.SelectorInputs {
		var nodes []selector.Node
		if scope != nil {
			if sub, ok := scope.(selector.SubSelectable); ok {
				nodes, err = sub.Find(sel.Selector)
			}
		} else {
			nodes, err = doc.Find(sel.Selector)
		}
		if err != nil || len(nodes) == 0 {
			if sel.Optional {
				continue
			}
			continue // skip missing selectors gracefully
		}
		var raw string
		if sel.Attribute != "" {
			raw, _ = nodes[0].Attr(sel.Attribute)
		} else {
			raw = nodes[0].Text()
		}
		if sel.Text != "" {
			raw = sel.Text
		}
		if len(sel.Filters) > 0 {
			steps := make([]filters.Step, len(sel.Filters))
			for i, f := range sel.Filters {
				steps[i] = filters.Step{Name: f.Name, Args: f.Args}
			}
			raw, err = filters.Apply(raw, steps)
			if err != nil {
				continue
			}
		}
		result.Values[name] = raw
	}
	return result, nil
}
