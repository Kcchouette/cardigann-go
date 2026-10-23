package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
)

// hostMapTransport dials the test server while presenting the logical host
// (old.test initially; the redirect Location points at new.test and the next
// hop presents that host). The server handler switches on r.Host.
type hostMapTransport struct {
	dial string // test server host:port
	base http.RoundTripper
}

func (t *hostMapTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Host = req.URL.Host // logical Host header for the handler
	clone.URL.Scheme = "http"
	clone.URL.Host = t.dial
	rt := t.base
	if rt == nil {
		rt = http.DefaultTransport
	}
	resp, err := rt.RoundTrip(clone)
	if err == nil && resp != nil {
		// DefaultTransport stamps resp.Request with the rewritten clone;
		// restore the logical request so observeFinalURL sees old/new.test,
		// not the dial address.
		resp.Request = req
	}
	return resp, err
}

func rotationDef() cmodel.IndexerDefinition {
	def, err := cmodel.Parse([]byte(`
id: rotator
name: Rotator
description: domain rotation fixture
language: en-US
type: ddl
links:
  - https://old.test/
search:
  paths:
    - path: /?p=films
      response:
        type: html
    - path: /?p=series
      response:
        type: html
  rows:
    selector: div.mov
  fields:
    title:
      selector: a.t
    details:
      selector: a.t
      attribute: href
    download:
      selector: a.t
      attribute: href
download:
  selectors:
    - selector: a.dl
      attribute: href
`))
	if err != nil {
		panic(err)
	}
	return def
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

// rotationProvider wires a provider configured on http://old.test/ to the
// given test server via hostMapTransport.
func rotationProvider(t *testing.T, srv *httptest.Server) *Provider {
	t.Helper()
	p := NewProvider(rotationDef(), "http://old.test/", nil)
	p.httpCl = &http.Client{Transport: &hostMapTransport{dial: srv.Listener.Addr().String()}}
	return p
}

// redirectServer 301s every request presented with oldHost to newHost, then
// serves the search-result body. It records "host:path" per request.
func redirectServer(t *testing.T, oldHost, newHost string, status int) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Host+r.URL.Path)
		mu.Unlock()
		if r.Host == oldHost {
			http.Redirect(w, r, "http://"+newHost+r.URL.RequestURI(), status)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<div class="mov"><a class="t" href="/?p=film&id=1">Dune</a></div>`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func TestEffectiveBase_Observe301SwapsOrigin(t *testing.T) {
	srv, _ := redirectServer(t, "old.test", "new.test", http.StatusMovedPermanently)
	p := rotationProvider(t, srv)

	releases, err := p.Search(context.Background(), Query{Keywords: "dune"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Two search paths (films + series) each return one row.
	if len(releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(releases))
	}
	if eff := p.EffectiveBaseURL(); eff != "http://new.test/" {
		t.Errorf("EffectiveBaseURL = %q, want http://new.test/", eff)
	}
	if want := "http://new.test/?p=film&id=1"; releases[0].DownloadLink != want {
		t.Errorf("DownloadLink = %q, want %q", releases[0].DownloadLink, want)
	}
	if want := "http://new.test/?p=film&id=1"; releases[0].Details != want {
		t.Errorf("Details = %q, want %q", releases[0].Details, want)
	}
}

func TestEffectiveBase_Observe302And307And308(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv, _ := redirectServer(t, "old.test", "new.test", status)
			p := rotationProvider(t, srv)
			if _, err := p.Search(context.Background(), Query{Keywords: "q"}); err != nil {
				t.Fatalf("Search: %v", err)
			}
			if eff := p.EffectiveBaseURL(); eff != "http://new.test/" {
				t.Errorf("EffectiveBaseURL = %q, want http://new.test/ (status %d)", eff, status)
			}
		})
	}
}

func TestEffectiveBase_MultiPathSecondHitsNewHost(t *testing.T) {
	srv, seen := redirectServer(t, "old.test", "new.test", http.StatusMovedPermanently)
	p := rotationProvider(t, srv)

	if _, err := p.Search(context.Background(), Query{Keywords: "q"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	oldHits := 0
	for _, h := range seen() {
		if strings.HasPrefix(h, "old.test") {
			oldHits++
		}
	}
	if oldHits != 1 {
		t.Errorf("old.test hits = %d, want 1 (second path uses effective base); seen=%v", oldHits, seen())
	}
}

func TestEffectiveBase_Path2StaysOnOldLastWriterWins(t *testing.T) {
	// Path 1 rotates; path 2 is served 200 on old.test (stale). Last writer
	// would keep old — but observe on path2's same-origin 200 resets to
	// configured only when final == configured AND we treat it as redirect
	// back. Per design: same-origin 200 with no redirect is a no-op, so the
	// effective base stays new (path1's rotation wins).
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Host+r.URL.Path)
		mu.Unlock()
		if r.Host == "old.test" && r.URL.Path == "/" && r.URL.Query().Get("p") == "films" {
			http.Redirect(w, r, "http://new.test"+r.URL.RequestURI(), http.StatusMovedPermanently)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<div class="mov"><a class="t" href="/x">T</a></div>`))
	}))
	defer srv.Close()

	p := rotationProvider(t, srv)
	if _, err := p.Search(context.Background(), Query{Keywords: "q"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if eff := p.EffectiveBaseURL(); eff != "http://new.test/" {
		t.Errorf("EffectiveBaseURL = %q, want rotation kept (http://new.test/)", eff)
	}
}

func TestObserveFinalURL_ConfiguredPathPrefixPreserved(t *testing.T) {
	p := NewProvider(rotationDef(), "http://old.test/subdir/", nil)
	p.observeFinalURL(mustParse(t, "http://new.test/subdir/whatever"))
	if eff := p.EffectiveBaseURL(); eff != "http://new.test/subdir/" {
		t.Errorf("EffectiveBaseURL = %q, want configured path prefix preserved on new origin", eff)
	}
}

func TestAbsolutizeHref_RewriteRules(t *testing.T) {
	p := NewProvider(rotationDef(), "http://old.test/", nil)
	p.observeFinalURL(mustParse(t, "http://new.test/whatever"))

	cases := []struct{ name, in, want string }{
		{"relative", "/?p=film&id=1", "http://new.test/?p=film&id=1"},
		{"query-only", "?p=film", "http://new.test/?p=film"},
		{"protocol-relative", "//new.test/x", "http://new.test/x"},
		{"absolute old origin", "http://old.test/x", "http://new.test/x"},
		{"absolute new origin", "http://new.test/x", "http://new.test/x"},
		{"third party", "https://uptobox.com/dl/x", "https://uptobox.com/dl/x"},
		{"default port old", "http://old.test:80/x", "http://new.test/x"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.absolutizeHref(tc.in); got != tc.want {
				t.Errorf("absolutizeHref(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestObserveFinalURL_RedirectBackResets(t *testing.T) {
	p := NewProvider(rotationDef(), "http://old.test/", nil)
	p.observeFinalURL(mustParse(t, "http://new.test/x"))
	if p.EffectiveBaseURL() != "http://new.test/" {
		t.Fatalf("precondition: EffectiveBaseURL = %q", p.EffectiveBaseURL())
	}
	p.observeFinalURL(mustParse(t, "http://old.test/x"))
	if p.EffectiveBaseURL() != "http://old.test/" {
		t.Errorf("after redirect-back EffectiveBaseURL = %q, want configured base", p.EffectiveBaseURL())
	}
}

func TestObserveFinalURL_SameOriginAndJunkNoop(t *testing.T) {
	p := NewProvider(rotationDef(), "http://old.test/", nil)
	p.observeFinalURL(mustParse(t, "http://old.test:80/other"))
	p.observeFinalURL(nil)
	p.observeFinalURL(mustParse(t, "ftp://old.test/"))
	if p.EffectiveBaseURL() != "http://old.test/" {
		t.Errorf("noop observes changed base to %q", p.EffectiveBaseURL())
	}
}

func TestObserveFinalURL_ConcurrentSafe(t *testing.T) {
	p := NewProvider(rotationDef(), "http://old.test/", nil)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.observeFinalURL(mustParse(t, "http://new.test/x"))
			_ = p.EffectiveBaseURL()
			_ = p.absolutizeHref("/y")
		}()
	}
	wg.Wait()
}

func TestSearch_BeforeChainObservesRotation(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Host+r.URL.Path)
		mu.Unlock()
		if r.Host == "old.test" && r.URL.Path == "/fiche" {
			http.Redirect(w, r, "http://new.test/fiche", http.StatusMovedPermanently)
			return
		}
		switch r.URL.Path {
		case "/fiche":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<a class="dl" href="/dl/file.torrent">DL</a>`))
		case "/dl/file.torrent":
			w.WriteHeader(http.StatusOK)
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<div class="mov"><a class="t" href="/fiche">T</a></div>`))
		}
	}))
	defer srv.Close()

	def, err := cmodel.Parse([]byte(`
id: chainrot
name: ChainRot
description: before-chain rotation
language: en-US
type: ddl
links:
  - https://old.test/
search:
  paths:
    - path: /search
      response:
        type: html
  rows:
    selector: div.mov
  fields:
    title:
      selector: a.t
    download:
      selector: a.t
      attribute: href
download:
  before:
    - path: "{{ .DownloadUri }}"
  selectors:
    - selector: a.dl
      attribute: href
      usebeforeresponse: true
`))
	if err != nil {
		t.Fatalf("parse def: %v", err)
	}
	p := NewProvider(def, "http://old.test/", nil)
	p.httpCl = &http.Client{Transport: &hostMapTransport{dial: srv.Listener.Addr().String()}}

	releases, err := p.Search(context.Background(), Query{Keywords: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	// Search hit /search on old.test → redirect → new.test; DownloadLink is
	// already absolute on new.test. The chain starts there.
	resolved, err := p.ResolveDownloadLink(context.Background(), releases[0])
	if err != nil {
		t.Fatalf("ResolveDownloadLink: %v", err)
	}
	if !strings.HasPrefix(resolved.DownloadLink, "http://new.test/") {
		t.Errorf("resolved DownloadLink = %q, want http://new.test/ origin", resolved.DownloadLink)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(seen, ",")
	if !strings.Contains(joined, "new.test/fiche") {
		t.Errorf("expected chain to hit new.test/fiche, seen=%v", seen)
	}
}

func TestSearch_FlareSolverrStyleNoopObserve(t *testing.T) {
	// A transport that synthesizes responses with Request pointing at the
	// original request URL (like FlareSolverr's challenge path) must not
	// change the effective base: observe sees the configured origin.
	def := rotationDef()
	p := NewProvider(def, "http://old.test/", nil)
	p.httpCl = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       nopCloser{strings.NewReader(`<div class="mov"><a class="t" href="/x">T</a></div>`)},
			Request:    req, // FlareSolverr-style: original request, no redirect observed
		}, nil
	})}

	if _, err := p.Search(context.Background(), Query{Keywords: "q"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if eff := p.EffectiveBaseURL(); eff != "http://old.test/" {
		t.Errorf("EffectiveBaseURL = %q, want unchanged configured base", eff)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type nopCloser struct{ *strings.Reader }

func (nopCloser) Close() error { return nil }
