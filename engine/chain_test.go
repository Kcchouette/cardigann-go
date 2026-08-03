package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/release"
	"github.com/Kcchouette/cardigann-go/selector"
	tmpl "github.com/Kcchouette/cardigann-go/template"
)

// thanksFixtureDef models the thanks-chain flow: GET the topic, POST the
// "thanks" link found in the topic, then extract the DDL links from the
// final response.
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

	html := `<ul><li class="row"><a class="topictitle" href="/viewtopic.php?f=1&t=7">Demo Release 01</a></li></ul>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("li.row")

	rctx := tmpl.Context{Config: map[string]string{}, Keywords: "demo", Query: map[string]string{"Keywords": "demo"}}
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

	html := `<ul><li class="row"><a class="topictitle" href="/viewtopic.php?f=1&t=8">Demo Release 02</a></li></ul>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("li.row")

	rctx := tmpl.Context{Config: map[string]string{}, Keywords: "demo", Query: map[string]string{"Keywords": "demo"}}
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
	// DownloadUri is seeded from the row's download link at chain start
	// (Prowlarr semantics) and overwritten per step; step 2 renders its path
	// from {{ .DownloadUri.Path }} proving the per-step population.
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
		Result:   map[string]string{"tid": "7", "download": "/topic/7"},
	}
	result, err := provider.resolveDownload(context.Background(), server.Client(), rows[0], server.URL, rctx, "")
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

func TestResolveDownload_SelectorInputs(t *testing.T) {
	var postedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/page" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><form><input type="hidden" name="csrf" value="tok123"/></form></body></html>`)
		case r.URL.Path == "/submit" && r.Method == http.MethodPost:
			buf := make([]byte, 4096)
			n, _ := r.Body.Read(buf)
			postedBody = string(buf[:n])
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><a class="real-dl" href="/final.torrent">DL</a></body></html>`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		ID: "selinputs", Name: "SelInputs", Language: "en", Type: "public",
		Links: []string{"http://example.invalid"},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{Path: "/page", Method: "get"},
				{Path: "/submit", Method: "post",
					Inputs: map[string]string{"static": "value"},
					SelectorInputs: map[string]cmodel.SelectorField{
						"csrf": {Selector: "input[name=csrf]", Attribute: "value"},
					}},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.real-dl", Attribute: "href", UseBeforeResponse: true},
			},
		},
	}

	provider := NewProvider(def, server.URL, nil)
	html := `<table><tr><td>row</td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	rctx := tmpl.Context{Config: map[string]string{}}

	result, err := provider.resolveDownload(context.Background(), server.Client(), rows[0], server.URL, rctx, "")
	if err != nil {
		t.Fatalf("resolveDownload: %v", err)
	}
	if result.URL != "/final.torrent" {
		t.Errorf("URL = %q, want /final.torrent", result.URL)
	}
	if !strings.Contains(postedBody, "csrf=tok123") {
		t.Errorf("POST body missing selector-extracted csrf: %q", postedBody)
	}
	if !strings.Contains(postedBody, "static=value") {
		t.Errorf("POST body missing static input: %q", postedBody)
	}
}

func TestResolveDownload_OnlyIfLocked(t *testing.T) {
	// Step 0 already reveals the link: the onlyiflocked POST step must not run.
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/page" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><a class="real-dl" href="/direct.torrent">DL</a></body></html>`)
		case r.URL.Path == "/submit" && r.Method == http.MethodPost:
			posts++
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><a class="real-dl" href="/unlocked.torrent">DL</a></body></html>`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		ID: "locktest", Name: "LockTest", Language: "en", Type: "public",
		Links: []string{"http://example.invalid"},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{Path: "/page", Method: "get"},
				{Path: "/submit", Method: "post", OnlyIfLocked: true},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.real-dl", Attribute: "href", UseBeforeResponse: true},
			},
		},
	}

	provider := NewProvider(def, server.URL, nil)
	html := `<table><tr><td>row</td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	rctx := tmpl.Context{Config: map[string]string{}}

	result, err := provider.resolveDownload(context.Background(), server.Client(), rows[0], server.URL, rctx, "")
	if err != nil {
		t.Fatalf("resolveDownload: %v", err)
	}
	if posts != 0 {
		t.Errorf("onlyiflocked step ran despite the link being present (%d posts)", posts)
	}
	if result.URL != "/direct.torrent" {
		t.Errorf("URL = %q, want /direct.torrent (from step 0)", result.URL)
	}
}

func TestResolveDownload_OnlyIfLockedRunsWhenHidden(t *testing.T) {
	// Step 0 hides the content (no matching link): the onlyiflocked POST
	// step runs and the link is extracted from its response.
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/page" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body>hidden</body></html>`)
		case r.URL.Path == "/submit" && r.Method == http.MethodPost:
			posts++
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><a class="real-dl" href="/unlocked.torrent">DL</a></body></html>`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		ID: "locktest", Name: "LockTest", Language: "en", Type: "public",
		Links: []string{"http://example.invalid"},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{Path: "/page", Method: "get"},
				{Path: "/submit", Method: "post", OnlyIfLocked: true},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.real-dl", Attribute: "href", UseBeforeResponse: true},
			},
		},
	}

	provider := NewProvider(def, server.URL, nil)
	html := `<table><tr><td>row</td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	rctx := tmpl.Context{Config: map[string]string{}}

	result, err := provider.resolveDownload(context.Background(), server.Client(), rows[0], server.URL, rctx, "")
	if err != nil {
		t.Fatalf("resolveDownload: %v", err)
	}
	if posts != 1 {
		t.Errorf("onlyiflocked step did not run while the content was hidden (%d posts)", posts)
	}
	if result.URL != "/unlocked.torrent" {
		t.Errorf("URL = %q, want /unlocked.torrent", result.URL)
	}
}

func TestResolveDownload_OnlyIfLockedRetriesOnFormError(t *testing.T) {
	// The unlock POST is intermittently rejected with a div.error ("form
	// invalid"); the chain must re-POST until the reveal succeeds.
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/page" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body>hidden</body></html>`)
		case r.URL.Path == "/submit" && r.Method == http.MethodPost:
			posts++
			w.Header().Set("Content-Type", "text/html")
			if posts == 1 {
				_, _ = fmt.Fprint(w, `<html><body><div class="error">Le formulaire envoyé n'est pas valide</div></body></html>`)
				return
			}
			_, _ = fmt.Fprint(w, `<html><body><a class="real-dl" href="/unlocked.torrent">DL</a></body></html>`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		ID: "locktest", Name: "LockTest", Language: "en", Type: "public",
		Links: []string{"http://example.invalid"},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{Path: "/page", Method: "get"},
				{Path: "/submit", Method: "post", OnlyIfLocked: true},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.real-dl", Attribute: "href", UseBeforeResponse: true},
			},
		},
	}

	provider := NewProvider(def, server.URL, nil)
	html := `<table><tr><td>row</td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	rctx := tmpl.Context{Config: map[string]string{}}

	result, err := provider.resolveDownload(context.Background(), server.Client(), rows[0], server.URL, rctx, "")
	if err != nil {
		t.Fatalf("resolveDownload: %v", err)
	}
	if posts != 2 {
		t.Errorf("expected the unlock POST to be retried once, got %d posts", posts)
	}
	if result.URL != "/unlocked.torrent" {
		t.Errorf("URL = %q, want /unlocked.torrent", result.URL)
	}
}

func TestResolveDownload_OnlyIfLockedRetriesOnTransientError(t *testing.T) {
	// The unlock POST fails transiently (5xx): the chain must re-POST until
	// it succeeds, mirroring the login-side retry.
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/page" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body>hidden</body></html>`)
		case r.URL.Path == "/submit" && r.Method == http.MethodPost:
			posts++
			if posts == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><a class="real-dl" href="/unlocked.torrent">DL</a></body></html>`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		ID: "locktest", Name: "LockTest", Language: "en", Type: "public",
		Links: []string{"http://example.invalid"},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{Path: "/page", Method: "get"},
				{Path: "/submit", Method: "post", OnlyIfLocked: true},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.real-dl", Attribute: "href", UseBeforeResponse: true},
			},
		},
	}

	provider := NewProvider(def, server.URL, nil)
	html := `<table><tr><td>row</td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	rctx := tmpl.Context{Config: map[string]string{}}

	result, err := provider.resolveDownload(context.Background(), server.Client(), rows[0], server.URL, rctx, "")
	if err != nil {
		t.Fatalf("resolveDownload: %v", err)
	}
	if posts != 2 {
		t.Errorf("expected the unlock POST to be retried after the 5xx, got %d posts", posts)
	}
	if result.URL != "/unlocked.torrent" {
		t.Errorf("URL = %q, want /unlocked.torrent", result.URL)
	}
}

func TestResolveDownload_OnlyIfLockedGivesUpAfterRetries(t *testing.T) {
	// The unlock POST is persistently rejected: after the retries are
	// exhausted the chain soft-fails and the extraction reports the missing
	// link instead of looping forever.
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/page" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body>hidden</body></html>`)
		case r.URL.Path == "/submit" && r.Method == http.MethodPost:
			posts++
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><div class="error">Le formulaire envoyé n'est pas valide</div></body></html>`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		ID: "locktest", Name: "LockTest", Language: "en", Type: "public",
		Links: []string{"http://example.invalid"},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{Path: "/page", Method: "get"},
				{Path: "/submit", Method: "post", OnlyIfLocked: true},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.real-dl", Attribute: "href", UseBeforeResponse: true},
			},
		},
	}

	provider := NewProvider(def, server.URL, nil)
	html := `<table><tr><td>row</td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	rctx := tmpl.Context{Config: map[string]string{}}

	_, err := provider.resolveDownload(context.Background(), server.Client(), rows[0], server.URL, rctx, "")
	if err == nil {
		t.Fatal("expected an error when the unlock never succeeds")
	}
	if posts != 3 {
		t.Errorf("expected exactly 3 unlock POST attempts, got %d", posts)
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

// thanksChainServer serves the thanks-chain flow: search results page, the
// topic page carrying the thanks link, and the thanks POST revealing the DDL
// links. Counters let tests assert which requests actually happened.
func thanksChainServer(t *testing.T) (srv *httptest.Server, topicGets, thanksPosts *int) {
	t.Helper()
	var tg, tp int
	topicGets, thanksPosts = &tg, &tp
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search":
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><ul class="topiclist topics">
				<li class="row bg1"><a class="topictitle" href="/viewtopic.php?f=1&t=7">Demo Release 01</a></li>
			</ul></body></html>`)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/viewtopic.php"):
			tg++
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><div class="postbody">
				<a id="lnk_thanks_post42" href="/viewtopic.php?f=1&p=42&thanks=42">Merci</a>
			</div></body></html>`)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/viewtopic.php"):
			tp++
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><div class="postbody">
				<a href="https://www.uptobox.com/abc123">Uptobox</a>
			</div></body></html>`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	return srv, topicGets, thanksPosts
}

func TestSearch_SkipsSearchDownload(t *testing.T) {
	def := thanksFixtureDef()
	def.Search.Paths = []cmodel.SearchPath{{Path: "/search", Response: cmodel.ResponseInfo{Type: "html"}}}
	def.Search.Fields["download"] = cmodel.Field{Selector: "a.topictitle", Attribute: "href"}

	server, topicGets, thanksPosts := thanksChainServer(t)
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)
	provider.SkipSearchDownload()

	releases, err := provider.Search(context.Background(), Query{Keywords: "demo"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	if *topicGets != 0 || *thanksPosts != 0 {
		t.Errorf("search must not run the download chain: topicGets=%d thanksPosts=%d", *topicGets, *thanksPosts)
	}
	// The raw row download link is kept (unresolved).
	if releases[0].DownloadLink != "/viewtopic.php?f=1&t=7" {
		t.Errorf("DownloadLink = %q, want the raw row link", releases[0].DownloadLink)
	}
	if releases[0].Unlocked {
		t.Error("release must not be marked unlocked when the chain did not run")
	}
}

func TestSearch_RunsSearchDownloadByDefault(t *testing.T) {
	def := thanksFixtureDef()
	def.Search.Paths = []cmodel.SearchPath{{Path: "/search", Response: cmodel.ResponseInfo{Type: "html"}}}
	def.Search.Fields["download"] = cmodel.Field{Selector: "a.topictitle", Attribute: "href"}

	server, topicGets, thanksPosts := thanksChainServer(t)
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	releases, err := provider.Search(context.Background(), Query{Keywords: "demo"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	if *topicGets != 1 || *thanksPosts != 1 {
		t.Errorf("default search should resolve the chain: topicGets=%d thanksPosts=%d", *topicGets, *thanksPosts)
	}
	if releases[0].DownloadLink != "https://www.uptobox.com/abc123" {
		t.Errorf("DownloadLink = %q, want the revealed link", releases[0].DownloadLink)
	}
}

func TestResolveDownloadLink_FromStoredLink(t *testing.T) {
	server, topicGets, thanksPosts := thanksChainServer(t)
	defer server.Close()

	provider := NewProvider(thanksFixtureDef(), server.URL, nil)
	rel := release.Release{Title: "Demo Release 01", DownloadLink: "/viewtopic.php?f=1&t=7"}

	resolved, err := provider.ResolveDownloadLink(context.Background(), rel)
	if err != nil {
		t.Fatalf("ResolveDownloadLink: %v", err)
	}
	if *topicGets != 1 || *thanksPosts != 1 {
		t.Errorf("expected topic GET + thanks POST, got topicGets=%d thanksPosts=%d", *topicGets, *thanksPosts)
	}
	if resolved.DownloadLink != "https://www.uptobox.com/abc123" {
		t.Errorf("DownloadLink = %q, want the revealed link", resolved.DownloadLink)
	}
	if !resolved.Unlocked {
		t.Error("expected Unlocked=true after the thanks chain")
	}
}
