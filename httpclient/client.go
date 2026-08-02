// Package httpclient provides the per-indexer HTTP client: persistent cookies
// (SQLite), rate limiting based on `requestDelay`, optional proxy
// (HTTP/SOCKS5), and retries with exponential backoff on network/5xx errors.
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

// CookiePersister is the minimal contract required to persist cookies
// for an indexer between two CLI invocations.
type CookiePersister interface {
	Load(indexerID string) (string, error) // returns "" if no cookie stored
	Store(indexerID string, data string) error
}

// Options configures the construction of an IndexerClient.
type Options struct {
	IndexerID    string
	Persister    CookiePersister // optional: no persistence if nil
	ProxyURL     string          // optional: "socks5://host:port" or "http://host:port"
	RequestDelay time.Duration   // minimum delay between two requests (0 = no limit)
	Timeout      time.Duration   // default 20s if 0
	MaxRetries   int             // default 3 if 0
}

// IndexerClient wraps an *http.Client with persistent cookies, rate
// limiting, and retries, specific to a given indexer.
type IndexerClient struct {
	indexerID  string
	httpClient *http.Client
	jar        *cookieJar
	limiter    *rate.Limiter
	maxRetries int
}

// New constructs an IndexerClient from the provided options.
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

// tunedTransport is the shared transport for the no-proxy path and the base
// for proxy paths: it keeps idle connections alive and pools them across
// requests instead of relying on http.DefaultTransport's tight per-host limit.
// Proxy: ProxyFromEnvironment preserves the default transport's env-var
// behavior (HTTP(S)_PROXY), which a bare transport would silently drop.
var tunedTransport = &http.Transport{
	Proxy:              http.ProxyFromEnvironment,
	MaxIdleConns:       100,
	MaxIdleConnsPerHost: 10,
	IdleConnTimeout:     90 * time.Second,
}

func buildTransport(proxyURL string) (http.RoundTripper, error) {
	if proxyURL == "" {
		return tunedTransport, nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url %q: %w", proxyURL, err)
	}

	switch u.Scheme {
	case "http", "https":
		tr := tunedTransport.Clone()
		tr.Proxy = http.ProxyURL(u)
		return tr, nil
	case "socks5":
		// Documented limitation (see architecture review): golang.org/x/net/proxy only
		// supports SOCKS5, not SOCKS4 — a configured SOCKS4 proxy will fail here.
		dialer, err := proxy.FromURL(u, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("building socks5 dialer: %w", err)
		}
		tr := tunedTransport.Clone()
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
		return tr, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q (only http/https/socks5)", u.Scheme)
	}
}

// Do executes the request with rate limiting and retries (exponential backoff
// on network/5xx errors only, never on 4xx).
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
		// Persists cookies after each successful exchange (e.g., login that sets a session cookie).
		if err := c.jar.persist(); err != nil {
			return resp, fmt.Errorf("persisting cookies: %w", err)
		}
		return resp, nil
	}
	return nil, fmt.Errorf("indexer %s: giving up after %d attempts: %w", c.indexerID, c.maxRetries+1, lastErr)
}

// HTTPClient returns the underlying *http.Client for direct reuse
// (e.g., by the Cardigann engine or the login module).
func (c *IndexerClient) HTTPClient() *http.Client { return c.httpClient }
