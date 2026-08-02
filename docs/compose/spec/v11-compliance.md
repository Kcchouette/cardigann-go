---
feature: v11-compliance
status: delivered
updated: 2026-08-02
branch: v11-round2
commits: a7e8daa..9cc9f72
---

# Cardigann v11 Compliance

## Report

**What was built** — 23 commits across two rounds (branches `v11-compliance` then `v11-round2`) fixing the fork's mis-parsing and mis-execution of the v11 Cardigann schema (the current Prowlarr/Indexers corpus gowlarr syncs). Round 1: the login path key is now the canonical `login.path` (the fork previously parsed the non-standard `login:` key and silently dropped `path:`); `login.method: post` (98 defs), `login.cookies` array / `login.headers` slice values (fixing 11 parse failures), `login.selectors: true`, captcha errors; `.True`/`.False`/`.Today`, `join` and every field filter as template functions, rendered filter args, hyphenated `.Config.cat-id` (7 defs); field `case`/`remove`/`default` (394/142/165), `settings.options`, `caps.categories`, .NET `dateparse`, `validfilename`, `search.path` singular, `allowEmptyInputs`, `rows.count`, `error.message.text`; per-path tolerance (1337x's corpus `))` typo no longer aborts searches). Round 2: response bodies are decoded per the `encoding:` field (543/543 defs declare it, 42 non-UTF-8 — windows-1251/1256, ISO-8859-x; previously mojibake on RU/CN trackers); `.DownloadUri` exposes the .NET members (12 defs) with the chain seeded from the row's download link; template filter funcs follow the corpus's DIRECT value-first (Scriban) convention (the round-1 pipe convention was corrected); `rows.attribute` re-bases JSON rows (79 defs, incl. `multiple` and `..` recursive descent); empty keyword-less searches are gated on `caps.allowrawsearch` (20 defs).

**Verification** — `go test -race -count=1 ./...` green in both repos; `go vet ./...` clean; `golangci-lint run ./...` 0 issues both repos; `go build ./cmd/gowlarr` ok; live corpus sweep `go test -tags=live ./internal/defstest/` passes with zero REGRESSION lines. New regression coverage: httputil encoding table, windows-1251/tis-620/UTF-8/limit tests, kinozal-magnet fixture e2e (windows-1251 search + before chain + DownloadUri seed), login selectorinputs decode, Validate encoding warning, direct-form + corpus-verbatim template tests, `.Query.id` first-value, 6 rows.attribute engine tests (incl. count-order pin and descent walker guard), aither-api fixture e2e, 3 allowrawsearch gate tests. The reviewer approved round 2 (APPROVE) after a focused correctness review.

**Journey log** —
- The user's note said the login key "must be `login:`"; the official v11 schema and 325/325 corpus defs use `path:` — the fork's tag was the bug (verified, user chose strict `path:` + migration).
- `.False` must render `""`, not `"false"`: the engine always populates every Query key and excludes `"false"` config defaults, and aither-api renders `.False` into `Result._internal` tested by `{{ if }}`.
- gjson has no JSONPath recursive descent (`..` is JSON-Lines-only — `Get(x,"..k")` = "") and `golang.org/x/text` has no TIS-620 decoder (no `thaitext` package): both required a custom walker / a passthrough+warning decision instead of a library call.
- The corpus calls template filter functions in DIRECT value-first form (44/44: re_replace 20, join 24; zero pipes) — the round-1 pipe convention silently corrupted direct calls; the flip is safe because no other caller exists.
- `rows.count` must cap the TOP-LEVEL selection before the attribute/multiple expansion (yts) — the fork's slice-cap semantics differ from Prowlarr's early-exit gate; the kinozal flat-`li` infohash extraction quirk is pre-existing (descendant-only selector re-application), not a round-2 regression.

## [S1] Problem

The fork mis-parses and mis-executes the v11 Cardigann schema (current Prowlarr/Indexers corpus) in ways that silently break real indexers: the login path key is parsed as `login:` while the schema and 325/325 corpus defs use `path:` (logins hit the site root); `login.method: post` (98), template `join`/`re_replace`/`.True`/`.False`/`.Today` (144/27/24/5), field `case`/`remove`/`default` (394/142/165), settings `options` (447), error `message.text` (99), caps `categories` (28), filter `validfilename` (8) are dropped or fail; filter args are never template-rendered; `dateparse` args are .NET formats treated as Go layouts; `login.cookies` (array) and `login.headers` (array values) cause whole-definition parse failures (11); `search.path` singular (2), `login.selectors: true` (5), `rows.count` (11), `allowEmptyInputs` (9), `captcha` (45) are ignored; a broken search path aborts the whole search. Round 2 (amendment): response bodies are never decoded per the `encoding:` field (42 non-UTF-8 defs render mojibake), `.DownloadUri` .NET members render `<no value>`/`[x]` (12 defs), `rows.attribute` is parsed but unused (79 defs return zero rows), template filter functions use the wrong (pipe) convention, and keyword-less searches fire broken requests. Live smoke with real accounts (japanfan/thelostspace) is reserved for the user; all automated verification is offline.

## [S2] Design

Settled contracts (validated by two independent reviews before implementation):

1. **Login path**: strict canonical `yaml:"path"` (no `login:` alias); Go field renamed `Login`→`Path`. A stray legacy `login:` key lands in `Extra map[string]any yaml:",inline"`; `Validate()` warns and the corpus/fixture suites assert `Extra["login"] == nil`. The 3 gowlarr `definitions-local/*.yml` migrated to `path:`.
2. **Login method `post`**: POST the rendered `inputs` to the rendered `Path` (no page fetch); shared POST tail in `doLoginRequest` (headers, 4 MiB cap, `checkLoginErrors`, `verifyTest`).
3. **Template context**: `.True="true"`, `.False=""` (corpus eq/if idioms), `.Today=time.Now()` — Context methods.
4. **Template functions**: `join` (variadic) + every registered field filter as a template func in **DIRECT (Scriban) form with the value FIRST** (`{{ re_replace .X "a" "b" }}` — the corpus is 100% direct, 44/44); `andmatch` excluded. Acyclic imports (`template→filters`, `selector→template`). (Amended round 2: the round-1 pipe convention contradicted the corpus and silently corrupted direct calls.)
5. **Field filters**: args are template-rendered before execution (`template.RenderFilterSteps`); unresolved args defer via the multi-pass contract for field/rows filters, hard-error for keywordsfilters (which run after `configOnce`/rctx construction). `dateparse`: .NET-translated layout → raw Go layout → `dateLayouts`. `validfilename` registered.
6. **Field extraction**: `case` — HTML/XML rows match CSS-selector keys against the row in document order, JSON rows match literal value keys; `"*"` only as explicit fallback; values rendered templates. `remove` — read-only `TextExcluding` (parallel extraction shares one document). `default` — rendered fallback for optional/empty extraction (unresolved defers). Fields iterate in sorted name order.
7. **Rows**: `rows.count` is `*SelectorField` evaluated against the response document (JSON via gjson, `$.` prefix normalized), slicing after `after` — **fork semantics: slice-cap, not Prowlarr's early-exit gate; `after` is not applied to JSON**. `rows.attribute` re-bases each JSON row onto its named key (see S2.13). (Amended round 2.)
8. **Login extras**: `cookies []string` (static cookies injected before method dispatch), `headers map[string][]string` (sent on all methods), `selectors bool` (inputs keys are CSS selectors against the login page; the matched element's `name` attribute is the field name, the value is the rendered input template — Prowlarr semantics; non-HTML login paths fall back to the selector as the field name), `captcha` block (clear `ErrLoginFailed`).
9. **Settings/Caps**: `settings[].options` parsed (ordered StringMap); `caps.categories` used by `mapCategories` only when no `categorymappings` exist (exact numeric newznab representation match, appends the native key) — known limitation: name-based values (e.g. `{1: TV}`) unmatched without a name table.
10. **Search**: `search.path` singular folded into `paths`; `allowEmptyInputs` overrides the empty/`"false"` filter entirely; `error[].message.text` rendered via `CheckErrors(body, sels, rctx)`; search paths skip technical failures (search.error matches and login failures abort; a fully-failed search still returns an error); hyphenated `.Config.foo-bar` rewritten to `index` access.
11. **Response encoding** (round 2): every response body (search, download before-chain, login form/get/oneurl/selectorinputs, verifyTest) is decoded with the definition's `encoding:` via `httputil.ReadBodyLimited` (transform reader gated on a normalized table: windows-125x, ISO-8859-x; UTF-8 passthrough). Unknown encodings — including TIS-620, which has no x/text decoder — pass through with a `Validate()` warning (lenient; Prowlarr throws at instantiation). `verifyTest` gained a `def` parameter.
12. **`.DownloadUri`** (round 2): `template.DownloadURL` exposes the .NET System.Uri surface — `.AbsoluteUri`, `.AbsolutePath`, `.PathAndQuery`, `.Scheme`, `.Host`, `.Port`, `.Path`, `.RawQuery`, and `.Query.<key>` = the FIRST query value (scalar, no brackets). Seeded once from the row's download link at chain start (Prowlarr semantics; all 12 corpus usages are at before-chain step 0) AND overwritten per step (fork chained tests read `.DownloadUri.Path` of the previous step).
13. **rows.attribute runtime** (round 2): for JSON responses each matched row is re-based onto its named key (`ExpandAttribute`); `rows.multiple` fans an array attribute into individual rows; `..`-prefixed field selectors walk the ORIGINAL row in document order via a `descentKey` walker (gjson has no recursive descent); expansion runs AFTER `rows.count` (the cap applies to the top-level selection — required for yts). An attribute-less row is **always skipped** (lenient; `missingAttributeEqualsNoResults` is parsed for schema compatibility with its exact camelCase tag — deliberate divergence from Prowlarr's hard failure without the flag). HTML/XML rows ignore attribute (Prowlarr parity).
14. **allowrawsearch gate** (round 2, deliberate fork deviation): `engine.Search` returns `(nil, nil)` with a warning when the query has no keywords and no media ID (IMDBID/TMDBID/TVDBID/DoubanID/Season/Ep) and the def lacks `caps.allowrawsearch` — no HTTP request fires. Prowlarr never gates (the flag is caps-XML-only); this protects callers passing an empty query (e.g. `gowlarr search ""` browse).

## [S3] Out of Scope

- `.DownloadUri` bare `.Query` renders a Go map (not the raw query string); the corpus only uses `.Query.<key>` — unused surface, kept as-is.
- Caps XML `searchEngine="raw"` per-indexer advertisement (round-3 candidate); the gate in S2.14 already protects empty-query callers.
- POST form bodies are sent UTF-8, not re-encoded with `encoding:` (GET query parity already holds; `urlencode`/`urldecode` are operational filters).
- `..a.b` compound recursive-descent selectors (bare keys only); `rows.dateheaders`; `path.followredirect`; root `replaces/certificates/testlinktorrent`; `FilterStep` bare-int args; `title|append` pipe fields; `search.preprocessingfilters`; `login.getselectorinputs` — sub-5-def counts, no fixture coverage.
- Corpus-side fixes (1337x `))` typo, anibt seq-form cats, `\/` escape defs) — handled in the engine, not the corpus.
- Live smoke with real credentials (user-owned).

## Tasks

- [x] T1: Login field rename + safety net + migrate `login:`→`path:` everywhere — acceptance: `go test -race ./...` green; `rg "def.Login.Login|Login: \""` empty; gowlarr defs-local migrated (covers: S2.1)
- [x] T2: cookies/headers types + static-cookie injection — acceptance: cookie/header login tests green (covers: S2.8)
- [x] T3: `method: post` — acceptance: `TestLogin_PostMethod` + all login tests green (covers: S2.2)
- [x] T4: `.True/.False/.Today` — acceptance: `TestRender_TrueFalse`, `TestRender_TodayYear` green (covers: S2.3)
- [x] T5: `join` + filters-as-funcs — acceptance: join/pipe-filter template tests green (covers: S2.4)
- [x] T6: `validfilename` + .NET dateparse — acceptance: filter tests green (covers: S2.5)
- [x] T7: field case/remove/default + StringMap + TextExcluding — acceptance: 3 engine tests green, `-race` clean (covers: S2.6)
- [x] T8: settings options + caps.categories — acceptance: parse round-trip + mapCategories unit green (covers: S2.9)
- [x] T9: error message.text — acceptance: `TestCheckErrors_MessageTextTemplate` green (covers: S2.10)
- [x] T10: filter-arg rendering + keywordsfilters ordering — acceptance: `TestSearch_FilterArgsRendered` green (covers: S2.5)
- [x] T11: search.path + allowEmptyInputs + rows.count — acceptance: 3 unit tests green (covers: S2.7, S2.10)
- [x] T12: login.selectors + captcha — acceptance: 2 login tests green (covers: S2.8)
- [x] T13: rows.attribute parse-only — acceptance: parse round-trip green (covers: S2.7)
- [x] T14: corpus closeout (baseline, legacy-key gate, real fixtures, path-render smoke) + full verification + spec finalize — acceptance: 0 REGRESSION lines in live sweep; all commands pass; spec delivered (covers: S1, S2.1)
- [x] T15: response `encoding:` decoding (httputil.ReadBodyLimited + 8 sites + verifyTest signature + Validate warning + tests) — acceptance: windows-1251/tis-620/passthrough tests green; kinozal-magnet e2e decodes; Validate warns on unknown (covers: S2.11)
- [x] T16: `.DownloadUri` .NET members + chain seed + value-first filter funcs — acceptance: direct-form tests + corpus-verbatim + `.Query.id` first-value green; chain test seeds via `download` key (covers: S2.4, S2.12)
- [x] T17: rows.attribute/multiple/`..` walker + expansion order — acceptance: 6 engine JSON tests green incl. count-order pin and descent guard; aither fixture e2e (covers: S2.7, S2.13)
- [x] T18: allowrawsearch gate — acceptance: 3 gate tests green (gated/flagged/ID-bypass); full-pipeline unaffected (covers: S2.14)
