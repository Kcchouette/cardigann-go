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
		name   string
		json   string
		path   string
		want   string
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
