# metaX

metaX is a native Go Provider extension and migration workspace for bringing additional site-scraping capabilities into MetaTube, so the existing MetaTube Jellyfin plugin can use those providers through the MetaTube service.

**Target architecture:** Jellyfin → existing MetaTube Jellyfin plugin → MetaTube API → existing and additional native Go Providers.

Amane is a functional reference for capability coverage and edge cases, not a separate service that Jellyfin should call. New Go code is written for MetaTube's Provider interfaces and compared against existing SDK implementations before deciding what to keep or improve.

## License and attribution

metaX keeps Apache-2.0 for project-authored code and MetaTube-derived code, subject to the original notices. Amane is acknowledged as a functional reference; its GPL-3.0 source and fixtures are not relicensed by attribution alone. See [LICENSE](LICENSE), [NOTICE.md](NOTICE.md), and [docs/LICENSING.md](docs/LICENSING.md).

## Current status

- The `feat/amane-provider-migration` branch currently contains 13 native Go Provider packages: AVBaseActor, Airav, AVSOX, FC2Club, FreeJAVBT, GIGA, IQQTV, Jav321, JavDB (movie and actor), JavLibrary, Minnano (actor), WikipediaActor, and XCity.
- The branch maintains 13 unique SDK patches for existing-provider correctness and URL/ID validation. The permanent SDK patch workflow applies the patch set and runs focused deterministic tests plus the full upstream SDK regression suite.
- A reproducible build entry point applies all 13 maintained SDK patches, overlays native providers, verifies engine registration and `/v1/providers` discovery, runs the pinned SDK regression suite, and builds the server binary. The four-provider version passed in [run 39139447663](https://github.com/strange-dragon/metaX/actions/runs/39139447663); the earlier eleven-package build passed in [run 39139794806](https://github.com/strange-dragon/metaX/actions/runs/39139794806). The expanded twelve-package build, including `AVBaseActor`, passed in [run 39207291492](https://github.com/strange-dragon/metaX/actions/runs/39207291492). The thirteen-package build including `WikipediaActor` passed in [run 39402434294](https://github.com/strange-dragon/metaX/actions/runs/39402434294).
- These are migration-workspace packages, not yet a claim of end-to-end runtime integration: the existing MetaTube engine must import the Provider packages (or merge them into the SDK) for `init()` registration to take effect. Live-site verification and Jellyfin/MetaTube integration remain pending.
- A focused audit of all six Amane actor-source contracts is recorded in the compatibility matrix. The pinned SDK engine's registration mechanism is confirmed to be `engine/register.go`; native packages in this separate workspace still need to be integrated into the SDK provider tree or another supported server build path.
- Current overall migration estimate is approximately 40%; this is a stage-weighted estimate, not a live-site success rate. See [migration plan](docs/MIGRATION_PLAN.md) for the breakdown and remaining integration work.
- No live-site support is claimed until current site behavior and integration are verified.

## Engineering documents

- [Contributor instructions](AGENTS.md)
- [Migration plan](docs/MIGRATION_PLAN.md)
- [Compatibility matrix](docs/COMPATIBILITY_MATRIX.md)
- [Initial implementation comparison](docs/INITIAL_FINDINGS.md)
- [Implementation-level Amane ↔ MetaTube audit](docs/IMPLEMENTATION_AUDIT.md)
- [Metadata aggregation and source-record API](docs/METADATA_AGGREGATION.md)
- [Provider access, cookies, and proxy configuration](docs/PROVIDER_ACCESS.md)
- [Licensing and attribution policy](docs/LICENSING.md)

## Reproducible integrated server build

The branch includes a reproducible build script that applies 13 maintained SDK patches, integrates thirteen native provider packages, applies the category-search overlay, runs engine/API contract tests and upstream regression tests, then builds a MetaTube server binary. It requires a clean checkout of the pinned SDK revision.

```bash
git clone https://github.com/metatube-community/metatube-sdk-go.git ./metatube-sdk-go
git -C ./metatube-sdk-go checkout c62584675c593816cc07e7b5b977d14c7eb505a3
bash .github/scripts/build-metatube-server.sh ./metatube-sdk-go ./dist/metatube-server
```

The script refuses a dirty SDK checkout or a different commit. The generated binary includes ten movie providers (`Airav`, `AVSOX`, `FC2Club`, `Freejavbt`, `GIGA`, `IQQTV`, `Jav321`, `JavDB`, `JavLibrary`, `XCity`) and four actor providers (`AVBaseActor`, `JavDBActor`, `MinnanoActor`, `WikipediaActor`), plus the maintained SDK patches. The earlier eleven-package integration passed in [GitHub Actions run 39191776372](https://github.com/strange-dragon/metaX/actions/runs/39191776372). The earlier twelve-package integration, including `AVBaseActor`, passed in [run 39207291492](https://github.com/strange-dragon/metaX/actions/runs/39207291492), including provider registration, `/v1/providers` API discovery, focused tests, the full pinned SDK regression suite, and server compilation. Live API checks were added in `.github/scripts/live-metax-api-smoke.sh` and the `Live MetaX API smoke test` workflow; they cover censored/FC2 search, `MetaX` detail plus `sources[]`, images, unknown-category errors, and fingerprint-pin startup failure. A running Jellyfin deployment remains unverified. The workflow uploads the built binary as a seven-day artifact after all checks pass.


## Backend orchestration work

The backend orchestration layer is being implemented separately from the Jellyfin plugin.

- The `routing` package defines category routes for censored, uncensored, FC2, Chinese-subtitle, amateur, western, and hentai content (`animation` remains an alias). The FC2 route includes Airav, matching Amane’s default route. It preserves explicit provider selection as a manual override and skips providers not registered in the current SDK build.
- The SDK overlay adds an optional `category` query parameter to `/v1/movies/search`. For example, `/v1/movies/search?q=ABC-123&category=uncensored` fans out concurrently to all registered providers in that category. Omitting `category` preserves MetaTube's original all-provider behavior; specifying `provider` preserves single-provider behavior.
- Search results keep MetaTube's existing response envelope and model. Category searches prepend one merged candidate (`provider=MetaX`, `id=<catalog number>`) that fills empty display fields from later route results, while retaining each provider's original `provider` + `id` result in route order, so the Jellyfin plugin can offer both the merged record and the original provider records. Per-provider errors and empty results do not discard successful results from other providers.
- Real-provider detail keeps the stock MetaTube response by default; `?aggregate=true` adds `category` and `sources[]`. The reserved `MetaX` detail path resolves the catalog number through the category route and returns merged `MovieInfo` fields at the top level plus each provider’s independent structured `MovieInfo` under `data.sources`, with per-source status/error information. See [metadata aggregation contract](docs/METADATA_AGGREGATION.md).
- Aggregation uses an exact normalized catalog-number identity gate, route-order first-non-empty scalar fields, stable list deduplication, and no cross-provider score averaging. An explicit but unknown category is rejected instead of silently broadening the search; the real-provider path still falls back to a `baseline_only` record when the number itself is ambiguous.
- Route order is configurable at startup through `METAX_ROUTES` (inline JSON) or `METAX_ROUTES_FILE`; malformed or unknown-category tables abort startup instead of silently using the default. `METAX_ROUTES_FINGERPRINT` optionally pins the expected route hash so multi-instance deployments fail fast on configuration drift, and startup logs every configured provider that is unavailable in the current binary. Aggregated details and `MetaX` image resolution are cached in-process with `singleflight` (`METAX_AGGREGATE_CACHE_TTL`, `METAX_AGGREGATE_CACHE_MAX`), lazy number resolution consults local `movie_metadata` before live search, and category fan-out concurrency is bounded by `METAX_AGGREGATE_FANOUT` (default 16). The server's `--request-timeout` remains the per-provider HTTP deadline. See [metadata aggregation contract](docs/METADATA_AGGREGATION.md).
- Added deterministic engine and route tests for the merged search candidate, stock detail, `aggregate=true`, the reserved `MetaX` detail path, unknown-category rejection, fill-empty behavior, route-order conflicts, list deduplication, and conservative number normalization. Focused tests and the integrated server build pass on the pinned SDK checkout; see the verification section in `docs/METADATA_AGGREGATION.md`.
- Remaining backend milestones: close the provider-specific gaps recorded in [the compatibility matrix](docs/COMPATIBILITY_MATRIX.md) (JavDB FC2 login, FANZA region access, token-gated ThePornDB, deferred Prestige/R18Dev, and fuzzy Getchu catalog search), and add network-resilience coverage where it can be deterministic. Live API smoke now covers the integrated search/detail/image path; raw website HTML/HTTP payload capture remains out of scope because the current Provider contract exposes normalized `MovieInfo` records, not wire-level responses.
- Jellyfin plugin changes are explicitly out of scope for this backend phase.

### Automatic category routing

The existing Jellyfin plugin does not send an Amane content type. The backend infers categories from Amane-style catalog families: ordinary censored catalog numbers, known uncensored prefixes and six-digit sequence IDs, FC2, Chinese MD/MKY families, amateur families, KIN8/Western studio-date IDs, and Getchu IDs. For non-catalog identifiers, selected source names such as ThePornDB, Getchu, and FC2 marketplaces can provide a narrow fallback hint. Ambiguous identifiers stay on the safe baseline-only detail path instead of triggering speculative aggregation. The FC2 and uncensored route remains intentionally combined.
