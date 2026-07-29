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
	lastURLs  map[string]struct{} // origines vues, pour ré-extraire les cookies à persister

	// generation est incrémenté à chaque mutation de l'état des cookies
	// (SetCookies). persistedGen mémorise la dernière génération réellement
	// écrite via CookiePersister.Store. persist() ne réécrit que si
	// generation != persistedGen, ce qui évite une écriture (souvent SQLite)
	// après chaque requête HTTP réussie alors que rien n'a changé.
	generation   uint64
	persistedGen uint64
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

// load recharge les cookies précédemment persistés pour cet indexeur (no-op
// si aucun persister n'est configuré ou si aucun cookie n'existe encore).
// Les origines rechargées sont aussi enregistrées dans lastURLs : sans ça,
// un persist() déclenché après usage d'une seule origine écraserait l'état
// persisté et perdrait les cookies restaurés des autres origines jamais
// recontactées durant la session.
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
		// Utilise directement inner.SetCookies (pas j.SetCookies) : recharger
		// un état déjà persisté ne doit pas être considéré comme une
		// mutation "sale" nécessitant une re-persistance immédiate.
		j.inner.SetCookies(u, entry.Cookies)
	}
	return nil
}

// persist sauvegarde les cookies courants pour toutes les origines vues
// pendant la session, afin qu'un futur `cardigann-go` (nouveau
// process) puisse les recharger. N'effectue une écriture réelle
// (CookiePersister.Store) que si l'état a changé depuis le dernier appel
// réussi (cf. generation/persistedGen), afin d'éviter une écriture par
// requête HTTP.
func (j *cookieJar) persist() error {
	if j.persister == nil {
		return nil
	}

	j.mu.Lock()
	genAtStart := j.generation
	if genAtStart == j.persistedGen {
		// Rien n'a changé depuis la dernière persistance réussie.
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
		// Toujours inclure l'origine, même sans cookie courant : un cookie
		// supprimé/expiré doit être reflété dans l'état persisté, sinon il
		// resterait indéfiniment rechargé depuis un ancien persist().
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
	// Ne marque "propre" que si aucune nouvelle mutation n'a eu lieu pendant
	// la sérialisation/l'écriture ci-dessus ; sinon la prochaine persist()
	// détectera à nouveau un état "sale" et retentera (pas de mutation
	// perdue en cas d'écriture concurrente).
	if j.generation == genAtStart {
		j.persistedGen = genAtStart
	}
	j.mu.Unlock()
	return nil
}
