// Package defs synchronise les définitions Cardigann depuis le dépôt GitHub
// Prowlarr/Indexers (jamais vendorisées ni redistribuées, cf. disclaimer
// légal — uniquement mises en cache localement pour l'utilisateur final).
package defs

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
)

// RawDefinition est une définition Cardigann encore non parsée, telle que
// récupérée depuis le tarball du dépôt.
type RawDefinition struct {
	ID   string // dérivé du nom de fichier, ex: "1337x" pour "1337x.yml"
	YAML string
	SHA  string // sha256 hex du contenu, pour détection de changement/cache
}

// Fetcher télécharge les définitions Cardigann d'une version donnée
// (ex: "v11") depuis un tarball GitHub (`codeload.github.com`), ce qui évite
// la limite de rate-limit sévère de l'API GitHub Contents (60 req/h non-authentifié).
type Fetcher struct {
	HTTPClient *http.Client
	Repo       string // "Prowlarr/Indexers"
	Ref        string // branche/tag, ex: "master"

	// URLOverride, si non vide, remplace l'URL calculée du tarball —
	// utilisé par les tests pour pointer vers un serveur httptest local
	// plutôt que vers le vrai GitHub.
	URLOverride string
}

// NewFetcher construit un Fetcher avec les valeurs par défaut du dépôt réel.
func NewFetcher() *Fetcher {
	return &Fetcher{
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		Repo:       "Prowlarr/Indexers",
		Ref:        "master",
	}
}

func (f *Fetcher) tarballURL() string {
	if f.URLOverride != "" {
		return f.URLOverride
	}
	return fmt.Sprintf("https://codeload.github.com/%s/tar.gz/refs/heads/%s", f.Repo, f.Ref)
}

// FetchVersion télécharge le tarball complet et n'en extrait que les
// définitions YAML situées sous `definitions/<version>/`.
func (f *Fetcher) FetchVersion(ctx context.Context, version string) ([]RawDefinition, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.tarballURL(), nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := f.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading tarball: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected http status %d while downloading tarball", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("opening gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	// Les tarballs GitHub ont un dossier racine unique du type
	// "Indexers-<sha>/", donc on matche sur le suffixe du chemin plutôt que
	// sur un préfixe absolu.
	wantSuffix := path.Join("definitions", version) + "/"

	var results []RawDefinition
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar entry: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		normalized := strings.ReplaceAll(hdr.Name, "\\", "/")
		idx := strings.Index(normalized, wantSuffix)
		if idx < 0 {
			continue
		}
		rel := normalized[idx+len(wantSuffix):]
		if rel == "" || strings.Contains(rel, "/") {
			continue // sous-dossier imprévu, on ignore
		}
		ext := path.Ext(rel)
		if ext != ".yml" && ext != ".yaml" {
			continue
		}

		content, err := io.ReadAll(io.LimitReader(tr, 4<<20)) // 4 MiB par définition, garde-fou.
		if err != nil {
			return nil, fmt.Errorf("reading definition %q: %w", rel, err)
		}

		sum := sha256.Sum256(content)
		results = append(results, RawDefinition{
			ID:   strings.TrimSuffix(rel, ext),
			YAML: string(content),
			SHA:  hex.EncodeToString(sum[:]),
		})
	}

	return results, nil
}
