# metaX

metaX is a native Go Provider extension and migration workspace for bringing additional site-scraping capabilities into MetaTube, so the existing MetaTube Jellyfin plugin can use those providers through the MetaTube service.

**Target architecture:** Jellyfin → existing MetaTube Jellyfin plugin → MetaTube API → existing and additional native Go Providers.

Amane is a functional reference for capability coverage and edge cases, not a separate service that Jellyfin should call. New Go code is written for MetaTube's Provider interfaces and compared against existing SDK implementations before deciding what to keep or improve.

## License and attribution

metaX keeps Apache-2.0 for project-authored code and MetaTube-derived code, subject to the original notices. Amane is acknowledged as a functional reference; its GPL-3.0 source and fixtures are not relicensed by attribution alone. See [LICENSE](LICENSE), [NOTICE.md](NOTICE.md), and [docs/LICENSING.md](docs/LICENSING.md).

## Current status

- The `feat/amane-provider-migration` branch currently contains 11 native Go Provider packages: Airav, AVSOX, FC2Club, FreeJAVBT, GIGA, IQQTV, Jav321, JavDB (movie and actor), JavLibrary, Minnano (actor), and XCity.
- The branch maintains 13 unique SDK patches for existing-provider correctness and URL/ID validation. The permanent SDK patch workflow applies the patch set and runs focused deterministic tests plus the full upstream SDK regression suite.
- A reproducible build entry point applies all 13 maintained SDK patches, overlays native providers, verifies engine registration and `/v1/providers` discovery, runs the pinned SDK regression suite, and builds the server binary. The four-provider version passed in [run 39139447663](https://github.com/strange-dragon/metaX/actions/runs/39139447663); the expanded eleven-package build passed in [run 39139794806](https://github.com/strange-dragon/metaX/actions/runs/39139794806), producing artifact `metatube-server-cd8ba9f73e2409a9dce7efd73a8266fc2a295a8c` (available until 2026-10-25).
- These are migration-workspace packages, not yet a claim of end-to-end runtime integration: the existing MetaTube engine must import the Provider packages (or merge them into the SDK) for `init()` registration to take effect. Live-site verification and Jellyfin/MetaTube integration remain pending.
- A focused audit of all six Amane actor-source contracts is recorded in the compatibility matrix. The pinned SDK engine's registration mechanism is confirmed to be `engine/register.go`; native packages in this separate workspace still need to be integrated into the SDK provider tree or another supported server build path.
- Current overall migration estimate is approximately 40%; this is a stage-weighted estimate, not a live-site success rate. See [migration plan](docs/MIGRATION_PLAN.md) for the breakdown and remaining integration work.
- No live-site support is claimed until current site behavior and integration are verified.

## Engineering documents

- [Contributor instructions](AGENTS.md)
- [Migration plan](docs/MIGRATION_PLAN.md)
- [Compatibility matrix](docs/COMPATIBILITY_MATRIX.md)
- [Initial implementation comparison](docs/INITIAL_FINDINGS.md)
- [Licensing and attribution policy](docs/LICENSING.md)

## Reproducible integrated server build

The branch includes a reproducible build script that applies 13 maintained SDK patches, integrates eleven native provider packages, applies the category-search overlay, runs engine/API contract tests and upstream regression tests, then builds a MetaTube server binary. It requires a clean checkout of the pinned SDK revision.

```bash
git clone https://github.com/metatube-community/metatube-sdk-go.git ./metatube-sdk-go
git -C ./metatube-sdk-go checkout c62584675c593816cc07e7b5b977d14c7eb505a3
bash .github/scripts/build-metatube-server.sh ./metatube-sdk-go ./dist/metatube-server
```

The script refuses a dirty SDK checkout or a different commit. The generated binary is intended to include ten movie providers (`Airav`, `AVSOX`, `FC2Club`, `Freejavbt`, `GIGA`, `IQQTV`, `Jav321`, `JavDB`, `JavLibrary`, `XCity`) and two actor providers (`JavDBActor`, `MinnanoActor`), plus the maintained SDK patches. The eleven-package integration has passed in [GitHub Actions run 39191776372](https://github.com/strange-dragon/metaX/actions/runs/39191776372), including provider registration, `/v1/providers` API discovery, focused tests, the full pinned SDK regression suite, and server compilation. Live scraping and a running Jellyfin deployment remain unverified. The workflow uploads the built binary as a seven-day artifact after all checks pass.


## Backend orchestration work

The backend orchestration layer is being implemented separately from the Jellyfin plugin.

- The `routing` package defines category routes for censored, uncensored, FC2, Chinese-subtitle, amateur, western, and hentai content (`animation` remains an alias). The FC2 route includes Airav, matching Amane’s default route. It preserves explicit provider selection as a manual override and skips providers not registered in the current SDK build.
- The SDK overlay adds an optional `category` query parameter to `/v1/movies/search`. For example, `/v1/movies/search?q=ABC-123&category=uncensored` fans out concurrently to all registered providers in that category. Omitting `category` preserves MetaTube's original all-provider behavior; specifying `provider` preserves single-provider behavior.
- Search results keep MetaTube's existing response envelope and model, including each record's `provider` + `id` identity for detail lookup. Per-provider errors and empty results do not discard successful results from other providers. Results are deduplicated by provider + ID and sorted using MetaTube's existing relevance/priority approach.
- The SDK overlay includes API contract tests for search/detail response shape, unknown-category rejection, and explicit-provider override. The latest category-routing validation run is [GitHub Actions run 39192134634](https://github.com/strange-dragon/metaX/actions/runs/39192134634); final status should be confirmed before treating this milestone as complete.
- Next backend milestones: validate live-source behavior, reviewed category membership against Amane’s original route definitions; the audit added the omitted Airav source to FC2 and aligned the canonical `hentai` category name. Next, validate live-source behavior and address any remaining metadata-field enrichment and network resilience. Avoid cross-provider record merging unless a clear matching key and merge policy are established.
- Jellyfin plugin changes are explicitly out of scope for this backend phase.
