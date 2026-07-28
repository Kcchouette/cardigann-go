// Package filters implémente le pipeline de filtres de post-traitement
// Cardigann (distinct du moteur de template) appliqué aux valeurs de champs
// extraites (`fields.<name>.filters`).
//
// Liste volontairement réduite à un socle de filtres réellement fréquents
// dans le corpus (Slice B du MVP) ; d'autres filtres seront ajoutés au fil
// des définitions réelles rencontrées (Slice E).
package filters

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// FilterFunc applique un filtre nommé à une valeur, avec ses arguments positionnels.
type FilterFunc func(value string, args []string) (string, error)

// Registry est la table des filtres disponibles, indexée par nom Cardigann.
var Registry = map[string]FilterFunc{
	"trim":        trim,
	"tolower":     toLower,
	"toupper":     toUpper,
	"strip":       strip,
	"replace":     replace,
	"re_replace":  reReplace,
	"split":       split,
	"append":      appendFilter,
	"prepend":     prependFilter,
	"regexp":      regexpExtract,
	"urldecode":   urlDecode,
	"urlencode":   urlEncode,
	"htmldecode":  htmlDecode,
	"querystring": queryString,
	"dateparse":   dateParse,
	"jsonparse":   jsonParse,
}

// Apply exécute une chaîne de filtres sur une valeur, dans l'ordre.
func Apply(value string, steps []Step) (string, error) {
	for _, step := range steps {
		fn, ok := Registry[step.Name]
		if !ok {
			return "", fmt.Errorf("unknown filter %q", step.Name)
		}
		var err error
		value, err = fn(value, step.Args)
		if err != nil {
			return "", fmt.Errorf("filter %q: %w", step.Name, err)
		}
	}
	return value, nil
}

// Step représente une étape du pipeline (nom de filtre + arguments positionnels).
type Step struct {
	Name string
	Args []string
}

func trim(v string, _ []string) (string, error)    { return strings.TrimSpace(v), nil }
func toLower(v string, _ []string) (string, error) { return strings.ToLower(v), nil }
func toUpper(v string, _ []string) (string, error) { return strings.ToUpper(v), nil }
func strip(v string, args []string) (string, error) {
	if len(args) == 0 {
		return strings.TrimSpace(v), nil
	}
	return strings.ReplaceAll(v, args[0], ""), nil
}

func replace(v string, args []string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("replace requires 2 args, got %d", len(args))
	}
	return strings.ReplaceAll(v, args[0], args[1]), nil
}

func reReplace(v string, args []string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("re_replace requires 2 args, got %d", len(args))
	}
	re, err := regexp.Compile(args[0])
	if err != nil {
		return "", fmt.Errorf("invalid regexp %q: %w", args[0], err)
	}
	return re.ReplaceAllString(v, args[1]), nil
}

func split(v string, args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("split requires at least 1 arg")
	}
	sep := args[0]
	idx := 0
	if len(args) > 1 {
		parsed, err := strconv.Atoi(args[1])
		if err != nil {
			return "", fmt.Errorf("invalid split index %q: %w", args[1], err)
		}
		idx = parsed
	}
	parts := strings.Split(v, sep)
	if idx < 0 {
		idx += len(parts)
	}
	if idx < 0 || idx >= len(parts) {
		return "", fmt.Errorf("split index %d out of range (%d parts)", idx, len(parts))
	}
	return parts[idx], nil
}

func appendFilter(v string, args []string) (string, error) {
	if len(args) < 1 {
		return v, nil
	}
	return v + args[0], nil
}

func prependFilter(v string, args []string) (string, error) {
	if len(args) < 1 {
		return v, nil
	}
	return args[0] + v, nil
}

func regexpExtract(v string, args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("regexp requires 1 arg")
	}
	re, err := regexp.Compile(args[0])
	if err != nil {
		return "", fmt.Errorf("invalid regexp %q: %w", args[0], err)
	}
	m := re.FindStringSubmatch(v)
	if m == nil {
		return "", fmt.Errorf("regexp %q did not match %q", args[0], v)
	}
	if len(m) > 1 {
		return m[1], nil
	}
	return m[0], nil
}

func urlDecode(v string, _ []string) (string, error) {
	return url.QueryUnescape(v)
}

func urlEncode(v string, _ []string) (string, error) {
	return url.QueryEscape(v), nil
}

func htmlDecode(v string, _ []string) (string, error) {
	return html.UnescapeString(v), nil
}

func queryString(v string, args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("querystring requires 1 arg (param name)")
	}
	u, err := url.Parse(v)
	if err != nil {
		return "", fmt.Errorf("invalid url %q: %w", v, err)
	}
	return u.Query().Get(args[0]), nil
}

// dateLayouts liste les formats de date rencontrés dans les définitions réelles.
// Choix déterministe (pas de dépendance non maintenue type araddon/dateparse) :
// on tente chaque layout dans l'ordre jusqu'à un succès.
var dateLayouts = []string{
	time.RFC1123Z,
	time.RFC1123,
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02",
	"02 Jan 2006",
	"Mon, 02 Jan 2006 15:04:05 -0700",
}

func dateParse(v string, args []string) (string, error) {
	layouts := dateLayouts
	if len(args) > 0 {
		layouts = append([]string{args[0]}, dateLayouts...)
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t.Format(time.RFC3339), nil
		}
	}
	return "", fmt.Errorf("dateparse: no layout matched %q", v)
}

func jsonParse(v string, args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("jsonparse requires 1 arg (field path)")
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(v), &data); err != nil {
		return "", fmt.Errorf("invalid json: %w", err)
	}
	val, ok := data[args[0]]
	if !ok {
		return "", fmt.Errorf("jsonparse: field %q not found", args[0])
	}
	return fmt.Sprintf("%v", val), nil
}
