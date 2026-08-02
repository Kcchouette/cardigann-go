package httputil

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

func TestKnownEncoding(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"utf-8", "UTF-8", true},
		{"utf8", "utf8", true},
		{"windows-1251", "windows-1251", true},
		{"iso-8859-1 lowercase", "iso-8859-1", true},
		{"iso-8859-1 uppercase", "ISO-8859-1", true},
		{"tis-620 unsupported", "tis-620", false},
		{"klingon", "klingon", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := KnownEncoding(tt.in); got != tt.want {
				t.Errorf("KnownEncoding(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestReadBodyLimited_Decodes(t *testing.T) {
	// "Привет" in windows-1251.
	cyrillic := "Привет"
	encoded, _, err := transform.Bytes(charmap.Windows1251.NewEncoder(), []byte(cyrillic))
	if err != nil {
		t.Fatalf("encoding fixture: %v", err)
	}

	resp := &http.Response{Body: io.NopCloser(strings.NewReader(string(encoded)))}
	got, err := ReadBodyLimited(resp, "windows-1251", 64<<10)
	if err != nil {
		t.Fatalf("ReadBodyLimited: %v", err)
	}
	if string(got) != cyrillic {
		t.Errorf("decoded = %q, want %q", string(got), cyrillic)
	}
}

func TestReadBodyLimited_CaseInsensitive(t *testing.T) {
	// windows-1256 (arabtorrents) with the same bytes under both spellings.
	arabic := "مرحبا"
	encoded, _, err := transform.Bytes(charmap.Windows1256.NewEncoder(), []byte(arabic))
	if err != nil {
		t.Fatalf("encoding fixture: %v", err)
	}
	for _, name := range []string{"windows-1256", "WINDOWS-1256", " Windows-1256 "} {
		resp := &http.Response{Body: io.NopCloser(strings.NewReader(string(encoded)))}
		got, err := ReadBodyLimited(resp, name, 64<<10)
		if err != nil {
			t.Fatalf("ReadBodyLimited(%q): %v", name, err)
		}
		if string(got) != arabic {
			t.Errorf("decoded = %q, want %q", string(got), arabic)
		}
	}
}

func TestReadBodyLimited_UTF8Passthrough(t *testing.T) {
	body := `<html><body>é à ü</body></html>`
	for _, name := range []string{"UTF-8", "utf8", ""} {
		resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
		got, err := ReadBodyLimited(resp, name, 64<<10)
		if err != nil {
			t.Fatalf("ReadBodyLimited(%q): %v", name, err)
		}
		if string(got) != body {
			t.Errorf("passthrough = %q, want %q", string(got), body)
		}
	}
}

func TestReadBodyLimited_UnknownPassthrough(t *testing.T) {
	// Unknown encodings (including tis-620) are read as UTF-8 unchanged.
	body := "<html><body>raw bytes</body></html>"
	for _, name := range []string{"tis-620", "klingon"} {
		resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
		got, err := ReadBodyLimited(resp, name, 64<<10)
		if err != nil {
			t.Fatalf("ReadBodyLimited(%q): %v", name, err)
		}
		if string(got) != body {
			t.Errorf("passthrough = %q, want %q", string(got), body)
		}
	}
}

func TestReadBodyLimited_Limit(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(strings.NewReader("abcdef"))}
	got, err := ReadBodyLimited(resp, "UTF-8", 3)
	if err != nil {
		t.Fatalf("ReadBodyLimited: %v", err)
	}
	if string(got) != "abc" {
		t.Errorf("limited = %q, want %q", string(got), "abc")
	}
}
