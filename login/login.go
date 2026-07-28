// Package login implémente le moteur d'authentification Cardigann pour le
// MVP : méthodes `form` et `cookie` uniquement (`get`/`oneurl` sont reportées
// en post-MVP, cf. plan). Le résultat d'un login réussi est simplement les
// cookies posés sur le *http.Client fourni (généralement un
// httpclient.IndexerClient avec persistance SQLite en amont).
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
	default:
		// get/oneurl : post-MVP (cf. plan Slice C).
		return fmt.Errorf("login method %q not supported yet (post-MVP)", def.Login.Method)
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
func loginForm(ctx context.Context, client *http.Client, def cmodel.IndexerDefinition, baseURL string, config map[string]string) error {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(values.Encode()))
	if err != nil {
		return fmt.Errorf("building login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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
	if len(errorSelectors) == 0 {
		return nil
	}
	doc, err := selector.NewHTMLDocument(body)
	if err != nil {
		return nil // corps non-HTML : on ne peut pas vérifier, on suppose un succès.
	}
	for _, es := range errorSelectors {
		nodes, err := doc.Find(es.Selector)
		if err != nil || len(nodes) == 0 {
			continue
		}
		msg := "login error detected"
		if es.Message.Selector != "" {
			if msgNodes, err := doc.Find(es.Message.Selector); err == nil && len(msgNodes) > 0 {
				msg = msgNodes[0].Text()
			}
		} else {
			msg = nodes[0].Text()
		}
		return &ErrLoginFailed{Reason: msg}
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
