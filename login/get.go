package login

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/internal/httputil"
	tmpl "github.com/Kcchouette/cardigann-go/template"
)

// loginGet performs a login via GET request: declared inputs are sent as URL
// query parameters, then verified via login.error and/or login.test.
func loginGet(ctx context.Context, client *http.Client, def cmodel.IndexerDefinition, baseURL string, config map[string]string) error {
	rctx := tmpl.Context{Config: config}

	loginPath, err := tmpl.Render(def.Login.Path, rctx)
	if err != nil {
		return fmt.Errorf("rendering login path: %w", err)
	}
	loginURL := httputil.ResolveURL(baseURL, loginPath)

	values := url.Values{}
	for name, valueTmpl := range def.Login.Inputs {
		rendered, err := tmpl.Render(valueTmpl, rctx)
		if err != nil {
			return fmt.Errorf("rendering login input %q: %w", name, err)
		}
		values.Set(name, rendered)
	}

	reqURL := loginURL
	if len(values) > 0 {
		reqURL = loginURL + "?" + values.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("building login request: %w", err)
	}
	for k, v := range def.Login.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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

// loginOneURL performs a login via a simple GET request to the login URL
// without any input parameters. Useful for indexers that use a cookie or
// token passed directly in the URL.
func loginOneURL(ctx context.Context, client *http.Client, def cmodel.IndexerDefinition, baseURL string, config map[string]string) error {
	rctx := tmpl.Context{Config: config}

	loginPath, err := tmpl.Render(def.Login.Path, rctx)
	if err != nil {
		return fmt.Errorf("rendering login path: %w", err)
	}
	loginURL := httputil.ResolveURL(baseURL, loginPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return fmt.Errorf("building login request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
