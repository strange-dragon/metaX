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

- Request-scoped provider cookies from the Jellyfin plugin: the server accepts a
  per-request `X-MetaX-Provider-Cookies` JSON dictionary, creates request-local
  provider instances, and never stores or replays cookies. The old server-side
  runtime configuration endpoint was removed.
- Native actor providers for JavDB, Minnano, AVBase, and Wikipedia/Wikidata.
  The stock SDK's Gfriends image injection for Japanese actor profiles was
  verified; it is not a missing provider. The stock ThePornDB actor provider was
  audited and its config plumbing fixed so `ACCESS_TOKEN`/`api_token` reaches
  the provider; actor-profile aggregation remains intentionally unimplemented.
- Big Task 7 adds a bounded shared retry transport for native providers and a
  non-secret `GET /v1/metax/status` surface. Retry attempts, delays, jitter, and
  `Retry-After` are per provider; there is no global retry serialization.
  JavLibrary's browser-fingerprint `fhttp` path, AVSOX's POST search, and
  upstream scraper providers remain documented exceptions. Gin request
  cancellation is not propagated through provider interfaces; that limitation
  is recorded rather than hidden behind a partial context setter.
- Remaining backend coverage and hardening are tracked as big/small tasks in
  `docs/BACKEND_TASKS.md`; plugin-side work is recorded but intentionally
  deferred in `docs/JELLYFIN_PLUGIN.md`.

**Integration status (2026-10-21).** Focused SDK integration is verified on a clean checkout at `c62584675c593816cc07e7b5b977d14c7eb505a3` with Go 1.27.3: `routing`, `engine`, `route`, `provider/httpretry`, and the thirteen native provider package test targets pass, and `go build ./cmd/server` succeeds. A full pinned-SDK regression was also attempted; it did not pass because upstream live provider tests hit the documented regional and anti-bot blocks (for example FANZA/FALENO/FC2HUB/FC2PPVDB/MGS/SOD). The existing `engine/dbengine` Docker requirement and Go-version-sensitive `imageutil` check remain environment caveats. CI pins Go 1.26.x and sets `GITHUB_ACTIONS=true` to skip live provider tests. Do not claim a full live regression pass, and do not re-run live-site tests as the only evidence.

## 5. Current execution plan

The environment and core-backend blockers are settled. Request-scoped cookies,
the ThePornDB actor audit, and Big Task 7 (status, bounded retry, per-provider
backoff, cancellation evaluation, and deterministic tests) are complete.
Remaining work is maintained as small, independently verifiable tasks in
`docs/BACKEND_TASKS.md`. The next unblocked task is:

1. the Official manufacturer-router contract, which must be designed before it
   is implemented.

Prestige, R18Dev, and fuzzy Getchu work remain blocked on permitted-region
responses/fixtures or an explicit data-source architecture decision. Do not
claim any blocked source works from configuration alone.

At each task boundary, update `docs/EXECUTION_CHECKLIST.md` with the commit,
commands actually run, pass/fail outcomes, and the next small task. Stop after
reporting the result unless the user explicitly asks to continue.

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
- `docs/BACKEND_TASKS.md` — big/small backend task breakdown and blockers.
- `docs/JELLYFIN_PLUGIN.md` — deferred plugin configuration and transport plan.
