# Amane → MetaTube migration execution checklist

This file is the durable handoff for small, independently reviewable work units. Update it after each task so work can resume without relying on a long chat session.

## Working rules

- Work only on `feat/amane-provider-migration`; do not merge to `main` without explicit approval.
- One task per commit. Keep each change narrow; inspect the diff and record verification before starting the next task.
- Do not mark tests as passing unless they were actually run. Distinguish test failures, tests not run, and live-site checks.
- Compare Amane behavior with the SDK implementation before porting. Prefer the best implementation based on correctness, coverage, resilience, maintainability, and runtime cost.
- The intended runtime remains Jellyfin → existing MetaTube Jellyfin plugin → MetaTube API → native SDK Providers. Do not introduce a standalone Amane service.
- Preserve Apache-2.0 only for code that can properly be distributed under it. MetaTube SDK-derived code retains required upstream notices. Amane attribution alone does not grant permission to copy GPL-3.0 source or fixtures; resolve licensing before any such reuse.

## Task queue

### Task 0 — Recover and audit repository state
- [x] Confirm repository and feature branch.
- [x] Read contributor instructions, migration plan, compatibility matrix, licensing notes, and initial findings.
- [x] Confirm existing JavDB and Jav321 Provider implementations and tests.
- [x] Inspect the SDK Provider interface and explicit `engine/register.go` registration imports.
- [ ] Local tests, code formatting, and server integration were not run as part of this audit.

### Task 1 — Establish a trustworthy test baseline for the existing JavDB Provider
- [x] Inspect the complete JavDB implementation and existing deterministic HTTP tests.
- [x] Review found that the default HTTP client followed redirects without constraining the destination; this could escape the configured origin or port.
- [x] Add a redirect policy that restricts redirects to the configured host/port, permits the canonical www alias on the same effective port, blocks HTTPS downgrade, and caps redirect hops.
- [x] Add a deterministic regression test that redirects to a different origin/port and asserts the destination is never requested.
- [x] GitHub Actions deterministic test job passed on commit `ec2b52fd7d656c3748114972d5727f330e8bbb16` (run [38869568850](https://github.com/strange-dragon/metaX/actions/runs/38869568850)); CI formatting check passed. Race checks and live-site checks were not run.
- [x] No live-site support is claimed; live JavDB compatibility remains unverified.
- [x] CI exposed an unrelated `provider/fc2club` compile error (`appendUnique` was referenced but undefined in that package); added the package-local helper in commit `ec2b52f` so the repository test job can build.

### Task 2 — Verify the integration path before expanding providers
- [x] Verified the stock integration path in `metatube-community/metatube-sdk-go` at upstream commit `c62584675c593816cc07e7b5b977d14c7eb505a3`: `cmd/server/main.go` imports `engine`; `engine/register.go` blank-imports provider packages; `engine/init.go` initializes providers from the factory registries.
- [x] Confirmed that adding metaX as a normal module dependency does not register its providers. The correct delivery path is to transplant reviewed provider packages and registration imports into an SDK fork or propose them upstream; do not make the SDK depend on metaX.
- [x] Existing Jellyfin plugin contract can remain unchanged; no plugin-side change is indicated by the current architecture.
- [x] Documented the verified path in `docs/INTEGRATION.md` (commit `9d34a3e`).

### Task 3 — Validate Jav321 independently
- [x] Reviewed implementation and deterministic redirect regression test.
- [x] Added redirect restrictions: allowed canonical host/www alias, same effective port, no HTTPS downgrade, and a 10-hop cap; detail URL validation uses the same origin policy.
- [x] Added `TestFetchRejectsRedirectOutsideConfiguredOrigin`; corrected the test handler to pass its actual request to `http.Redirect` (commit `66413c6`).
- [x] GitHub Actions Go tests passed on `66413c63637b3951512a0172368319faf0059bc8` (run [38870437328](https://github.com/strange-dragon/metaX/actions/runs/38870437328)); formatting passed (run [38870437455](https://github.com/strange-dragon/metaX/actions/runs/38870437455)).
- [x] Compared the native behavior with Amane's `sites/jav321.py`: Amane's `_search` performs a direct `/video/{number}` lookup and `_scrape` extracts actors, studio, release, runtime, genres, cover, and plot. The Go provider covers those fields and additionally resolves relative cover URLs, normalizes IDs, maps SDK models, and validates detail origins.
- [x] Added deterministic tests for HTTPS downgrade, unexpected ports, lookalike hosts, 404 mapping, and upstream 502 handling in commit `90cb231`.
- [x] The first Go workflow attempt on `90cb231` stopped at its formatting gate; the formatter workflow completed successfully and normalized tracked Go sources. Fresh post-format CI passed on `d057e70` — Go tests run [38871428088](https://github.com/strange-dragon/metaX/actions/runs/38871428088) and formatting run [38871428188](https://github.com/strange-dragon/metaX/actions/runs/38871428188).
- [ ] Race checks and live-site compatibility were not tested.

### Task 4 — Compare existing SDK providers against Amane, one site per task
- [x] First comparison completed: JavBus at SDK commit `c62584675c593816cc07e7b5b977d14c7eb505a3` vs Amane commit `a2d45880e83df7047d461452965919692701e8c9`; findings and porting decision are recorded in `docs/COMPATIBILITY_MATRIX.md`.
- [x] Recorded differences before changing code: Amane's exact number-candidate matching and required-field validation are stronger; SDK JavBus URL parsing does not validate the origin. No live-site requests were made.
- [x] Prepared a focused upstream patch for JavBus detail-URL origin/path validation plus deterministic parsing tests: `patches/metatube-sdk-go/0001-javbus-validate-detail-url.patch` (latest patch metadata correction commit `c7befd2` (initial patch `c857f4b`)).
- [x] CI applies the patch to a clean checkout of pinned SDK commit `c62584675c593816cc07e7b5b977d14c7eb505a3`; formatting and focused URL-origin plus exact-number tests passed in run [38875105403](https://github.com/strange-dragon/metaX/actions/runs/38875105403). This verifies patch applicability and focused tests only, not the full SDK regression suite or live-site behavior.
- [x] Implement bounded number variants, exact-first selection, and a unique loose match only when the difference is the leading numeric segment on one side. Preserve Amane's rule that hyphens/spaces fold but underscores remain significant. Duplicate result URLs are deduplicated before ambiguity is assessed.
- [x] Add deterministic tests for leading-digit variants, exact-match priority, wrong-number rejection, ambiguous candidates, duplicate URLs, and required title/number fields. Patch application, gofmt, and focused tests passed in run [38878430661](https://github.com/strange-dragon/metaX/actions/runs/38878430661). The SDK patch-validation workflow now explicitly runs all five deterministic test groups rather than an obsolete test name.
- [x] Compared AVBase against Amane and found that SDK search results dropped the prefix from canonical detail URLs; added a proposed SDK patch plus prefixed/unprefixed URL tests. Combined patch application, gofmt, JavBus focused tests, and AVBase URL tests passed in run [38880331157](https://github.com/strange-dragon/metaX/actions/runs/38880331157).
- [ ] Full SDK regression suite, live-site compatibility, and end-to-end MetaTube server integration remain unverified.


### Task 4d — FC2Club compatibility and request-origin hardening
- [x] Compared Amane's FC2Club number lookup and field extraction with the native Go provider; confirmed SDK FC2HUB/FC2PPVDB target different sites.
- [x] Hardened URL validation against HTTP downgrade, unexpected ports, userinfo, and lookalike hosts.
- [x] Restricted redirects to the configured origin with a 10-redirect limit.
- [x] Added deterministic tests for unsafe detail origins, the allowed `www` alias, and cross-origin redirects.
- [x] Full metaX Go test workflow passed after formatting in [run 38884644252](https://github.com/strange-dragon/metaX/actions/runs/38884644252); formatting passed in [run 38884644417](https://github.com/strange-dragon/metaX/actions/runs/38884644417).
- [ ] Live-site behavior and end-to-end MetaTube integration remain unverified.


### Task 4e — official FC2 detail URL validation
- [x] Compared Amane's official FC2 scraper with SDK `provider/fc2`; preserved the SDK's broader metadata extraction.
- [x] Added `patches/metatube-sdk-go/0004-fc2-validate-detail-url.patch` to validate the official HTTPS origin/path, normalize IDs, and reject redirects to other works or origins.
- [x] Added deterministic tests for malformed IDs, invalid origins, nonstandard ports, userinfo, spoofed hosts, and malformed paths.
- [x] Combined patch application, formatting, and all focused SDK patch tests passed in [run 38885581605](https://github.com/strange-dragon/metaX/actions/runs/38885581605); full metaX tests passed in [run 38885581539](https://github.com/strange-dragon/metaX/actions/runs/38885581539).
- [ ] Live FC2 behavior and full upstream SDK regression remain unverified.

### Task 5a — Add native Airav provider
- [x] Compared Amane's Airav lookup behavior with the pinned SDK provider inventory; no native Airav provider was found.
- [x] Added independent Go implementation in `provider/airav/airav.go`; no Amane source or fixtures were copied.
- [x] Added exact normalized catalog-number matching, detail lookup by opaque ID, field mapping, same-origin redirect limits, final detail-path identity checks, and response-size cap.
- [x] Added deterministic local HTTP tests for exact vs near matches, detail parsing, opaque IDs, unsafe origins, cross-origin redirects, redirected work identity, registration, proxy and timeout configuration.
- [x] Full metaX Go tests passed in [run 38890986424](https://github.com/strange-dragon/metaX/actions/runs/38890986424); gofmt workflow passed in [run 38890986411](https://github.com/strange-dragon/metaX/actions/runs/38890986411).
- [ ] Live Airav compatibility and end-to-end MetaTube server registration remain unverified; the pinned upstream SDK does not yet import this metaX package.
### Task 5b — Add native AVSOX API provider
- [x] Compared Amane's AVSOX request sequence and model mapping; confirmed the pinned SDK has no `provider/avsox` package.
- [x] Added an independent Go implementation for the CSRF-token + JSON API flow, with exact-first number selection and separator-folded fallback.
- [x] Mapped localized titles/descriptions, actors, studio, label, series, director, genres, release date, runtime, posters, and sample images.
- [x] Added deterministic local API tests for CSRF headers, exact/folded search behavior, underscore significance, metadata mapping, API ID mismatch, URL validation, redirect restrictions, registration, proxy and timeout.
- [x] Full metaX Go tests passed in [run 38892023957](https://github.com/strange-dragon/metaX/actions/runs/38892023957); gofmt passed in [run 38892023918](https://github.com/strange-dragon/metaX/actions/runs/38892023918).
- [ ] Live AVSOX behavior and end-to-end MetaTube server registration remain unverified; this provider is not imported by the pinned upstream SDK.
### Task 5 — Add uncovered providers in small increments
- [ ] Prioritize sites with a clear mapping to existing SDK interfaces and manageable network behavior.
- [ ] Each provider gets focused tests, registration verification, and a separate commit.
- [ ] Defer browser-only/anti-bot-heavy sites until feasibility is assessed.

### Task 6 — Actor-provider inventory and migration
- [ ] Inventory Amane actor crawlers separately from movie crawlers.
- [ ] Compare with SDK ActorProvider/ActorSearcher interfaces and preserve actor IDs, images, and field semantics.
- [ ] Migrate one actor site per task where feasible.

### Task 7 — Aggregation, full regression, and release readiness
- [ ] Compare Amane field-level aggregation with MetaTube's existing orchestration.
- [ ] Implement a shared aggregation change only if a demonstrated gap exists and the behavior is tested independently.
- [ ] Run the available SDK tests, formatting/lint checks, and integration checks.
- [ ] Document known gaps and verification limits. Do not merge to `main` without explicit approval.

## Current verified status

- Repository: `strange-dragon/metaX`
- Branch: `feat/amane-provider-migration`
- Existing providers: `provider/javdb`, `provider/jav321`
- Current blocker for production use: the new providers are not automatically loaded by MetaTube merely because this repository exists; the runtime registration/import path must be verified and integrated.
- Latest audit: 2026-10-16. JavDB redirect restrictions and regression coverage are in commits `d3b80d0` and `4dc8e86`; CI found and the follow-up commit `ec2b52f` fixed the `fc2club` compile error. GitHub Actions deterministic tests and formatting passed on `ec2b52f` (run [38869568850](https://github.com/strange-dragon/metaX/actions/runs/38869568850)). Jav321 redirect restrictions and regression coverage are in commits `13de277`, `43ee0c3`, and `66413c6`; Go tests passed before the latest additional test coverage (run [38870437328](https://github.com/strange-dragon/metaX/actions/runs/38870437328)) and formatting passed (run [38870437455](https://github.com/strange-dragon/metaX/actions/runs/38870437455)). Additional URL-origin and HTTP-status tests were added in `90cb231`; after formatting, fresh Go tests and formatting passed on `d057e70` (runs [38871428088](https://github.com/strange-dragon/metaX/actions/runs/38871428088) and [38871428188](https://github.com/strange-dragon/metaX/actions/runs/38871428188)). JavBus comparison findings are recorded in `docs/COMPATIBILITY_MATRIX.md` (commit `01b490e`). The SDK patch applies cleanly and focused URL, exact-number, leading-digit variant, ambiguity, duplicate-result, and required-field tests passed in run `38878430661`. Full SDK regression, live-site compatibility, and end-to-end server integration remain unverified. Integration path was verified against SDK commit `c62584675c593816cc07e7b5b977d14c7eb505a3`; details are in `docs/INTEGRATION.md`. Race checks and live-site checks remain unrun.


### Task 4b — AVBase canonical detail URL
- [x] Compared Amane's exact resolver with the SDK's broader search-results semantics; preserved the SDK list behavior rather than copying single-result selection.
- [x] Fixed the concrete prefix loss in SDK search-result `Homepage` generation through `patches/metatube-sdk-go/0002-avbase-preserve-prefixed-detail-url.patch`.
- [x] Added deterministic tests for prefixed and unprefixed canonical URLs.
- [x] Combined patch application, formatting, JavBus focused tests, and AVBase focused test passed in [CI run 38880331157](https://github.com/strange-dragon/metaX/actions/runs/38880331157).
- [ ] Live-site behavior, full SDK regression, and end-to-end integration remain unverified.


### Task 4c — DAHLIA / FALENO exact slug validation review
- [x] Compared Amane's shared WordPress Works crawler with SDK shared `provider/dahlia/core`.
- [x] Identified a correctness risk: SDK HTML detail parsing does not confirm that the returned page matches the requested slug; Amane's REST lookup requires an exact slug match because the site can redirect unknown slugs to another work.
- [x] Added `patches/metatube-sdk-go/0003-dahlia-verify-exact-work-slug.patch`: preflight the WordPress REST endpoint for the requested slug, then reject a detail response whose final URL resolves to a different work.
- [x] Added deterministic local HTTP tests for exact slug success, missing slug, mismatched slug, and redirect to another work; existing metadata extraction is preserved.
- [x] Patch application, formatting, and focused JavBus/AVBase/DAHLIA-FALENO tests passed in [CI run 38883877346](https://github.com/strange-dragon/metaX/actions/runs/38883877346).
- [ ] Full SDK regression, live-site behavior, and end-to-end integration remain unverified.

## Category routing fan-out and fallback follow-up (2026-10-20)

- [x] Fixed the SDK integration script so category resolution uses the injected `routing.Config` rather than shadowing it with `routing.DefaultConfig()`; this addresses the empty provider fan-out seen in the latest SDK integration test.
- [x] Kept category search as concurrent provider fan-out while aggregating results in configured route order; an individual provider error does not discard successful results from other providers.
- [x] Delegated per-provider cache fallback to the SDK's existing `searchMovie(keyword, provider, fallback)` path, preserving provider-specific keyword normalization and the SDK's live-result/cache merge semantics; removed the duplicate category-level database fallback query.
- [ ] Full SDK integration/build tests intentionally not run in this task per user direction. Source-level checks only; no workflow was manually dispatched.

 
### JavDB actor name normalization follow-up (2026-10-20)

- [x] Compared Amane's JavDB actor name parser with the native Go actor provider.
- [x] Added suffix normalization for `(無碼)`, `(无码)`, and `(Uncensored)`, including full-width parentheses.
- [x] Added a deterministic test for suffix removal and duplicate elimination.
- [ ] Local Go tests and formatting not run; no GitHub Actions workflow was dispatched.

### JavDB actor canonical identity follow-up (2026-10-20)

- [x] Compared Amane's canonical URL / provider-ID resolution with the native JavDB actor parser.
- [x] Prefer a valid same-origin canonical actor URL for ActorInfo.ID and Homepage; ignore invalid canonical URLs and retain the requested URL fallback.
- [x] Added a deterministic local HTTP test for canonical identity resolution.
- [ ] Go tests and formatting not run; no GitHub Actions workflow was dispatched.

## Automatic category routing for unambiguous identifiers (2026-10-20)

- [x] Expanded routing.InferCategory to Amane-style number families: ordinary censored catalog IDs, uncensored prefixes and six-digit sequence IDs, FC2, Chinese MD/MKY, amateur prefixes, KIN8/Western studio-date IDs, and Getchu identifiers. `101926_01` classifies as uncensored; the project's FC2 and uncensored routes remain merged.
- [x] Integrated inference into the pinned SDK overlay: explicit provider and explicit category still win; URLs still use the existing detail path; unclassified search terms preserve the original all-provider search; if a classified route has no installed providers, broad search is retained as a compatibility fallback.
- [x] Added classifier regression cases and an SDK-level HTTP test proving an FC2 query without a category parameter fans out, tolerates one provider failure, and keeps successful results in route order.
- [ ] Go tests, formatting, and integrated server build were not run. No GitHub Actions workflow was dispatched.

- [x] Corrected the SDK integration script's generated Go statement and its static marker assertion so category-routing integration produces valid Go source.


### Task 6 — Implementation-level Amane ↔ MetaTube audit
- [x] Read Amane's pinned crawler base contract, movie metadata model, movie/actor crawler registries, aggregation engine/result model, and default category routes at `a2d45880e83df7047d461452965919692701e8c9`.
- [x] Read the pinned SDK provider interfaces, engine search/detail implementation, and movie model at `c62584675c593816cc07e7b5b977d14c7eb505a3`.
- [x] Compared those behaviors with the current metaX routing and SDK search overlay.
- [x] Recorded the durable findings and exact next-task boundary in `docs/IMPLEMENTATION_AUDIT.md`; consult this before repeating source exploration.
- [x] Main finding: current metaX/SDK has concurrent candidate search, ordered route fan-out, provider+ID deduplication, provider-priority ranking, per-provider error tolerance, and SDK cache fallback. It does **not** yet implement Amane-style field-by-field detail metadata aggregation across providers.
- [x] Next recommended task: design safe detail metadata enrichment with identity validation, fill-empty semantics, field/list/image merge rules, and deterministic tests. Do not merge search candidates solely by title.
- [ ] No tests, formatter, integrated build, live-site requests, or Jellyfin runtime were executed for this source audit.


### Task 7 — Detail metadata aggregation and independent source records

- [x] Added `.github/overlays/metatube-sdk-go/engine/metax_aggregate.go`: route-based concurrent detail lookups, conservative catalog-number identity validation, first-non-empty scalar fields, stable list unions, per-source status/errors, and retained independent structured `MovieInfo` records.
- [x] Preserved the selected provider + ID as the public detail identity and wired the detail route with an `aggregate=false` opt-out in the integration script; the default response is additive and keeps existing top-level detail fields.
- [x] Added deterministic merge/normalization tests under `.github/overlays/metatube-sdk-go/engine/metax_aggregate_test.go` and wired the overlay test into the integration script.
- [x] Documented response shape, merge rules, and the distinction between normalized provider metadata and raw HTTP/site payloads in `docs/METADATA_AGGREGATION.md`.
- [ ] **Not verified yet:** the integration script has not been executed against the pinned SDK checkout; Go tests, gofmt, full server build, endpoint-level JSON tests, and live-site checks have not run. No GitHub Actions workflow was dispatched.
- [ ] Next: apply locally to the pinned SDK, correct any compile/integration errors, add endpoint contract coverage for `sources` and `aggregate=false`, then validate the expanded Amane-style category classifier and its route behavior.


### Aggregated detail API contract coverage (2026-10-20)
- [x] Added SDK-route contract tests asserting the default movie detail response keeps the selected provider + ID and original MovieInfo fields at the top level while adding `category` and one independent `sources[].metadata` record.
- [x] Added an `aggregate=false` contract test asserting the stock detail response omits the additive `category` and `sources` fields.
- [x] Confirmed the integration script already copies the API test file into the pinned SDK checkout.
- [ ] These SDK endpoint tests have not been executed: the local environment cannot resolve GitHub to obtain the pinned SDK checkout; the installed Go toolchain is 1.23 while the metaX module declares Go 1.26. Do not treat source inspection or the independent number-normalization smoke test as SDK integration verification.
- [ ] Next verification step when the SDK checkout/toolchain is available: run gofmt, the focused aggregation and API contract tests, the classifier/routing tests, and the server build. Keep raw HTTP/site payload capture separate from the current independent structured `MovieInfo` source records.


### Project plan and handoff saved (2026-10-20)

- [x] Added `docs/PROJECT_PLAN.md` as the durable project roadmap, architecture/dependency boundary, non-negotiable design rules, current status, verification sequence, future priorities, and handoff protocol.
- [x] Explicitly documented that metaX currently extends a pinned upstream MetaTube SDK through an overlay; it is not an independent SDK rewrite.
- [x] Recorded that the previous local clone failed with `Could not resolve host: github.com`, which proves a DNS resolution failure at that time but does not identify the root cause or establish the user's CachyOS network status.
- [x] Recorded the local Go `1.23.2` versus project Go `1.26` mismatch, and distinguished the independent number-normalization smoke test from actual SDK integration tests.
- [ ] Immediate next task: establish a usable runtime/network and Go 1.26 toolchain, check out the pinned SDK commit `c62584675c593816cc07e7b5b977d14c7eb505a3`, run the integration script, focused aggregation/API/classifier tests, full relevant tests, and the integrated server build. Fix failures and rerun before feature work.
- [ ] No SDK integration tests or server build were run as part of this documentation commit.


### Jellyfin-facing search/detail contract and verified integration (2026-10-20)

This section supersedes the earlier default-aggregation and `aggregate=false` notes above.

- [x] Confirmed the user-facing model: the Jellyfin plugin sends a configured category plus a movie name/number; end users never select a provider directly.
- [x] Category search now prepends one merged candidate (`provider=MetaX`, `id=<catalog number>`) while retaining every provider's original result in route order.
- [x] Real-provider detail defaults to the stock response. `?aggregate=true` opts into the additive `category` + `sources[]` response.
- [x] Reserved `MetaX` detail resolves the catalog number through the category route, keeps the `MetaX/<number>` identity stable across refreshes, and returns merged metadata + `sources[]`.
- [x] Image routes resolve `MetaX/<number>` to the route-first real provider before the stock image path.
- [x] Explicit unknown categories return HTTP 400 on category search and `MetaX` detail instead of silently broadening the search.
- [x] Added deterministic endpoint tests for the merged search candidate, default stock detail, `aggregate=true`, `MetaX` detail, and unknown-category rejection.
- [x] Verified on a clean SDK checkout at `c62584675c593816cc07e7b5b977d14c7eb505a3` with Go 1.27.3: `go test ./routing ./engine ./route` passes; 12 provider package test targets pass; `go build -o /tmp/metatube-server-metax ./cmd/server` succeeds.
- [ ] Plugin follow-up: if provider filtering is enabled, allow `MetaX`; forward the configured `category` on detail requests. Without `category`, detail falls back to catalog-number inference.
- [ ] Full SDK regression remains environment-blocked: `engine/dbengine` needs a Docker daemon and `imageutil` differs under Go 1.27; CI pins Go 1.26.x.

### Task: aggregation cache and request coalescing (2026-10-20)

- [x] Added a bounded in-process TTL cache with `singleflight` coalescing for aggregated detail responses, `MetaX/<number>` aggregation, and image identity resolution.
- [x] Cache keys include the route-table fingerprint, category, lazy flag, and provider/number identity; FC2 and uncensored share a key because they resolve to the same route.
- [x] Cached results are cloned before returning so the route's `MetaX` identity rewrite cannot corrupt later reads.
- [x] Errors are not cached; only successful results survive for the TTL.
- [x] Added deterministic tests for TTL expiry, oldest-entry eviction, concurrent-load coalescing, cache-key separation, clone isolation, cache-hit lookup avoidance, and resolve-cache lookup avoidance.
- [x] Verification: `go test ./... -count=1` in metaX passes; `go test ./routing ./engine ./route -count=1` in the integrated SDK passes; `go build -o /tmp/metatube-server-metax ./cmd/server` succeeds.
- [ ] Not run: full SDK regression, because `engine/dbengine` requires a Docker daemon and `imageutil` differs under Go 1.27 versus the CI-pinned Go 1.26.
- Next: DB-first catalog-number resolution before live provider search.

### Task: DB-first catalog-number resolution (2026-10-20)

- [x] Added `routing.NumberLookupKeys` to generate the normalized SQL candidates, including common FC2 separator spellings.
- [x] Added `movie_metadata` lookup restricted to the resolved route providers, with route-order selection and a `SameNumber` re-check.
- [x] `lazy=true` aggregation now resolves `number -> provider/id` from the database before any live search; query errors and misses fall back to the existing live route.
- [x] Image identity resolution (`ResolveAggregatedProviderID`) now uses the same database-first lookup, avoiding a search per image request.
- [x] `lazy=false` intentionally bypasses the database-first path so refreshes still see provider changes.
- [x] Added deterministic tests for DB hits, FC2 spelling variants, out-of-route rows, route-order preference, lazy gating, and live fallback.
- [x] Verification: `go test ./routing ./engine ./route -count=1` in the integrated SDK passes; focused DB/cache tests pass; `go test ./... -count=1` in metaX passes; server build succeeds.
- [ ] Not run: full SDK regression, because `engine/dbengine` requires a Docker daemon and `imageutil` differs under Go 1.27 versus the CI-pinned Go 1.26.
- Next: make the route table configurable and validated at startup.

### Task: startup route-table configuration (2026-10-20)

- [x] Added `routing.LoadConfig`, `ParseConfig`, `LoadConfigFromEnv`, `InitActiveConfigFromEnv`, `SetActiveConfig`, and `ActiveConfig`.
- [x] Supports inline `METAX_ROUTES` JSON or `METAX_ROUTES_FILE`; inline wins, and neither uses `DefaultConfig`.
- [x] Accepts wrapped (`{"routes":{...}}`) and bare (`{"censored":[...]}`) JSON forms.
- [x] Validation rejects unknown categories, legacy category aliases, empty routes, empty/duplicate providers, malformed JSON, and extra top-level fields.
- [x] Wired `cmd.Router` to load and validate the table once at startup; invalid configuration exits nonzero before opening the database or listening.
- [x] Stock search/detail wrappers and image resolution now read `routing.ActiveConfig()`; explicit `...WithConfig` entry points remain for tests.
- [x] Added tests for parsing, validation, environment precedence, invalid-init rollback, defensive copying, and a route-level search using the active table.
- [x] Verification: `go test ./... -count=1` in metaX passes; `go test ./routing ./engine ./route -count=1` in the integrated SDK passes; server build succeeds; an invalid `METAX_ROUTES` exits with code 1 and `unknown category "bogus"`.
- [ ] Not run: full SDK regression, because `engine/dbengine` requires a Docker daemon and `imageutil` differs under Go 1.27 versus the CI-pinned Go 1.26.
- Next: tighten partial-failure and provider-error semantics with endpoint tests.

### Task: explicit-category error and partial-failure semantics (2026-10-20)

- [x] Added `routing.ParseCategory` to validate and canonicalize category inputs while still accepting `fc2`, `chinese_subtitles`, and `animation` aliases.
- [x] Explicit unknown or unroutable categories now return errors from the engine instead of a misleading `baseline_only` success when the caller supplied the category.
- [x] Category search maps both `ErrUnknownCategory` and `ErrNoAvailableProvider` to HTTP 400; all-provider failures remain HTTP 404 like stock MetaTube search.
- [x] Real-provider `aggregate=true` now rejects unknown categories and categories without registered providers with HTTP 400.
- [x] `MetaX` image resolution maps invalid/unroutable categories to HTTP 400 instead of 500.
- [x] Preserved compatibility: an explicit provider search override and stock (`aggregate=false`) detail continue to ignore `category`.
- [x] Added engine and endpoint tests for unknown categories, missing providers on search/detail/image, all-provider search failure, and alias canonicalization.
- [x] Verification: `go test ./... -count=1` in metaX passes; `go test ./routing ./engine ./route -count=1` in the integrated SDK passes; focused new tests pass; server build succeeds.
- [ ] Not run: full SDK regression, because `engine/dbengine` requires a Docker daemon and `imageutil` differs under Go 1.27 versus the CI-pinned Go 1.26.
- Next: bound aggregation fan-out and expose a per-request provider deadline.

### Task: bounded provider fan-out (2026-10-20)

- [x] Added a process-wide `METAX_AGGREGATE_FANOUT` limiter shared by category search and aggregated-detail source fan-out (default `16`, disabled with `0`/`off`/`disabled`/`none`).
- [x] The limiter wraps only remote provider calls; baseline cache hits and the already-fetched baseline detail do not consume a slot.
- [x] Provider HTTP calls remain bounded by the stock `--request-timeout`; the limiter does not abandon running goroutines or create timeout/partial-result states.
- [x] Added tests for env parsing, search fan-out concurrency, and detail source fan-out concurrency with six providers and a limit of two.
- [x] Updated the build format gate to cover the routing config and fan-out files.
- [x] Verification: `go test ./... -count=1` in metaX passes; `go test ./routing ./engine ./route -count=1` in the integrated SDK passes; focused fan-out tests pass; server build succeeds.
- [ ] Not run: full SDK regression, because `engine/dbengine` requires a Docker daemon and `imageutil` differs under Go 1.27 versus the CI-pinned Go 1.26.
- Backend core tasks are complete. Remaining work is provider coverage or the separate Jellyfin plugin project.

### Task: startup provider coverage and route fingerprint pin (2026-10-20)

- [x] Added `routing.Config.ProviderCoverage` and `ValidateRegisteredProviders`; startup now rejects a configured category with zero available providers and logs individual unavailable entries.
- [x] Added aliases for Amane route names that differ from SDK package names: `DMM` → `FANZA`, `MGStage` → `MGS`, `ThePornDB` → `ThePornDBMovie`/`ThePornDBScene`.
- [x] Added `Config.FingerprintHash()` (SHA-256 of the canonical route table) and `METAX_ROUTES_FINGERPRINT`; a mismatch aborts startup before the database opens. The actual fingerprint is logged at startup.
- [x] Wired coverage/fingerprint checks into the generated `cmd.Router` integration and covered both in deterministic tests.
- [x] Verified the default integrated build logs only the intentional gaps
  (`Official`, token-gated `ThePornDB`) instead of the previous false
  `MGStage`/`ThePornDB` name gaps. `Official` was later removed from the
  default route in Small Task 3A.
- [x] Verification: `go test ./routing -count=1` in metaX; `go test ./routing ./engine ./route -count=1` in the integrated SDK; `go build -o /tmp/metatube-server-metax ./cmd/server`; live startup log and fingerprint mismatch both observed.
- [x] CI on Go 1.26.x passed patch application, focused tests, full pinned-SDK regression, formatting, and server build in [run 39395605056](https://github.com/strange-dragon/metaX/actions/runs/39395605056). Local full regression remains environment-blocked (`engine/dbengine` needs Docker; `imageutil` differs under Go 1.27).

### Task: search-stage merged candidate (2026-10-20)

- [x] Replaced the previous copy-the-first-result search candidate with a real display merge: first valid catalog number supplies identity; empty title/homepage/thumbnail/cover/score/release-date fields are filled from later route results, and actor names are deduplicated case-insensitively.
- [x] Originals remain in route order after the `MetaX` candidate; results without a catalog number do not produce a candidate.
- [x] Added deterministic route tests for field filling, actor deduplication, nil/empty results, and original-result preservation.
- [x] Made the integration-script helper migration idempotent so older checkouts do not retain duplicate helpers.
- [x] Added `route/search.go` and the new test file to the build format gate.
- [x] Verification: integrated `go test ./routing ./engine ./route -count=1` passes; server build succeeds; live censored and FC2 searches showed populated merged candidates.

### Task: provider coverage and live API verification (2026-10-20)

- [x] Added `.github/scripts/live-metax-api-smoke.sh` and a `workflow_dispatch`/path-triggered workflow. The script checks category search, `MetaX` detail plus `sources[]`, image bytes, FC2 search, unknown-category HTTP 400, and fingerprint-pin startup failure.
- [x] Local live run passed: `bash .github/scripts/live-metax-api-smoke.sh /tmp/metatube-server-metax 18086` → `LIVE_API_SMOKE_OK`.
- [x] Live censored detail merged JavDB/JavBus data and returned a JPEG; live FC2 detail merged FC2 summary/genres/cover/release date.
- [x] Live amateur (`200GANA-123`), hentai (`GETCHU-12345`), and actor (`三上悠亜`) searches returned HTTP 200; Gfriends supplied the actor result.
- [x] Recorded provider-specific limits in `docs/COMPATIBILITY_MATRIX.md`: JavDB FC2 detail login redirect; FANZA region/age gate; Freejavbt 503; FC2PPVDB/FALENO/MGS 403; Kin8/Chinese/Western samples 404 or empty.
- [x] Updated the Faleno matrix entry to reflect the existing exact REST metadata/search patches (`0013`, `0014`) and their deterministic tests; live Faleno remains blocked by HTTP 403.
- [x] Fixed the trailing blank line in patch `0013` that failed the CI gofmt gate.
- [x] CI validation passed on commit `61f345f`: SDK patch/full-regression/build [39395605056](https://github.com/strange-dragon/metaX/actions/runs/39395605056), integrated live MetaX API smoke [39395605130](https://github.com/strange-dragon/metaX/actions/runs/39395605130), existing live FC2/uncensored smoke [39395605027](https://github.com/strange-dragon/metaX/actions/runs/39395605027), Go tests [39395605111](https://github.com/strange-dragon/metaX/actions/runs/39395605111), and formatting [39395604984](https://github.com/strange-dragon/metaX/actions/runs/39395604984). Local live results remain point-in-time evidence.
- [ ] Still deferred or not planned: Official manufacturer router (withdrawn in favor of category fan-out), Prestige, R18Dev offline PostgreSQL, fuzzy Getchu catalog search, and Wikipedia/Wikidata actor biographies.

### Task: provider access configuration (2026-10-20)

- [x] Compared the live failures with Amane's HTTP stack. Amane's general workarounds are per-site cookies and proxy; TLS impersonation, retries, and browser fallback do not bypass IP bans, regional redirects, or login-only pages.
- [x] Added `COOKIES`/`COOKIE` configuration to native JavDB and Freejavbt, including CR/LF rejection and deterministic request-header tests.
- [x] Added `0015-scraper-provider-cookies.patch` so scraper-based SDK providers (including FALENO, MGSTAGE, and FC2PPVDB) apply the same Cookie setting through `Scraper.SetConfig`.
- [x] Probed FC2PPVDB, MGS, FALENO work paths, and Freejavbt with Chrome 131/133 and Firefox 135 TLS profiles. FC2PPVDB remained Cloudflare 1005, MGS remained CloudFront 403, FALENO work/search remained XSERVER 403, and Freejavbt remained a login page/503; these require a different egress IP or account cookies.
- [x] Documented the configuration and failure matrix in `docs/PROVIDER_ACCESS.md` and linked it from the README and compatibility matrix.
- [x] CI on commit `b417e0f` passed the SDK patch/full-regression/build job in [run 39400027824](https://github.com/strange-dragon/metaX/actions/runs/39400027824), Go tests in [39400027781](https://github.com/strange-dragon/metaX/actions/runs/39400027781), formatting in [39400027799](https://github.com/strange-dragon/metaX/actions/runs/39400027799), and the existing live FC2/MOON smoke workflows. This validates the cookie-configuration patch; it does not claim a blocked source now works from this network.

### Task: Wikipedia/Wikidata actor provider (2026-10-20)

- [x] Added `provider/wikipediaactor` with `ActorProvider`, `ActorSearcher`, proxy, and request-timeout support.
- [x] Search uses `wbsearchentities` for ja/zh/en, filters descriptions and P106 occupations to AV entities, and requires an exact NFKC/case-folded label or alias match before returning a candidate.
- [x] Detail maps the preferred label, label aliases, P569 birthday, P18 Commons image, Wikidata homepage, and Wikipedia biography into existing `ActorInfo` fields. Section extraction follows Amane's intro plus biography whitelist and excludes work/filmography sections.
- [x] Wikipedia page lookup follows the configured zh/ja/en order, including the Chinese `zh-cn` variant path; Wikidata remains the stable identity source and homepage.
- [x] Added deterministic mocked API tests for exact alias matching, non-AV rejection, different-name rejection, field mapping, summary section filtering, birthday fallback, malformed JSON, URL/origin validation, proxy/timeout configuration, and factory registration.
- [x] Registered `WikipediaActor` in the engine overlay and added the package to the provider-overlay, focused-test, format-gate, and workflow path lists.
- [x] Local verification: `go test ./... -count=1` passes; the clean pinned-SDK overlay focused suite (including `provider/wikipediaactor`) passes; `go build ./cmd/server` succeeds; `/v1/providers` lists `WikipediaActor` with `https://www.wikidata.org`.
- [ ] Full pinned SDK regression remains environment-blocked for the known `engine/dbengine` Docker dependency and the Go 1.27 `imageutil/TestJPEGDecode` mismatch.
- [x] CI on commit `63e2688` passed Go tests in [run 39402434168](https://github.com/strange-dragon/metaX/actions/runs/39402434168), SDK patch/full-regression/focused-provider/build in [39402434294](https://github.com/strange-dragon/metaX/actions/runs/39402434294), live FC2/uncensored in [39402434186](https://github.com/strange-dragon/metaX/actions/runs/39402434186), and live MOON-023 in [39402434223](https://github.com/strange-dragon/metaX/actions/runs/39402434223). The integrated CI build registered `WikipediaActor`.
- Note: the pinned Jellyfin plugin has no provider-cookie configuration or pass-through protocol. Server-side environment cookies remain the current fallback until an additive plugin change is made.

### Task: request-scoped provider cookies from the plugin (2026-10-20)

- [x] Reversed the server-side `PUT /v1/providers/config` direction: the Jellyfin plugin configuration is the only persistent cookie source, and the server must not store or replay cookies.
- [x] Added the `X-MetaX-Provider-Cookies` request header as a JSON dictionary (`provider name -> cookie header value`) on movie/actor search, detail, review, and image routes.
- [x] Added `Engine.WithRequestCookies`: for each known provider entry it creates a fresh provider instance from the registered factory, overlays the request cookie on the startup provider configuration, and rebuilds host lookups. Shared provider instances are never mutated.
- [x] Added 16 KiB header, malformed/trailing JSON, non-string value, empty-name, CR/LF, unsupported-provider, unknown-provider, and concurrency-isolation coverage.
- [x] Removed the runtime configuration endpoint and its files so there is no second cookie source of truth. Environment variables remain the fallback when the header is absent.
- [x] Wired the overlay files into the integration script, focused tests, and formatting gate. `go test ./engine ./route -count=1` passes on the clean pinned SDK checkout; `go build ./cmd/server` succeeds. A live server smoke check returned `400` for a malformed header and `200` for `/v1/providers`.
- [x] CI on commit `6280829` passed Go tests in [run 39415020867](https://github.com/strange-dragon/metaX/actions/runs/39415020867), SDK focused/full-regression/build in [39415020810](https://github.com/strange-dragon/metaX/actions/runs/39415020810), formatting in [39415020918](https://github.com/strange-dragon/metaX/actions/runs/39415020918), and live FC2/uncensored in [39415020842](https://github.com/strange-dragon/metaX/actions/runs/39415020842).
- [ ] The Jellyfin plugin still needs its own additive change to store the provider dictionary and send the header; that work is outside this backend-only phase.
- [ ] Full pinned SDK regression remains environment-blocked locally for the known `engine/dbengine` Docker dependency and the Go 1.27 `imageutil/TestJPEGDecode` mismatch; CI runs the full suite.

### Task: route-alias keys for request-scoped cookies (2026-10-20)

- [x] Added `routing.ResolveProviderAliases` and expanded `DMM` → `FANZA`, `MGStage` → `MGS`, and `ThePornDB` → `ThePornDBMovie`/`ThePornDBScene` before applying the request cookie dictionary.
- [x] Identical alias/canonical values are accepted; conflicting values return HTTP 400. Empty values are omitted; names remain case-insensitive.
- [x] Added routing unit tests and internal route tests for alias expansion, conflict rejection, and empty-value handling.
- [x] Documented the alias behavior in `docs/PROVIDER_ACCESS.md` and marked task 1.5 complete in `docs/BACKEND_TASKS.md`.
- [x] Verification: `go test ./... -count=1` in metaX passes; `go test ./routing ./engine ./route -count=1` in the clean integrated SDK passes; `go build ./cmd/server` succeeds; changed files are gofmt-clean.
- [x] CI on commit `87ebb77` passed Go tests in [run 39417583702](https://github.com/strange-dragon/metaX/actions/runs/39417583702), SDK focused/full-regression/build in [39417583847](https://github.com/strange-dragon/metaX/actions/runs/39417583847), formatting in [39417583830](https://github.com/strange-dragon/metaX/actions/runs/39417583830), and live FC2/uncensored in [39417583703](https://github.com/strange-dragon/metaX/actions/runs/39417583703).

### Task: ThePornDB actor audit and config-setter fix (2026-10-20)

- [x] Compared Amane `src/amane/crawlers/actor/sites/theporndb.py` with the pinned SDK `provider/theporndb/actor.go`, scene/movie config code, and the engine's `provider.ConfigSetter` hook.
- [x] Found the concrete gap: the SDK ThePornDB actor and scene/movie providers still declared `SetConfig(map[string]string)`, which no longer satisfies `provider.ConfigSetter`. `engine/init.go` therefore never applied the documented `ACCESS_TOKEN`, so the token-gated provider could not be enabled by configuration.
- [x] Added `patches/metatube-sdk-go/0016-theporndb-config-setter.patch`: switch both providers to `provider.Config`, read `ACCESS_TOKEN` or Amane's `api_token` case-insensitively, trim whitespace, delegate cookie transport settings to `Scraper.SetConfig`, and add compile-time interface assertions plus deterministic token/cookie tests.
- [x] Kept the REST implementation and the existing URL/ID hardening (`0008`) rather than rewriting the provider into Amane's GraphQL flow. Documented remaining differences: GraphQL merge-chain and exact-search behavior, image area ranking, provider IDs, and tagline/disambiguation are not fully equivalent.
- [x] Decided not to add actor-profile aggregation. No plugin contract or identity/merge rules exist for actor profiles, images, and biographies; the movie-path `MetaX` provider remains the only aggregation contract.
- [x] Verification: all patches applied cleanly to the pinned SDK; `GOTOOLCHAIN=local GITHUB_ACTIONS=true go test ./provider/theporndb -count=1` passed; `gofmt -l provider/theporndb` was empty; `git diff --check` passed; `GOTOOLCHAIN=local go test ./... -count=1` passed in metaX.
- [x] CI on commit `e46be39` passed SDK patch application/focused tests/full pinned-SDK regression/formatting/server build in [run 39420345234](https://github.com/strange-dragon/metaX/actions/runs/39420345234), Go tests in [39420345245](https://github.com/strange-dragon/metaX/actions/runs/39420345245), and formatting in [39420345331](https://github.com/strange-dragon/metaX/actions/runs/39420345331). The live FC2/uncensored smoke job on the preceding code commit `cd6442e` passed in [39419659171](https://github.com/strange-dragon/metaX/actions/runs/39419659171); it does not exercise ThePornDB.
- [ ] No live ThePornDB API call was made; token acceptance and live actor metadata remain unverified. The next unblocked task is **7.3 health/status surface**.

## Big Task 7 — Request resilience and observability (2026-10-21)

- [x] Added `GET /v1/metax/status` with registered movie/actor provider counts,
  route category count/fingerprint, route provider gaps, and cookie-transport
  version `1`; no cookies, tokens, proxies, credentials, or URLs are returned.
  Commit `f56da89`.
- [x] Added `provider/httpretry`, a bounded retry transport for idempotent
  `GET`/`HEAD`/`OPTIONS` requests. It retries 408/429/503/504 and transient
  network errors, caps attempts at three, uses 200 ms exponential base delay,
  a 2 s cap, ±20% jitter, and bounded numeric/HTTP-date `Retry-After`; permanent
  4xx, unsafe methods, and unreplayable bodies are not retried. Commit
  `c9b6c56`.
- [x] Wired the shared transport into all thirteen native provider `net/http`
  clients and the Minnano actor client. Documented exceptions: JavLibrary's
  actual browser-fingerprint `fhttp` path, AVSOX's POST search, and upstream
  scraper providers.
- [x] Defined rate-limit behavior as per-provider retry/backoff state with no
  global retry mutex or token bucket. Added a deterministic test proving one
  provider's backoff does not block another. The existing request-scoped
  `METAX_AGGREGATE_FANOUT` bound is unchanged.
- [x] Evaluated Gin request-context propagation. It is not implemented because
  provider interfaces do not accept `context.Context`; a context setter would
  require request-scoped provider cloning and an interface change. Per-client
  timeouts and retry-context cancellation remain the documented bounds.
- [x] Added deterministic tests for retry status boundaries, permanent 4xx,
  unsafe/unreplayable requests, attempt exhaustion, numeric and HTTP-date
  `Retry-After`, transient/permanent network errors, cancellation before and
  during backoff, provider isolation, status JSON, route gaps, cookie transport
  version, and no secret fields.
- [x] Closed the log-leak path: the upstream proxy override log redacts URL
  userinfo and query data, `SetProxy` failures no longer echo the raw value, and
  malformed scraper cookie settings no longer include configured cookie text.
  Added deterministic tests for these cases.
- [x] Local verification: `GOTOOLCHAIN=local go test ./... -count=1` in metaX
  passes; `gofmt -l .` is empty.
- [x] Clean pinned-SDK focused verification: `go test ./engine ./route
  ./provider/httpretry ./provider/avbaseactor ./provider/airav
  ./provider/avsox ./provider/fc2club ./provider/freejavbt ./provider/giga
  ./provider/iqqtv ./provider/jav321 ./provider/javdb ./provider/javlibrary
  ./provider/minnano ./provider/wikipediaactor ./provider/xcity
  ./provider/internal/scraper -count=1` passes, including the status endpoint
  and log-redaction tests; `go build -o
  /private/var/folders/.../metatube-server ./cmd/server` succeeds.
- [x] Full pinned-SDK regression was attempted and failed on upstream live
  provider tests blocked by region/anti-bot responses (including FANZA, FALENO,
  FC2HUB, FC2PPVDB, MGS, SOD, Getchu, KIN8, Gfriends, and JAVFREE). This is not
  a regression pass and does not establish live-site behavior.
- [x] CI on commit `2f1805d` passed: SDK patch application, focused integration,
  full pinned-SDK regression, formatting, and server build in [run 39534885960](https://github.com/strange-dragon/metaX/actions/runs/39534885960);
  Go tests in [39534885829](https://github.com/strange-dragon/metaX/actions/runs/39534885829);
  formatting in [39534885871](https://github.com/strange-dragon/metaX/actions/runs/39534885871);
  live FC2/uncensored smoke in [39534885817](https://github.com/strange-dragon/metaX/actions/runs/39534885817);
  and live MOON-023 smoke in [39534885875](https://github.com/strange-dragon/metaX/actions/runs/39534885875).
- [x] Follow-up log-redaction commit `1060ccc` passed CI: SDK integration/build
  in [39536369716](https://github.com/strange-dragon/metaX/actions/runs/39536369716),
  Go tests in [39536369702](https://github.com/strange-dragon/metaX/actions/runs/39536369702),
  formatting in [39536369734](https://github.com/strange-dragon/metaX/actions/runs/39536369734),
  and live FC2/uncensored smoke in [39536369761](https://github.com/strange-dragon/metaX/actions/runs/39536369761).
- [ ] Not run: race detector, Jellyfin end-to-end runtime, and a targeted live
  retry/`Retry-After` exercise. Local live regression failures are recorded
  above; they are not evidence of retry success.

### Task: category provider manifest and cookie scope (2026-10-21)

- [x] Added `data.route_providers` to `GET /v1/metax/status`: category ->
  registered provider names, with route aliases resolved and unavailable
  providers omitted. The manifest contains no cookies, tokens, proxies, URLs,
  or provider configuration.
- [x] Added category-scoped cookie filtering in `requestEngine`. Category
  requests keep only cookies for resolved providers; explicit `provider`
  overrides win; requests without either keep the dictionary. The filter is
  applied before `WithRequestCookies`, so an out-of-category provider without
  a `ConfigSetter` cannot fail the request.
- [x] Added deterministic tests for manifest contents, category filtering,
  explicit-provider override, route aliases, unscoped requests, and the
  out-of-category unsupported-provider case.
- [x] Removed `Official` from the default censored route. It is Amane's
  manufacturer router, not a category provider; the default route now has no
  deferred provider gap with the integrated provider inventory.
- [x] Local verification on the clean pinned SDK after patches and provider
  overlay: `GOTOOLCHAIN=local go test ./route ./routing -count=1` passed.
  MetaX: `GOTOOLCHAIN=local go test ./... -count=1` passed; `gofmt -l` was
  empty.
- [x] CI on commit `717974d` passed: SDK patch/integration/full regression/build
  in [39539784711](https://github.com/strange-dragon/metaX/actions/runs/39539784711),
  Go tests in [39539784696](https://github.com/strange-dragon/metaX/actions/runs/39539784696),
  and formatting in [39539784702](https://github.com/strange-dragon/metaX/actions/runs/39539784702).
- [x] Follow-up `122e4f0` (drop `Official` from the default route) passed:
  SDK patch/integration/full regression/build in
  [39540447925](https://github.com/strange-dragon/metaX/actions/runs/39540447925),
  Go tests in [39540447927](https://github.com/strange-dragon/metaX/actions/runs/39540447927),
  formatting in [39540447917](https://github.com/strange-dragon/metaX/actions/runs/39540447917),
  and live FC2/uncensored smoke in
  [39540447920](https://github.com/strange-dragon/metaX/actions/runs/39540447920).
- [ ] Not run for this change: Jellyfin plugin implementation. The plugin
  remains deferred; its plan now documents library-category mapping and
  client-side `route_providers` filtering.

### Task: Amane route-order result sorting (2026-10-21)

- [x] Changed search ordering to category route position first, then match
  quality within one provider. The SDK numeric `Provider.Priority()` no longer
  controls candidate ordering.
- [x] Kept the reserved `MetaX` merged candidate prepended as the first result.
- [x] Added a deterministic test with a route-first provider whose SDK priority
  is much lower than the second provider; the route-first candidate is returned
  first.
- [x] Local verification on the clean pinned SDK:
  `GOTOOLCHAIN=local go test ./engine ./route ./routing -count=1` passed.
- [x] CI passed on `9e7191e`:
  [Go tests 39541866177](https://github.com/strange-dragon/metaX/actions/runs/39541866177),
  [SDK patch 39541866361](https://github.com/strange-dragon/metaX/actions/runs/39541866361).
- Next: **5.1** R18Dev architecture decision — optional local PostgreSQL source
  vs external service vs out of scope.
