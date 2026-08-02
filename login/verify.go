package login

import (
	"context"
	"fmt"
	"net/http"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/internal/httputil"
	"github.com/Kcchouette/cardigann-go/selector"
	tmpl "github.com/Kcchouette/cardigann-go/template"
)

// checkLoginErrors inspects the login response body for `login.error`
// selectors: their presence signals an authentication failure.
// rctx is threaded through for template-rendered error messages (message.text).
func checkLoginErrors(body string, errorSelectors []cmodel.ErrorSelector, rctx tmpl.Context) error {
	err := selector.CheckErrors(body, errorSelectors, rctx)
	if err != nil {
		return &ErrLoginFailed{Reason: err.Error()}
	}
	return nil
}

// verifyTest sends a GET request to login.test.path and checks for the
// expected selector (indicator of an authenticated session).
func verifyTest(ctx context.Context, client *http.Client, baseURL string, def cmodel.IndexerDefinition, test cmodel.Test) error {
	testURL := httputil.ResolveURL(baseURL, test.Path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		return fmt.Errorf("building test request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("test request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := httputil.ReadBodyLimited(resp, def.Encoding, 4<<20)
	if err != nil {
		return fmt.Errorf("reading test response: %w", err)
	}

	if test.Selector == "" {
		return nil
	}
	doc, err := selector.NewHTMLDocument(string(body))
	if err != nil {
		return fmt.Errorf("parsing test response: %w", err)
	}
	nodes, err := doc.Find(test.Selector)
	if err != nil || len(nodes) == 0 {
		return &ErrLoginFailed{Reason: "post-login verification selector not found (session likely not authenticated)"}
	}
	return nil
}
