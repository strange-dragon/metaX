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
