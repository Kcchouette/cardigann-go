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

func TestResolveDownload_SimpleSelectors(t *testing.T) {
	def := cmodel.IndexerDefinition{
		ID:       "test",
		Name:     "Test",
		Language: "en",
		Type:     "public",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows:   cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{},
		},
		Download: &cmodel.Download{
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.dl", Attribute: "href"},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected HTTP request to %s", r.URL.Path)
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	html := `<table><tr><td><a class="dl" href="/download/1.torrent">DL</a></td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	row := rows[0]

	rctx := tmpl.Context{
		Config:   map[string]string{},
		Keywords: "test",
		Query:    map[string]string{"Keywords": "test"},
	}

	result, err := provider.resolveDownload(context.Background(), server.Client(), row, server.URL, rctx, "")
	if err != nil {
		t.Fatalf("resolveDownload returned error: %v", err)
	}
	if result.URL != server.URL+"/download/1.torrent" {
		t.Errorf("expected %q, got %q", server.URL+"/download/1.torrent", result.URL)
	}
}

func TestResolveDownload_BeforePostPath(t *testing.T) {
	intermediateServed := false

	def := cmodel.IndexerDefinition{
		ID:       "test",
		Name:     "Test",
		Language: "en",
		Type:     "public",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows:   cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{},
		},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{
					Path:   "/details/{{ .Result.details_id }}",
					Method: "post",
					Inputs: map[string]string{
						"action": "download",
					},
				},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.real-dl", Attribute: "href", UseBeforeResponse: true},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/details/123":
			intermediateServed = true
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><a class="real-dl" href="/real/download.torrent">Download</a></body></html>`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	html := `<table><tr><td class="details"><a href="/details/123">Info</a></td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	row := rows[0]

	rctx := tmpl.Context{
		Config:   map[string]string{},
		Keywords: "test",
		Query:    map[string]string{"Keywords": "test"},
		Result:   map[string]string{"details_id": "123"},
	}

	result, err := provider.resolveDownload(context.Background(), server.Client(), row, server.URL, rctx, "")
	if err != nil {
		t.Fatalf("resolveDownload returned error: %v", err)
	}
	if !intermediateServed {
		t.Error("intermediate page was not fetched")
	}
	if result.URL != server.URL+"/real/download.torrent" {
		t.Errorf("expected %q, got %q", server.URL+"/real/download.torrent", result.URL)
	}
}

func TestResolveDownload_BeforePathSelector(t *testing.T) {
	def := cmodel.IndexerDefinition{
		ID:       "test",
		Name:     "Test",
		Language: "en",
		Type:     "public",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows:   cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{},
		},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{
					PathSelector: &cmodel.SelectorField{
						Selector:  "a.intermediate",
						Attribute: "href",
					},
				},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.real-dl", Attribute: "href", UseBeforeResponse: true},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/intermediate/42":
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><a class="real-dl" href="/dl/final.torrent">Download</a></body></html>`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	html := `<table><tr><td><a class="intermediate" href="/intermediate/42">Go</a></td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	row := rows[0]

	rctx := tmpl.Context{
		Config:   map[string]string{},
		Keywords: "test",
		Query:    map[string]string{"Keywords": "test"},
	}

	result, err := provider.resolveDownload(context.Background(), server.Client(), row, server.URL, rctx, "")
	if err != nil {
		t.Fatalf("resolveDownload returned error: %v", err)
	}
	if result.URL != server.URL+"/dl/final.torrent" {
		t.Errorf("expected %q, got %q", server.URL+"/dl/final.torrent", result.URL)
	}
}

func TestResolveDownload_Headers(t *testing.T) {
	def := cmodel.IndexerDefinition{
		ID:       "test",
		Name:     "Test",
		Language: "en",
		Type:     "public",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows:   cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{},
		},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{
					Path:   "/page",
					Method: "get",
				},
			},
			Headers: map[string][]string{
				"X-Custom-Token": {"secret123"},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.dl", Attribute: "href", UseBeforeResponse: true},
			},
		},
	}

	var receivedHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Custom-Token")
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<html><body><a class="dl" href="/dl/file.torrent">DL</a></body></html>`)
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	html := `<table><tr><td>row</td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	row := rows[0]

	rctx := tmpl.Context{
		Config:   map[string]string{},
		Keywords: "test",
		Query:    map[string]string{"Keywords": "test"},
	}

	result, err := provider.resolveDownload(context.Background(), server.Client(), row, server.URL, rctx, "")
	if err != nil {
		t.Fatalf("resolveDownload returned error: %v", err)
	}
	if receivedHeader != "secret123" {
		t.Errorf("expected X-Custom-Token 'secret123', got %q", receivedHeader)
	}
	if result.URL != server.URL+"/dl/file.torrent" {
		t.Errorf("expected %q, got %q", server.URL+"/dl/file.torrent", result.URL)
	}
}

func TestResolveDownload_Infohash(t *testing.T) {
	def := cmodel.IndexerDefinition{
		ID:       "test",
		Name:     "Test",
		Language: "en",
		Type:     "public",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows:   cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{},
		},
		Download: &cmodel.Download{
			Infohash: &cmodel.DownloadInfohash{
				Hash: cmodel.SelectorField{
					Selector: "td.hash",
				},
				Title: cmodel.SelectorField{
					Selector: "td.title",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected HTTP request")
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	html := `<table><tr><td class="hash">abc123def456</td><td class="title">My Torrent</td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	row := rows[0]

	rctx := tmpl.Context{
		Config:   map[string]string{},
		Keywords: "test",
		Query:    map[string]string{"Keywords": "test"},
	}

	result, err := provider.resolveDownload(context.Background(), server.Client(), row, server.URL, rctx, "")
	if err != nil {
		t.Fatalf("resolveDownload returned error: %v", err)
	}
	if result.InfoHash != "abc123def456" {
		t.Errorf("expected infohash 'abc123def456', got %q", result.InfoHash)
	}
	if result.Title != "My Torrent" {
		t.Errorf("expected title 'My Torrent', got %q", result.Title)
	}
}

func TestResolveDownload_SelectorFilters(t *testing.T) {
	def := cmodel.IndexerDefinition{
		ID:       "test",
		Name:     "Test",
		Language: "en",
		Type:     "public",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows:   cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{},
		},
		Download: &cmodel.Download{
			Selectors: []cmodel.DownloadSelector{
				{
					Selector:  "a.dl",
					Attribute: "href",
					Filters: []cmodel.FilterStep{
						{Name: "replace", Args: []string{"/details/", "/download/"}},
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected HTTP request")
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	html := `<table><tr><td><a class="dl" href="/details/file.torrent">DL</a></td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	row := rows[0]

	rctx := tmpl.Context{
		Config:   map[string]string{},
		Keywords: "test",
		Query:    map[string]string{"Keywords": "test"},
	}

	result, err := provider.resolveDownload(context.Background(), server.Client(), row, server.URL, rctx, "")
	if err != nil {
		t.Fatalf("resolveDownload returned error: %v", err)
	}
	if result.URL != server.URL+"/download/file.torrent" {
		t.Errorf("expected %q, got %q", server.URL+"/download/file.torrent", result.URL)
	}
}

func TestResolveDownload_NoDefinition(t *testing.T) {
	def := cmodel.IndexerDefinition{
		ID:       "test",
		Name:     "Test",
		Language: "en",
		Type:     "public",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows:   cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected HTTP request")
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	html := `<table><tr><td>row</td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	row := rows[0]

	rctx := tmpl.Context{
		Config:   map[string]string{},
		Keywords: "test",
		Query:    map[string]string{"Keywords": "test"},
	}

	result, err := provider.resolveDownload(context.Background(), server.Client(), row, server.URL, rctx, "")
	if err != nil {
		t.Fatalf("resolveDownload returned error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for no download definition, got %+v", result)
	}
}

func TestResolveDownload_BeforeGetPath(t *testing.T) {
	def := cmodel.IndexerDefinition{
		ID:       "test",
		Name:     "Test",
		Language: "en",
		Type:     "public",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows:   cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{},
		},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{
					Path:   "/page?id={{ .Result.page_id }}",
					Method: "get",
					Inputs: map[string]string{
						"token": "abc",
					},
				},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.dl", Attribute: "href", UseBeforeResponse: true},
			},
		},
	}

	var receivedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<html><body><a class="dl" href="/dl/get.torrent">DL</a></body></html>`)
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	html := `<table><tr><td>row</td></tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	row := rows[0]

	rctx := tmpl.Context{
		Config:   map[string]string{},
		Keywords: "test",
		Query:    map[string]string{"Keywords": "test"},
		Result:   map[string]string{"page_id": "42"},
	}

	result, err := provider.resolveDownload(context.Background(), server.Client(), row, server.URL, rctx, "")
	if err != nil {
		t.Fatalf("resolveDownload returned error: %v", err)
	}
	if !strings.Contains(receivedQuery, "id=42") {
		t.Errorf("expected query to contain 'id=42', got %q", receivedQuery)
	}
	if !strings.Contains(receivedQuery, "token=abc") {
		t.Errorf("expected query to contain 'token=abc', got %q", receivedQuery)
	}
	if result.URL != server.URL+"/dl/get.torrent" {
		t.Errorf("expected %q, got %q", server.URL+"/dl/get.torrent", result.URL)
	}
}

func TestExtractRelease_WithDownloadResolution(t *testing.T) {
	def := cmodel.IndexerDefinition{
		ID:       "test",
		Name:     "Test",
		Language: "en",
		Type:     "public",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows: cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{
				"title":    {Selector: "td.title"},
				"details":  {Selector: "a.details", Attribute: "href"},
				"download": {Selector: "a.dl", Attribute: "href"},
			},
		},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{
					Path:   "/details/{{ .Result.details_id }}",
					Method: "get",
				},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.real-dl", Attribute: "href", UseBeforeResponse: true},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/details/99":
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><a class="real-dl" href="/resolved.torrent">DL</a></body></html>`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	provider := NewProvider(def, server.URL, nil)

	html := `<table><tr>
		<td class="title">Test Title</td>
		<td><a class="details" href="/details/99">Details</a></td>
		<td><a class="dl" href="/raw.torrent">DL</a></td>
	</tr></table>`
	doc, _ := selector.NewHTMLDocument(html)
	rows, _ := doc.Find("tr")
	row := rows[0]

	rctx := tmpl.Context{
		Config:   map[string]string{},
		Keywords: "test",
		Query:    map[string]string{"Keywords": "test"},
		Result:   map[string]string{"details_id": "99"},
	}

	rel, err := provider.extractRelease(context.Background(), row, rctx)
	if err != nil {
		t.Fatalf("extractRelease returned error: %v", err)
	}
	if rel.Title != "Test Title" {
		t.Errorf("expected title 'Test Title', got %q", rel.Title)
	}
	if rel.DownloadLink != server.URL+"/resolved.torrent" {
		t.Errorf("expected resolved download %q, got %q", server.URL+"/resolved.torrent", rel.DownloadLink)
	}
}

func TestExtractRelease_FallbackWhenResolveFails(t *testing.T) {
	def := cmodel.IndexerDefinition{
		ID:       "test",
		Name:     "Test",
		Language: "en",
		Type:     "public",
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
				{
					Path:   "/nonexistent",
					Method: "get",
				},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.nonexistent", Attribute: "href", UseBeforeResponse: true},
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
	row := rows[0]

	rctx := tmpl.Context{
		Config:   map[string]string{},
		Keywords: "test",
		Query:    map[string]string{"Keywords": "test"},
	}

	rel, err := provider.extractRelease(context.Background(), row, rctx)
	if err != nil {
		t.Fatalf("extractRelease returned error: %v", err)
	}
	if rel.DownloadLink != server.URL+"/raw.torrent" {
		t.Errorf("expected fallback download %q, got %q", server.URL+"/raw.torrent", rel.DownloadLink)
	}
}

func TestExtractSelectorField_TextTemplate(t *testing.T) {
	sf := &cmodel.SelectorField{
		Text: "https://example.com/download/{{ .Result.id }}.torrent",
	}

	rctx := tmpl.Context{
		Config:   map[string]string{},
		Keywords: "test",
		Query:    map[string]string{"Keywords": "test"},
		Result:   map[string]string{"id": "42"},
	}

	rendered, err := tmpl.Render(sf.Text, rctx)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if rendered != "https://example.com/download/42.torrent" {
		t.Errorf("expected 'https://example.com/download/42.torrent', got %q", rendered)
	}
}

// magnetFicheDef mirrors the vidlox/darkfox shape: before GET of the stored
// fiche URL + usebeforeresponse magnet selector.
func magnetFicheDef() cmodel.IndexerDefinition {
	return cmodel.IndexerDefinition{
		ID:       "magnetfiche",
		Name:     "Magnet Fiche",
		Language: "en",
		Type:     "public",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows:   cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{},
		},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{
				{Path: "{{ .DownloadUri }}"},
			},
			Selectors: []cmodel.DownloadSelector{
				{Selector: `a[href^="magnet:"]`, Attribute: "href", UseBeforeResponse: true},
			},
		},
	}
}

func TestResolveDownloadLink_MagnetFiche(t *testing.T) {
	var gets int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets++
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<html><body>
<a href="magnet:?xt=urn:btih:a0b1c2d3e4f5a0b1c2d3e4f5a0b1c2d3e4f5a0b1&tr=udp://t.example/announce">Magnet</a>
</body></html>`)
	}))
	defer server.Close()

	provider := NewProvider(magnetFicheDef(), server.URL, nil)
	rel := release.Release{Title: "Demo", DownloadLink: server.URL + "/detail/1"}
	resolved, err := provider.ResolveDownloadLink(context.Background(), rel)
	if err != nil {
		t.Fatalf("ResolveDownloadLink: %v", err)
	}
	if gets != 1 {
		t.Errorf("expected 1 fiche GET, got %d", gets)
	}
	if !strings.HasPrefix(resolved.DownloadLink, "magnet:?xt=urn:btih:a0b1c2d3") {
		t.Errorf("DownloadLink = %q, want magnet", resolved.DownloadLink)
	}
}

func TestResolveDownloadLink_MagnetFiche_NoMagnet_Errors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<html><body><p>no magnet here</p></body></html>`)
	}))
	defer server.Close()

	provider := NewProvider(magnetFicheDef(), server.URL, nil)
	rel := release.Release{Title: "Demo", DownloadLink: server.URL + "/detail/1"}
	_, err := provider.ResolveDownloadLink(context.Background(), rel)
	if err == nil {
		t.Fatal("expected error when chain extracts neither URL nor infohash")
	}
	// usebeforeresponse miss surfaces as "matched nothing"; a pure soft-fail
	// with empty before body surfaces as "did not resolve". Both are strict.
	if !strings.Contains(err.Error(), "matched nothing") && !strings.Contains(err.Error(), "did not resolve") {
		t.Errorf("error = %v, want strict chain failure", err)
	}
}

func TestResolveDownloadLink_AlreadyMagnet_NoHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected HTTP request to %s", r.URL)
	}))
	defer server.Close()

	provider := NewProvider(magnetFicheDef(), server.URL, nil)
	in := "magnet:?xt=urn:btih:a0b1c2d3e4f5a0b1c2d3e4f5a0b1c2d3e4f5a0b1"
	rel := release.Release{Title: "Demo", DownloadLink: in}
	resolved, err := provider.ResolveDownloadLink(context.Background(), rel)
	if err != nil {
		t.Fatalf("ResolveDownloadLink: %v", err)
	}
	if resolved.DownloadLink != in {
		t.Errorf("DownloadLink = %q, want unchanged %q", resolved.DownloadLink, in)
	}
}

func TestResolveDownloadLink_InfoHashOnly_SynthesizesMagnet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// Nested .hash: UseBeforeResponse Find lands on the outer node, then
		// extractSelectorField Find's the same selector among its descendants.
		_, _ = fmt.Fprint(w, `<html><body>
<div class="hash"><span class="hash">deadbeefdeadbeefdeadbeefdeadbeefdeadbeef</span></div>
</body></html>`)
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		ID:       "infohashonly",
		Name:     "InfoHash Only",
		Language: "en",
		Type:     "public",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows:   cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{},
		},
		Download: &cmodel.Download{
			Before: cmodel.DownloadBeforeList{{Path: "{{ .DownloadUri }}"}},
			Infohash: &cmodel.DownloadInfohash{
				UseBeforeResponse: true,
				Hash: cmodel.SelectorField{
					Selector: ".hash",
				},
			},
		},
	}
	provider := NewProvider(def, server.URL, nil)
	rel := release.Release{Title: "Demo", DownloadLink: server.URL + "/detail/1"}
	resolved, err := provider.ResolveDownloadLink(context.Background(), rel)
	if err != nil {
		t.Fatalf("ResolveDownloadLink: %v", err)
	}
	want := "magnet:?xt=urn:btih:deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	if resolved.DownloadLink != want {
		t.Errorf("DownloadLink = %q, want %q", resolved.DownloadLink, want)
	}
	if resolved.InfoHash != "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef" {
		t.Errorf("InfoHash = %q", resolved.InfoHash)
	}
}

func TestResolveDownloadLink_NoDownloadBlock_NoOp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected HTTP request to %s", r.URL)
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		ID:       "noblock",
		Name:     "No Block",
		Language: "en",
		Type:     "public",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows:   cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{},
		},
	}
	provider := NewProvider(def, server.URL, nil)
	rel := release.Release{Title: "Demo", DownloadLink: server.URL + "/file.torrent"}
	resolved, err := provider.ResolveDownloadLink(context.Background(), rel)
	if err != nil {
		t.Fatalf("ResolveDownloadLink: %v", err)
	}
	if resolved.DownloadLink != rel.DownloadLink {
		t.Errorf("DownloadLink = %q, want unchanged %q", resolved.DownloadLink, rel.DownloadLink)
	}
}

func TestResolveDownloadLink_RowOnlySelectors_NoStrict(t *testing.T) {
	// Download-time has no row: row-only selectors cannot re-extract, and the
	// stored link must be kept without a strict error.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected HTTP request to %s", r.URL)
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		ID:       "rowonly",
		Name:     "Row Only",
		Language: "en",
		Type:     "public",
		Links:    []string{"http://example.invalid"},
		Search: cmodel.Search{
			Rows:   cmodel.Rows{Selector: "tr"},
			Fields: map[string]cmodel.Field{},
		},
		Download: &cmodel.Download{
			Selectors: []cmodel.DownloadSelector{
				{Selector: "a.dl", Attribute: "href"},
			},
		},
	}
	provider := NewProvider(def, server.URL, nil)
	stored := server.URL + "/file.torrent"
	rel := release.Release{Title: "Demo", DownloadLink: stored}
	resolved, err := provider.ResolveDownloadLink(context.Background(), rel)
	if err != nil {
		t.Fatalf("ResolveDownloadLink: %v", err)
	}
	if resolved.DownloadLink != stored {
		t.Errorf("DownloadLink = %q, want stored %q", resolved.DownloadLink, stored)
	}
}
