# metaX backend task breakdown

Last updated: 2026-10-20
Branch: `feat/amane-provider-migration`

This is the working breakdown for the remaining backend work. It is deliberately
separate from the Jellyfin plugin plan in `docs/JELLYFIN_PLUGIN.md`.

Status legend:

- **DONE** — implemented, tested, and recorded.
- **NEXT** — unblocked and small enough to start.
- **PLANNED** — agreed direction, not started.
- **BLOCKED** — needs evidence, a configuration decision, or live access before
  implementation would be honest.

The backend already provides the core MetaTube contract: category routing,
concurrent search fan-out, merged search candidates, field-level detail
aggregation, source records, bounded fan-out, startup coverage/fingerprint
checks, native providers, actor providers, and request-scoped provider cookies.

## Big Task 1 — Request-scoped provider cookies

Goal: the plugin can send `provider name -> cookie header` per request without
creating a second server-side cookie store.

- [x] **1.1 Header contract.** `X-MetaX-Provider-Cookies` as a JSON string
  dictionary, 16 KiB limit, malformed/trailing JSON, empty names, and CR/LF
  rejected.
- [x] **1.2 Request isolation.** `Engine.WithRequestCookies` creates fresh
  provider instances from registered factories, overlays the request cookie on
  startup configuration, and rebuilds host lookups. Shared instances are never
  mutated.
- [x] **1.3 Route coverage.** Applied to movie/actor search, detail, reviews,
  and image routes.
- [x] **1.4 Remove the wrong architecture.** The server-side
  `PUT /v1/providers/config` endpoint was removed; the plugin configuration is
  the only persistent source.
- [x] **1.5 Route aliases in the dictionary.** Accept the documented route
  aliases (`DMM`, `MGStage`, `ThePornDB`) in addition to registered provider
  names, with conflict detection.
- [x] **1.6 Failure semantics.** Unknown provider names are ignored; a known
  provider without `ConfigSetter` returns HTTP 400; cookie values are not
  logged or echoed.
- [x] **1.7 Tests.** Header validation, unsupported provider, unknown provider,
  actor path, host lookup, CR/LF, and concurrent differing-cookie isolation.
- [x] **1.8 CI.** Go tests, focused SDK tests, full pinned-SDK regression,
  formatting, and server build are green for commit `6280829`.

Acceptance for 1.5: alias expansion is deterministic, duplicate/conflicting
values are rejected, and the behavior is documented in `PROVIDER_ACCESS.md`.

## Big Task 2 — Actor coverage and enrichment

Goal: cover the remaining actor-source behavior without turning image indexes
into fake profiles.

- [x] **2.1 Verify Gfriends image enrichment.** The pinned SDK already injects
  Gfriends images into Japanese actor profiles in `engine/actor.go`; Gfriends is
  not a missing provider and does not need to be modeled as a profile source.
  This corrects the older deferred note in `COMPATIBILITY_MATRIX.md`.
- [ ] **2.2 ThePornDB actor audit.** Compare the pinned SDK actor provider with
  Amane's actor source, focusing on URL parsing, token requirements, and field
  mapping. Add a deterministic patch/test only if a concrete gap exists.
- [ ] **2.3 Decide whether actor profile aggregation is required.** The movie
  path has a reserved `MetaX` provider; the actor path does not. Do not add a
  second aggregation contract until the plugin needs it and identity/merge rules
  are specified.
- [ ] **2.4 Live actor checks.** Optional and point-in-time only; deterministic
  tests must remain the regression coverage.

Acceptance for 2.2: a documented comparison with either "no code change" or a
focused, tested patch. No live-only claims.

## Big Task 3 — Official manufacturer router

Goal: reproduce Amane's official-studio fallback without hard-coding a partial
provider.

- [ ] **3.1 Contract decision.** Define how per-user `official_routes` and
  previous-provider studio metadata reach a router. The stock
  `MovieSearcher.SearchMovie(keyword)` signature has no studio context.
- [ ] **3.2 Route/alias table.** Implement deterministic manufacturer and
  series-prefix resolution in project configuration or code, with tests for
  explicit routes, series prefixes, aliases, and ambiguity.
- [ ] **3.3 Manufacturer adapter.** Implement the shared Works-like extraction
  only after the route contract exists; preserve origin, redirect, size, and
  timeout rules.
- [ ] **3.4 Integration and coverage.** Register the router, verify startup
  coverage logs, and add deterministic route/parser tests.
- [ ] **3.5 Documentation.** Record the configuration schema, priority order,
  and known manufacturer gaps.

Blocked by 3.1. Do not implement a static `Official` provider that silently
loses explicit-route and studio-context precedence.

## Big Task 4 — Prestige provider

Goal: exact SKU lookup through the JSON API.

- [ ] **4.1 Obtain test evidence.** Permitted-region responses or stable,
  independently sourced API fixtures for SKU, parent product, and product.
- [ ] **4.2 Implement exact lookup.** Number/compact/GOOE variants; resolve
  `parentProduct.uuid`; reject fuzzy `/api/search` matches.
- [ ] **4.3 Map metadata.** Title, release, runtime, actresses, directors,
  genres, maker/label/series, plot, poster/package/gallery images.
- [ ] **4.4 Harden transport and tests.** URL/origin validation, redirects,
  response limits, and deterministic fixtures.

**BLOCKED**: no permitted-region response or reliable fixture is currently
recorded; live access is reported as CloudFront 403/region-restricted.

## Big Task 5 — R18Dev offline database source

Goal: decide whether metaX should support an optional local PostgreSQL mirror.

- [ ] **5.1 Architecture decision.** A local database is not a normal HTTP
  `MovieProvider`. Decide whether to add a separate optional metadata source
  interface, keep it out of scope, or expose it as an external service.
- [ ] **5.2 Repository contract.** If approved, define schema/query boundaries,
  failure degradation, and configuration.
- [ ] **5.3 Tests.** Deterministic database tests without requiring a live
  mirror; document the operational dependency.

**BLOCKED** by 5.1. Do not disguise a database adapter as a website provider.

## Big Task 6 — Getchu fuzzy catalog search

Goal: support the legacy `www.getchu.com` catalog search only with exact
identity validation.

- [ ] **6.1 Evidence.** Obtain current catalog HTML/JSON and identify stable
  number/ID markers.
- [ ] **6.2 Exact identity rule.** Reject first-result behavior; require a
  normalized catalog match before returning a candidate.
- [ ] **6.3 Implement searcher.** Keep the existing digital-download detail
  provider and ID namespace separate.
- [ ] **6.4 Tests and docs.** Deterministic search/detail fixtures, ambiguity
  cases, and route coverage.

**BLOCKED**: current live catalog behavior and reliable fixtures are missing.

## Big Task 7 — Request resilience and observability

Goal: make transient failures and operational state visible and bounded.

- [ ] **7.1 Shared retry policy.** Retry idempotent requests on 408/429/503/504
  with capped attempts, exponential backoff, jitter, and `Retry-After` support;
  do not retry permanent 4xx. Start with native providers, then assess scraper
  providers.
- [ ] **7.2 Rate-limit behavior.** Define per-provider limits/backoff without
  serializing unrelated providers.
- [ ] **7.3 Health/status surface.** Expose provider counts, route fingerprint,
  registered gaps, and cookie-transport version without exposing secrets.
- [ ] **7.4 Request cancellation.** Evaluate passing Gin request context into
  provider HTTP calls; preserve existing provider interfaces where possible.
- [ ] **7.5 Tests.** HTTP fixture tests for retry boundaries, cancellation,
  status output, and no secret leakage.

Acceptance: no unbounded retries, no secret values in status/logs, and all
behavior deterministic under test.

## Big Task 8 — Release readiness

- [ ] **8.1 Refresh compatibility/access matrices** after each completed task.
- [ ] **8.2 Consolidate focused/full CI evidence** and document environment-only
  failures.
- [ ] **8.3 Review the final diff for API compatibility, dependency growth,
  duplicated helpers, and unrelated changes.**
- [ ] **8.4 Do not merge to `main` without explicit user approval.**

## Recommended execution order

1. **1.5** route aliases for the cookie dictionary — small and directly closes
   the current contract.
2. **2.2** ThePornDB actor audit — deterministic and unblocked.
3. **7.3** health/status surface — useful for operations without touching
   metadata semantics.
4. **7.1** shared retry policy — highest-value resilience work.
5. **3.1** Official router contract — large, but the only remaining
   high-impact provider gap that can be designed without live scraping.
6. Blocked tasks 4–6 only after evidence or an explicit architecture decision.

Each item should be its own small commit with focused tests, a checklist entry,
and CI evidence. Do not start a second item until the current one is verified.
