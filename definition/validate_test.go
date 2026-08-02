package definition

import (
	"strings"
	"testing"
)

func TestValidateNeverFails(t *testing.T) {
	defs := []IndexerDefinition{
		{ID: "torrent-plain", Type: ""},
		{ID: "torrent-public", Type: "public"},
		{ID: "usenet-no-download", Type: "usenet"},
		{ID: "ddl-no-download", Type: "ddl"},
		{ID: "streaming-no-download", Type: "streaming"},
		{ID: "unknown-type", Type: "weird"},
	}
	for _, d := range defs {
		// Validate must never panic and must not fail the definition:
		// it only collects warnings.
		_ = d.Validate()
	}
}

func TestValidateWarnsOnMissingDownload(t *testing.T) {
	d := IndexerDefinition{ID: "ddl", Type: "ddl"}
	warns := d.Validate()
	if len(warns) == 0 {
		t.Fatal("expected a warning for a ddl definition without download")
	}
	if !strings.Contains(warns[0], "ddl") {
		t.Errorf("warning should mention the protocol, got %q", warns[0])
	}

	// With a download field the definition is complete: no warning.
	d.Search.Fields = map[string]Field{"download": {Selector: "a.dl", Attribute: "href"}}
	if w := d.Validate(); len(w) != 0 {
		t.Errorf("expected no warnings with a download field, got %v", w)
	}
}

func TestValidateUnknownType(t *testing.T) {
	d := IndexerDefinition{ID: "x", Type: "weird"}
	found := false
	for _, w := range d.Validate() {
		if strings.Contains(w, "unknown type") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an unknown-type warning, got %v", d.Validate())
	}
}

func TestValidateDocumentedTrackerTypes(t *testing.T) {
	// public/semi-private/private are the documented tracker types: they must
	// not trigger the "unknown type" warning.
	for _, typ := range []string{"public", "semi-private", "private"} {
		d := IndexerDefinition{ID: "x", Type: typ}
		for _, w := range d.Validate() {
			if strings.Contains(w, "unknown type") {
				t.Errorf("type %q should not warn as unknown, got %q", typ, w)
			}
		}
	}
}

func TestValidate_UnknownEncoding(t *testing.T) {
	yaml := `
id: enc
name: Enc
type: public
encoding: klingon
caps:
  modes:
    search: [q]
search:
  path: /s
  rows:
    selector: x
  fields:
    title:
      selector: y
`
	def, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	warns := def.Validate()
	found := false
	for _, w := range warns {
		if strings.Contains(w, "klingon") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a warning about unknown encoding klingon, got %v", warns)
	}
}

func TestValidate_UTF8NoWarning(t *testing.T) {
	def := IndexerDefinition{Encoding: "UTF-8"}
	for _, w := range def.Validate() {
		if strings.Contains(w, "encoding") {
			t.Errorf("unexpected encoding warning for UTF-8: %q", w)
		}
	}
}
