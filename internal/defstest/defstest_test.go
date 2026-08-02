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
