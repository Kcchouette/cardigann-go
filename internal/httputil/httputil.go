// Package httputil fournit des utilitaires HTTP partagés entre les packages
// engine et login pour éviter la duplication de logique commune.
package httputil

import "strings"

// ResolveURL résout un chemin relatif ou absolu par rapport à une URL de base.
// Si le path commence par "http://" ou "https://", il est retourné tel quel.
// Sinon, il est concaténé à la base en normalisant les slashes.
func ResolveURL(baseURL, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}
