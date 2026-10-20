# Amane ↔ MetaTube implementation audit

Audit date: 2026-10-20

This document is the durable handoff for the implementation-level comparison. Read it before repeating repository exploration or designing the next aggregation task. It complements (rather than replaces) the source-by-source records in `COMPATIBILITY_MATRIX.md`.

## Pinned source snapshots

- Amane: [`sqzw-x/amane`](https://github.com/sqzw-x/amane), commit `a2d45880e83df7047d461452965919692701e8c9` (GPL-3.0).
- MetaTube SDK: [`metatube-community/metatube-sdk-go`](https://github.com/metatube-community/metatube-sdk-go), commit `c62584675c593816cc07e7b5b977d14c7eb505a3` (Apache-2.0).
- metaX integration workspace: branch `feat/amane-provider-migration`. The branch contents were read directly for this audit. Individual file blob SHAs are not commit SHAs; re-read the branch before making code changes.

No live-site requests, Go tests, formatter, or integration build were run for this audit. This is a source-level comparison, not a runtime parity claim.

## Executive conclusion

**Amane's main differentiator is not simply parallel site scraping. It is field-level, source-prioritized metadata aggregation.** The current MetaTube SDK already has a strong multi-provider *search* path: it searches providers concurrently, tolerates per-provider search failures, applies provider priorities, deduplicates by provider + ID, and can merge cached search candidates. The metaX category overlay reuses that SDK search path and adds ordered route fan-out.

However, the SDK search path returns multiple `MovieSearchResult` candidates. It does **not** combine different providers' detailed `MovieInfo` records into one field-enriched movie. The existing detail path retrieves metadata from the selected provider and uses the SDK's cache; it is not equivalent to Amane's field-level aggregation engine. This is the clearest architectural gap to address after this audit.

## 1. Amane behavior verified from source

### Crawler contract

Relevant source:
- `src/amane/crawlers/base.py`
- `src/amane/crawlers/models.py`
- `src/amane/crawlers/sites/__init__.py`
- `src/amane/crawlers/actor/sites/__init__.py`

Observed behavior:
- The default movie-crawler lifecycle is `fetch(query, options) → _search(query, options) → _scrape(url, options)`; specialized crawlers may override `fetch()`.
- A crawler profile declares its name, base URL, headers, cookies, capabilities, behavioral traits, and optional actor-gender support. Site configuration can override base URL and cookies.
- Movie and actor crawlers use separate registries/contracts. Amane's source inventory exports 24 movie crawlers and 6 actor crawlers in the pinned snapshot.
- `MediaMetadata` carries title, actors (with gender), studio, publisher, release, runtime, tags, series, plot, poster/thumb/trailer URLs, score, external ID, source URL, directors, and extra-fanart URLs. Runtime is normalized to minutes and release dates to calendar dates.
- The crawler contract returns a source result or a miss; aggregation, rather than an individual crawler, owns cross-source selection.

### Aggregation engine

Relevant source:
- `src/amane/aggregate/engine.py`
- `src/amane/aggregate/models.py`
- `src/amane/config/manager.py`
- `src/amane/crawlers/site_roles.py`

Observed behavior:
- `compile_priority(route, prefer, exclude)` builds ordered per-field source chains. The route is the eligibility boundary; field-specific preferences reorder only eligible sources, and exclusions remove sources from that field.
- The execution graph can distinguish a source from its language variant. Independent nodes are fetched concurrently.
- Aggregation has two phases. Sources marked `NEEDS_PARTIAL` run in a later phase and can receive a read-only partial result containing resolved scalar fields and first-phase image URLs.
- Scalar fields (title, actors, tags, release, runtime, directors, series, studio, publisher, plot) resolve to the first non-empty value in the field's source chain. A field is not prematurely finalized while an earlier source in that chain is still unprocessed.
- URL collections (poster, thumbnail, trailer) and extra-fanart are aggregated separately. Scores preserve source identity instead of pretending unlike rating scales are directly comparable. External IDs, source URLs, and field-source provenance are retained.
- The result reports metadata, per-field source attribution, failed sources, queried sources, and raw source payloads. Missing/disabled sources are treated as empty nodes rather than crashing the whole aggregation.
- Actor metadata has dedicated aggregation/merge behavior; it should not be forced into movie-only provider methods.

### Amane default content routes

The pinned `src/amane/config/manager.py` defines ordered defaults:
- Censored: DMM → JavDB → JavBus → Official.
- Uncensored: JavDB → JavBus → AVSOX → Freejavbt.
- FC2: JavDB → FC2PPVDB → FC2 → FreeJAVBT.
- Chinese: IQQTV → JavDB → Airav → FreeJAVBT.
- Amateur: MGStage → DMM → JavDB → JavBus.
- Western: ThePornDB → JavDB → FreeJAVBT.
- Hentai: Getchu → DMM → JavDB.

These are source defaults, not proof every site is available in a target build. The metaX project deliberately merges FC2 and uncensored into one shared route, per the project requirement; do not “correct” this to Amane's separate routes.

## 2. MetaTube SDK behavior verified from source

Relevant source at the pinned SDK commit:
- `provider/provider.go`
- `engine/init.go`
- `engine/register.go` (registration path previously verified and documented in `INTEGRATION.md`)
- `engine/movie.go`
- `model/movie.go`

Observed behavior:
- Movie providers implement the SDK's `MovieProvider` contract; search is an optional `MovieSearcher` interface. Actor providers/searchers are separate interfaces.
- Provider packages must be registered through the SDK factory/engine import path. A standalone metaX dependency does not automatically register providers in a stock server.
- `SearchMovieAll` runs provider searches concurrently. Individual provider errors are logged and skipped; successful candidates remain available.
- Search post-processing validates candidates, removes duplicate identities using provider + ID, and ranks results using number similarity multiplied by provider priority.
- The SDK's optional search fallback queries its database and merges cached search candidates; the provider-specific search helper also preserves this behavior.
- `MovieSearchResult` intentionally carries provider + ID along with number/title/homepage and summary display fields. The provider + ID pair is the detail identity.
- Detail lookup is provider-specific: it resolves a provider ID/URL, may use cached `MovieInfo` in lazy mode, fetches from that provider when needed, validates the result, and stores successful metadata. This is not a multi-source detail aggregation pipeline.
- SDK `MovieInfo` contains director, actors, multiple image/video fields, maker/label/series, genres, score, runtime, and release date. It is a useful existing schema and should remain the external model unless a demonstrated compatibility gap requires otherwise.

## 3. Current metaX orchestration behavior

Relevant files:
- `routing/routes.go`
- `.github/overlays/metatube-sdk-go/engine/metax_search.go`
- `.github/scripts/integrate-category-routing.sh`
- `.github/overlays/metatube-sdk-go/route/metax_category_fanout_test.go`
- `docs/INTEGRATION.md`

Already implemented in source:
- Category routing is ordered, filters out unregistered providers, preserves explicit-provider override, maps DMM to the registered FANZA provider, and retains the combined FC2/uncensored route.
- The SDK overlay concurrently calls the selected providers and writes responses back in configured route order. A provider error does not discard successful provider results.
- It reuses the SDK's `searchMovie` helper, so normalization, provider-specific cache fallback, and SDK semantics are not independently reimplemented.
- Results are deduplicated by provider + ID, validated, and ranked by number match and provider priority; configured route order breaks ties.
- The API retains MetaTube's existing response envelope/model and the provider + ID identity used for detail lookup.
- Automatic inference now recognizes Amane-style catalog families: ordinary censored catalog numbers, known uncensored prefixes and sequence IDs, FC2, Chinese MD/MKY, amateur prefixes, KIN8/Western studio-date IDs, and Getchu identifiers. A few non-catalog provider IDs use source-specific hints.

Limitations to carry forward:
- This is a search fan-out layer, not Amane's field-level metadata aggregation.
- Classification remains intentionally conservative where a number or provider identity does not identify a route; such detail requests return baseline-only metadata rather than fan-out. Directory-based type overrides are unavailable at the provider-ID API boundary.
- `Official` is a manufacturer-routing concept in Amane, not one ordinary SDK scraper. Do not assume a registered provider named `Official` exists.
- A category with no registered route providers and an unknown category are different failure modes; preserve the existing explicit-provider and URL lookup precedence.
- The route overlay has source-level tests, but this audit did not run them or build the integrated SDK.

## 4. Cross-source capability gap and recommended design boundary

| Capability | Amane | MetaTube SDK / current metaX | Decision |
|---|---|---|---|
| Concurrent provider search | Yes | SDK yes; metaX adds ordered category fan-out | Reuse current search path; do not create another search engine |
| Per-provider search failure tolerance | Yes | Yes in SDK/metaX search fan-out | Keep; add tests only for uncovered edge cases |
| Candidate deduplication | Source-dependent | Provider + ID | Keep provider + ID as search/detail identity |
| Cached search fallback | Amane has its own persistence flow | SDK DB fallback already exists | Reuse SDK behavior |
| One final record enriched field-by-field from multiple sources | Yes | Not in current search/detail orchestration | Main missing capability; design separately |
| Field-specific source priority and provenance | Yes | Provider-wide priority only in search ranking | Needs an explicit metadata-merge policy if added |
| Multi-language per-field selection | Yes | No equivalent generic orchestration identified in the reviewed path | Evaluate only if required; avoid expanding scope before basic merge semantics |
| Two-phase sources that consume partial metadata | Yes | No equivalent generic mechanism identified in the reviewed path | Defer until a real provider demonstrates the requirement |
| Source-attributed scores, extra-fanart, external IDs and source URLs | Yes | Some fields exist in `MovieInfo`; no equivalent generic aggregation result/provenance object | Map only what fits SDK models; avoid silently discarding or misrepresenting fields |
| Actor profile/image aggregation | Separate Amane subsystem | Separate SDK actor interfaces | Separate workstream; do not mix into movie aggregation |
| Source failure/provenance reporting | `failed_sites`, `sites_queried`, `field_sources`, raw payloads | Logs/errors and SDK API models | Decide what is necessary without changing the public plugin contract |

### Implemented first pass: detail aggregation plus source records

The project owner has chosen to include Amane-style detail aggregation. The first integration pass is implemented in `.github/overlays/metatube-sdk-go/engine/metax_aggregate.go` and wired into the SDK detail route by `.github/scripts/integrate-category-routing.sh`.

- The existing detail response keeps the original `MovieInfo` fields and adds `category` and `sources` to the same `data` object. `?aggregate=false` opts out and returns the stock detail response.
- The selected provider + ID remain the top-level public identity. Supplemental records are accepted only after a normalized catalog-number match.
- Scalar fields use route-order first-non-empty semantics; list fields are stably deduplicated; scores are not averaged across unlike provider scales.
- Each source record keeps the provider name, ID, status, diagnostic error where relevant, and that provider's independent structured SDK metadata. This is not raw HTML/HTTP/API wire data; the existing `MovieProvider` interface does not expose those payloads.
- Unknown/ambiguous categories do not trigger speculative fan-out. The selected provider record is returned with a `baseline_only` source status and an explanation.
- Regression tests were added to the SDK overlay, but the integration script, tests, formatter, and build have **not yet been run** after this change. Do not treat compilation or runtime compatibility as verified.

### Remaining aggregation work

1. Apply the overlay to the pinned SDK checkout and fix any compile/test issues without triggering GitHub Actions.
2. Add endpoint-level contract tests for the additive `sources` response and `aggregate=false` compatibility path.
3. Expand automatic classification only where source evidence supports a high-confidence category; keep ambiguous IDs on the safe baseline-only path.
4. Consider Amane's per-field preference configuration and two-phase partial-source protocol only after the basic integration builds and deterministic tests pass.
5. If raw source-site HTML/JSON is required, design a separate provider result contract; do not mislabel normalized `MovieInfo` as wire-level raw data.

## 5. Existing documentation to consult before repeating work

- `docs/COMPATIBILITY_MATRIX.md`: source-by-source findings, patches, and per-site verification status. Blank or pending means unverified.
- `docs/INITIAL_FINDINGS.md`: initial comparisons and rationale for not rewriting existing SDK providers wholesale.
- `docs/INTEGRATION.md`: SDK registration path, integration boundary, and delivery constraints.
- `docs/MIGRATION_PLAN.md`: scope, migration stages, licensing, and definition of done.
- `docs/LICENSING.md` and `NOTICE.md`: Amane GPL-3.0 source/fixture restrictions and attribution policy.
- `docs/EXECUTION_CHECKLIST.md`: durable task handoff and verification history.

## Audit completion status

- [x] Read the pinned Amane crawler contract, metadata model, movie/actor registries, field-level aggregation engine, aggregation result model, and default route definitions.
- [x] Read the pinned SDK provider interfaces, engine search/detail path, and movie model.
- [x] Compared those behaviors with the current metaX routing/search overlay.
- [x] Recorded the architecture gap, implementation boundaries, aggregation contract, and verification boundary above.
- [ ] Tests, formatting, integrated build, live sites, and Jellyfin runtime were not run as part of this audit.
