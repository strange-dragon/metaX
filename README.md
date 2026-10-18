# metaX

metaX is a native Go Provider extension and migration workspace for bringing additional site-scraping capabilities into MetaTube, so the existing MetaTube Jellyfin plugin can use those providers through the MetaTube service.

**Target architecture:** Jellyfin → existing MetaTube Jellyfin plugin → MetaTube API → existing and additional native Go Providers.

Amane is a functional reference for capability coverage and edge cases, not a separate service that Jellyfin should call. New Go code is written for MetaTube's Provider interfaces and compared against existing SDK implementations before deciding what to keep or improve.

## License and attribution

metaX keeps Apache-2.0 for project-authored code and MetaTube-derived code, subject to the original notices. Amane is acknowledged as a functional reference; its GPL-3.0 source and fixtures are not relicensed by attribution alone. See [LICENSE](LICENSE), [NOTICE.md](NOTICE.md), and [docs/LICENSING.md](docs/LICENSING.md).

## Current status

- The `feat/amane-provider-migration` branch currently contains 11 native Go Provider packages: Airav, AVSOX, FC2Club, FreeJAVBT, GIGA, IQQTV, Jav321, JavDB (movie and actor), JavLibrary, Minnano (actor), and XCity.
- The branch maintains 13 unique SDK patches for existing-provider correctness and URL/ID validation. The permanent SDK patch workflow applies the patch set and runs focused deterministic tests plus the full upstream SDK regression suite.
- A reproducible build entry point now applies all 13 maintained SDK patches, overlays the four providers covered by integration tests, verifies engine registration and `/v1/providers` discovery, runs the full pinned SDK regression suite, and builds the server binary. The end-to-end build workflow passed on commit [`27c816f`](https://github.com/strange-dragon/metaX/commit/27c816f02cc6e3ba6a1967e85b8a20056dcd7b27) in [run 39139447663](https://github.com/strange-dragon/metaX/actions/runs/39139447663); it uploaded artifact `metatube-server-27c816f02cc6e3ba6a1967e85b8a20056dcd7b27` (available for seven days from 2026-10-18).
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

The branch includes a build script that applies the 13 maintained SDK patches, integrates the four providers currently covered by engine and API discovery tests, runs deterministic and upstream regression tests, then builds a MetaTube server binary. It requires a clean checkout of the pinned SDK revision.

```bash
git clone https://github.com/metatube-community/metatube-sdk-go.git ./metatube-sdk-go
git -C ./metatube-sdk-go checkout c62584675c593816cc07e7b5b977d14c7eb505a3
bash .github/scripts/build-metatube-server.sh ./metatube-sdk-go ./dist/metatube-server
```

The script refuses a dirty SDK checkout or a different commit. The generated binary includes movie providers `Jav321` and `Airav`, and actor providers `JavDBActor` and `MinnanoActor`, plus the maintained SDK patches. It does **not** yet include every native provider package in this repository, and CI does not yet prove live scraping or a running Jellyfin deployment. The GitHub Actions workflow uploads the built binary as a seven-day artifact after all checks pass.
