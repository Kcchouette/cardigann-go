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

// memPersister est une implémentation en mémoire de CookiePersister pour les
// tests (simule ce que ferait internal/store avec SQLite entre deux process).
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

// storeCountingPersister enveloppe memPersister et compte les appels à
// Store, pour vérifier la fréquence réelle de persistance.
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

	// Premier persist sans aucune mutation de cookie : rien à écrire.
	if err := jar.persist(); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if got := persister.storeCount(); got != 0 {
		t.Fatalf("expected 0 Store call before any cookie mutation, got %d", got)
	}

	// Un premier cookie posé : une seule écriture attendue même si on
	// appelle persist() plusieurs fois de suite (simulateur de N requêtes
	// HTTP successives sans nouveau cookie).
	jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "abc"}})
	for i := 0; i < 5; i++ {
		if err := jar.persist(); err != nil {
			t.Fatalf("persist %d: %v", i, err)
		}
	}
	if got := persister.storeCount(); got != 1 {
		t.Fatalf("expected exactly 1 Store call after a single cookie mutation followed by repeated persist(), got %d", got)
	}

	// Une nouvelle mutation doit déclencher exactement une nouvelle écriture.
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

	// Suppression du cookie (MaxAge négatif = expiration immédiate) : l'état
	// persisté doit refléter sa disparition, pas garder l'ancienne valeur.
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

	// Session précédente : cookies posés pour deux origines distinctes.
	first := newCookieJar("idx", persister)
	uA, _ := url.Parse("https://tracker-a.example/")
	uB, _ := url.Parse("https://tracker-b.example/")
	first.SetCookies(uA, []*http.Cookie{{Name: "session", Value: "a-cookie", Path: "/"}})
	first.SetCookies(uB, []*http.Cookie{{Name: "session", Value: "b-cookie", Path: "/"}})
	if err := first.persist(); err != nil {
		t.Fatalf("persist: %v", err)
	}

	// Nouvelle session : recharge les cookies persistés, puis ne contacte
	// (SetCookies/Cookies) que l'origine A.
	second := newCookieJar("idx", persister)
	if err := second.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := second.inner.Cookies(uB); len(got) != 1 || got[0].Value != "b-cookie" {
		t.Fatalf("expected origin B cookie restored after load, got %v", got)
	}

	// Une mutation sur A seule doit rendre l'état "sale" et déclencher un
	// persist() qui ne doit PAS perdre les cookies de B (jamais recontactée
	// dans cette session).
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
