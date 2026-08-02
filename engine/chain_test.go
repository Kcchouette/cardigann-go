package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/selector"
	tmpl "github.com/Kcchouette/cardigann-go/template"
)

// thanksFixtureDef models the japanfan flow: GET the topic, POST the "thanks"
// link found in the topic, then extract the DDL links from the final response.
func thanksFixtureDef() cmodel.IndexerDefinition {
	return cmodel.IndexerDefinition{
		ID:       "thanks-fixture",
		Name:     "Thanks Fixture",
		Language: "en",
		Type:     "ddl",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows: cmodel.Rows{Selector: "li.row"},
			Fields: map[string]cmodel.Field{
				"title":   {Selector: "a.topictitle"},
				"details": {Selector: "a.topictitle", Attribute: "href"},
			},
		},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{PathSelector: &cmodel.SelectorField{Selector: "a.topictitle", Attribute: "href"}},
				{PathSelector: &cmodel.SelectorField{Selector: "a[id^='lnk_thanks_post']", Attribute: "href"}, Method: "post"},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a[href*='uptobox.com'], a[href*='1fichier.com']", Attribute: "href", UseBeforeResponse: true},
			},
		},
	}
}

func TestExtractRelease_ThanksChain(t *testing.T) {
	var topicGets, thanksPosts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/viewtopic.php"):
			topicGets++
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><div class="postbody">
				{HIDEBB_MESSAGE_HIDDEN_DESCRIPTION}
				<a id="lnk_thanks_post42" href="/viewtopic.php?f=1&p=42&thanks=42">Merci</a>
			</div></body></html>`)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/viewtopic.php"):
			thanksPosts++
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><div class="postbody">
				<a href="https://www.uptobox.com/abc123">Uptobox</a>
				<a href="https://1fichier.com/?xyz">1fichier</a>
			</div></body></html>`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	provider := NewProvider(thanksFixtureDef(), server.URL, nil)

	html := `<ul><li class="row"><a class="topictitle" href="/viewtopic.php?f=1&t=7">Bleach 01</a></li></ul>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("li.row")

	rctx := tmpl.Context{Config: map[string]string{}, Keywords: "bleach", Query: map[string]string{"Keywords": "bleach"}}
	rel, err := provider.extractRelease(context.Background(), rows[0], rctx)
	if err != nil {
		t.Fatalf("extractRelease: %v", err)
	}
	if topicGets != 1 {
		t.Errorf("expected 1 topic GET, got %d", topicGets)
	}
	if thanksPosts != 1 {
		t.Errorf("expected 1 thanks POST, got %d", thanksPosts)
	}
	if rel.DownloadLink != "https://www.uptobox.com/abc123" {
		t.Errorf("expected uptobox link, got %q", rel.DownloadLink)
	}
	if !rel.Unlocked {
		t.Error("expected Unlocked=true after a successful thanks chain")
	}
	if len(rel.Hosts) != 1 || rel.Hosts[0] != "uptobox.com" {
		t.Errorf("expected hosts [uptobox.com], got %v", rel.Hosts)
	}
}

func TestExtractRelease_ThanksChainAlreadyUnlocked(t *testing.T) {
	// The topic already shows its links and has no thanks button: step 2
	// matches nothing -> the chain stops softly and the final selectors run
	// on the step-1 response.
	var topicGets, thanksPosts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/viewtopic.php"):
			topicGets++
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><div class="postbody">
				<a href="https://www.uptobox.com/ready">Uptobox</a>
			</div></body></html>`)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/viewtopic.php"):
			thanksPosts++
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	provider := NewProvider(thanksFixtureDef(), server.URL, nil)

	html := `<ul><li class="row"><a class="topictitle" href="/viewtopic.php?f=1&t=8">Bleach 02</a></li></ul>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("li.row")

	rctx := tmpl.Context{Config: map[string]string{}, Keywords: "bleach", Query: map[string]string{"Keywords": "bleach"}}
	rel, err := provider.extractRelease(context.Background(), rows[0], rctx)
	if err != nil {
		t.Fatalf("extractRelease: %v", err)
	}
	if topicGets != 1 {
		t.Errorf("expected 1 topic GET, got %d", topicGets)
	}
	if thanksPosts != 0 {
		t.Errorf("expected 0 thanks POST (no thanks link), got %d", thanksPosts)
	}
	if rel.DownloadLink != "https://www.uptobox.com/ready" {
		t.Errorf("expected already-visible link, got %q", rel.DownloadLink)
	}
	if !rel.Unlocked {
		t.Error("expected Unlocked=true when the link comes from a chain response")
	}
}

func TestExtractRelease_ChainSoftFailKeepsRow(t *testing.T) {
	// A 404 on the thanks POST (one-shot token already consumed) must not
	// kill the row: the raw download field value is kept.
	def := cmodel.IndexerDefinition{
		ID:       "softfail",
		Name:     "Softfail",
		Language: "en",
		Type:     "ddl",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows: cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{
				"title":    {Selector: "td.title"},
				"download": {Selector: "a.dl", Attribute: "href"},
			},
		},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{PathSelector: &cmodel.SelectorField{Selector: "a.dl", Attribute: "href"}},
				{PathSelector: &cmodel.SelectorField{Selector: "a[id^='lnk_thanks_post']", Attribute: "href"}, Method: "post"},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a[href*='uptobox.com']", Attribute: "href", UseBeforeResponse: true},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	html := `<table><tr><td class="title">Title</td><td><a class="dl" href="/raw.torrent">DL</a></td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")

	rctx := tmpl.Context{Config: map[string]string{}, Keywords: "test", Query: map[string]string{"Keywords": "test"}}
	rel, err := provider.extractRelease(context.Background(), rows[0], rctx)
	if err != nil {
		t.Fatalf("extractRelease: %v", err)
	}
	if rel.DownloadLink != "/raw.torrent" {
		t.Errorf("expected raw row link kept after soft-fail, got %q", rel.DownloadLink)
	}
	if rel.Unlocked {
		t.Error("expected Unlocked=false after a soft-failed chain")
	}
}

func TestResolveDownload_ChainPopulatesDownloadUri(t *testing.T) {
	// Step 2 renders its path from {{ .DownloadUri.Path }}, proving the
	// previously-dead DownloadUri context field is populated per chain step.
	def := cmodel.IndexerDefinition{
		ID:       "downloaduri",
		Name:     "DownloadUri",
		Language: "en",
		Type:     "public",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows:   cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{},
		},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{Path: "/topic/{{ .Result.tid }}", Method: "get"},
				{Path: "{{ .DownloadUri.Path }}/thanks", Method: "post"},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.real-dl", Attribute: "href", UseBeforeResponse: true},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/topic/7" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><a class="real-dl" href="/final.torrent">DL</a></body></html>`)
		case r.URL.Path == "/topic/7/thanks" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><a class="real-dl" href="/unlocked.torrent">DL</a></body></html>`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	html := `<table><tr><td>row</td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")

	rctx := tmpl.Context{
		Config:   map[string]string{},
		Keywords: "test",
		Query:    map[string]string{"Keywords": "test"},
		Result:   map[string]string{"tid": "7"},
	}
	result, err := provider.resolveDownload(context.Background(), server.Client(), rows[0], server.URL, rctx)
	if err != nil {
		t.Fatalf("resolveDownload: %v", err)
	}
	if result.URL != "/unlocked.torrent" {
		t.Errorf("expected '/unlocked.torrent', got %q", result.URL)
	}
	if !result.Unlocked {
		t.Error("expected Unlocked=true for a chained resolution")
	}
}

func TestDetectHosts(t *testing.T) {
	cases := []struct {
		link string
		want []string
	}{
		{"https://www.uptobox.com/abc123", []string{"uptobox.com"}},
		{"https://uptobox.com/abc123", []string{"uptobox.com"}},
		{"https://1fichier.com/?xyz", []string{"1fichier.com"}},
		{"https://drive.google.com/file/d/1/view", []string{"drive.google.com"}},
		{"https://mega.nz/file/xyz", []string{"mega.nz"}},
		{"https://www.mediafire.com/file/x", []string{"mediafire.com"}},
		{"https://UPTOBOX.com/case", []string{"uptobox.com"}},
		{"https://example.com/file", nil},
		{"", nil},
		{"not a url", nil},
	}
	for _, tc := range cases {
		if got := DetectHosts(tc.link); !equalStrings(got, tc.want) {
			t.Errorf("DetectHosts(%q) = %v, want %v", tc.link, got, tc.want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
