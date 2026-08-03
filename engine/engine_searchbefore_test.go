package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
)

// searchBeforeYAML is a two-step Cardigann definition exercising search.before:
// a keyword lookup resolves a title id, then the search path lists the title's
// DDL liens. %s is replaced by the test server base URL.
const searchBeforeYAML = `
id: hydraddl
name: Hydra DDL
description: fixture for search.before
language: fr-FR
type: ddl
links:
  - %s
search:
  before:
    - name: title
      path: api/v1/titles
      inputs:
        query: "{{ .Keywords }}"
      response:
        type: json
      selectors:
        id: {selector: "$.pagination.data.0.id"}
        name: {selector: "$.pagination.data.0.name"}
        type: {selector: "$.pagination.data.0.type"}
  paths:
    - path: "api/v1/titles/{{ .Before.title.id }}/content/liens"
      inputs:
        loader: linksdl
      response:
        type: json
  headers:
    Accept: ["application/json"]
    Authorization: ["Bearer {{ .Config.apikey }}"]
  rows:
    selector: "$.liens"
  fields:
    _season:      {selector: saison, optional: true}
    _episode:     {selector: episode, optional: true}
    _full_saison: {selector: full_saison, optional: true}
    _quality:     {selector: "qual.qual", optional: true}
    _hostname:    {selector: "host.name", optional: true}
    title:
      text: '{{ .Before.title.name }}{{ if eq .Result._full_saison "1" }} S{{ .Result._season }} (saison complete){{ else }} S{{ .Result._season }}E{{ .Result._episode }}{{ end }} - {{ .Result._quality }} [{{ .Result._hostname }}]'
    size:
      selector: taille
    date:
      selector: created_at
      filters:
        - name: dateparse
    category:
      text: "{{ .Before.title.type }}"
      case:
        series: 5000
        movie: 2000
        "*": 8000
    details:
      text: "https://hydracker.com/titles/{{ .Before.title.id }}"
    download:
      text: '{{ if eq .Result._full_saison "1" }}https://hydracker.com/titles/{{ .Before.title.id }}/season/{{ .Result._season }}/download{{ else }}https://hydracker.com/titles/{{ .Before.title.id }}/season/{{ .Result._season }}/episode/{{ .Result._episode }}/download{{ end }}'
`

const liensFixture = `{"liens":[
	{"id":1,"saison":1,"episode":1,"full_saison":0,"taille":9800362833,"created_at":"2022-12-31T03:21:19.000000Z","host":{"name":"1Fichier"},"qual":{"qual":"Blu-Ray 1080p"}},
	{"id":2,"saison":2,"episode":0,"full_saison":1,"taille":19022414451,"created_at":"2024-08-05T05:53:19.000000Z","host":{"name":"1Fichier"},"qual":{"qual":"ULTRA HD (x265)"}}
]}`

// twoStepServer serves the before (title lookup) and search (liens) endpoints,
// counting calls (atomically: searches may run concurrently) and recording the
// Authorization header.
func twoStepServer(t *testing.T, titleBody string, searchBody string) (*httptest.Server, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var beforeCalls, searchCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/titles":
			beforeCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer sekret" {
				t.Errorf("before step: Authorization header = %q, want Bearer sekret", got)
			}
			_, _ = w.Write([]byte(titleBody))
		case "/api/v1/titles/179751/content/liens":
			searchCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer sekret" {
				t.Errorf("search path: Authorization header = %q, want Bearer sekret", got)
			}
			_, _ = w.Write([]byte(searchBody))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	return server, &beforeCalls, &searchCalls
}

func TestSearchBefore_TwoStepJSON(t *testing.T) {
	server, beforeCalls, searchCalls := twoStepServer(t,
		`{"pagination":{"data":[{"id":179751,"name":"House of the Dragon","type":"series"}]}}`,
		liensFixture,
	)
	defer server.Close()

	def, err := cmodel.Parse([]byte(fmt.Sprintf(searchBeforeYAML, server.URL)))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}
	provider := NewProvider(def, server.URL, map[string]string{"apikey": "sekret"})

	releases, err := provider.Search(context.Background(), Query{Keywords: "house of the dragon"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(releases))
	}
	if beforeCalls.Load() != 1 || searchCalls.Load() != 1 {
		t.Fatalf("expected 1 before + 1 search call, got %d + %d", beforeCalls.Load(), searchCalls.Load())
	}

	ep := releases[0]
	if ep.Title != "House of the Dragon S1E1 - Blu-Ray 1080p [1Fichier]" {
		t.Errorf("episode title = %q", ep.Title)
	}
	if ep.Size != 9800362833 {
		t.Errorf("episode size = %d, want 9800362833", ep.Size)
	}
	if got := ep.PublishDate.Format("2006-01-02"); got != "2022-12-31" {
		t.Errorf("episode date = %s, want 2022-12-31", got)
	}
	if len(ep.Categories) != 1 || ep.Categories[0] != 5000 {
		t.Errorf("episode categories = %v, want [5000]", ep.Categories)
	}
	if ep.Details != "https://hydracker.com/titles/179751" {
		t.Errorf("episode details = %q", ep.Details)
	}
	if ep.DownloadLink != "https://hydracker.com/titles/179751/season/1/episode/1/download" {
		t.Errorf("episode download = %q", ep.DownloadLink)
	}

	season := releases[1]
	if season.Title != "House of the Dragon S2 (saison complete) - ULTRA HD (x265) [1Fichier]" {
		t.Errorf("season title = %q", season.Title)
	}
	if season.Size != 19022414451 {
		t.Errorf("season size = %d, want 19022414451", season.Size)
	}
	if season.DownloadLink != "https://hydracker.com/titles/179751/season/2/download" {
		t.Errorf("season download = %q", season.DownloadLink)
	}
}

func TestSearchBefore_NoTitleMatch_ReturnsEmptyWithoutSearchRequest(t *testing.T) {
	server, _, searchCalls := twoStepServer(t,
		`{"pagination":{"data":[]}}`, // required selector $.pagination.data.0.id matches nothing
		liensFixture,
	)
	defer server.Close()

	def, err := cmodel.Parse([]byte(fmt.Sprintf(searchBeforeYAML, server.URL)))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}
	provider := NewProvider(def, server.URL, map[string]string{"apikey": "sekret"})

	releases, err := provider.Search(context.Background(), Query{Keywords: "zzzznothing"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 0 {
		t.Fatalf("expected 0 releases, got %d", len(releases))
	}
	if searchCalls.Load() != 0 {
		t.Fatalf("search path must not fire when the before step matched nothing, got %d calls", searchCalls.Load())
	}
}

func TestSearchBefore_HTTPFailure_AbortsWithError(t *testing.T) {
	// The before step (/api/v1/titles) answers 401: the search must abort
	// with a visible error (a wrong/expired token is not "no results").
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/titles" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Unauthenticated."}`))
	}))
	defer server.Close()

	def, err := cmodel.Parse([]byte(fmt.Sprintf(searchBeforeYAML, server.URL)))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}
	provider := NewProvider(def, server.URL, map[string]string{"apikey": "sekret"})

	if _, err := provider.Search(context.Background(), Query{Keywords: "house of the dragon"}); err == nil {
		t.Fatal("Search must abort with an error on before-step HTTP failure")
	}
}

func TestSearchBefore_SingleMappingForm(t *testing.T) {
	const yaml = `
id: singlebefore
name: Single Before
description: fixture
language: en-US
type: ddl
links:
  - %s
search:
  before:
    name: title
    path: api/v1/titles
    response:
      type: json
    selectors:
      id: {selector: "$.pagination.data.0.id"}
  paths:
    - path: "api/v1/titles/{{ .Before.title.id }}/content/liens"
      response:
        type: json
  rows:
    selector: "$.liens"
  fields:
    title:
      selector: id
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/titles" {
			_, _ = w.Write([]byte(`{"pagination":{"data":[{"id":42}]}}`))
			return
		}
		if r.URL.Path == "/api/v1/titles/42/content/liens" {
			_, _ = w.Write([]byte(`{"liens":[{"id":7}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	def, err := cmodel.Parse([]byte(fmt.Sprintf(yaml, server.URL)))
	if err != nil {
		t.Fatalf("parsing single-mapping before: %v", err)
	}
	if len(def.Search.Before) != 1 || def.Search.Before[0].Name != "title" {
		t.Fatalf("single-mapping before not parsed: %+v", def.Search.Before)
	}
	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "x"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 1 || releases[0].Title != "7" {
		t.Fatalf("unexpected releases: %+v", releases)
	}
}

func TestSearchBefore_ChainedSteps(t *testing.T) {
	const yaml = `
id: chained
name: Chained Before
description: fixture
language: en-US
type: ddl
links:
  - %s
search:
  before:
    - name: first
      path: lookup
      response:
        type: json
      selectors:
        token: {selector: "$.token"}
    - name: second
      path: "resolve/{{ .Before.first.token }}"
      response:
        type: json
      selectors:
        id: {selector: "$.id"}
  paths:
    - path: "items/{{ .Before.second.id }}"
      response:
        type: json
  rows:
    selector: "$.items"
  fields:
    title:
      selector: name
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lookup":
			_, _ = w.Write([]byte(`{"token":"abc123"}`))
		case "/resolve/abc123":
			_, _ = w.Write([]byte(`{"id":99}`))
		case "/items/99":
			_, _ = w.Write([]byte(`{"items":[{"name":"row one"},{"name":"row two"}]}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	def, err := cmodel.Parse([]byte(fmt.Sprintf(yaml, server.URL)))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}
	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "x"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 2 || releases[0].Title != "row one" {
		t.Fatalf("unexpected releases: %+v", releases)
	}
}

func TestSearchBefore_PostStepAndRawInputs(t *testing.T) {
	const yaml = `
id: postbefore
name: Post Before
description: fixture
language: en-US
type: ddl
links:
  - %s
search:
  before:
    - name: title
      path: lookup
      method: post
      inputs:
        q: "{{ .Keywords }}"
        $raw: "&flag=1"
      response:
        type: json
      selectors:
        id: {selector: "$.id"}
  paths:
    - path: "items/{{ .Before.title.id }}"
      response:
        type: json
  rows:
    selector: "$.items"
  fields:
    title:
      selector: name
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/lookup" && r.Method == http.MethodPost:
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if r.PostFormValue("q") != "needle" || r.PostFormValue("flag") != "1" {
				t.Fatalf("unexpected POST body: %v", r.PostForm)
			}
			_, _ = w.Write([]byte(`{"id":5}`))
		case r.URL.Path == "/items/5":
			_, _ = w.Write([]byte(`{"items":[{"name":"found"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	def, err := cmodel.Parse([]byte(fmt.Sprintf(yaml, server.URL)))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}
	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "needle"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 1 || releases[0].Title != "found" {
		t.Fatalf("unexpected releases: %+v", releases)
	}
}

func TestSearchBefore_HTMLStep(t *testing.T) {
	const yaml = `
id: htmlbefore
name: HTML Before
description: fixture
language: en-US
type: ddl
links:
  - %s
search:
  before:
    - name: title
      path: lookup.html
      selectors:
        id:
          selector: "#result"
          attribute: data-id
  paths:
    - path: "items/{{ .Before.title.id }}"
      response:
        type: json
  rows:
    selector: "$.items"
  fields:
    title:
      selector: name
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lookup.html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body><span id="result" data-id="33">ok</span></body></html>`))
		case "/items/33":
			_, _ = w.Write([]byte(`{"items":[{"name":"html row"}]}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	def, err := cmodel.Parse([]byte(fmt.Sprintf(yaml, server.URL)))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}
	provider := NewProvider(def, server.URL, nil)
	releases, err := provider.Search(context.Background(), Query{Keywords: "x"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 1 || releases[0].Title != "html row" {
		t.Fatalf("unexpected releases: %+v", releases)
	}
}

func TestSearchBefore_EmptySearchGateSkipsBefore(t *testing.T) {
	server, beforeCalls, _ := twoStepServer(t,
		`{"pagination":{"data":[{"id":1}]}}`,
		liensFixture,
	)
	defer server.Close()

	def, err := cmodel.Parse([]byte(fmt.Sprintf(searchBeforeYAML, server.URL)))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}
	provider := NewProvider(def, server.URL, map[string]string{"apikey": "sekret"})

	// No keywords, no media id, no allowrawsearch: the gate returns early and
	// the before chain must not fire any request.
	releases, err := provider.Search(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(releases) != 0 {
		t.Fatalf("expected 0 releases, got %d", len(releases))
	}
	if beforeCalls.Load() != 0 {
		t.Fatalf("before chain must not run on gated empty searches, got %d calls", beforeCalls.Load())
	}
}

func TestSearchBefore_ConcurrentSearches(t *testing.T) {
	server, _, _ := twoStepServer(t,
		`{"pagination":{"data":[{"id":179751,"name":"House of the Dragon","type":"series"}]}}`,
		liensFixture,
	)
	defer server.Close()

	def, err := cmodel.Parse([]byte(fmt.Sprintf(searchBeforeYAML, server.URL)))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}
	provider := NewProvider(def, server.URL, map[string]string{"apikey": "sekret"})

	const goroutines = 8
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			releases, err := provider.Search(context.Background(), Query{Keywords: "house of the dragon"})
			if err != nil {
				errs[i] = err
				return
			}
			if len(releases) != 2 {
				errs[i] = fmt.Errorf("expected 2 releases, got %d", len(releases))
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
}
