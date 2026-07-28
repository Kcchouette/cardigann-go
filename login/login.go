// Package login implémente le moteur d'authentification Cardigann :
// méthodes `form`, `cookie`, `get` et `oneurl` supportées. Le résultat
// d'un login réussi est simplement les cookies posés sur le *http.Client
// fourni (généralement un httpclient.IndexerClient avec persistance SQLite
// en amont).
package login

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
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
