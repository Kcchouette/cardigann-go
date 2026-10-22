package filters

import (
	"strings"
	"testing"
)

func TestApply_Pipeline(t *testing.T) {
	cases := []struct {
		name  string
		value string
		steps []Step
		want  string
	}{
		{
			name:  "trim then tolower",
			value: "  HeLLo  ",
			steps: []Step{{Name: "trim"}, {Name: "tolower"}},
			want:  "hello",
		},
		{
			name:  "re_replace",
			value: "Season.01.Episode.02",
			steps: []Step{{Name: "re_replace", Args: []string{`\.`, " "}}},
			want:  "Season 01 Episode 02",
		},
		{
			name:  "split index",
			value: "a|b|c",
			steps: []Step{{Name: "split", Args: []string{"|", "1"}}},
			want:  "b",
		},
		{
			name:  "replace",
			value: "foo-bar",
			steps: []Step{{Name: "replace", Args: []string{"-", "_"}}},
			want:  "foo_bar",
		},
		{
			name:  "urldecode",
			value: "hello%20world",
			steps: []Step{{Name: "urldecode"}},
			want:  "hello world",
		},
		{
			name:  "htmldecode",
			value: "Tom &amp; Jerry",
			steps: []Step{{Name: "htmldecode"}},
			want:  "Tom & Jerry",
		},
		{
			name:  "regexp extract group",
			value: "size: 1234 MB",
			steps: []Step{{Name: "regexp", Args: []string{`(\d+) MB`}}},
			want:  "1234",
		},
		{
			name:  "append prepend",
			value: "core",
			steps: []Step{{Name: "prepend", Args: []string{"pre-"}}, {Name: "append", Args: []string{"-post"}}},
			want:  "pre-core-post",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Apply(tc.value, tc.steps)
			if err != nil {
				t.Fatalf("Apply returned error: %v", err)
			}
			if got != tc.want {
				t.Errorf("Apply(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestApply_UnknownFilter(t *testing.T) {
	_, err := Apply("x", []Step{{Name: "doesnotexist"}})
	if err == nil {
		t.Fatal("expected error for unknown filter")
	}
}

func TestDateParse(t *testing.T) {
	got, err := Apply("2024-01-15", []Step{{Name: "dateparse"}})
	if err != nil {
		t.Fatalf("dateparse failed: %v", err)
	}
	want := "2024-01-15T00:00:00Z"
	if got != want {
		t.Errorf("dateparse = %q, want %q", got, want)
	}
}

func TestTimeParse_Alias(t *testing.T) {
	got, err := Apply("2024-01-15", []Step{{Name: "timeparse"}})
	if err != nil {
		t.Fatalf("timeparse failed: %v", err)
	}
	want := "2024-01-15T00:00:00Z"
	if got != want {
		t.Errorf("timeparse = %q, want %q", got, want)
	}
}

func TestDateParse_NetLayout(t *testing.T) {
	// Corpus args are .NET format strings; the translated layout must match.
	tests := []struct {
		name  string
		value string
		args  []string
		want  string
	}{
		{"RFC1123Z style", "Sat, 20 Jan 2024 14:30:45 +0100", []string{"ddd, dd MMM yyyy HH:mm:ss zzz"}, "2024-01-20T14:30:45+01:00"},
		{"slash format", "01/15/2024 09:05:07 -0700", []string{"MM/dd/yyyy HH:mm:ss zzz"}, "2024-01-15T09:05:07-07:00"},
		{"dash format", "2024-01-15 09:05:07", []string{"yyyy-MM-dd HH:mm:ss"}, "2024-01-15T09:05:07Z"},
		{"quoted literal", "15 January 2024", []string{"dd MMMM yyyy"}, "2024-01-15T00:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(tt.value, []Step{{Name: "dateparse", Args: tt.args}})
			if err != nil {
				t.Fatalf("dateparse failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("dateparse = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDateParse_GoLayoutStillWorks(t *testing.T) {
	// A def that already passes a Go layout keeps working (raw arg fallback).
	got, err := Apply("2024-01-15 09:05:07", []Step{{Name: "dateparse", Args: []string{"2006-01-02 15:04:05"}}})
	if err != nil {
		t.Fatalf("dateparse failed: %v", err)
	}
	want := "2024-01-15T09:05:07Z"
	if got != want {
		t.Errorf("dateparse = %q, want %q", got, want)
	}
}

func TestUnixDate(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{"epoch seconds", "1792340016", "2026-10-18T16:13:36Z", false},
		{"surrounding spaces trimmed", "  1792340016\n", "2026-10-18T16:13:36Z", false},
		{"pre-epoch negative", "-1", "1969-12-31T23:59:59Z", false},
		{"empty rejected", "", "", true},
		{"non-numeric rejected", "abc", "", true},
		{"fractional rejected", "1.5", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(tt.value, []Step{{Name: "unixdate"}})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("unixdate(%q) = %q, want error", tt.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unixdate failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("unixdate(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestValidFilename(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"windows chars", `a/b:c*?"<>|`, "a_b_c______"},
		{"control chars", "a\x00b\x1fc", "a_b_c"},
		{"leading/trailing dots", "...foo...", "foo"},
		{"dot guard", "...", "_"},
		{"normal", "Show.S01E01.720p", "Show.S01E01.720p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(tt.value, []Step{{Name: "validfilename"}})
			if err != nil {
				t.Fatalf("validfilename failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("validfilename = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAndMatch(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		keywords string
		want     bool
	}{
		{"all keywords present", "The Matrix 1999 1080p", "matrix 1999", true},
		{"keyword missing", "The Matrix 1999 1080p", "avatar", false},
		{"case insensitive", "The Matrix 1999", "MATRIX", true},
		{"empty keywords", "anything", "", true},
		{"multiple keywords all present", "Inception 2010 720p BluRay", "inception 2010", true},
		{"multiple keywords one missing", "Inception 2010 720p BluRay", "inception avatar", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyAndMatch(tt.text, tt.keywords)
			if got != tt.want {
				t.Errorf("ApplyAndMatch(%q, %q) = %v, want %v", tt.text, tt.keywords, got, tt.want)
			}
		})
	}
}

func TestTimeAgo(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"3 hours ago", "3 hours ago", false},
		{"1 day ago", "1 day ago", false},
		{"just now", "just now", false},
		{"yesterday", "yesterday", false},
		{"2 weeks ago", "2 weeks ago", false},
		{"1 month ago", "1 month ago", false},
		{"invalid", "something invalid", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(tt.value, []Step{{Name: "timeago"}})
			if (err != nil) != tt.wantErr {
				t.Fatalf("timeago error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got == "" {
				t.Error("timeago returned empty string")
			}
		})
	}
}

func TestFuzzyTime(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"today", "today", false},
		{"yesterday", "yesterday", false},
		{"just now", "just now", false},
		{"french today", "aujourd'hui", false},
		{"french yesterday", "hier", false},
		{"portuguese today", "hoje", false},
		{"standard date", "2024-01-15", false},
		{"invalid", "not a date at all xyz", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(tt.value, []Step{{Name: "fuzzytime"}})
			if (err != nil) != tt.wantErr {
				t.Fatalf("fuzzytime error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got == "" {
				t.Error("fuzzytime returned empty string")
			}
		})
	}
}

func TestDiacritics(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"cafe", "café", "cafe"},
		{"czech", "Příliš žluťoučký", "Prilis zlutoucky"},
		{"no diacritics", "hello", "hello"},
		{"mixed", "Ñoño café", "Nono cafe"},
		{"french", "résumé", "resume"},
		{"german", "über", "uber"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(tt.value, []Step{{Name: "diacritics"}})
			if err != nil {
				t.Fatalf("diacritics error: %v", err)
			}
			if got != tt.want {
				t.Errorf("diacritics(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name  string
		value string
		args  []string
		want  string
	}{
		{"match", "Action", []string{"Action, Comedy, Drama"}, "Action"},
		{"match trimmed", " Action ", []string{"Action, Comedy, Drama"}, "Action"},
		{"no match", "Horror", []string{"Action, Comedy, Drama"}, ""},
		{"empty value", "", []string{"Action, Comedy"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(tt.value, []Step{{Name: "validate", Args: tt.args}})
			if err != nil {
				t.Fatalf("validate error: %v", err)
			}
			if got != tt.want {
				t.Errorf("validate(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestAndMatch_FieldLevelError(t *testing.T) {
	_, err := Apply("test value", []Step{{Name: "andmatch"}})
	if err == nil {
		t.Fatal("expected error when andmatch used in field-level")
	}
	if !strings.Contains(err.Error(), "row-level filter") {
		t.Errorf("expected row-level error message, got: %v", err)
	}
}

func TestJsonParse_NestedPath(t *testing.T) {
	tests := []struct {
		name string
		json string
		path string
		want string
	}{
		{"flat field", `{"title":"Ubuntu"}`, "title", "Ubuntu"},
		{"nested path", `{"result":{"data":{"title":"Ubuntu"}}}`, "result.data.title", "Ubuntu"},
		{"array index", `{"items":[{"name":"a"},{"name":"b"}]}`, "items.0.name", "a"},
		{"missing path", `{"title":"Ubuntu"}`, "missing", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(tt.json, []Step{{Name: "jsonparse", Args: []string{tt.path}}})
			if tt.want == "" {
				if err == nil {
					t.Fatal("expected error for missing path")
				}
				return
			}
			if err != nil {
				t.Fatalf("jsonparse error: %v", err)
			}
			if got != tt.want {
				t.Errorf("jsonparse(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestFuzzyTime_OrdinalSuffixes(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"1st", "Jan 1st '26", false},
		{"2nd", "Jan 2nd '26", false},
		{"3rd", "Jan 3rd '26", false},
		{"21st", "Jan 21st '26", false},
		{"22nd", "Jan 22nd '26", false},
		{"23rd", "Jan 23rd '26", false},
		{"28th with time", "Jul. 28th '26 09:20", false},
		{"no suffix", "Jan 5 '26", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(tt.value, []Step{{Name: "fuzzytime"}})
			if (err != nil) != tt.wantErr {
				t.Fatalf("fuzzytime error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got == "" {
				t.Error("fuzzytime returned empty string")
			}
		})
	}
}

func TestDateParse_Microseconds(t *testing.T) {
	// Test date formats with microseconds (API Abnormal)
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"RFC3339 with microseconds", "2026-07-28T21:06:19.348985+00:01", "2026-07-28T21:06:19+00:01"},
		{"ISO without timezone", "2026-07-10T07:51:24.880001", "2026-07-10T07:51:24Z"},
		{"Simple datetime", "2026-01-15T10:30:00", "2026-01-15T10:30:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(tt.value, []Step{{Name: "dateparse"}})
			if err != nil {
				t.Fatalf("dateparse error: %v", err)
			}
			if got != tt.want {
				t.Errorf("dateparse(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestReReplace_InvalidRegexRepeated(t *testing.T) {
	// The invalid pattern must be re-tested (via the failure cache) without
	// ever panicking, even when called multiple times on the same pattern.
	for i := 0; i < 5; i++ {
		got, err := Apply("test", []Step{{Name: "re_replace", Args: []string{`[invalid`, ""}}})
		if err != nil {
			t.Fatalf("call %d: re_replace error: %v", i, err)
		}
		if got != "test" {
			t.Errorf("call %d: got %q, want %q", i, got, "test")
		}
	}

	// regexpExtract must return an error (not panic) on the same invalid
	// pattern cached by the previous re_replace call.
	if _, err := Apply("test", []Step{{Name: "regexp", Args: []string{`[invalid`}}}); err == nil {
		t.Fatal("expected error for invalid regexp pattern")
	}
}

func BenchmarkReReplace_CachedPattern(b *testing.B) {
	steps := []Step{{Name: "re_replace", Args: []string{`\d+`, "#"}}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Apply("item 12345 title", steps); err != nil {
			b.Fatal(err)
		}
	}
}

func TestReReplace_InvalidRegex(t *testing.T) {
	// Test that invalid regex patterns (PCRE features) return original value
	tests := []struct {
		name  string
		value string
		regex string
		want  string
	}{
		{"lookahead", "MULTI FRENCH", `(?i)MULTI(?!.*FRENCH)`, "MULTI FRENCH"},
		{"lookbehind", "test123", `(?<=test)123`, "test123"},
		{"invalid syntax", "test", `[invalid`, "test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(tt.value, []Step{{Name: "re_replace", Args: []string{tt.regex, ""}}})
			if err != nil {
				t.Fatalf("re_replace error: %v", err)
			}
			if got != tt.want {
				t.Errorf("re_replace(%q, %q) = %q, want %q", tt.value, tt.regex, got, tt.want)
			}
		})
	}
}
