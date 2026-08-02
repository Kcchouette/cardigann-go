package engine

import (
	"testing"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/release"
)

func TestProviderProtocolByType(t *testing.T) {
	cases := []struct {
		name string
		typ  string
		want release.Protocol
	}{
		{"usenet explicit", "usenet", release.ProtocolUsenet},
		{"ddl", "ddl", release.ProtocolDDL},
		{"streaming", "streaming", release.ProtocolStreaming},
		{"public default", "public", release.ProtocolTorrent},
		{"semi-private default", "semi-private", release.ProtocolTorrent},
		{"empty type", "", release.ProtocolTorrent},
		{"unknown type", "weird", release.ProtocolTorrent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := cmodel.IndexerDefinition{Type: tc.typ}
			p := NewProvider(def, "http://example.invalid/", nil)
			if got := p.Protocol(); got != tc.want {
				t.Errorf("Protocol() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProviderProtocolIsUsenetFallback(t *testing.T) {
	// A definition without an explicit type but with usenet category mappings
	// must keep resolving to usenet through the IsUsenet() heuristic.
	def := cmodel.IndexerDefinition{
		Caps: cmodel.Caps{
			CategoryMappings: []cmodel.CategoryMapping{
				{ID: "2000", Cat: "2000", Desc: "NZB Movies"},
			},
		},
	}
	if !def.IsUsenet() {
		t.Fatal("fixture should be detected as usenet by heuristic")
	}
	p := NewProvider(def, "http://example.invalid/", nil)
	if got := p.Protocol(); got != release.ProtocolUsenet {
		t.Errorf("Protocol() = %q, want usenet via heuristic", got)
	}
}
