---
feature: v11-compliance
status: delivered
updated: 2026-08-02
branch: v11-compliance
commits: a7e8daa..11c8f99
---

# Cardigann v11 Compliance

## Report

**What was built** — 18 commits on `v11-compliance` fixing the fork's mis-parsing and mis-execution of the v11 Cardigann schema (the current Prowlarr/Indexers corpus gowlarr syncs). The login path key is now the canonical `login.path` (the fork previously parsed the non-standard `login:` key and silently dropped `path:`, sending logins to the site root); a `Validate()` warning plus corpus/fixture assertions guard against the legacy key. The engine now supports `login.method: post` (98 corpus defs), parses `login.cookies` as an array and `login.headers` as slice values (fixing 11 whole-definition parse failures), injects static cookies on every login method, resolves `login.selectors: true` input keys against the login page, and reports a clear "captcha required" error instead of failing silently (45 defs). Template support grew `.True`/`.False`/`.Today`, `join`, every field filter as a template function, template-rendered filter args, and a preprocessor for hyphenated `.Config.cat-id` settings (7 defs) — previously `.True`/`.Today`/`join`/`re_replace` hard-failed on 144/27/24/5 defs. Field extraction implements `case` (HTML selector keys vs JSON literal keys, `*` as explicit fallback), `remove` (read-only traversal), and `default` (394/142/165 defs); `settings.options` and `caps.categories` are parsed; `dateparse` translates .NET format strings; `validfilename` was added; `search.path` singular, `allowEmptyInputs`, `rows.count` (selector object), and rendered `error.message.text` complete the set. Search paths now tolerate broken paths (1337x's corpus `))` typo no longer aborts searches) while login and search.error failures still abort.

**Verification** — `go test -race -count=1 ./...` green in both repos (fork: login/definition/template/filters/selector/engine/defstest/…; gowlarr incl. fixture_test.go against migrated local defs); `go vet ./...` clean; `golangci-lint run ./...` 0 issues both repos; `go build ./cmd/gowlarr` ok; live corpus sweep `go test -tags=live ./internal/defstest/` passes with zero REGRESSION lines and the baseline pruned from 16 to 5 (11 cookies/headers parse failures now parse). New regression coverage: ~25 tests across the two repos plus committed real fixtures speedmasterhd (cookies array + selectors: true) and torrenttrader (cookies) and a fixture path-render smoke test.

**Journey log** —
- The user's note said the login key "must be `login:`"; the official v11 schema and 325/325 corpus defs use `path:` — the fork's tag was the bug (verified, user chose strict `path:` + migration).
- `.False` must render `""`, not `"false"`: the engine always populates every Query key and excludes `"false"` config defaults, and aither-api renders `.False` into `Result._internal` tested by `{{ if }}`.
- Go text/template `eq` on a *missing* map key is false (not "" equality) — corpus idioms only work because the engine pre-populates Query/Config keys; tests must mirror that.
- `rows.count` is a selector object (`{selector: $.numFound}`), not an int — an int would break torrentleech parsing; gjson paths are unprefixed so the JSON document normalizes the `$.` prefix.
- Extraction field order was map-random: a `text:` field referencing a helper rendered its falsy branch before the helper was extracted and never re-rendered — sorted field order (v11 mandates `_`-prefixed helpers) made it deterministic and fixed a flaky test.
- 1337x's TV path carries a stray `)` (corpus typo); the hyphenated `.Config.cat-id` idiom is invalid Go template syntax — both surfaced by the path-render smoke test and fixed in the engine, not the corpus.

## [S1] Problem

The fork mis-parses and mis-executes the v11 Cardigann schema (current Prowlarr/Indexers corpus) in ways that silently break real indexers: the login path key is parsed as `login:` while the schema and 325/325 corpus defs use `path:` (logins hit the site root); `login.method: post` (98), template `join`/`re_replace`/`.True`/`.False`/`.Today` (144/27/24/5), field `case`/`remove`/`default` (394/142/165), settings `options` (447), error `message.text` (99), caps `categories` (28), filter `validfilename` (8) are dropped or fail; filter args are never template-rendered; `dateparse` args are .NET formats treated as Go layouts; `login.cookies` (array) and `login.headers` (array values) cause whole-definition parse failures (11); `search.path` singular (2), `login.selectors: true` (5), `rows.count` (11), `allowEmptyInputs` (9), `captcha` (45) are ignored; a broken search path aborts the whole search. Live smoke with real accounts (japanfan/thelostspace) is reserved for the user; all automated verification is offline.

## [S2] Design

Settled contracts (validated by two independent reviews before implementation):

1. **Login path**: strict canonical `yaml:"path"` (no `login:` alias); Go field renamed `Login`→`Path`. A stray legacy `login:` key lands in `Extra map[string]any yaml:",inline"`; `Validate()` warns and the corpus/fixture suites assert `Extra["login"] == nil`. The 3 gowlarr `definitions-local/*.yml` migrated to `path:`.
2. **Login method `post`**: POST the rendered `inputs` to the rendered `Path` (no page fetch); shared POST tail in `doLoginRequest` (headers, 4 MiB cap, `checkLoginErrors`, `verifyTest`).
3. **Template context**: `.True="true"`, `.False=""` (corpus eq/if idioms), `.Today=time.Now()` — Context methods.
4. **Template functions**: `join` (variadic) + every registered field filter as a template func (`(args..., value)` pipe convention); `andmatch` excluded. Acyclic imports (`template→filters`, `selector→template`).
5. **Field filters**: args are template-rendered before execution (`template.RenderFilterSteps`); unresolved args defer via the multi-pass contract for field/rows filters, hard-error for keywordsfilters (which run after `configOnce`/rctx construction). `dateparse`: .NET-translated layout → raw Go layout → `dateLayouts`. `validfilename` registered.
6. **Field extraction**: `case` — HTML/XML rows match CSS-selector keys against the row in document order, JSON rows match literal value keys; `"*"` only as explicit fallback; values rendered templates. `remove` — read-only `TextExcluding` (parallel extraction shares one document). `default` — rendered fallback for optional/empty extraction (unresolved defers). Fields iterate in sorted name order.
7. **Rows**: `rows.count` is `*SelectorField` evaluated against the response document (JSON via gjson, `$.` prefix normalized), slicing after `after`. `rows.attribute` parsed only (known limitation).
8. **Login extras**: `cookies []string` (static cookies injected before method dispatch), `headers map[string][]string` (sent on all methods), `selectors bool` (inputs keys are CSS selectors against the login page; the matched element's `name` attribute is the field name, the value is the rendered input template — Prowlarr semantics; non-HTML login paths fall back to the selector as the field name), `captcha` block (clear `ErrLoginFailed`).
9. **Settings/Caps**: `settings[].options` parsed (ordered StringMap); `caps.categories` used by `mapCategories` only when no `categorymappings` exist (exact numeric newznab representation match, appends the native key) — known limitation: name-based values (e.g. `{1: TV}`) unmatched without a name table.
10. **Search**: `search.path` singular folded into `paths`; `allowEmptyInputs` overrides the empty/`"false"` filter entirely; `error[].message.text` rendered via `CheckErrors(body, sels, rctx)`; search paths skip technical failures (search.error matches and login failures abort; a fully-failed search still returns an error); hyphenated `.Config.foo-bar` rewritten to `index` access.

## [S3] Out of Scope

- `.DownloadUri` .NET member names (`.AbsoluteUri`/`.PathAndQuery`/`.AbsolutePath`, 4 defs) — needs a URL-property abstraction.
- Response `encoding` charset decoding (543 defs carry `encoding`, many non-UTF-8) — transversal; arabtorrents/kinozal-magnet/siambit render mojibake until then.
- `allowrawsearch` gating; `rows.dateheaders/multiple/missingAttributeEqualsNoResults`; `path.followredirect`; root `replaces/certificates/testlinktorrent`; `FilterStep` bare-int args; `title|append` pipe fields; `search.preprocessingfilters`; `login.getselectorinputs` — sub-5-def counts, no fixture coverage.
- `rows.attribute` behavior (element→value row semantics) — parse-only; aither-api rows diverge silently at runtime (known limitation).
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
