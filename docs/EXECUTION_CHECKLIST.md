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
- [x] Compared AVBase against Amane and found that SDK search results dropped the prefix from canonical detail URLs; added a proposed SDK patch plus prefixed/unprefixed URL tests. Combined patch application, gofmt, JavBus focused tests, and AVBase URL tests passed in run [38880331157](https://github.com/strange-dragon/metaX/actions/runs/38880331157).\n- [ ] Full SDK regression suite, live-site compatibility, and end-to-end MetaTube server integration remain unverified.


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
