package httpclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
)

// cookieJar wraps a standard cookiejar.Jar (in-memory only, see documented
// limitation) with explicit persistence to SQLite between two CLI
// invocations of the same indexer.
type cookieJar struct {
	mu        sync.Mutex
	inner     *cookiejar.Jar
	indexerID string
	persister CookiePersister
	lastURLs  map[string]struct{} // seen origins, for re-extracting cookies to persist

	// generation is incremented on each mutation of cookie state
	// (SetCookies). persistedGen remembers the last generation actually
	// written via CookiePersister.Store. persist() only rewrites when
	// generation != persistedGen, avoiding an often-SQLite write
	// after each successful HTTP request when nothing has changed.
	generation   uint64
	persistedGen uint64
}

// persistedCookie is the serialized form of a cookie, independent of the exact
// source URL (the cookie is replayed on its declared Domain/Path).
type persistedCookie struct {
	URL     string         `json:"url"`
	Cookies []*http.Cookie `json:"cookies"`
}

func newCookieJar(indexerID string, persister CookiePersister) *cookieJar {
	inner, _ := cookiejar.New(nil)
	return &cookieJar{
		inner:     inner,
		indexerID: indexerID,
		persister: persister,
		lastURLs:  make(map[string]struct{}),
	}
}

func (j *cookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	j.lastURLs[originOf(u)] = struct{}{}
	j.generation++
	j.mu.Unlock()
	j.inner.SetCookies(u, cookies)
}

func (j *cookieJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	j.lastURLs[originOf(u)] = struct{}{}
	j.mu.Unlock()
	return j.inner.Cookies(u)
}

func originOf(u *url.URL) string {
	return u.Scheme + "://" + u.Host + "/"
}

// load reloads previously persisted cookies for this indexer (no-op if no
// persister is configured or if no cookie exists yet). The reloaded origins
// are also registered in lastURLs: without this, a persist() triggered after
// using only one origin would overwrite the persisted state and lose the
// restored cookies from other origins never contacted during the session.
func (j *cookieJar) load() error {
	if j.persister == nil {
		return nil
	}
	raw, err := j.persister.Load(j.indexerID)
	if err != nil {
		return err
	}
	if raw == "" {
		return nil
	}
	var entries []persistedCookie
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return fmt.Errorf("decoding persisted cookies: %w", err)
	}
	for _, entry := range entries {
		u, err := url.Parse(entry.URL)
		if err != nil {
			continue
		}
		j.mu.Lock()
		j.lastURLs[originOf(u)] = struct{}{}
		j.mu.Unlock()
		// Uses inner.SetCookies directly (not j.SetCookies): reloading an
		// already persisted state must not be considered a "dirty" mutation
		// requiring immediate re-persistence.
		j.inner.SetCookies(u, entry.Cookies)
	}
	return nil
}

// persist saves the current cookies for all origins seen during the session,
// so a future cardigann-go (new process) can reload them. Only performs a
// real write (CookiePersister.Store) if the state has changed since the last
// successful call (see generation/persistedGen), to avoid a write per HTTP
// request.
func (j *cookieJar) persist() error {
	if j.persister == nil {
		return nil
	}

	j.mu.Lock()
	genAtStart := j.generation
	if genAtStart == j.persistedGen {
		// Nothing has changed since the last successful persistence.
		j.mu.Unlock()
		return nil
	}
	urls := make([]string, 0, len(j.lastURLs))
	for u := range j.lastURLs {
		urls = append(urls, u)
	}
	j.mu.Unlock()

	entries := make([]persistedCookie, 0, len(urls))
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		// Always include the origin, even without current cookies: a deleted/
		// expired cookie must be reflected in the persisted state, otherwise it
		// would be indefinitely reloaded from an old persist().
		entries = append(entries, persistedCookie{URL: raw, Cookies: j.inner.Cookies(u)})
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("encoding cookies: %w", err)
	}
	if err := j.persister.Store(j.indexerID, string(data)); err != nil {
		return fmt.Errorf("storing cookies: %w", err)
	}

	j.mu.Lock()
	// Only marks "clean" if no new mutation occurred during the
	// serialization/write above; otherwise the next persist() will
	// detect a "dirty" state again and retry (no mutation lost on
	// concurrent write).
	if j.generation == genAtStart {
		j.persistedGen = genAtStart
	}
	j.mu.Unlock()
	return nil
}
