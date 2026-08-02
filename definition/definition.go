// Package definition contains the Go structs corresponding to the Cardigann
// YAML format (schema v11) used by Prowlarr/Indexers.
//
// Intentionally lenient: most fields are optional (`omitempty` on the YAML side)
// because not all definitions use every feature of the schema.
package definition

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// IndexerDefinition represents a complete Cardigann definition (v11).
type IndexerDefinition struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Language    string   `yaml:"language"`
	Type        string   `yaml:"type"` // public | private | semi-private | usenet | ddl | streaming
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

// IsUsenet reports whether the definition produces usenet results rather than torrent.
//
// The heuristic uses two complementary signals:
//  1. The type: "usenet" field (direct signal, but rarely used in the v11 corpus).
//  2. The presence of Newznab categories in the 2000–8999 range (standard usenet signal),
//     with a guard against false positives: at least one category whose description
//     contains an explicit usenet keyword (e.g. "NZB", "Usenet") is required,
//     or ALL categorymappings have IDs in this range (purely usenet indexer).
func (d IndexerDefinition) IsUsenet() bool {
	if d.Type == "usenet" {
		return true
	}
	if len(d.Caps.CategoryMappings) == 0 {
		return false
	}

	usenetKeywords := []string{"usenet", "nzb", "newznab"}
	allInUsenetRange := true
	hasUsenetKeyword := false

	for _, cm := range d.Caps.CategoryMappings {
		catID := 0
		for _, r := range cm.Cat {
			if r >= '0' && r <= '9' {
				catID = catID*10 + int(r-'0')
			} else {
				catID = 0
				break
			}
		}
		if catID < 2000 || catID > 8999 {
			allInUsenetRange = false
		}
		desc := strings.ToLower(cm.Desc)
		for _, kw := range usenetKeywords {
			if strings.Contains(desc, kw) {
				hasUsenetKeyword = true
				break
			}
		}
	}

	// Purely usenet indexer (all categories in the Newznab range)
	// or at least one category with an explicit usenet keyword.
	return allInUsenetRange || hasUsenetKeyword
}

// Validate returns non-fatal warnings about the definition, checked against
// its protocol. It never returns an error: the v11 corpus must stay fully
// usable even for definitions that omit optional fields.
func (d IndexerDefinition) Validate() []string {
	var warns []string

	if _, legacy := d.Login.Extra["login"]; legacy {
		warns = append(warns, "login block uses legacy 'login:' key; use 'path:' (the legacy key is ignored)")
	}

	switch d.Type {
	case "usenet", "ddl", "streaming":
		// explicit protocols are checked below
	case "", "public", "semi-private", "private":
		// documented tracker types: no warning
	default:
		warns = append(warns, fmt.Sprintf("unknown type %q, treated as torrent", d.Type))
	}

	hasDownload := false
	for name := range d.Search.Fields {
		if name == "download" || name == "downloadurl" || name == "magnet" || name == "infohash" {
			hasDownload = true
			break
		}
	}
	if d.Download != nil {
		hasDownload = true
	}

	if !hasDownload {
		switch d.Type {
		case "usenet", "ddl", "streaming":
			warns = append(warns, fmt.Sprintf("%s definition without download field, magnet/infohash field or download block", d.Type))
		}
	}
	return warns
}

// Caps describes the search/category capabilities of the indexer.
type Caps struct {
	CategoryMappings []CategoryMapping   `yaml:"categorymappings"`
	Categories       StringMap           `yaml:"categories,omitempty"` // native -> newznab representation
	Modes            map[string][]string `yaml:"modes"`
	AllowRawSearch   bool                `yaml:"allowrawsearch"`
}

// CategoryMapping maps a native indexer category to a standard Torznab/Newznab category.
type CategoryMapping struct {
	ID      string `yaml:"id"`
	Cat     string `yaml:"cat"`
	Desc    string `yaml:"desc"`
	Default bool   `yaml:"default"`
}

// Setting describes a configuration field exposed to the user (credentials, options, etc.).
type Setting struct {
	Name    string    `yaml:"name"`
	Type    string    `yaml:"type"` // text | password | checkbox | select | info | ...
	Label   string    `yaml:"label"`
	Default string    `yaml:"default"`
	Options StringMap `yaml:"options,omitempty"` // select choices (display -> value)
}

// Login describes the authentication mechanism of the indexer.
type Login struct {
	Method         string                   `yaml:"method"`
	Path           string                   `yaml:"path"` // login page path (v11 canonical key)
	Inputs         map[string]string        `yaml:"inputs"`
	SelectorInputs map[string]SelectorField `yaml:"selectorinputs,omitempty"`
	Selectors      bool                     `yaml:"selectors,omitempty"` // inputs keys are CSS selectors against the login page
	Form           string                   `yaml:"form,omitempty"`
	Error          []ErrorSelector          `yaml:"error"`
	Test           *Test                    `yaml:"test,omitempty"`
	Headers        map[string][]string      `yaml:"headers,omitempty"`
	Cookies        []string                 `yaml:"cookies,omitempty"`
	SubmitPath     string                   `yaml:"submitpath,omitempty"`
	Captcha        *Captcha                 `yaml:"captcha,omitempty"`
	Extra          map[string]any           `yaml:",inline"`
}

// Captcha describes a login captcha block (type image|text, selector, input).
// Interactive solving is not supported; the engine reports a clear error.
type Captcha struct {
	Type     string `yaml:"type"`
	Selector string `yaml:"selector"`
	Input    string `yaml:"input"`
}

// ErrorSelector describes a selector used to detect a login failure.
type ErrorSelector struct {
	Selector string          `yaml:"selector"`
	Message  MessageSelector `yaml:"message,omitempty"`
}

// MessageSelector describes how to extract the associated error message.
type MessageSelector struct {
	Selector string `yaml:"selector"`
	Text     string `yaml:"text,omitempty"` // literal/template message, takes precedence over selector
}

// Test describes a way to verify that a session is valid (post-login).
type Test struct {
	Path     string `yaml:"path"`
	Selector string `yaml:"selector"`
}

// Search describes how to perform a search and parse the results.
type Search struct {
	Paths            []SearchPath        `yaml:"paths"`
	Rows             Rows                `yaml:"rows"`
	Fields           map[string]Field    `yaml:"fields"`
	KeywordsFilters  []FilterStep        `yaml:"keywordsfilters"`
	Error            []ErrorSelector     `yaml:"error"`
	Headers          map[string][]string `yaml:"headers,omitempty"`
	Inputs           map[string]string   `yaml:"inputs,omitempty"`
	AllowEmptyInputs bool                `yaml:"allowEmptyInputs,omitempty"`
}

// UnmarshalYAML folds the singular `search.path` form (v11 schema oneOf
// paths|path) into Paths; all other fields are mirrored verbatim.
func (s *Search) UnmarshalYAML(unmarshal func(any) error) error {
	type rawSearch struct {
		Path             string              `yaml:"path"`
		Paths            []SearchPath        `yaml:"paths"`
		Rows             Rows                `yaml:"rows"`
		Fields           map[string]Field    `yaml:"fields"`
		KeywordsFilters  []FilterStep        `yaml:"keywordsfilters"`
		Error            []ErrorSelector     `yaml:"error"`
		Headers          map[string][]string `yaml:"headers,omitempty"`
		Inputs           map[string]string   `yaml:"inputs,omitempty"`
		AllowEmptyInputs bool                `yaml:"allowEmptyInputs,omitempty"`
	}
	var raw rawSearch
	if err := unmarshal(&raw); err != nil {
		return err
	}
	s.Paths = raw.Paths
	if raw.Path != "" && len(s.Paths) == 0 {
		s.Paths = []SearchPath{{Path: raw.Path}}
	}
	s.Rows = raw.Rows
	s.Fields = raw.Fields
	s.KeywordsFilters = raw.KeywordsFilters
	s.Error = raw.Error
	s.Headers = raw.Headers
	s.Inputs = raw.Inputs
	s.AllowEmptyInputs = raw.AllowEmptyInputs
	return nil
}

// SearchPath describes a search request URL/parameter template.
type SearchPath struct {
	Path       string            `yaml:"path"`
	Method     string            `yaml:"method"` // get | post
	Inputs     map[string]string `yaml:"inputs"`
	Response   ResponseInfo      `yaml:"response"`
	Categories []int             `yaml:"categories,omitempty"`
}

// ResponseInfo describes the expected content type of the response (html by default, json, xml).
type ResponseInfo struct {
	Type string `yaml:"type"`
}

// Rows describes how to select result rows in the response document.
type Rows struct {
	Selector string         `yaml:"selector"`
	After    int            `yaml:"after"`
	Filters  []FilterStep   `yaml:"filters"`
	Count    *SelectorField `yaml:"count,omitempty"` // row limit selector (e.g. JSON $.numFound)
}

// Field describes how to extract and transform a value for a result row.
type Field struct {
	Selector  string       `yaml:"selector"`
	Attribute string       `yaml:"attribute"`
	Text      string       `yaml:"text"` // literal/template value instead of a selector
	Optional  bool         `yaml:"optional"`
	Filters   []FilterStep `yaml:"filters"`
	Case      StringMap    `yaml:"case,omitempty"`    // value mapping (CSS selectors for HTML rows, literal keys for JSON)
	Remove    string       `yaml:"remove,omitempty"`  // comma-separated selectors stripped from the text
	Default   string       `yaml:"default,omitempty"` // fallback when extraction yields nothing
}

// StringMap is a string-keyed map that preserves document (insertion) order
// and coerces numeric keys/values to strings. yaml.v3 refuses int->string
// coercion, but the v11 schema allows numeric keys and values (e.g.
// `case: {0: 0, "*": 1}`), so a node-based unmarshal is required.
type StringMap struct {
	entries []stringMapEntry
	m       map[string]string
}

type stringMapEntry struct {
	key   string
	value string
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (sm *StringMap) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("expected a mapping, got kind %d", node.Kind)
	}
	*sm = StringMap{m: make(map[string]string, len(node.Content)/2)}
	for i := 0; i+1 < len(node.Content); i += 2 {
		k, v := node.Content[i], node.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		value := ""
		if v.Kind == yaml.ScalarNode {
			value = v.Value
		}
		sm.entries = append(sm.entries, stringMapEntry{key: k.Value, value: value})
		sm.m[k.Value] = value
	}
	return nil
}

// Keys returns the map keys in document (insertion) order.
func (sm StringMap) Keys() []string {
	keys := make([]string, len(sm.entries))
	for i, e := range sm.entries {
		keys[i] = e.key
	}
	return keys
}

// Value returns the value for key, or "" when absent.
func (sm StringMap) Value(key string) string { return sm.m[key] }

// Lookup returns the value and whether the key is present.
func (sm StringMap) Lookup(key string) (string, bool) {
	v, ok := sm.m[key]
	return v, ok
}

// Len returns the number of entries.
func (sm StringMap) Len() int { return len(sm.entries) }

// StringMapFrom builds a StringMap from alternating key/value pairs
// (programmatic construction; YAML definitions use the custom unmarshal).
func StringMapFrom(pairs ...string) StringMap {
	sm := StringMap{m: make(map[string]string, len(pairs)/2)}
	for i := 0; i+1 < len(pairs); i += 2 {
		sm.entries = append(sm.entries, stringMapEntry{key: pairs[i], value: pairs[i+1]})
		sm.m[pairs[i]] = pairs[i+1]
	}
	return sm
}

// FilterStep represents a step in the post-extraction filter pipeline
// (e.g. `re_replace`, `split`, `dateparse`...), with its positional arguments.
type FilterStep struct {
	Name string   `yaml:"name"`
	Args []string `yaml:"args"`
}

// UnmarshalYAML supports args as a single string OR a string array.
// Compatible with Prowlarr definitions that sometimes use a string
// instead of an array (e.g. args: " +00:01" instead of args: [" +00:01"]).
func (f *FilterStep) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawFilterStep struct {
		Name string      `yaml:"name"`
		Args interface{} `yaml:"args"`
	}
	var raw rawFilterStep
	if err := unmarshal(&raw); err != nil {
		return err
	}
	f.Name = raw.Name
	switch v := raw.Args.(type) {
	case string:
		if v != "" {
			f.Args = []string{v}
		}
	case []interface{}:
		f.Args = make([]string, len(v))
		for i, item := range v {
			f.Args[i] = fmt.Sprintf("%v", item)
		}
	}
	return nil
}

// SelectorField describes a CSS selector with attribute extraction and filters.
type SelectorField struct {
	Selector  string       `yaml:"selector"`
	Attribute string       `yaml:"attribute,omitempty"`
	Text      string       `yaml:"text,omitempty"`
	Optional  bool         `yaml:"optional,omitempty"`
	Filters   []FilterStep `yaml:"filters,omitempty"`
}

// Download describes how to resolve the real download link (torrent/nzb) from a row.
type Download struct {
	Selectors []DownloadSelector  `yaml:"selectors,omitempty"`
	Method    string              `yaml:"method,omitempty"`
	Before    DownloadBeforeList  `yaml:"before,omitempty"`
	Headers   map[string][]string `yaml:"headers,omitempty"`
	Infohash  *DownloadInfohash   `yaml:"infohash,omitempty"`
}

// DownloadBeforeList is a chain of intermediate requests performed before
// extracting the real link. It accepts a single DownloadBefore mapping (legacy
// form, used by most of the v11 corpus) or a sequence of them.
type DownloadBeforeList []DownloadBefore

// UnmarshalYAML accepts both the legacy single-mapping form and a sequence,
// because yaml.v3 does not coerce a single mapping into a slice. The failed
// single attempt leaves no poisoned error state (yaml.v3 resets per call).
func (l *DownloadBeforeList) UnmarshalYAML(unmarshal func(any) error) error {
	var single DownloadBefore
	if err := unmarshal(&single); err == nil {
		*l = DownloadBeforeList{single}
		return nil
	}
	var list []DownloadBefore
	if err := unmarshal(&list); err != nil {
		return err
	}
	*l = DownloadBeforeList(list)
	return nil
}

// DownloadBefore describes an intermediate request to perform before extracting the real link.
type DownloadBefore struct {
	Path         string            `yaml:"path"`
	Method       string            `yaml:"method"`
	Inputs       map[string]string `yaml:"inputs"`
	PathSelector *SelectorField    `yaml:"pathselector,omitempty"`
}

// DownloadInfohash describes how to extract the hash and title of a torrent.
type DownloadInfohash struct {
	UseBeforeResponse bool          `yaml:"usebeforeresponse,omitempty"`
	Hash              SelectorField `yaml:"hash"`
	Title             SelectorField `yaml:"title,omitempty"`
}

// DownloadSelector describes a download link resolution step (e.g. following an intermediate page).
type DownloadSelector struct {
	Selector          string       `yaml:"selector"`
	Attribute         string       `yaml:"attribute,omitempty"`
	UseBeforeResponse bool         `yaml:"usebeforeresponse,omitempty"`
	Filters           []FilterStep `yaml:"filters,omitempty"`
}
