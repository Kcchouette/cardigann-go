package engine

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	"github.com/Kcchouette/cardigann-go/release"
	"github.com/Kcchouette/cardigann-go/selector"
	tmpl "github.com/Kcchouette/cardigann-go/template"
	"github.com/Kcchouette/cardigann-go/template/filters"
)

// maxParallelExtract caps the number of rows processed in parallel by
// parseResults. Each extraction may trigger an additional HTTP request
// (download link resolution via def.Download.Before), which was previously
// performed sequentially row by row — costly in latency for a result page
// with many rows. Rate limiting (RequestDelay / IndexerClient) is still
// respected: rate.Limiter and IndexerClient.Do are designed for concurrent
// use, so parallelizing here does not bypass the configured HTTP request
// spacing.
const maxParallelExtract = 8

func (p *Provider) parseResults(ctx context.Context, body string, responseType string, rctx tmpl.Context, keywords string) ([]release.Release, error) {
	doc, err := newDocument(responseType, body)
	if err != nil {
		return nil, err
	}

	rows, err := doc.Find(p.def.Search.Rows.Selector)
	if err != nil {
		return nil, fmt.Errorf("selecting rows: %w", err)
	}
	if p.def.Search.Rows.After > 0 && p.def.Search.Rows.After < len(rows) {
		rows = rows[p.def.Search.Rows.After:]
	}
	// rows.count caps the number of rows (e.g. JSON $.numFound).
	if c := p.def.Search.Rows.Count; c != nil && c.Selector != "" {
		if nodes, err := doc.Find(c.Selector); err == nil && len(nodes) > 0 {
			if n, perr := strconv.Atoi(strings.TrimSpace(nodes[0].Text())); perr == nil && n > 0 && n < len(rows) {
				rows = rows[:n]
			}
		}
	}

	// rows.attribute: re-base each JSON row onto its named key (UNIT3D
	// `data` -> `attributes`, yts/hebits `torrents`), expanding arrays into
	// individual rows when rows.multiple is set. HTML/XML rows ignore
	// attribute entirely (Prowlarr parity). An attribute-less row is skipped
	// (lenient, with or without missingAttributeEqualsNoResults). This runs
	// AFTER count so the cap applies to the top-level selection (movies),
	// not the expanded rows (required for yts).
	if p.def.Search.Rows.Attribute != "" {
		expanded := make([]selector.Node, 0, len(rows))
		for _, row := range rows {
			sub, ok := selector.ExpandAttribute(row, p.def.Search.Rows.Attribute, p.def.Search.Rows.Multiple)
			if !ok {
				continue
			}
			expanded = append(expanded, sub...)
		}
		rows = expanded
	}

	// Apply row-level filters (e.g. andmatch) before field extraction.
	if len(p.def.Search.Rows.Filters) > 0 {
		filtered := rows[:0]
		for _, row := range rows {
			text := row.Text()
			passed := true
			for _, f := range p.def.Search.Rows.Filters {
				if f.Name == "andmatch" {
					if !filters.ApplyAndMatch(text, keywords) {
						passed = false
						break
					}
					continue
				}
				steps, rerr := tmpl.RenderFilterSteps([]cmodel.FilterStep{{Name: f.Name, Args: f.Args}}, rctx)
				if rerr != nil {
					passed = false
					break
				}
				result, err := filters.Apply(text, steps)
				if err != nil || result == "" {
					passed = false
					break
				}
				text = result
			}
			if passed {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}

	// Parallel (bounded) row extraction, each row remaining independent (rctx
	// is passed by value: each goroutine mutates its own copy of rctx.Result
	// without ever touching another's or the caller's original value). Result
	// order is preserved (indexed by row position) for identical observable
	// behavior to sequential execution.
	extracted := make([]release.Release, len(rows))
	rowErrs := make([]error, len(rows))
	sem := make(chan struct{}, maxParallelExtract)
	var wg sync.WaitGroup
	for i, row := range rows {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, row selector.Node) {
			defer wg.Done()
			defer func() { <-sem }()
			rel, err := p.extractRelease(ctx, row, rctx)
			if err != nil {
				rowErrs[i] = err
				return
			}
			rel.IndexerID = p.def.ID
			rel.IndexerName = p.def.Name
			rel.Protocol = p.Protocol()
			extracted[i] = rel
		}(i, row)
	}
	wg.Wait()

	releases := make([]release.Release, 0, len(rows))
	var extractErr error
	for i := range rows {
		if rowErrs[i] != nil {
			if extractErr == nil {
				extractErr = rowErrs[i]
			}
			continue
		}
		releases = append(releases, extracted[i])
	}
	if len(releases) == 0 && extractErr != nil {
		return nil, fmt.Errorf("no releases extracted (last error: %w)", extractErr)
	}
	return releases, nil
}

func (p *Provider) extractRelease(ctx context.Context, row selector.Node, rctx tmpl.Context) (release.Release, error) {
	values := make(map[string]string, len(p.def.Search.Fields))

	// Extract fields in multiple passes to handle dependencies
	// (e.g. title_phase2 depends on title_phase1).
	// Continue as long as progress is being made.
	if rctx.Result == nil {
		rctx.Result = make(map[string]string)
	}

	for pass := 0; pass < 10; pass++ { // Max 10 passes
		progress := false
		// Iterate fields in sorted name order: Go map iteration is random,
		// and a `text:` field referencing a helper (e.g. `{{ if .Result._internal }}`)
		// would render its falsy branch successfully before the helper is
		// extracted, never re-rendering afterwards. The v11 schema mandates
		// `_`-prefixed helper fields, which sort before their dependents.
		for _, name := range sortedFieldNames(p.def.Search.Fields) {
			field := p.def.Search.Fields[name]
			// Skip if field is already extracted
			if _, done := values[name]; done {
				continue
			}

			var raw string
			var ok bool
			target := row

			if field.Selector != "" {
				if sub, isSub := row.(selector.SubSelectable); isSub {
					nodes, err := sub.Find(field.Selector)
					if err != nil || len(nodes) == 0 {
						if field.Optional {
							if v, okD := fieldDefault(field, rctx); okD {
								values[name] = v
								rctx.Result[name] = v
								progress = true
							}
							continue
						}
						// Required field not found, keep for next pass
						continue
					}
					target = nodes[0]
				}
			}

			if field.Text != "" {
				// Render the template with context (Result, Config, etc.)
				rendered, err := tmpl.Render(field.Text, rctx)
				if err != nil {
					if field.Optional {
						if v, okD := fieldDefault(field, rctx); okD {
							values[name] = v
							rctx.Result[name] = v
							progress = true
						}
						continue
					}
					// Unresolved template, keep for next pass
					continue
				}
				// Check if rendered output still contains unresolved placeholders
				if strings.Contains(rendered, "<no value>") || strings.Contains(rendered, "{{") {
					continue
				}
				raw = rendered
				ok = true
			} else if field.Attribute != "" {
				raw, ok = target.Attr(field.Attribute)
			} else {
				if field.Remove != "" {
					if ex, ok2 := target.(selector.TextExcluder); ok2 {
						raw = ex.TextExcluding(splitRemoveSelectors(field.Remove))
					} else {
						raw = target.Text()
					}
				} else {
					raw = target.Text()
				}
				ok = raw != ""
			}

			// Map the raw value through the field's case table before the
			// optional/required gate so an unmatched case can defer.
			if field.Case.Len() > 0 {
				raw, ok = applyCase(raw, row, rctx, field.Case)
			}

			if !ok {
				if field.Optional {
					if v, okD := fieldDefault(field, rctx); okD {
						values[name] = v
						rctx.Result[name] = v
						progress = true
					}
					continue
				}
				// Required field missing, keep for next pass
				continue
			}

			steps, rerr := tmpl.RenderFilterSteps(field.Filters, rctx)
			if rerr != nil {
				if field.Optional {
					if v, okD := fieldDefault(field, rctx); okD {
						values[name] = v
						rctx.Result[name] = v
						progress = true
					}
					continue
				}
				// Filter (or its unresolved args) failed, keep for next pass
				continue
			}
			filtered, err := filters.Apply(raw, steps)
			if err != nil {
				if field.Optional {
					if v, okD := fieldDefault(field, rctx); okD {
						values[name] = v
						rctx.Result[name] = v
						progress = true
					}
					continue
				}
				// Filter failed, keep for next pass
				continue
			}
			if filtered == "" && field.Default != "" {
				// Render the default as a template; unresolved defaults defer
				// to a later pass.
				if v, okD := fieldDefault(field, rctx); okD {
					filtered = v
				} else {
					continue
				}
			}
			values[name] = filtered
			rctx.Result[name] = filtered
			progress = true
		}

		if !progress {
			// No progress, stop
			break
		}
	}

	rel, err := fieldsToRelease(values)
	if err != nil {
		return release.Release{}, err
	}
	// fieldsToRelease emits raw (often root-relative) hrefs; absolutize
	// against the effective base so a rotated domain is reflected now.
	rel.Details = p.absolutizeHref(rel.Details)
	rel.DownloadLink = p.absolutizeHref(rel.DownloadLink)

	// Resolve download link via download block if defined (unless the
	// provider opts out: some chains perform side-effecting actions such as
	// posting a "thanks", which must only happen at explicit download time).
	if p.def.Download != nil && !p.skipSearchDownload {
		client, clientErr := p.httpClient(ctx)
		if clientErr == nil {
			dlResult, dlErr := p.resolveDownload(ctx, client, row, p.EffectiveBaseURL(), rctx, "")
			if dlErr == nil && dlResult != nil {
				if dlResult.URL != "" {
					rel.DownloadLink = dlResult.URL
				}
				if dlResult.InfoHash != "" {
					rel.InfoHash = dlResult.InfoHash
				}
				if dlResult.Title != "" {
					rel.Title = dlResult.Title
				}
				if dlResult.Unlocked {
					rel.Unlocked = true
				}
			}
		}
		// If resolveDownload fails, fall back to the raw field value already in rel.DownloadLink
	}

	rel.Hosts = DetectHosts(rel.DownloadLink)

	return rel, nil
}

// ddlHostSubstrings are the file hosts recognized for DDL releases.
var ddlHostSubstrings = []string{"uptobox", "1fichier", "drive.google", "mega.nz", "mediafire"}

// DetectHosts returns the recognized DDL file host of a link, normalized
// (no scheme, no port, no "www." prefix) and lowercased. It returns nil for
// links without a recognized host or for empty links.
func DetectHosts(link string) []string {
	if link == "" {
		return nil
	}
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	if host == "" {
		return nil
	}
	for _, sub := range ddlHostSubstrings {
		if strings.Contains(host, sub) {
			return []string{host}
		}
	}
	return nil
}

func toFilterSteps(steps []cmodel.FilterStep) []filters.Step {
	out := make([]filters.Step, len(steps))
	for i, s := range steps {
		out[i] = filters.Step{Name: s.Name, Args: s.Args}
	}
	return out
}

// sortedFieldNames returns the field names in deterministic (byte) order.
func sortedFieldNames(fields map[string]cmodel.Field) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// splitRemoveSelectors splits a comma-separated `remove` value into trimmed
// non-empty selectors.
func splitRemoveSelectors(remove string) []string {
	parts := strings.Split(remove, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// applyCase maps the raw value through the field's case table. For HTML/XML
// rows the keys are CSS selectors matched against the row; for JSON rows the
// keys are literal value strings compared to raw. "*" is an explicit
// fallback applied only when no specific key matched. Case values are
// rendered as templates. The second return value is false when nothing
// matched and there is no "*" entry (deferring the field).
func applyCase(raw string, row selector.Node, rctx tmpl.Context, caseMap cmodel.StringMap) (string, bool) {
	if _, isJSON := row.(selector.JSONNode); isJSON {
		for _, key := range caseMap.Keys() {
			if key == "*" {
				continue
			}
			if raw == key {
				return renderCaseValue(caseMap.Value(key), rctx)
			}
		}
	} else {
		if sub, ok := row.(selector.SubSelectable); ok {
			for _, key := range caseMap.Keys() {
				if key == "*" {
					continue
				}
				if nodes, err := sub.Find(key); err == nil && len(nodes) > 0 {
					return renderCaseValue(caseMap.Value(key), rctx)
				}
			}
		}
	}
	if fallback, ok := caseMap.Lookup("*"); ok {
		return renderCaseValue(fallback, rctx)
	}
	return "", false
}

// fieldDefault renders the field's default value (a template) when set.
// Returns ok=false when the default is still unresolved (defer to a later
// pass); with no default it returns the empty string with ok=true.
func fieldDefault(field cmodel.Field, rctx tmpl.Context) (string, bool) {
	if field.Default == "" {
		return "", true
	}
	rendered, err := tmpl.Render(field.Default, rctx)
	if err != nil || strings.Contains(rendered, "<no value>") || strings.Contains(rendered, "{{") {
		return "", false
	}
	return rendered, true
}

// renderCaseValue renders a case value as a template when it contains one.
// Returns ok=false when the rendered result is still unresolved.
func renderCaseValue(value string, rctx tmpl.Context) (string, bool) {
	if !strings.Contains(value, "{{") {
		return value, true
	}
	rendered, err := tmpl.Render(value, rctx)
	if err != nil || strings.Contains(rendered, "<no value>") || strings.Contains(rendered, "{{") {
		return "", false
	}
	return rendered, true
}

func fieldsToRelease(values map[string]string) (release.Release, error) {
	rel := release.Release{
		Title:        values["title"],
		Details:      values["details"],
		DownloadLink: values["download"],
		InfoHash:     values["infohash"],
	}
	if rel.Title == "" {
		return rel, fmt.Errorf("title is required")
	}
	if v, ok := values["size"]; ok {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			rel.Size = parsed
		}
	}
	if v, ok := values["seeders"]; ok {
		if parsed, err := strconv.Atoi(v); err == nil {
			rel.Seeders = parsed
		}
	}
	if v, ok := values["peers"]; ok {
		if parsed, err := strconv.Atoi(v); err == nil {
			rel.Peers = parsed
		}
	}
	if v, ok := values["date"]; ok {
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			rel.PublishDate = parsed
		}
	}
	// The category field carries the native (or case-mapped) category id:
	// propagate it to the release so consumers can filter/display it.
	if v, ok := values["category"]; ok {
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			rel.Categories = []int{parsed}
		}
	}
	return rel, nil
}

// extractSelectorField extracts a value from a node using a SelectorField.
func extractSelectorField(node selector.Node, sf *cmodel.SelectorField) (string, error) {
	if sf == nil {
		return "", nil
	}

	if sf.Text != "" {
		raw := sf.Text
		filtered, err := filters.Apply(raw, toFilterSteps(sf.Filters))
		if err != nil {
			return "", fmt.Errorf("applying filters: %w", err)
		}
		return filtered, nil
	}

	if sf.Selector == "" {
		return "", nil
	}

	sub, isSub := node.(selector.SubSelectable)
	if !isSub {
		return "", fmt.Errorf("node does not support Find")
	}

	nodes, err := sub.Find(sf.Selector)
	if err != nil {
		return "", fmt.Errorf("selector %q: %w", sf.Selector, err)
	}
	if len(nodes) == 0 {
		if sf.Optional {
			return "", nil
		}
		return "", fmt.Errorf("selector %q matched nothing", sf.Selector)
	}

	target := nodes[0]
	var raw string
	var ok bool
	if sf.Attribute != "" {
		raw, ok = target.Attr(sf.Attribute)
		if !ok {
			if sf.Optional {
				return "", nil
			}
			return "", fmt.Errorf("attribute %q not found", sf.Attribute)
		}
	} else {
		raw = target.Text()
	}

	filtered, err := filters.Apply(raw, toFilterSteps(sf.Filters))
	if err != nil {
		return "", fmt.Errorf("applying filters: %w", err)
	}
	return filtered, nil
}

// extractSelectorFromNode extracts a value from a node using a selector and an attribute.
func extractSelectorFromNode(node selector.Node, selectorStr, attribute string) (string, error) {
	sub, isSub := node.(selector.SubSelectable)
	if !isSub {
		return "", fmt.Errorf("node does not support Find")
	}

	nodes, err := sub.Find(selectorStr)
	if err != nil {
		return "", fmt.Errorf("selector %q: %w", selectorStr, err)
	}
	if len(nodes) == 0 {
		return "", fmt.Errorf("selector %q matched nothing", selectorStr)
	}

	target := nodes[0]
	if attribute != "" {
		raw, ok := target.Attr(attribute)
		if !ok {
			return "", fmt.Errorf("attribute %q not found", attribute)
		}
		return raw, nil
	}
	return target.Text(), nil
}
