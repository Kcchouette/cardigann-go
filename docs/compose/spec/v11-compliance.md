---
feature: v11-compliance
status: designed
updated: 2026-08-02
branch: v11-compliance
commits: a7e8daa..HEAD # filled at delivery
---

# Cardigann v11 Compliance

## Report

## [S1] Problem

The fork mis-parses and mis-executes the v11 Cardigann schema (current Prowlarr/Indexers corpus) in ways that silently break real indexers:

- The login path key is parsed as `login:` (`definition/definition.go:142`) while the official v11 schema and **325/325** corpus definitions use `path:` — real definitions' `path:` is silently dropped and logins target the site root.
- `login.method: post` (98 defs) is unsupported; template functions `join`/`re_replace` and values `.True`/`.False`/`.Today` (144/27 defs) hard-fail; field `case`/`remove`/`default` (394/142/165 defs), settings `options` (447), error `message.text` (99), caps `categories` (28), filter `validfilename` (8) are dropped; filter args are never template-rendered; `dateparse` args are .NET formats treated as Go layouts; `login.cookies` (array) and `login.headers` (array values) cause whole-definition parse failures (10+1 defs); `search.path` singular (2), `login.selectors: true` (5), `rows.count` (11), `search.allowEmptyInputs` (9) and `login.captcha` (45) are ignored.

Live smoke with real accounts (japanfan/thelostspace, creds `~/.config/cardigann_creds.yml`) is reserved for the user; all automated verification is offline.

## [S2] Design

Settled contracts (validated by two independent reviews):

1. **Login path**: strict canonical `yaml:"path"` (no `login:` alias); Go field renamed `Login`→`Path`. A stray legacy `login:` key lands in a new `Extra map[string]any yaml:",inline"` inline map; `Validate()` emits a warning, and the corpus/fixture suites **assert** `d.Login.Extra["login"] == nil`. The 3 gowlarr `definitions-local/*.yml` are migrated to `path:`.
2. **Login method `post`**: POST the rendered `inputs` to the rendered `Path` (no login-page GET, no selector-input extraction, no form-action lookup); `SubmitPath` honored. Shared POST tail extracted into `doLoginRequest` (headers, 4 MiB body cap, `checkLoginErrors`, `verifyTest`).
3. **Template context**: `.True = "true"`, `.False = ""` (required by corpus `eq .Query.X .False` idioms and `if` truthiness — e.g. aither-api renders `.False` into `Result._internal`), `.Today = time.Now()` — all Context methods (value receivers).
4. **Template functions**: `join` (variadic, supports both arg orders) plus every registered field filter exposed as a template func (`(args..., value)` pipe convention); `andmatch` excluded. Import graph stays acyclic (`template→filters`, `selector→template`).
5. **Field filters**: filter args are template-rendered before execution with the current context; unresolved args (containing `<no value>`/`{{`) keep the multi-pass retry contract (`extract.go:178`) for field/rows filters, and are a hard error for keywordsfilters (which now run **after** `configOnce`/rctx construction). `dateparse` args are tried as .NET-translated layout → raw Go layout → `dateLayouts` fallback. `validfilename` added to the registry.
6. **Field extraction**: `case` — HTML/XML rows match CSS-selector keys against the **row** in **document order**, JSON rows match literal value keys; `"*"` is an explicit fallback applied only when no specific key matched; values are rendered templates. `remove` — comma-separated selectors stripped via read-only `TextExcluding` (never `Selection.Remove()`; parallel extraction shares one document). `default` — rendered template applied when extraction yields empty (unresolved retries next pass).
7. **Rows**: `rows.count` is a selector object (`*SelectorField`, e.g. `{selector: $.numFound}`), evaluated against the response document (JSON via gjson), then `rows = rows[:n]` after `after` slicing. `rows.attribute` parsed only (behavior deferred — known limitation).
8. **Login extras**: `cookies []string` (static cookies injected into the jar before the method dispatch, `applyStaticCookies`), `headers map[string][]string` (sent on all login methods), `selectors bool` (inputs keys resolved as CSS selectors against the login page), `captcha` block (returns a clear `ErrLoginFailed` "captcha required" instead of silent failure).
9. **Settings/Caps**: `settings[].options` parsed (ordered StringMap); `caps.categories` parsed and used by `mapCategories` only when no `categorymappings` exist (exact numeric Newznab match, appends the map value) — known limitation: semantics not verified against Prowlarr.
10. **Search**: `search.path` singular folded into `paths` (custom `UnmarshalYAML` mirroring all fields); `allowEmptyInputs` overrides the empty/`"false"` input filter entirely (Prowlarr semantics); `error[].message.text` rendered as a template via `CheckErrors(body, sels, rctx)`.

Corpus counts cited above come from a 543-definition sweep of `Prowlarr/Indexers` (fetched from codeload at verification time; not vendored). Regression tests follow repo conventions: table-driven, `httptest`, YAML via `definition.Parse`, engine via `NewProvider`/`NewAuthenticatedProvider`, no network, no credentials.

## [S3] Out of Scope

- `.DownloadUri` .NET member names (`.AbsoluteUri`/`.PathAndQuery`/`.AbsolutePath`, 4 defs) — needs a URL-property abstraction.
- Response `encoding` charset decoding (543 defs carry `encoding`, many non-UTF-8) — transversal change to every response read; arabtorrents/kinozal-magnet/siambit render mojibake until then.
- `allowrawsearch` gating; `rows.dateheaders/multiple/missingAttributeEqualsNoResults`; `path.followredirect`; root `replaces/certificates/testlinktorrent`; `FilterStep` bare-int args; `title|append` pipe fields; `search.preprocessingfilters`; `login.getselectorinputs` — sub-5-def counts with no fixture coverage.
- `rows.attribute` behavior (element→value row semantics) — parse-only; aither-api rows diverge silently at runtime (known limitation).
- Live smoke with real credentials (user-owned).

## Tasks

- [ ] T1: Login field rename + safety net + migrate `login:`→`path:` everywhere — acceptance: `go test -race ./...` green; `rg "def.Login.Login|Login: \""` empty; gowlarr defs-local migrated (covers: S2.1)
- [ ] T2: cookies/headers types + static-cookie injection — acceptance: cookie/header login tests green (covers: S2.8)
- [ ] T3: `method: post` — acceptance: `TestLogin_PostMethod` + all login tests green (covers: S2.2)
- [ ] T4: `.True/.False/.Today` — acceptance: `TestRender_TrueFalse`, `TestRender_TodayYear` green (covers: S2.3)
- [ ] T5: `join` + filters-as-funcs — acceptance: join/pipe-filter template tests green (covers: S2.4)
- [ ] T6: `validfilename` + .NET dateparse — acceptance: filter tests green (covers: S2.5)
- [ ] T7: field case/remove/default + StringMap + TextExcluding — acceptance: 3 engine tests green, `-race` clean (covers: S2.6)
- [ ] T8: settings options + caps.categories — acceptance: parse round-trip + mapCategories unit green (covers: S2.9)
- [ ] T9: error message.text — acceptance: `TestCheckErrors_MessageTextTemplate` green (covers: S2.10)
- [ ] T10: filter-arg rendering + keywordsfilters ordering — acceptance: `TestSearch_FilterArgsRendered` green (covers: S2.5)
- [ ] T11: search.path + allowEmptyInputs + rows.count — acceptance: 3 unit tests green (covers: S2.7, S2.10)
- [ ] T12: login.selectors + captcha — acceptance: 2 login tests green (covers: S2.8)
- [ ] T13: rows.attribute parse-only — acceptance: parse round-trip green (covers: S2.7)
- [ ] T14: corpus closeout (baseline, legacy-key gate, real fixtures) + full verification + spec finalize — acceptance: 0 REGRESSION lines in live sweep; all commands pass; spec delivered (covers: S1, S2.1)
