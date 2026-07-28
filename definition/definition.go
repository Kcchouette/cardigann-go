// Package model contient les structures Go correspondant au format YAML
// Cardigann (schéma v11) utilisé par Prowlarr/Indexers.
//
// Volontairement tolérant : la plupart des champs sont optionnels
// (`omitempty` côté YAML) car toutes les définitions n'utilisent pas
// toutes les fonctionnalités du schéma.
package definition

// IndexerDefinition représente une définition Cardigann complète (v11).
type IndexerDefinition struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Language    string   `yaml:"language"`
	Type        string   `yaml:"type"` // public | private | semi-private | usenet
	Encoding    string   `yaml:"encoding"`
	Links       []string `yaml:"links"`
	LegacyLinks []string `yaml:"legacylinks"`

	RequestDelay float64 `yaml:"requestDelay"`

	Caps     Caps           `yaml:"caps"`
	Settings []Setting      `yaml:"settings"`
	Login    Login          `yaml:"login"`
	Search   Search         `yaml:"search"`
	Download *Download      `yaml:"download,omitempty"`
	Extra    map[string]any `yaml:",inline"`
}

// IsUsenet indique si la définition produit des résultats usenet plutôt que torrent.
// NB: le mécanisme exact observé dans le corpus réel v11 associe le protocole usenet
// aux catégories Newznab (2000-8999) plutôt qu'à `type` (qui désigne le niveau
// d'accès public/private/semi-private). On expose donc une heuristique dédiée ici,
// affinée en Slice F au vu des vraies définitions usenet analysées.
func (d IndexerDefinition) IsUsenet() bool {
	return d.Type == "usenet"
}

// Caps décrit les capacités de recherche/catégories de l'indexeur.
type Caps struct {
	CategoryMappings []CategoryMapping   `yaml:"categorymappings"`
	Modes            map[string][]string `yaml:"modes"`
	AllowRawSearch   bool                `yaml:"allowrawsearch"`
}

// CategoryMapping relie une catégorie native de l'indexeur à une catégorie Torznab/Newznab standard.
type CategoryMapping struct {
	ID      string `yaml:"id"`
	Cat     string `yaml:"cat"`
	Desc    string `yaml:"desc"`
	Default bool   `yaml:"default"`
}

// Setting décrit un champ de configuration exposé à l'utilisateur (identifiants, options...).
type Setting struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"` // text | password | checkbox | select | info | ...
	Label   string `yaml:"label"`
	Default string `yaml:"default"`
}

// Login décrit le mécanisme d'authentification de l'indexeur.
type Login struct {
	Method    string            `yaml:"method"` // form | cookie | get | oneurl
	Login     string            `yaml:"login"`
	Inputs    map[string]string `yaml:"inputs"`
	Selectors map[string]string `yaml:"selectors"`
	Error     []ErrorSelector   `yaml:"error"`
	Test      *Test             `yaml:"test,omitempty"`
}

// ErrorSelector décrit un sélecteur permettant de détecter un échec de login.
type ErrorSelector struct {
	Selector string          `yaml:"selector"`
	Message  MessageSelector `yaml:"message,omitempty"`
}

// MessageSelector décrit comment extraire le message d'erreur associé.
type MessageSelector struct {
	Selector string `yaml:"selector"`
}

// Test décrit un moyen de vérifier qu'une session est valide (post-login).
type Test struct {
	Path     string `yaml:"path"`
	Selector string `yaml:"selector"`
}

// Search décrit comment effectuer une recherche et parser les résultats.
type Search struct {
	Paths           []SearchPath     `yaml:"paths"`
	Rows            Rows             `yaml:"rows"`
	Fields          map[string]Field `yaml:"fields"`
	KeywordsFilters []FilterStep     `yaml:"keywordsfilters"`
	Error           []ErrorSelector  `yaml:"error"`
}

// SearchPath décrit un template d'URL/paramètres de requête de recherche.
type SearchPath struct {
	Path     string            `yaml:"path"`
	Method   string            `yaml:"method"` // get | post
	Inputs   map[string]string `yaml:"inputs"`
	Response ResponseInfo      `yaml:"response"`
}

// ResponseInfo décrit le type de contenu attendu de la réponse (html par défaut, json, xml).
type ResponseInfo struct {
	Type string `yaml:"type"`
}

// Rows décrit comment sélectionner les lignes de résultats dans le document de réponse.
type Rows struct {
	Selector string `yaml:"selector"`
	After    int    `yaml:"after"`
}

// Field décrit comment extraire et transformer une valeur pour une ligne de résultat.
type Field struct {
	Selector  string       `yaml:"selector"`
	Attribute string       `yaml:"attribute"`
	Text      string       `yaml:"text"` // valeur littérale/template au lieu d'un sélecteur
	Optional  bool         `yaml:"optional"`
	Filters   []FilterStep `yaml:"filters"`
}

// FilterStep représente une étape du pipeline de filtres appliqué post-extraction
// (ex: `re_replace`, `split`, `dateparse`...), avec ses arguments positionnels.
type FilterStep struct {
	Name string   `yaml:"name"`
	Args []string `yaml:"args"`
}

// Download décrit comment résoudre le lien réel de téléchargement (torrent/nzb) à partir d'une ligne.
type Download struct {
	Selectors []DownloadSelector `yaml:"selectors,omitempty"`
	Method    string             `yaml:"method,omitempty"`
}

// DownloadSelector décrit une étape de résolution du lien de téléchargement (ex: suivre une page intermédiaire).
type DownloadSelector struct {
	Selector  string `yaml:"selector"`
	Attribute string `yaml:"attribute"`
}
