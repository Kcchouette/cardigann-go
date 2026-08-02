// Package defstest verifies that the multi-protocol fork does not regress the
// Cardigann v11 corpus: every definition must still parse and resolve to a
// known protocol (torrent/usenet for the corpus). The fixtures are a
// committed representative subset of the Prowlarr/Indexers corpus (all 14
// definitions using download.before plus a diverse sample by login method);
// the full sweep runs behind the `live` build tag.
package defstest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/engine"
	"github.com/Kcchouette/cardigann-go/release"
	tmpl "github.com/Kcchouette/cardigann-go/template"
)

// fixtureIDs returns the IDs of the committed definition fixtures.
func fixtureIDs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".yml"))
	}
	return ids
}

// knownParseFailures lists definitions of the v11 corpus that the engine
// cannot parse (verified on master before this fork — e.g. arabtorrents uses
// the `\/` YAML escape that gopkg.in/yaml.v3 rejects). They are asserted as
// PRE-EXISTING so CI fails on regressions, not on known gaps.
var knownParseFailures = map[string]string{
	"arabtorrents": "yaml.v3 rejects the `\\/` escape (PRE-EXISTING on master)",
}

func TestCorpusFixture_ParseAndProtocol(t *testing.T) {
	for _, id := range fixtureIDs(t) {
		t.Run(id, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", id+".yml"))
			if err != nil {
				t.Fatalf("reading fixture: %v", err)
			}
			def, err := cmodel.Parse(raw)
			if err != nil {
				if reason, ok := knownParseFailures[id]; ok {
					t.Skipf("known pre-existing parse failure: %s", reason)
				}
				t.Fatalf("parse: %v", err)
			}
			if def.ID != id {
				t.Errorf("definition id %q does not match fixture name %q", def.ID, id)
			}
			// Legacy login: keys are ignored by the parser (v11 uses
			// login.path); a def still carrying one would log in at the site root.
			if _, legacy := def.Login.Extra["login"]; legacy {
				t.Errorf("%s: uses the legacy 'login:' key (use 'path:')", id)
			}
			p := engine.NewProvider(def, "http://example.invalid/", nil)
			if proto := p.Protocol(); proto != release.ProtocolTorrent && proto != release.ProtocolUsenet {
				t.Errorf("unexpected protocol %q for a v11 definition", proto)
			}
		})
	}
}

// TestCorpusFixture_BeforeChainParses verifies that DownloadBeforeList still
// accepts the legacy single-mapping `download.before:` form used by the
// corpus, and that the chain does not panic.
func TestCorpusFixture_BeforeChainParses(t *testing.T) {
	checked := 0
	for _, id := range fixtureIDs(t) {
		raw, err := os.ReadFile(filepath.Join("testdata", id+".yml"))
		if err != nil {
			t.Fatalf("reading fixture %s: %v", id, err)
		}
		def, err := cmodel.Parse(raw)
		if err != nil {
			if _, ok := knownParseFailures[id]; ok {
				continue
			}
			t.Fatalf("parse %s: %v", id, err)
		}
		if def.Download == nil {
			continue
		}
		_ = len(def.Download.Before) // exercising the chain does not panic
		checked++
	}
	if checked == 0 {
		t.Fatal("expected at least one fixture with a download block (the 14 before: defs)")
	}
}

// knownBrokenPaths lists search path templates that are broken in the corpus
// itself (not by the fork): 1337x's TV path has a stray ')' in
// `eq .Config.disablesort .False))`. The engine skips such paths at runtime
// with a warning (per-path tolerance).
var knownBrokenPaths = map[string]string{
	"1337x": "corpus typo: extra ')' in the TV path",
}

// TestCorpusFixture_PathsRender exercises every search path template with a
// dummy context: defstest only checks parse+protocol, so template regressions
// (join, .True/.False/.Today, filter-as-funcs) would otherwise go unnoticed.
func TestCorpusFixture_PathsRender(t *testing.T) {
	rctx := tmpl.Context{
		Config:     map[string]string{},
		Keywords:   "test",
		Categories: []string{},
		Query: map[string]string{
			"Keywords": "test", "q": "test", "IMDBID": "", "IMDBIDShort": "",
			"TMDBID": "", "TVDBID": "", "DoubanID": "", "Season": "",
			"Ep": "", "Year": "", "Genre": "",
		},
	}
	checked := 0
	for _, id := range fixtureIDs(t) {
		raw, err := os.ReadFile(filepath.Join("testdata", id+".yml"))
		if err != nil {
			t.Fatalf("reading fixture %s: %v", id, err)
		}
		def, err := cmodel.Parse(raw)
		if err != nil {
			if _, ok := knownParseFailures[id]; ok {
				continue
			}
			t.Fatalf("parse %s: %v", id, err)
		}
		for _, p := range def.Search.Paths {
			if _, err := tmpl.Render(p.Path, rctx); err != nil {
				if _, ok := knownBrokenPaths[id]; ok {
					t.Logf("%s: skipping known-broken path: %v", id, err)
					continue
				}
				t.Errorf("%s: path %q fails to render: %v", id, p.Path, err)
			}
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("expected at least one fixture to render")
	}
}
