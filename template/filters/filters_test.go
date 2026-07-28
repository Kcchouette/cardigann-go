package filters

import "testing"

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
