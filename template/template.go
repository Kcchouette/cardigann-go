// Package template fournit le moteur de rendu des templates Cardigann
// (`{{ .Keywords }}`, `{{ if .Config.xxx }}...{{ end }}`, etc.), qui est
// nativement compatible avec la syntaxe Go text/template.
package template

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"text/template"
)

// Context est le contexte de rendu exposé aux templates Cardigann.
type Context struct {
	Config      map[string]string // valeurs de configuration utilisateur (settings)
	Keywords    string            // termes de recherche bruts
	Categories  []string          // IDs native des catégories mappées depuis les Newznab IDs
	Query       map[string]string // paramètres de requête additionnels (catégories, saison, etc.)
	Result      map[string]string // valeurs déjà extraites pour la ligne de résultat courante
	DownloadUri *url.URL          // URL de téléchargement résolue (utilisable dans les templates)
}

// Render exécute tmplStr comme un template Cardigann avec le contexte donné et
// renvoie le résultat sous forme de chaîne.
func Render(tmplStr string, ctx Context) (string, error) {
	tmpl, err := template.New("cardigann").Funcs(FuncMap()).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing template %q: %w", tmplStr, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("executing template %q: %w", tmplStr, err)
	}
	return buf.String(), nil
}

// FuncMap enregistre les fonctions custom disponibles dans les templates
// Cardigann (distinctes des *filtres* de champ, cf. package filters).
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"ToLower": strings.ToLower,
		"ToUpper": strings.ToUpper,
	}
}
