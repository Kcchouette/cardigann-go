package httputil

import "testing"

func TestResolveURL(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		path     string
		expected string
	}{
		{"absolute http", "https://example.com", "http://other.com/path", "http://other.com/path"},
		{"absolute https", "https://example.com", "https://other.com/path", "https://other.com/path"},
		{"relative path", "https://example.com", "/api/search", "https://example.com/api/search"},
		{"relative no leading slash", "https://example.com", "api/search", "https://example.com/api/search"},
		{"base with trailing slash", "https://example.com/", "/api/search", "https://example.com/api/search"},
		{"base without trailing slash", "https://example.com", "api/search", "https://example.com/api/search"},
		{"empty path", "https://example.com", "", "https://example.com/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveURL(tt.baseURL, tt.path)
			if got != tt.expected {
				t.Errorf("ResolveURL(%q, %q) = %q, want %q", tt.baseURL, tt.path, got, tt.expected)
			}
		})
	}
}
