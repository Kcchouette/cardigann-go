// Package login implémente le moteur d'authentification Cardigann :
// méthodes `form`, `cookie`, `get` et `oneurl` supportées. Le résultat
// d'un login réussi est simplement les cookies posés sur le *http.Client
// fourni (généralement un httpclient.IndexerClient avec persistance SQLite
// en amont).
package login

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/selector"
	tmpl "github.com/Kcchouette/cardigann-go/template"
	"github.com/Kcchouette/cardigann-go/template/filters"
)

// ErrLoginFailed est retourné quand l'indexeur signale explicitement un échec
// (via un sélecteur `login.error`) ou que la vérification post-login échoue.
type ErrLoginFailed struct {
	Reason string
}

func (e *ErrLoginFailed) Error() string { return fmt.Sprintf("login failed: %s", e.Reason) }

// Login authentifie client sur l'indexeur décrit par def, selon def.Login.Method.
// baseURL est l'URL de base de l'indexeur (un des def.Links), config contient
// les valeurs saisies par l'utilisateur pour les `settings` (identifiants...).
func Login(ctx context.Context, client *http.Client, def cmodel.IndexerDefinition, baseURL string, config map[string]string) error {
	switch def.Login.Method {
	case "", "none":
		return nil
	case "cookie":
		return loginCookie(client, baseURL, config)
	case "form":
		return loginForm(ctx, client, def, baseURL, config)
	case "get":
		return loginGet(ctx, client, def, baseURL, config)
	case "oneurl":
		return loginOneURL(ctx, client, def, baseURL, config)
	default:
		return fmt.Errorf("login method %q not supported yet", def.Login.Method)
	}
}

// loginCookie pose directement la valeur de cookie fournie par l'utilisateur
// (config["cookie"], format "name1=value1; name2=value2") sur baseURL.
func loginCookie(client *http.Client, baseURL string, config map[string]string) error {
	raw := config["cookie"]
	if raw == "" {
		return &ErrLoginFailed{Reason: "cookie method requires a 'cookie' setting value"}
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid base url %q: %w", baseURL, err)
	}

	jar := client.Jar
	if jar == nil {
		jar, err = cookiejar.New(nil)
		if err != nil {
			return fmt.Errorf("creating cookie jar: %w", err)
		}
		client.Jar = jar
	}

	req := &http.Request{Header: http.Header{"Cookie": {raw}}}
	cookies := req.Cookies()
	jar.SetCookies(u, cookies)
	return nil
}

// loginForm exécute un login par formulaire HTML : POST des inputs déclarés
// vers l'URL de login, puis vérification via login.error et/ou login.test.
// Si def.Login.Form est défini, le formulaire ciblé est sélectionné via ce
// sélecteur CSS (action URL et extraction des inputs.scopeées à ce formulaire).
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
	loginURL := resolveURL(baseURL, loginPath)

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
		loginURL = resolveURL(baseURL, extracted.FormAction)
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
	loginURL := resolveURL(baseURL, loginPath)

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

// loginGet effectue un login par requête GET : les inputs déclarés sont envoyés
// en paramètres d'URL (query string), puis vérification via login.error et/ou login.test.
func loginGet(ctx context.Context, client *http.Client, def cmodel.IndexerDefinition, baseURL string, config map[string]string) error {
	rctx := tmpl.Context{Config: config}

	loginPath, err := tmpl.Render(def.Login.Login, rctx)
	if err != nil {
		return fmt.Errorf("rendering login path: %w", err)
	}
	loginURL := resolveURL(baseURL, loginPath)

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

// loginOneURL effectue un login par une simple requête GET vers l'URL de login
// sans aucun paramètre d'entrée. Utile pour les indexeurs qui utilisent un
// cookie ou un token passé directement dans l'URL.
func loginOneURL(ctx context.Context, client *http.Client, def cmodel.IndexerDefinition, baseURL string, config map[string]string) error {
	rctx := tmpl.Context{Config: config}

	loginPath, err := tmpl.Render(def.Login.Login, rctx)
	if err != nil {
		return fmt.Errorf("rendering login path: %w", err)
	}
	loginURL := resolveURL(baseURL, loginPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return fmt.Errorf("building login request: %w", err)
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

// checkLoginErrors inspecte le corps de la réponse de login à la recherche
// des sélecteurs `login.error` : leur présence signale un échec d'authentification.
func checkLoginErrors(body string, errorSelectors []cmodel.ErrorSelector) error {
	err := selector.CheckErrors(body, errorSelectors)
	if err != nil {
		return &ErrLoginFailed{Reason: err.Error()}
	}
	return nil
}

// verifyTest effectue une requête GET sur login.test.path et vérifie la
// présence du sélecteur attendu (indicateur de session authentifiée).
func verifyTest(ctx context.Context, client *http.Client, baseURL string, test cmodel.Test) error {
	testURL := resolveURL(baseURL, test.Path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		return fmt.Errorf("building test request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("test request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
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

func resolveURL(baseURL, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}
