# metaX

metaX is a native Go Provider extension and migration workspace for bringing additional site-scraping capabilities into MetaTube, so the existing MetaTube Jellyfin plugin can use those providers through the MetaTube service.

**Target architecture:** Jellyfin → existing MetaTube Jellyfin plugin → MetaTube API → existing and additional native Go Providers.

Amane is a functional reference for capability coverage and edge cases, not a separate service that Jellyfin should call. New Go code is written for MetaTube's Provider interfaces and compared against existing SDK implementations before deciding what to keep or improve.

## License and attribution

metaX keeps Apache-2.0 for project-authored code and MetaTube-derived code, subject to the original notices. Amane is acknowledged as a functional reference; its GPL-3.0 source and fixtures are not relicensed by attribution alone. See [LICENSE](LICENSE), [NOTICE.md](NOTICE.md), and [docs/LICENSING.md](docs/LICENSING.md).

## Current status

- The `feat/amane-provider-migration` branch currently contains 11 native Go Provider packages: Airav, AVSOX, FC2Club, FreeJAVBT, GIGA, IQQTV, Jav321, JavDB (movie and actor), JavLibrary, Minnano (actor), and XCity.
- The branch maintains 13 unique SDK patches for existing-provider correctness and URL/ID validation. The permanent SDK patch workflow applies the patch set and runs focused deterministic tests plus the full upstream SDK regression suite.
- A disposable SDK integration overlay now also validates native movie providers `Jav321` and `Airav`, plus actor providers `JavDBActor` and `MinnanoActor`. Engine registration, focused tests, and the full SDK regression suite passed on commit [`be37f37`](https://github.com/strange-dragon/metaX/commit/be37f37916e28f556d1c3194d17df79e37f860e9) in [run 39138091636](https://github.com/strange-dragon/metaX/actions/runs/39138091636); metaX tests passed in [run 39138091625](https://github.com/strange-dragon/metaX/actions/runs/39138091625), and formatting passed in [run 39138091638](https://github.com/strange-dragon/metaX/actions/runs/39138091638).
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
