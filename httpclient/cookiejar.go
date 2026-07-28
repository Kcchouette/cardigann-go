package httpclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
)

// cookieJar enveloppe un cookiejar.Jar standard (en mémoire seulement, cf.
// limitation documentée) avec une persistance explicite vers SQLite entre
// deux invocations CLI du même indexeur.
type cookieJar struct {
	mu        sync.Mutex
	inner     *cookiejar.Jar
	indexerID string
	persister CookiePersister
	lastURLs  map[string]struct{} // URLs vues, pour ré-extraire les cookies à persister
}

// persistedCookie est la forme sérialisée d'un cookie, indépendante de l'URL
// d'origine exacte (on rejoue le cookie sur son Domain/Path déclarés).
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

// load recharge les cookies précédemment persistés pour cet indexeur (no-op
// si aucun persister n'est configuré ou si aucun cookie n'existe encore).
func (j *cookieJar) load() error {
	if j.persister == nil {
		return nil
	}
	raw, err := j.persister.LoadCookies(j.indexerID)
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
		j.inner.SetCookies(u, entry.Cookies)
	}
	return nil
}

// persist sauvegarde les cookies courants pour toutes les origines vues
// pendant la session, afin qu'un futur `cardigann-go` (nouveau
// process) puisse les recharger.
func (j *cookieJar) persist() error {
	if j.persister == nil {
		return nil
	}
	j.mu.Lock()
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
		cookies := j.inner.Cookies(u)
		if len(cookies) == 0 {
			continue
		}
		entries = append(entries, persistedCookie{URL: raw, Cookies: cookies})
	}
	if len(entries) == 0 {
		return nil
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("encoding cookies: %w", err)
	}
	return j.persister.SaveCookies(j.indexerID, string(data))
}
