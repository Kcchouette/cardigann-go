// Package httpclient fournit le client HTTP par-indexeur : cookies persistés
// (SQLite), limite de débit basée sur `requestDelay`, proxy optionnel
// (HTTP/SOCKS5), et retries avec backoff exponentiel sur erreurs réseau/5xx.
package httpclient

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
	"golang.org/x/time/rate"
)

// CookiePersister est le contrat minimal requis pour persister les cookies
// d'un indexeur entre deux invocations CLI.
type CookiePersister interface {
	Load(indexerID string) (string, error) // renvoie "" si aucun cookie stocké
	Store(indexerID string, data string) error
}

// Options configure la construction d'un IndexerClient.
type Options struct {
	IndexerID    string
	Persister    CookiePersister // optionnel : sans persistance si nil
	ProxyURL     string          // optionnel : "socks5://host:port" ou "http://host:port"
	RequestDelay time.Duration   // délai minimum entre deux requêtes (0 = pas de limite)
	Timeout      time.Duration   // défaut 20s si 0
	MaxRetries   int             // défaut 3 si 0
}

// IndexerClient encapsule un *http.Client avec cookies persistés, rate
// limiting et retries, propre à un indexeur donné.
type IndexerClient struct {
	indexerID  string
	httpClient *http.Client
	jar        *cookieJar
	limiter    *rate.Limiter
	maxRetries int
}

// New construit un IndexerClient à partir des options fournies.
func New(opts Options) (*IndexerClient, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	maxRetries := opts.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	jar := newCookieJar(opts.IndexerID, opts.Persister)
	if err := jar.load(); err != nil {
		return nil, fmt.Errorf("loading persisted cookies: %w", err)
	}

	transport, err := buildTransport(opts.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("building transport: %w", err)
	}

	var limiter *rate.Limiter
	if opts.RequestDelay > 0 {
		limiter = rate.NewLimiter(rate.Every(opts.RequestDelay), 1)
	}

	return &IndexerClient{
		indexerID: opts.IndexerID,
		httpClient: &http.Client{
			Timeout:   timeout,
			Jar:       jar,
			Transport: transport,
		},
		jar:        jar,
		limiter:    limiter,
		maxRetries: maxRetries,
	}, nil
}

func buildTransport(proxyURL string) (http.RoundTripper, error) {
	if proxyURL == "" {
		return http.DefaultTransport, nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url %q: %w", proxyURL, err)
	}

	switch u.Scheme {
	case "http", "https":
		return &http.Transport{Proxy: http.ProxyURL(u)}, nil
	case "socks5":
		// Limitation documentée (cf. revue architecture) : golang.org/x/net/proxy ne
		// supporte que SOCKS5, pas SOCKS4 — un proxy SOCKS4 configuré échouera ici.
		dialer, err := proxy.FromURL(u, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("building socks5 dialer: %w", err)
		}
		return &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q (only http/https/socks5)", u.Scheme)
	}
}

// Do exécute la requête avec rate limiting et retries (backoff exponentiel
// sur erreurs réseau/5xx uniquement, jamais sur 4xx).
func (c *IndexerClient) Do(req *http.Request) (*http.Response, error) {
	if c.limiter != nil {
		if err := c.limiter.Wait(req.Context()); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(backoff):
			}
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("http status %d", resp.StatusCode)
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			continue
		}
		// Persiste les cookies après chaque échange réussi (ex: login qui pose un cookie de session).
		if err := c.jar.persist(); err != nil {
			return resp, fmt.Errorf("persisting cookies: %w", err)
		}
		return resp, nil
	}
	return nil, fmt.Errorf("indexer %s: giving up after %d attempts: %w", c.indexerID, c.maxRetries+1, lastErr)
}

// HTTPClient retourne le *http.Client sous-jacent, pour réutilisation directe
// (ex: par le moteur Cardigann ou le module de login).
func (c *IndexerClient) HTTPClient() *http.Client { return c.httpClient }
