//go:build live

// The live corpus sweep downloads the full Prowlarr/Indexers corpus at test
// time and asserts that no definition REGRESSED under the fork: every def
// that parsed on master must still parse, and every parsed def must resolve
// to a known protocol. It is excluded from CI (network + ~1.5 MB download):
// run explicitly with `go test -tags=live ./internal/defstest/`.
package defstest

import (
	"context"
	"testing"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/defs"
	"github.com/Kcchouette/cardigann-go/engine"
	"github.com/Kcchouette/cardigann-go/release"
)

// baselineParseFailures is the set of v11 definitions that could not be
// parsed by cardigann-go on master before this fork (verified: all 16 fail
// identically against the unmodified code — yaml.v3 rejects the `\/` escape
// and some real defs use seq forms the schema does not model). New parse
// failures beyond this baseline are regressions.
var baselineParseFailures = map[string]bool{
	"anibt":              true,
	"arabtorrents":       true,
	"btarg":              true,
	"chdbits":            true,
	"coastalcrew":        true,
	"newretro":           true,
	"speedmasterhd":      true,
	"spiritofrevolution": true,
	"swarmazon-api":      true,
	"thebrothers":        true,
	"thecrazyones":       true,
	"thefallingangels":   true,
	"theparadiese":       true,
	"tormac":             true,
	"torrenttrader":      true,
	"unbreakable":        true,
}

func TestCorpusLive_FullSweep(t *testing.T) {
	fetcher := defs.NewFetcher()
	raws, err := fetcher.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("fetching corpus: %v", err)
	}

	checked := 0
	preExisting := 0
	for _, raw := range raws {
		def, err := cmodel.Parse([]byte(raw.YAML))
		if err != nil {
			if baselineParseFailures[raw.ID] {
				preExisting++
				continue
			}
			t.Errorf("REGRESSION: %s/%s now fails to parse: %v", raw.Version, raw.ID, err)
			continue
		}
		p := engine.NewProvider(def, "http://example.invalid/", nil)
		if proto := p.Protocol(); proto != release.ProtocolTorrent && proto != release.ProtocolUsenet {
			t.Errorf("%s: unexpected protocol %q", def.ID, proto)
		}
		checked++
	}
	t.Logf("checked %d definitions (%d pre-existing parse failures skipped)", checked, preExisting)
}
