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

// baselineParseFailures is the set of v11 definitions that still cannot be
// parsed by cardigann-go after the v11 compliance pass. Verified against the
// corpus: anibt uses seq-form categorymappings yaml.v3 cannot model, and the
// others use the `\/` YAML escape that gopkg.in/yaml.v3 rejects. The 11 IDs
// that previously failed on the login.cookies/headers map forms now parse
// (removed from this baseline). New parse failures beyond this baseline are
// regressions.
var baselineParseFailures = map[string]bool{
	"anibt":         true,
	"arabtorrents":  true,
	"btarg":         true,
	"swarmazon-api": true,
	"tormac":        true,
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
		// Legacy login: keys are ignored by the parser (v11 uses login.path);
		// any def still carrying one would silently log in at the site root.
		if _, legacy := def.Login.Extra["login"]; legacy {
			t.Errorf("REGRESSION: %s/%s uses the legacy 'login:' key (use 'path:')", raw.Version, raw.ID)
		}
		p := engine.NewProvider(def, "http://example.invalid/", nil)
		if proto := p.Protocol(); proto != release.ProtocolTorrent && proto != release.ProtocolUsenet {
			t.Errorf("%s: unexpected protocol %q", def.ID, proto)
		}
		checked++
	}
	t.Logf("checked %d definitions (%d pre-existing parse failures skipped)", checked, preExisting)
}
