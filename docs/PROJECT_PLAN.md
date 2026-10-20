# metaX Project Plan and Handoff

Last updated: 2026-10-20
Working branch: `feat/amane-provider-migration`

This document is the durable project-level plan and handoff for future work. Read it together with `docs/EXECUTION_CHECKLIST.md`, `docs/IMPLEMENTATION_AUDIT.md`, `docs/INTEGRATION.md`, `docs/COMPATIBILITY_MATRIX.md`, and `docs/METADATA_AGGREGATION.md` before making changes.

## 1. Project objective

Integrate Amane-inspired scraping, category classification, routing, and metadata aggregation into the MetaTube ecosystem so that the existing `metatube-jellyfin` plugin/API workflow can use the added functionality. The goal is not to make users operate Amane separately and not to require manual provider mapping.

The current architecture uses a metaX repository containing native provider implementations and an integration overlay applied to a pinned upstream `metatube-sdk-go` checkout. It is **not currently a fully independent rewrite of the SDK**. The upstream SDK supplies provider contracts, models, engine behavior, HTTP routes, response envelopes, cache behavior, and other base functionality; metaX owns its routing and aggregation extensions plus selected native providers. This dependency boundary must remain explicit in documentation and tests.

## 2. Non-negotiable design rules

1. **Amane is the behavioral reference for catalog identity, classification, source routing, and aggregation semantics.** MetaTube SDK behavior is the compatibility reference for its provider interface, HTTP API, response envelope, provider + ID identity, and cache semantics.
2. Never flatten away meaningful catalog identity. In particular, `101926_01` must not normalize to `10192601`. Case, spaces, and hyphens may be normalized conservatively where justified; underscores remain significant.
3. Route automatically from Amane-derived catalog-number families and provider-specific identity hints. Do not require users to configure a manual mapping for every category. Explicit provider selection wins; explicit category is honored; URL/detail requests retain the existing detail path; ambiguous terms preserve broad-search compatibility.
4. Search fan-out is concurrent but output ordering is deterministic according to configured route priority. One provider failure must not discard successful results from other providers. Reuse SDK provider-specific search/cache fallback instead of duplicating it.
5. Keep detail identity stable. Real-provider requests keep their `provider + id`; the merged view uses the reserved `MetaX` provider with the catalog number as its ID, so later refreshes continue to resolve through aggregation. Cross-provider metadata enrichment requires conservative catalog-number identity validation; never merge works solely because their titles look alike.
6. Category search returns every provider's original candidate plus one merged candidate under the reserved `MetaX` provider. Real-provider detail defaults to the stock response; `?aggregate=true` adds `category` plus `sources[]`; `MetaX` detail resolves the catalog number through the category route and returns the merged view with `category` and `sources[]`. Each source record identifies provider, provider ID when known, status/error, and that provider's independent structured `MovieInfo` metadata.
7. Be precise about “raw source data”: current `sources[].metadata` preserves provider-normalized structured `MovieInfo`, **not** the original HTML, JSON response body, or raw HTTP payload. Do not claim raw payload capture exists unless separately implemented and tested.
8. Scalar merge uses first non-empty values in configured route order; list/image collections use stable deduplicated unions; do not average incomparable provider scores; preserve field semantics and the selected provider identity.
9. Movie and actor crawling are separate contracts. Do not force actor metadata through movie-provider interfaces.
10. Every task is small and reviewable. Add deterministic tests and update docs/checklist. Do not claim tests passed unless they were actually run. After a test failure, fix and rerun the failed checks before proceeding. Do not merge to `main` without explicit user approval.
11. Preserve license/provenance obligations when adapting open-source behavior. The pinned Amane snapshot is GPL-3.0 and the pinned MetaTube SDK is Apache-2.0; review compatibility and attribution before copying code or fixtures. Prefer independent implementations and behavioral comparisons when appropriate.

## 3. Architecture and ownership

### metaX repository-owned code
- `routing/routes.go`: catalog category definitions, number-family inference, and ordered provider route configuration.
- `provider/*`: native provider implementations owned by metaX (current inventory must be checked against the latest tree; historical checklist records JavDB, Jav321, Airav, and AVSOX work).
- `.github/overlays/metatube-sdk-go/engine/metax_search.go`: SDK engine search fan-out extension.
- `.github/overlays/metatube-sdk-go/engine/metax_aggregate.go`: cross-provider detail lookup, identity validation, source records, and field merge.
- `.github/overlays/metatube-sdk-go/route/*_test.go`: SDK route contract tests and helpers injected into the SDK checkout.
- `.github/scripts/integrate-category-routing.sh`: applies metaX routing/engine/route changes to a specific SDK checkout and formats touched Go files.
- `patches/metatube-sdk-go/*.patch`: targeted fixes to upstream provider behavior where needed.
- `docs/*`: compatibility findings, integration instructions, aggregation contract, and progress handoff.

### Pinned upstream dependency
The integration audit records MetaTube SDK commit `c62584675c593816cc07e7b5b977d14c7eb505a3`. Do not silently upgrade it. Any SDK revision change requires re-auditing patch applicability, API behavior, tests, and build compatibility. The integration script modifies files inside the checkout; validate it against a clean checkout at exactly the pinned revision.

## 4. Current implementation status

Source-level work already recorded in `docs/EXECUTION_CHECKLIST.md` and `docs/IMPLEMENTATION_AUDIT.md` includes:
- Amane ↔ SDK implementation audit and compatibility matrix.
- Amane-derived catalog-number classification and ordered category routing.
- Concurrent category search fan-out with deterministic configured-order results, per-provider error tolerance, and SDK search/cache fallback.
- Native provider implementations and deterministic local HTTP tests for several uncovered sites.
- SDK overlay for category search's merged candidate, detail metadata aggregation, conservative number matching, field fill/union rules, independent source records, and reserved-provider image resolution.
- SDK-route tests for the merged search candidate, default stock detail, `aggregate=true`, the reserved `MetaX` detail path, and rejection of explicit unknown categories.
- Regression assertion that `101926_01` and `10192601` remain distinct.

**Integration status (2026-10-20).** Focused SDK integration is verified on a clean checkout at `c62584675c593816cc07e7b5b977d14c7eb505a3` with Go 1.27.3: `routing`, `engine`, `route`, and 12 provider package test targets pass, and `go build ./cmd/server` succeeds. Full SDK regression still encounters two environment-only failures: `engine/dbengine` requires a Docker daemon, and `imageutil` expects Go 1.26 JPEG behavior. CI pins Go 1.26.x and sets `GITHUB_ACTIONS=true` to skip live provider tests. Do not treat unresolved full-regression environment checks as product failures, and do not re-run live-site tests as the only evidence.

## 5. Immediate next actions — do these in order

### Step A — establish a usable verification environment
1. Retry DNS and HTTPS diagnostics from the actual execution environment: inspect `getent ahosts github.com`, `curl -I https://github.com`, proxy environment variables, and configured DNS/resolver. Do not infer a single root cause from the previous error alone.
2. If this execution environment cannot access GitHub, use an available connected GitHub connector to retrieve repository files only as source inspection, or switch to a runtime with Git/network access. Do not pretend the connector file reads constitute a local checkout.
3. Obtain a clean SDK checkout at the exact pinned commit `c62584675c593816cc07e7b5b977d14c7eb505a3`.
4. Obtain/select Go `1.26` as declared by the project. If auto-toolchain download is used, verify network access first; do not lower `go.mod` merely to accommodate the old local compiler.
5. Record the exact SDK commit, Go version, OS/runtime, and network outcome in the checklist.

### Step B — run integration and fix defects before feature work
From the metaX workspace, apply `.github/scripts/integrate-category-routing.sh` to the clean pinned SDK checkout. Then run, in order:
1. `gofmt -w` on modified/injected Go files (the script already formats its target set; inspect its output and run a formatting check after integration).
2. Focused engine aggregation tests, including number normalization, underscore identity, scalar/list merging, and top-level/source-record JSON contract.
3. Focused SDK route contract tests: merged search candidate, default stock detail, `?aggregate=true`, and the reserved `MetaX` detail path.
4. Category classifier/routing tests, category search fan-out API tests, and rejection of explicit unknown categories.
5. Full SDK tests and relevant metaX Go tests.
6. Build the actual integrated server/runtime that imports the modified SDK and metaX provider packages.
7. Fix any failures, rerun the failing tests, and rerun the complete relevant verification sequence. Capture actual command results/CI run links in the checklist.

Do not begin the next feature until these checks are green or a concrete blocker is documented.

### Step C — verify runtime wiring
The source audit warns that adding a provider package to this repository does not automatically register it in a stock MetaTube server. Inspect the actual server's dependency/import/registration path and prove that each intended provider is loaded. If the SDK integration workspace is only a patching workspace, document how the production server consumes the patched SDK and native provider packages. Do not call provider support end-to-end complete until registration and an API request are demonstrated.

### Step D — complete the aggregation contract
After integration is green:
1. Review whether provider results are matched by canonical number consistently with Amane, including FC2 and uncensored cases.
2. Add deterministic tests for multiple successful providers, partial provider failure, no matching candidate, detail-fetch failure, identity mismatch, and route ordering.
3. Verify field-by-field merge behavior and stable source records; ensure the selected `provider + id` and canonical number do not change.
4. Decide explicitly whether actual raw HTTP/site payload capture is required. If required, design a bounded, opt-in, privacy-conscious payload contract separately; do not conflate it with structured `MovieInfo`.
5. Validate response compatibility with the Jellyfin plugin/client and document additive fields.

## 6. Subsequent roadmap (priority order)

1. **Integration correctness and test baseline** — highest priority; currently blocks confidence in all overlay changes.
2. **Production server/provider registration** — confirm new native providers are actually imported and reachable.
3. **Aggregation robustness** — multi-source/partial-failure/identity edge cases, response compatibility, and raw-payload decision.
4. **Catalog classification and route parity** — compare number families and ordered defaults against the pinned Amane source; test positives, negatives, and ambiguous identifiers. Keep the project-required combined FC2/uncensored route.
5. **Provider coverage** — inventory Amane movie crawlers against the SDK provider inventory, prioritize sites with stable APIs/HTML and manageable anti-bot behavior, implement one provider per task with deterministic fixtures/tests and a separate commit.
6. **Actor provider migration** — inventory Amane actor crawlers separately, compare actor interfaces/identity/image semantics, then migrate one site at a time.
7. **Operational hardening** — timeouts, proxy configuration, redirect/origin safety, response size limits, observability, race checks, and rate-limit behavior.
8. **Documentation/release readiness** — refresh compatibility matrix, integration instructions, known gaps, licensing/provenance, and reproducible build/test commands.
9. **Release discipline** — keep working on `feat/amane-provider-migration`; do not merge to `main` or claim live-site/end-to-end readiness without explicit approval and evidence.

## 7. Required handoff format after each task

At each task boundary, update `docs/EXECUTION_CHECKLIST.md` with:
- what changed and the relevant commit(s);
- exact commands/tests actually executed and pass/fail results;
- tests not executed and the concrete reason;
- known risks or blockers;
- the single next recommended task.

Stop after reporting the result and wait for test/integration/build outcomes before starting another task, unless the user explicitly asks to continue through the next step.

## 8. Useful source documents

- `docs/EXECUTION_CHECKLIST.md` — chronological task and verification record.
- `docs/IMPLEMENTATION_AUDIT.md` — Amane vs SDK behavior and architectural gap analysis.
- `docs/INTEGRATION.md` — pinned SDK integration and patch process.
- `docs/COMPATIBILITY_MATRIX.md` — provider and behavior comparisons.
- `docs/METADATA_AGGREGATION.md` — aggregation response and merge contract.
