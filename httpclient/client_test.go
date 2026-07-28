package httpclient

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// memPersister est une implémentation en mémoire de CookiePersister pour les
// tests (simule ce que ferait internal/store avec SQLite entre deux process).
type memPersister struct {
	mu    sync.Mutex
	store map[string]string
}

func newMemPersister() *memPersister { return &memPersister{store: make(map[string]string)} }

func (p *memPersister) LoadCookies(indexerID string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.store[indexerID], nil
}

func (p *memPersister) SaveCookies(indexerID string, cookieJSON string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.store[indexerID] = cookieJSON
	return nil
}

func TestIndexerClient_CookiePersistenceAcrossInstances(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/set" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc123", Path: "/"})
			w.WriteHeader(http.StatusOK)
			return
		}
		// /check : renvoie le cookie reçu dans le corps de la réponse.
		cookie, err := r.Cookie("session")
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(cookie.Value))
	}))
	defer server.Close()

	persister := newMemPersister()

	// Premier "process" : login, cookie posé et persisté.
	client1, err := New(Options{IndexerID: "test-idx", Persister: persister})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req1, _ := http.NewRequest(http.MethodGet, server.URL+"/set", nil)
	if _, err := client1.Do(req1); err != nil {
		t.Fatalf("first request: %v", err)
	}

	// Deuxième "process" (nouvelle instance) : doit recharger le cookie persisté.
	client2, err := New(Options{IndexerID: "test-idx", Persister: persister})
	if err != nil {
		t.Fatalf("New (second instance): %v", err)
	}
	req2, _ := http.NewRequest(http.MethodGet, server.URL+"/check", nil)
	resp2, err := client2.Do(req2)
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	defer resp2.Body.Close()

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

	// 3 requêtes espacées d'au moins 200ms => au moins ~400ms au total.
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
	defer resp.Body.Close()
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
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	if attempts != 1 {
		t.Errorf("expected exactly 1 attempt (no retry on 4xx), got %d", attempts)
	}
}
