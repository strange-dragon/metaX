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

The backend already provides the core MetaTube contract: category -> provider
selection, concurrent search fan-out, merged search candidates, field-level
detail aggregation, source records, bounded fan-out, startup
coverage/fingerprint checks, native providers, actor providers, and
request-scoped provider cookies.

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

## Big Task 3 — Official manufacturer router (NOT PLANNED)

Decision (2026-10-21): metaX follows the plugin/library model instead:
a Jellyfin library maps to one content category, that category maps to a list
of providers, and the backend queries all of them concurrently and merges the
results. The user then chooses a candidate in Jellyfin.

This model does **not** need Amane's `official_routes`, series-prefix routing,
studio-alias fallback, or manufacturer priority. Those mechanisms solve a
different problem: choosing one manufacturer site before scraping. They are
source-comparison evidence only and must not be reintroduced as a hidden
provider. If a manufacturer site is wanted later, add it as a normal provider
in the relevant category.

The former 3.1–3.5 checklist is therefore withdrawn. The only routing
configuration is category -> provider list.

## Small Task 3A — Category provider manifest and cookie scope

Goal: let the plugin send only cookies for providers in the current library
category, while keeping the backend safe if a caller sends a wider dictionary.

- [x] **3A.1 Manifest.** `GET /v1/metax/status` now returns
  `data.route_providers`: category -> registered provider names, with Amane
  route aliases resolved (`DMM` -> `FANZA`, `MGStage` -> `MGS`, `ThePornDB` ->
  `ThePornDBMovie`). Unavailable providers are omitted. No cookie, token, proxy,
  URL, or provider-configuration value is included.
- [x] **3A.2 Backend defensive scope.** Category-routed movie requests keep
  only cookies for providers in the resolved category. An explicit `provider`
  query wins over category routing and keeps that provider's alias cookies.
  Requests with neither a category nor an explicit provider (for example actor
  search) preserve the dictionary because their provider set is not
  category-routed; review/image paths follow the same rule.
- [x] **3A.3 Tests and docs.** Tests cover category filtering, explicit
  override, route aliases, unscoped requests, manifest contents, and an
  out-of-category provider that lacks a `ConfigSetter`. The plugin plan now
  documents `route_providers`, library-category mapping, and client-side cookie
  filtering. Plugin implementation remains deferred.
- **Evidence (2026-10-21).** Clean pinned SDK after patches + provider overlay:
  `GOTOOLCHAIN=local go test ./route ./routing -count=1` passed. MetaX:
  `GOTOOLCHAIN=local go test ./... -count=1` passed. CI on commit `717974d`:
  [SDK integration/build 39539784711](https://github.com/strange-dragon/metaX/actions/runs/39539784711),
  [Go tests 39539784696](https://github.com/strange-dragon/metaX/actions/runs/39539784696),
  [format 39539784702](https://github.com/strange-dragon/metaX/actions/runs/39539784702).
  Follow-up route cleanup `122e4f0` passed:
  [SDK integration/build 39540447925](https://github.com/strange-dragon/metaX/actions/runs/39540447925),
  [Go tests 39540447927](https://github.com/strange-dragon/metaX/actions/runs/39540447927),
  [format 39540447917](https://github.com/strange-dragon/metaX/actions/runs/39540447917),
  [live FC2/uncensored 39540447920](https://github.com/strange-dragon/metaX/actions/runs/39540447920).

## Small Task 3B — Amane route-order result sorting

Goal: use the Amane content-route order that already defines each category's
provider list as the returned-candidate priority, without importing Amane's
runtime or routing implementation.

- [x] **3B.1 Route priority.** Real-provider candidates sort by their position
  in the category route first. Match quality orders results only within one
  provider. The SDK numeric `Provider.Priority()` no longer controls ordering.
- [x] **3B.2 Merged candidate.** The reserved `MetaX` merged candidate remains
  the first result, ahead of every real-provider candidate.
- [x] **3B.3 Tests.** Added a deterministic engine test where the route-first
  provider has a much lower SDK priority than the second provider; the
  route-first candidate must still come first. Existing merge tests continue to
  assert `MetaX` first.
- [x] **3B.4 CI.** Go tests and the pinned-SDK patch/build job passed on commit
  `9e7191e`: [Go tests 39541866177](https://github.com/strange-dragon/metaX/actions/runs/39541866177),
  [SDK patch 39541866361](https://github.com/strange-dragon/metaX/actions/runs/39541866361).

## Provider tasks 4–6: Amane reference policy

Amane is pinned at `a2d45880e83df7047d461452965919692701e8c9` and is GPL-3.0.
The SDK/metaX side is Apache-2.0. We may reuse **behavior, algorithms, schema
knowledge, and test scenarios**, but do not copy Amane source code or captured
fixtures into this repository without an explicit license decision. Implement
clean-room Go code with independent deterministic tests.

## Big Task 4 — Prestige provider

Goal: exact SKU lookup through the JSON API.

Amane's `prestige.py` is the behavioral reference: try dashed, compact, GOOE
dashed, and GOOE compact SKUs against `/api/sku/item/{sku}`, follow
`parentProduct.uuid` to `/api/product/{uuid}`, and never use fuzzy
`/api/search`. The algorithm can be reimplemented; real responses are still
required before claiming support.

- [ ] **4.1 Obtain live test evidence.** Permitted-region responses or stable,
  independently sourced API fixtures for SKU, parent product, and product.
  Synthetic schema fixtures are committed for deterministic tests, but they are
  not captured site responses.
- [x] **4.2 Implement exact lookup.** Number/compact/GOOE variants; resolve
  `parentProduct.uuid`; reject fuzzy `/api/search` matches. Commit `b242fb9`.
- [x] **4.3 Map metadata.** Title, release, runtime, actresses, directors,
  genres, maker/label/series, plot, poster/package/gallery images.
- [x] **4.4 Harden transport and tests.** URL/origin validation, redirects,
  response limits, proxy/timeout/retry support, and deterministic synthetic
  fixtures.

**IMPLEMENTED, LIVE-UNVERIFIED**: the native `provider/prestige` package follows
Amane's SKU JSON flow and passes deterministic tests. Live access remains
CloudFront 403 from this network (`x-amz-cf-pop: SIN2-P9`), so no live-site
support is claimed. Prestige remains outside the default category routes,
matching Amane's default route table. CI passed focused tests, full pinned-SDK
regression, formatting, and server build in [run 39554383405](https://github.com/strange-dragon/metaX/actions/runs/39554383405);
Go tests passed in [39554383379](https://github.com/strange-dragon/metaX/actions/runs/39554383379).

## Big Task 5 — R18Dev offline database source (NOT PLANNED)

Decision (2026-10-21): **do not implement R18Dev.** Amane's source is not an
HTTP scraper; it reads a user-supplied offline PostgreSQL mirror. r18.dev
publishes a PostgreSQL-specific dump, not a per-title HTTP API. Supporting it
would add an optional external database, import workflow, and schema-coupling
contract that the category-route model does not require. R18Dev is also absent
from Amane's default content routes. Revisit only if a concrete product
requirement or a stable HTTP source appears.

The original comparison remains as evidence: Amane's `r18dev.py` +
`r18dev/repository.py` normalize `content_id` candidates (five-digit padding,
preserved padding, plain digits), query `derived_video` by exact `dvd_id` or
`content_id`, then load named entities, people, categories, and trailer.
Database errors degrade to a miss; they are not HTTP errors.

- [x] **5.1 Architecture decision.** Out of scope. Do not add a PostgreSQL
  dependency, import job, SQLite conversion, or separate Amane service for
  R18Dev.
- [ ] **5.2 Repository contract.** Not planned.
- [ ] **5.3 Tests.** Not planned.

**OUT OF SCOPE.** Do not disguise a database adapter as a website provider.

## Big Task 6 — Getchu fuzzy catalog search

Goal: support the legacy `www.getchu.com` catalog search only with exact
identity validation.

Amane's `getchu.py` is the reference for the legacy search URL/cookie flow and
result HTML shape, but its first-`soft.phtml`-result behavior must not be
reused. The implementation must add a normalized catalog-identity gate and
keep the existing `dl.getchu.com` detail provider separate.

- [ ] **6.1 Live evidence.** Obtain current catalog HTML/JSON and identify
  stable number/ID markers. Synthetic HTML fixtures cover the documented
  selectors but are not captured site pages.
- [x] **6.2 Exact identity rule.** Reject first-result behavior; require a
  normalized catalog/title match, or an exact numeric catalog ID, before
  returning a candidate.
- [x] **6.3 Implement searcher.** Patch `0017` adds `MovieSearcher` to the
  pinned Getchu provider while preserving the existing `dl.getchu.com`
  detail parser and numeric ID namespace.
- [x] **6.4 Tests and docs.** Deterministic search/detail, near-match
  rejection, numeric fallback, cookie propagation, and URL-origin tests were
  added.

**IMPLEMENTED, LIVE-UNVERIFIED**: exact `GETCHU-<digits>` lookup and catalog
search now exist. The legacy `www.getchu.com` search endpoint returned HTTP 403
with an empty body from this network, so live catalog behavior and current HTML
selectors remain unverified. The implementation never copies Amane's blind
first-result behavior. CI applied patch `0017`, passed focused tests, full
pinned-SDK regression, formatting, and server build in [run 39554383405](https://github.com/strange-dragon/metaX/actions/runs/39554383405).

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

- [x] **8.1 Refresh compatibility/access matrices** after completed tasks.
  Updated `Official` to withdrawn and `R18Dev` to out of scope.
- [x] **8.2 Consolidate focused/full CI evidence** and document environment-only
  failures. Local metaX tests/vet/tidy pass; clean pinned-SDK focused tests,
  formatting, and server build pass. Full local regression is limited to the
  known Docker and Go-version cases; CI is the full-regression gate.
- [x] **8.3 Review the final diff for API compatibility, dependency growth,
  duplicated helpers, and unrelated changes.** No module drift, no vet errors,
  no added secrets, and the Jellyfin-facing route/API tests pass.
- [ ] **8.4 Do not merge to `main` without explicit user approval.**

## Recommended execution order

1. **Big Task 8** release-readiness checks — complete through 8.3; 8.4 remains
   gated on explicit merge approval.
2. **4** Prestige and **6** Getchu are implemented as live-unverified in this
   branch. Promote them only after permitted-region responses or current page
   fixtures validate the parsers.

R18Dev is out of scope. Small Tasks 3A and 3B are complete: the manifest/cookie
boundary is in place, and candidate ordering follows the Amane category route
with `MetaX` first. The Jellyfin plugin still needs its client-side
cookie-filtering implementation.

Each item should be its own small commit with focused tests, a checklist entry,
and CI evidence. Do not start a second item until the current one is verified.
