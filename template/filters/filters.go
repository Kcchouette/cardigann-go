// Package filters implémente le pipeline de filtres de post-traitement
// Cardigann (distinct du moteur de template) appliqué aux valeurs de champs
// extraites (`fields.<name>.filters`).
//
// Liste volontairement réduite à un socle de filtres réellement fréquents
// dans le corpus (Slice B du MVP) ; d'autres filtres seront ajoutés au fil
// des définitions réelles rencontrées (Slice E).
package filters

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/tidwall/gjson"
	"golang.org/x/text/unicode/norm"
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
	"andmatch":    andMatch,
	"timeago":     timeAgo,
	"fuzzytime":   fuzzyTime,
	"diacritics":  diacritics,
	"validate":    validate,
	"timeparse":   dateParse, // alias de dateparse
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
	result := gjson.Get(v, args[0])
	if !result.Exists() {
		return "", fmt.Errorf("jsonparse: path %q not found", args[0])
	}
	return result.String(), nil
}

// andMatch est un filtre row-level qui vérifie que tous les mots-clés sont
// présents dans la valeur. Il ne peut pas être utilisé en field-level car
// il nécessite les mots-clés de la recherche (passés via ApplyAndMatch).
func andMatch(v string, args []string) (string, error) {
	return "", fmt.Errorf("andmatch is a row-level filter and cannot be used in field filters; use it in rows.filters instead")
}

// ApplyAndMatch vérifie que tous les mots-clés sont présents dans la valeur.
// Utilisé par le moteur pour les filtres row-level de type andmatch.
func ApplyAndMatch(v string, keywords string) bool {
	if keywords == "" {
		return true
	}
	lower := strings.ToLower(v)
	for _, kw := range strings.Fields(keywords) {
		if !strings.Contains(lower, strings.ToLower(kw)) {
			return false
		}
	}
	return true
}

// timeAgo convertit des expressions de temps relatif en timestamps RFC3339.
// Supporte : "X minutes/hours/days/weeks/months/years ago",
// "just now", "yesterday".
func timeAgo(v string, _ []string) (string, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	now := time.Now()

	if v == "just now" || v == "right now" {
		return now.Format(time.RFC3339), nil
	}
	if v == "yesterday" {
		return time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, now.Location()).Format(time.RFC3339), nil
	}
	if v == "today" {
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Format(time.RFC3339), nil
	}

	// Parse "X unit(s) ago"
	re := regexp.MustCompile(`^(\d+)\s+(minute|hour|day|week|month|year)s?\s*(?:ago)?$`)
	m := re.FindStringSubmatch(v)
	if m == nil {
		return "", fmt.Errorf("timeago: cannot parse %q", v)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return "", fmt.Errorf("timeago: invalid number %q: %w", m[1], err)
	}

	var d time.Duration
	switch m[2] {
	case "minute":
		d = time.Duration(n) * time.Minute
	case "hour":
		d = time.Duration(n) * time.Hour
	case "day":
		d = time.Duration(n) * 24 * time.Hour
	case "week":
		d = time.Duration(n) * 7 * 24 * time.Hour
	case "month":
		d = time.Duration(n) * 30 * 24 * time.Hour
	case "year":
		d = time.Duration(n) * 365 * 24 * time.Hour
	}

	return now.Add(-d).Format(time.RFC3339), nil
}

// fuzzyTime convertit des expressions temporelles floues en timestamps RFC3339.
// Supporte : "today", "yesterday", "just now", et les formats de date abrégés.
func fuzzyTime(v string, _ []string) (string, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	now := time.Now()

	switch v {
	case "today", "hoje", "aujourd'hui", "heute", "oggi":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Format(time.RFC3339), nil
	case "yesterday", "ontem", "hier", "gestern", "ieri":
		return time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, now.Location()).Format(time.RFC3339), nil
	case "just now", "right now", "agora", "maintenant", "jetzt", "adesso":
		return now.Format(time.RFC3339), nil
	}

	// Try standard date layouts (same as dateParse).
	original := v
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, original); err == nil {
			return t.Format(time.RFC3339), nil
		}
	}

	// Strip ordinal suffixes (st, nd, rd, th) and retry.
	stripped := stripOrdinalSuffixes(v)
	if stripped != v {
		for _, layout := range dateLayouts {
			if t, err := time.Parse(layout, stripped); err == nil {
				return t.Format(time.RFC3339), nil
			}
		}
	}

	// Try fuzzy formats with stripped input: "Jan 2 '06 15:04", "Jan 2 '06", etc.
	fuzzyLayouts := []string{
		"Jan 2 '06 15:04",
		"Jan 2 '06",
		"Jan. 2 '06 15:04",
		"Jan. 2 '06",
		"2 Jan 2006 15:04",
		"2 Jan 2006",
	}
	for _, layout := range fuzzyLayouts {
		if t, err := time.Parse(layout, stripped); err == nil {
			return t.Format(time.RFC3339), nil
		}
	}

	return "", fmt.Errorf("fuzzytime: cannot parse %q", original)
}

// stripOrdinalSuffixes supprime les suffixes ordinaux (st, nd, rd, th) d'une chaîne.
// Ex: "Jan 1st '26" → "Jan 1 '26", "Jul. 28th '26" → "Jul. 28 '26"
func stripOrdinalSuffixes(s string) string {
	re := regexp.MustCompile(`(\d)(st|nd|rd|th)\b`)
	return re.ReplaceAllString(s, "$1")
}

// diacritics supprime les diacritiques (accents, cédilles, etc.) d'une chaîne.
// Décompose d'abord en NFD (é → e + ́) puis supprime les marqueurs de combinaison.
func diacritics(v string, _ []string) (string, error) {
	// Decompose to NFD: é (U+00E9) → e (U+0065) + combining acute (U+0301)
	decomposed := norm.NFD.String(v)
	var b strings.Builder
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) { // Mark, Nonspacing
			continue
		}
		b.WriteRune(r)
	}
	return b.String(), nil
}

// validate vérifie que la valeur fait partie d'une allowlist séparée par virgules.
// Retourne la valeur si elle matche, chaîne vide sinon.
func validate(v string, args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("validate requires 1 arg (comma-separated allowlist)")
	}
	v = strings.TrimSpace(v)
	for _, allowed := range strings.Split(args[0], ",") {
		if strings.TrimSpace(allowed) == v {
			return v, nil
		}
	}
	return "", nil
}
