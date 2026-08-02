package httpclient

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

func benchJarURLs() []*url.URL {
	raw := []string{
		"https://a.invalid/", "https://b.invalid/", "https://c.invalid/",
		"https://d.invalid/", "https://e.invalid/",
	}
	urls := make([]*url.URL, len(raw))
	for i, s := range raw {
		u, err := url.Parse(s)
		if err != nil {
			panic(err)
		}
		urls[i] = u
	}
	return urls
}

func benchCookies() []*http.Cookie {
	cookies := make([]*http.Cookie, 8)
	for i := range cookies {
		cookies[i] = &http.Cookie{Name: "c" + strconv.Itoa(i), Value: "v" + strconv.Itoa(i)}
	}
	return cookies
}

// BenchmarkCookieJar_Persist measures the dirty-state persist path (JSON
// serialization + Store). A mutation per iteration forces the full
// serialize+store; without it the generation gate would skip the write.
func BenchmarkCookieJar_Persist(b *testing.B) {
	p := newMemPersister()
	jar := newCookieJar("bench", p)
	urls := benchJarURLs()
	for _, u := range urls {
		jar.SetCookies(u, benchCookies())
	}
	if err := jar.persist(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jar.SetCookies(urls[0], []*http.Cookie{{Name: "iter", Value: strconv.Itoa(i)}})
		if err := jar.persist(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCookieJar_Load measures restoring a persisted cookie blob (JSON
// decode + SetCookies per origin) into a fresh jar.
func BenchmarkCookieJar_Load(b *testing.B) {
	seed := newMemPersister()
	jar := newCookieJar("bench", seed)
	for _, u := range benchJarURLs() {
		jar.SetCookies(u, benchCookies())
	}
	if err := jar.persist(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j := newCookieJar("bench", seed)
		if err := j.load(); err != nil {
			b.Fatal(err)
		}
	}
}
