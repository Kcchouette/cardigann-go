// Package login implements the Cardigann authentication engine:
// supported methods are `form`, `cookie`, `get`, and `oneurl`. The result
// of a successful login is simply the cookies set on the provided *http.Client
// (typically an httpclient.IndexerClient with SQLite persistence upstream).
package login

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
)

// ErrLoginFailed is returned when the indexer explicitly signals a failure
// (via a `login.error` selector) or when post-login verification fails.
type ErrLoginFailed struct {
	Reason string
}

func (e *ErrLoginFailed) Error() string { return fmt.Sprintf("login failed: %s", e.Reason) }

// Login authenticates client against the indexer described by def, using def.Login.Method.
// baseURL is the base URL of the indexer (one of def.Links), config contains
// the user-entered values for the `settings` (credentials, etc.).
func Login(ctx context.Context, client *http.Client, def cmodel.IndexerDefinition, baseURL string, config map[string]string) error {
	// Static definition-level cookies (login.cookies) apply to every method.
	if len(def.Login.Cookies) > 0 {
		if err := applyStaticCookies(client, baseURL, def.Login.Cookies); err != nil {
			return err
		}
	}
	// Reuse an existing authenticated session: when the definition's login
	// test already passes (e.g. session cookies persisted by a previous run),
	// skip login entirely. Some sites (phpBB) redirect the login page away
	// when already logged in, so the login form would not be found there.
	if def.Login.Test != nil && def.Login.Test.Path != "" && def.Login.Test.Selector != "" {
		if err := verifyTest(ctx, client, baseURL, def, *def.Login.Test); err == nil {
			return nil
		}
	}
	switch def.Login.Method {
	case "", "none":
		return nil
	case "cookie":
		return loginCookie(client, baseURL, config)
	case "form":
		return retryFormLogin(ctx, client, def, baseURL, config)
	case "post":
		return loginPost(ctx, client, def, baseURL, config)
	case "get":
		return loginGet(ctx, client, def, baseURL, config)
	case "oneurl":
		return loginOneURL(ctx, client, def, baseURL, config)
	default:
		return fmt.Errorf("login method %q not supported yet", def.Login.Method)
	}
}

// retryFormLogin performs the form login with a few attempts: some sites'
// anti-bot layer intermittently rejects the login POST with a generic
// "form invalid" response despite correct CSRF tokens; a fresh extraction
// and POST succeeds shortly after. Only ErrLoginFailed (form rejected or
// post-login verification failed) is retried; hard errors pass through.
func retryFormLogin(ctx context.Context, client *http.Client, def cmodel.IndexerDefinition, baseURL string, config map[string]string) error {
	const attempts = 3
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(i) * 500 * time.Millisecond):
			}
		}
		err := loginForm(ctx, client, def, baseURL, config)
		if err == nil {
			return nil
		}
		var lf *ErrLoginFailed
		if !errors.As(err, &lf) {
			return err
		}
		lastErr = err
	}
	return lastErr
}

// jarInitMu protects the read-then-write of client.Jar in loginCookie
// (creating a default jar if none exists). Without this lock, two concurrent
// Login() calls sharing the same *http.Client (e.g., an HTTP client reused
// across multiple Providers) could race on this field. Login itself is
// infrequent (once per logical session on the Provider side), so this global
// lock has no notable performance impact.
var jarInitMu sync.Mutex

// loginCookie directly sets the cookie value provided by the user
// (config["cookie"], format "name1=value1; name2=value2") on baseURL.
func loginCookie(client *http.Client, baseURL string, config map[string]string) error {
	raw := config["cookie"]
	if raw == "" {
		return &ErrLoginFailed{Reason: "cookie method requires a 'cookie' setting value"}
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid base url %q: %w", baseURL, err)
	}

	jarInitMu.Lock()
	jar := client.Jar
	if jar == nil {
		jar, err = cookiejar.New(nil)
		if err != nil {
			jarInitMu.Unlock()
			return fmt.Errorf("creating cookie jar: %w", err)
		}
		client.Jar = jar
	}
	jarInitMu.Unlock()

	req := &http.Request{Header: http.Header{"Cookie": {raw}}}
	cookies := req.Cookies()
	jar.SetCookies(u, cookies)
	return nil
}

// applyStaticCookies injects the definition-level static cookies
// (login.cookies, format "name=value") into the client's jar for baseURL.
// Unlike loginCookie (the user-supplied cookie method), these are applied
// regardless of the login method.
func applyStaticCookies(client *http.Client, baseURL string, cookies []string) error {
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid base url %q: %w", baseURL, err)
	}

	jarInitMu.Lock()
	jar := client.Jar
	if jar == nil {
		jar, err = cookiejar.New(nil)
		if err != nil {
			jarInitMu.Unlock()
			return fmt.Errorf("creating cookie jar: %w", err)
		}
		client.Jar = jar
	}
	jarInitMu.Unlock()

	var toSet []*http.Cookie
	for _, entry := range cookies {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		toSet = append(toSet, &http.Cookie{Name: name, Value: value})
	}
	jar.SetCookies(u, toSet)
	return nil
}
