# metaX backend task breakdown

Last updated: 2026-10-21
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
- [x] **2.2 ThePornDB actor audit.** Compared the pinned SDK actor provider with
  Amane's actor source. The concrete gap was config plumbing: both ThePornDB
  actor and scene/movie providers declared `SetConfig(map[string]string)`, which
  does not satisfy `provider.ConfigSetter`, so the engine never applied the
  configured access token. Patch `0016` switches both providers to
  `provider.Config`, reads `ACCESS_TOKEN` or `api_token`, delegates cookie
  transport settings to the shared scraper, and adds compile-time and
  deterministic tests. The GraphQL-vs-REST architecture and field differences
  remain documented rather than rewritten.
- [x] **2.3 Decide whether actor profile aggregation is required.** Decision:
  do not add actor aggregation now. The plugin has no actor-detail aggregation
  contract, and identity/merge rules for profile, image, and biography sources
  are not specified. Keep the movie-path `MetaX` provider as the only
  aggregation contract until a concrete plugin requirement exists.
- [x] **2.4 Live actor checks (optional).** Not run in this pass. No live-only
  claim is made; deterministic provider tests remain the regression coverage.

Acceptance for 2.2 is met: the comparison is documented, the concrete config
gap has a focused patch and deterministic tests, and no live-only claim is
made. Big Task 2 is complete for required work; 2.4 remains optional.

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

- [x] **7.1 Shared retry policy.** `provider/httpretry` retries idempotent
  `GET`/`HEAD`/`OPTIONS` requests on 408/429/503/504 and transient network
  errors. It uses at most three attempts, 200 ms exponential base delay, a 2 s
  cap, ±20% jitter, and bounded `Retry-After` seconds/date handling. Permanent
  4xx responses, unsafe methods, and unreplayable request bodies are never
  retried. All thirteen native provider `net/http` transports use it.
  Documented exceptions: JavLibrary's separate browser-fingerprint `fhttp`
  rotation path, AVSOX's POST search, and upstream scraper providers are not
  wrapped by this shared transport.
- [x] **7.2 Rate-limit behavior.** Each provider transport owns its own retry
  policy and backoff state; there is no global retry mutex or token bucket, so
  a retrying provider cannot serialize unrelated providers. The bounded engine
  fan-out (`METAX_AGGREGATE_FANOUT`, default 16) remains request-scoped. A
  deterministic test proves one provider's backoff does not block another.
- [x] **7.3 Health/status surface.** `GET /v1/metax/status` exposes registered
  movie/actor provider counts, the route-table fingerprint and category count,
  route provider gaps, and cookie-transport version `1`. It never exposes
  cookies, tokens, proxy credentials, or provider configuration values.
- [x] **7.4 Request cancellation.** Evaluated and deliberately deferred. The
  provider interfaces do not accept `context.Context`; adding a context setter
  would require request-scoped provider cloning and an interface change. Gin
  request cancellation therefore does not reach upstream provider HTTP calls.
  Existing bounds are per-client timeouts and retry attempts that stop when the
  transport request context is canceled. Revisit only with a measured need and
  an interface-preserving design.
- [x] **7.5 Tests.** Deterministic tests cover retry status boundaries,
  permanent 4xx, unsafe/unreplayable requests, attempt exhaustion, numeric and
  HTTP-date `Retry-After`, transient/permanent network errors, context
  cancellation before and during backoff, per-provider isolation, status JSON,
  route gaps, cookie-transport version, and absence of secret fields. Log
  redaction tests cover proxy credentials, query tokens, malformed proxy
  values, and malformed cookie settings.

Acceptance: no unbounded retries, no secret values in status, and retry/status
behavior deterministic under test. Full pinned-SDK live regression remains
subject to the documented regional and anti-bot limits; focused integration and
server build are the release gate for this task.

## Big Task 8 — Release readiness

- [ ] **8.1 Refresh compatibility/access matrices** after each completed task.
- [ ] **8.2 Consolidate focused/full CI evidence** and document environment-only
  failures.
- [ ] **8.3 Review the final diff for API compatibility, dependency growth,
  duplicated helpers, and unrelated changes.**
- [ ] **8.4 Do not merge to `main` without explicit user approval.**

## Recommended execution order

1. **3.1** Official router contract — large, but the only remaining
   high-impact provider gap that can be designed without live scraping.
2. Blocked tasks 4–6 only after evidence or an explicit architecture decision.
3. Big Task 8 release-readiness checks after the remaining provider work.

Each item should be its own small commit with focused tests, a checklist entry,
and CI evidence. Do not start a second item until the current one is verified.
