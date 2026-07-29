// Package template fournit le moteur de rendu des templates Cardigann
// (`{{ .Keywords }}`, `{{ if .Config.xxx }}...{{ end }}`, etc.), qui est
// nativement compatible avec la syntaxe Go text/template.
package template

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"sync"
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

// templateCache mémorise les *template.Template déjà compilés, indexés par
// leur source brute, pour éviter de reparser le même template Cardigann à
// chaque appel de Render (appelé par ligne de résultat x champ x passe
// d'extraction). Seuls les parses réussis sont mis en cache : un template
// invalide est reparsé (et échoue à nouveau) à chaque appel, ce qui reste
// correct et évite de cacher un état d'erreur.
//
// Hypothèse de dimensionnement : les chaînes de template proviennent de
// définitions Cardigann (YAML) chargées une fois, donc leur cardinalité est
// bornée par le nombre de champs/paths/headers du corpus de définitions.
// Cette fonction étant exportée, un appelant qui générerait des templates
// dynamiquement à partir d'entrées non bornées (ex: valeurs utilisateur)
// ferait grossir ce cache indéfiniment ; ce n'est pas l'usage prévu.
var templateCache sync.Map // string -> *template.Template

// funcMap est statique : FuncMap() ne dépend d'aucun état, autant la
// construire une seule fois plutôt qu'à chaque appel de Render.
var funcMap = template.FuncMap{
	"ToLower": strings.ToLower,
	"ToUpper": strings.ToUpper,
}

// Render exécute tmplStr comme un template Cardigann avec le contexte donné et
// renvoie le résultat sous forme de chaîne.
func Render(tmplStr string, ctx Context) (string, error) {
	tmpl, err := compile(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing template %q: %w", tmplStr, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("executing template %q: %w", tmplStr, err)
	}
	return buf.String(), nil
}

// compile retourne le *template.Template compilé pour tmplStr, en le
// réutilisant depuis templateCache si déjà vu. Un *template.Template déjà
// parsé est sûr pour des Execute concurrents, ce qui rend ce cache partagé
// utilisable par plusieurs goroutines.
func compile(tmplStr string) (*template.Template, error) {
	if cached, ok := templateCache.Load(tmplStr); ok {
		return cached.(*template.Template), nil
	}
	tmpl, err := template.New("cardigann").Funcs(funcMap).Parse(tmplStr)
	if err != nil {
		return nil, err
	}
	// LoadOrStore pour éviter d'écraser un template déjà stocké par une
	// autre goroutine avec le même tmplStr (les deux sont équivalents,
	// mais on garde une seule instance canonique).
	actual, _ := templateCache.LoadOrStore(tmplStr, tmpl)
	return actual.(*template.Template), nil
}

// FuncMap enregistre les fonctions custom disponibles dans les templates
// Cardigann (distinctes des *filtres* de champ, cf. package filters).
func FuncMap() template.FuncMap {
	return funcMap
}
