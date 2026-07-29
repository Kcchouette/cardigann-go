// Package httputil provides shared HTTP utilities between the engine and login
// packages to avoid duplicating common logic.
package httputil

import "strings"

// ResolveURL resolves a relative or absolute path against a base URL.
// If the path starts with "http://" or "https://", it is returned as-is.
// Otherwise it is concatenated to the base with normalized slashes.
func ResolveURL(baseURL, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}
