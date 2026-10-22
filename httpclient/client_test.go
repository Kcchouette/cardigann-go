package httpclient

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// memPersister is an in-memory implementation of CookiePersister for testing
// (simulates what internal/store would do with SQLite between two processes).
type memPersister struct {
	mu    sync.Mutex
	store map[string]string
}

func newMemPersister() *memPersister { return &memPersister{store: make(map[string]string)} }

func (p *memPersister) Load(indexerID string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.store[indexerID], nil
}

func (p *memPersister) Store(indexerID string, data string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.store[indexerID] = data
	return nil
}

// storeCountingPersister wraps memPersister and counts Store calls
// to verify the actual persistence frequency.
type storeCountingPersister struct {
	*memPersister
	mu     sync.Mutex
	stores int
}

func newStoreCountingPersister() *storeCountingPersister {
	return &storeCountingPersister{memPersister: newMemPersister()}
}

func (p *storeCountingPersister) Store(indexerID string, data string) error {
	p.mu.Lock()
	p.stores++
	p.mu.Unlock()
	return p.memPersister.Store(indexerID, data)
}

func (p *storeCountingPersister) storeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stores
}

func TestCookieJar_PersistSkippedWhenClean(t *testing.T) {
	persister := newStoreCountingPersister()
	jar := newCookieJar("idx", persister)

	u, _ := url.Parse("https://tracker.example/")

	// First persist with no cookie mutation: nothing to write.
	if err := jar.persist(); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if got := persister.storeCount(); got != 0 {
		t.Fatalf("expected 0 Store call before any cookie mutation, got %d", got)
	}

	// A first cookie set: only one write expected even if persist()
	// is called multiple times in a row (simulator of N consecutive HTTP
	// requests without new cookies).
	jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "abc"}})
	for i := 0; i < 5; i++ {
		if err := jar.persist(); err != nil {
			t.Fatalf("persist %d: %v", i, err)
		}
	}
	if got := persister.storeCount(); got != 1 {
		t.Fatalf("expected exactly 1 Store call after a single cookie mutation followed by repeated persist(), got %d", got)
	}

	// A new mutation must trigger exactly one new write.
	jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "def"}})
	if err := jar.persist(); err != nil {
		t.Fatalf("persist after mutation: %v", err)
	}
	if got := persister.storeCount(); got != 2 {
		t.Fatalf("expected 2 total Store calls after a second mutation, got %d", got)
	}
}

func TestCookieJar_DeletedCookieIsPersisted(t *testing.T) {
	persister := newMemPersister()
	jar := newCookieJar("idx", persister)
	u, _ := url.Parse("https://tracker.example/")

	jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "abc", Path: "/"}})
	if err := jar.persist(); err != nil {
		t.Fatalf("persist: %v", err)
	}
	raw, _ := persister.Load("idx")
	if raw == "" || !strings.Contains(raw, "abc") {
		t.Fatalf("expected persisted state to contain the cookie, got %q", raw)
	}

	// Cookie deletion (negative MaxAge = immediate expiration): the persisted
	// state must reflect its removal, not keep the old value.
	jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "", Path: "/", MaxAge: -1}})
	if err := jar.persist(); err != nil {
		t.Fatalf("persist after deletion: %v", err)
	}
	raw, _ = persister.Load("idx")
	if strings.Contains(raw, "abc") {
		t.Fatalf("expected deleted cookie to no longer be present in persisted state, got %q", raw)
	}
}

func TestCookieJar_LoadThenPersistKeepsOtherOrigins(t *testing.T) {
	persister := newMemPersister()

	// Previous session: cookies set for two distinct origins.
	first := newCookieJar("idx", persister)
	uA, _ := url.Parse("https://tracker-a.example/")
	uB, _ := url.Parse("https://tracker-b.example/")
	first.SetCookies(uA, []*http.Cookie{{Name: "session", Value: "a-cookie", Path: "/"}})
	first.SetCookies(uB, []*http.Cookie{{Name: "session", Value: "b-cookie", Path: "/"}})
	if err := first.persist(); err != nil {
		t.Fatalf("persist: %v", err)
	}

	// New session: reloads persisted cookies, then only contacts
	// (SetCookies/Cookies) origin A.
	second := newCookieJar("idx", persister)
	if err := second.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := second.inner.Cookies(uB); len(got) != 1 || got[0].Value != "b-cookie" {
		t.Fatalf("expected origin B cookie restored after load, got %v", got)
	}

	// A mutation on A alone must make the state "dirty" and trigger a
	// persist() that must NOT lose B's cookies (never contacted again
	// in this session).
	second.SetCookies(uA, []*http.Cookie{{Name: "session", Value: "a-cookie-updated", Path: "/"}})
	if err := second.persist(); err != nil {
		t.Fatalf("persist: %v", err)
	}

	raw, _ := persister.Load("idx")
	if !strings.Contains(raw, "b-cookie") {
		t.Fatalf("expected origin B cookies to survive persist() after load(), got %q", raw)
	}
	if !strings.Contains(raw, "a-cookie-updated") {
		t.Fatalf("expected updated origin A cookie in persisted state, got %q", raw)
	}
}

func TestIndexerClient_CookiePersistenceAcrossInstances(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/set" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc123", Path: "/"})
			w.WriteHeader(http.StatusOK)
			return
		}
		// /check: returns the received cookie in the response body.
		cookie, err := r.Cookie("session")
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(cookie.Value))
	}))
	defer server.Close()

	persister := newMemPersister()

	// First process: login, cookie set and persisted.
	client1, err := New(Options{IndexerID: "test-idx", Persister: persister})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req1, _ := http.NewRequest(http.MethodGet, server.URL+"/set", nil)
	if _, err := client1.Do(req1); err != nil {
		t.Fatalf("first request: %v", err)
	}

	// Second process (new instance): must reload the persisted cookie.
	client2, err := New(Options{IndexerID: "test-idx", Persister: persister})
	if err != nil {
		t.Fatalf("New (second instance): %v", err)
	}
	req2, _ := http.NewRequest(http.MethodGet, server.URL+"/check", nil)
	resp2, err := client2.Do(req2)
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (cookie restored), got %d", resp2.StatusCode)
	}
}

func TestIndexerClient_RequestDelayEnforced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(Options{IndexerID: "delay-idx", RequestDelay: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	start := time.Now()
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		if _, err := client.Do(req); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	elapsed := time.Since(start)

	// 3 requests spaced at least 200ms apart => at least ~400ms total.
	if elapsed < 350*time.Millisecond {
		t.Errorf("expected requestDelay to be enforced, elapsed only %v", elapsed)
	}
}

func TestIndexerClient_RetriesOn5xx(t *testing.T) {
	var attempts int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(Options{IndexerID: "retry-idx", MaxRetries: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected eventual success, got error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after retries, got %d", resp.StatusCode)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestIndexerClient_NoRetryOn4xx(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := New(Options{IndexerID: "notfound-idx"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	if attempts != 1 {
		t.Errorf("expected exactly 1 attempt (no retry on 4xx), got %d", attempts)
	}
}

func TestWrapUserAgent_SetsDefaultUA(t *testing.T) {
	var gotUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.UserAgent()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(Options{IndexerID: "ua-idx"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.HTTPClient().Transport = WrapUserAgent(client.HTTPClient().Transport, "gowlarr-test/1.0")

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	if _, err := client.Do(req); err != nil {
		t.Fatalf("request: %v", err)
	}
	if gotUA != "gowlarr-test/1.0" {
		t.Errorf("expected User-Agent %q on the wire, got %q", "gowlarr-test/1.0", gotUA)
	}
}

func TestWrapUserAgent_PreservesExistingUA(t *testing.T) {
	var gotUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.UserAgent()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(Options{IndexerID: "ua-idx"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.HTTPClient().Transport = WrapUserAgent(client.HTTPClient().Transport, "gowlarr-test/1.0")

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	req.Header.Set("User-Agent", "custom-agent/2.0")
	if _, err := client.Do(req); err != nil {
		t.Fatalf("request: %v", err)
	}
	if gotUA != "custom-agent/2.0" {
		t.Errorf("expected caller-set User-Agent %q to win, got %q", "custom-agent/2.0", gotUA)
	}
}

func TestWrapUserAgent_EmptyUAIsNoop(t *testing.T) {
	client, err := New(Options{IndexerID: "ua-idx"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	base := client.HTTPClient().Transport
	if wrapped := WrapUserAgent(base, ""); wrapped != base {
		t.Errorf("expected WrapUserAgent with empty UA to return base transport unchanged")
	}
}

// TestBuildTransport_ProxyPathsForceHTTP2 pins the fix for the shared-transport
// Clone footgun: Clone() inherits the source's post-auto-setup TLSClientConfig
// (ALPN h2) but not its TLSNextProto map, so without ForceAttemptHTTP2 the
// proxy-path transport advertises h2 via ALPN while reading HTTP/1.1 —
// h2-speaking servers then fail with "malformed HTTP response". SOCKS5 also
// installs a custom DialContext, which disables HTTP/2 unless forced.
func TestBuildTransport_ProxyPathsForceHTTP2(t *testing.T) {
	for _, tc := range []struct {
		name  string
		proxy string
	}{
		{"http", "http://127.0.0.1:3128"},
		{"https", "https://127.0.0.1:3128"},
		{"socks5", "socks5://127.0.0.1:1080"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, err := buildTransport(tc.proxy)
			if err != nil {
				t.Fatalf("buildTransport(%q): %v", tc.proxy, err)
			}
			ht, ok := tr.(*http.Transport)
			if !ok {
				t.Fatalf("expected *http.Transport, got %T", tr)
			}
			if !ht.ForceAttemptHTTP2 {
				t.Error("ForceAttemptHTTP2 = false, want true (h2/ALPN hybrid clone bug)")
			}
		})
	}
}

// TestBuildTransport_NoProxySharesTunedTransport documents the no-proxy path:
// the package-level transport is returned as-is (no Clone), so HTTP/2 is
// enabled by the stdlib default and ForceAttemptHTTP2 is already true.
func TestBuildTransport_NoProxySharesTunedTransport(t *testing.T) {
	tr, err := buildTransport("")
	if err != nil {
		t.Fatalf("buildTransport(\"\"): %v", err)
	}
	if tr != http.RoundTripper(tunedTransport) {
		t.Error("expected no-proxy path to return the shared tunedTransport")
	}
	if !tunedTransport.ForceAttemptHTTP2 {
		t.Error("tunedTransport.ForceAttemptHTTP2 = false, want true")
	}
}
