# Amane → MetaTube Provider migration plan

## Goal

Make MetaTube the single scraping backend used by the existing `metatube-jellyfin` plugin, by bringing selected Amane scraping capabilities into the native `metatube-sdk-go` Provider ecosystem.

The intended runtime is:

```text
Jellyfin
  └─ existing MetaTube Jellyfin plugin
       └─ MetaTube API / provider orchestration
            └─ native Go Providers (existing + migrated Amane capabilities)
```

Amane is a source of behavior and scraping knowledge, not a required sidecar service in the target architecture. The objective is not to install Amane as-is or to make Jellyfin call Amane directly.

## Verified starting point

At the initial repository inspection on 2026-10-16:

- `strange-dragon/metaX` `main` contained only `.gitignore`, `LICENSE`, and a short `README.md`; no migration script or handoff document was present in that branch tree.
- A working branch was created: `feat/amane-provider-migration`.
- The public Amane repository identified for comparison is [sqzw-x/amane](https://github.com/sqzw-x/amane). Its README describes multi-source, field-level metadata aggregation and the source tree contains Python crawlers under `src/amane/crawlers/sites/`, actor crawlers under `src/amane/crawlers/actor/sites/`, handlers, and crawler tests.
- The public target SDK is [metatube-community/metatube-sdk-go](https://github.com/metatube-community/metatube-sdk-go). Its Provider API defines `Provider`, `MovieProvider`, `MovieSearcher`, `ActorProvider`, `ActorSearcher`, optional `Fetcher`, request timeout/proxy setters, and config interfaces; registration is handled by `provider.Register`.
- The existing client is [metatube-community/jellyfin-plugin-metatube](https://github.com/metatube-community/jellyfin-plugin-metatube). Its compatibility must be verified against the MetaTube API and model output; do not modify the plugin merely to bypass a server-side integration problem.

These observations are a starting snapshot, not a substitute for pinning exact upstream SHAs and rechecking before code migration.

## Phase 0 — repository and licensing baseline

- [x] Create isolated feature branch.
- [x] Add repository-level development instructions.
- [x] Add migration and compatibility planning documents.
- [x] Record exact upstream SHAs for Amane, SDK, and Jellyfin plugin in the compatibility matrix.
- [ ] Inspect all existing metaX files, scripts, workflow permissions, and prior branches/commits for handoff material.
- [x] Record the intended license strategy: keep metaX project-authored code Apache-2.0; credit Amane as a feature reference; do not copy Amane source files or fixtures into Apache-only code.
- [x] Identify SDK's actual Go toolchain and the available formatting, unit-test, and CI commands; a full upstream SDK regression run is still pending.

**Exit criteria:** the migration is technically scoped; source provenance is documented; direct reuse of Amane source or fixtures is excluded unless its license treatment is separately approved.

## Phase 1 — capability inventory

Inventory Amane movie crawlers and actor crawlers separately. For each site, record:

- Site name, canonical URL, and available functions (search, detail by ID/URL, actor search/detail).
- ID and URL normalization rules, locale, query normalization, pagination, and empty-result behavior.
- Metadata fields and their semantics, including dates, runtime, maker/label/series, genres, actors, cover/thumb/preview images, and external IDs where available.
- HTTP behavior: headers, cookies, redirect handling, proxy, timeout, retry/backoff, rate limit, and any browser/JavaScript dependency.
- Test fixtures, expected output, live-site stability, known access restrictions, and license/attribution concerns.
- Whether the SDK already has an equivalent Provider and whether that implementation is better, worse, or complementary.

Use the compatibility matrix to record findings. Do not rank sites by filename count alone.

## Phase 2 — SDK compatibility and test baseline

- Read the SDK Provider interfaces, factory, shared fetcher/scraper helpers, model types, and representative providers.
- Compare relevant Amane capability descriptions and observable site behavior against the SDK. Do not translate source files line by line; use the SDK's idioms and independently authored parsers/tests.
- Map Amane data into existing SDK model types before proposing model changes.
- Build the unmodified SDK and run its existing tests in a suitable Go environment.
- Add a small, deterministic test skeleton for one representative migrated Provider before broad migration.
- Define tests for valid metadata, missing optional fields, empty/not-found results, malformed HTML/JSON, HTTP errors, and normalization edge cases.

**Exit criteria:** existing SDK behavior is recorded, the target interface is clear, and tests can detect regressions.

## Phase 3 — migrate in small batches

Recommended order:

1. Sites with a clear one-to-one mapping to existing SDK interfaces and low infrastructure complexity.
2. Sites requiring shared parser/fetcher enhancements, after those enhancements are independently tested.
3. Sites with API/GraphQL or unusual response formats.
4. Sites requiring JavaScript rendering, anti-bot challenges, browser automation, or special session behavior; determine whether a maintainable native-Go approach exists before implementation.
5. Actor sites and cross-site actor matching, without forcing actor data into movie-only Provider methods.

For each site:

1. Compare the current Amane implementation with any existing SDK provider.
2. Write down the behavior to preserve and the implementation to reuse/replace, with reasons.
3. Implement the smallest coherent change.
4. Add deterministic fixtures and focused tests; keep live checks optional.
5. Run format, focused tests, and broader tests.
6. Update the matrix and mark the site complete only when the acceptance criteria pass.

Do not port Amane's whole application framework, database, scheduler, UI, AI agent, media organization, or image-enhancement pipeline unless a separately demonstrated requirement places that behavior inside MetaTube's Provider boundary.

## Phase 4 — aggregation and orchestration

Amane supports multi-source field-level aggregation. Verify whether MetaTube already performs equivalent provider selection, priority handling, merge, fallback, and partial-result behavior.

- Reuse MetaTube orchestration if it meets the behavior requirements.
- If a gap exists, add the smallest generic SDK-level mechanism that benefits more than one provider and test it independently.
- Avoid implementing a private aggregation layer inside each migrated Provider.
- Define precedence for empty values, conflicting values, localized titles, actor names, and image URLs; do not silently overwrite a populated field with an empty result.

## Phase 5 — integration and release readiness

- Verify the existing Jellyfin plugin can reach the MetaTube API and display/search the migrated metadata without plugin changes.
- Check API response/model compatibility, provider discovery/registration, configuration, and error behavior.
- Run all SDK tests and lint/format checks available in CI.
- Document migrated sites, known gaps, required configuration, and any network/access limitations.
- Do not merge to `main` or claim production readiness until tests and compatibility checks are reviewed.

## Definition of done

- Every migrated Provider implements the expected native SDK interfaces and is registered/discoverable.
- Search and detail flows behave correctly for valid, missing, and malformed inputs.
- Metadata is mapped to the existing SDK models without unintended data loss.
- Regression tests are deterministic and do not depend solely on live sites.
- Existing providers and the Jellyfin plugin contract remain compatible.
- License, attribution, dependency, and test status are documented.
- Remaining gaps are explicit; untested sites are not labelled complete.


## Progress snapshot (2026-10-16)

This is a migration workspace, not a claim that every inventoried site has shipped.

- **Inventory scope:** 24 Amane movie crawler sources and 6 actor crawler sources are listed for comparison in `docs/COMPATIBILITY_MATRIX.md`.
- **Native Go extension packages:** 10 package directories currently exist in metaX: Airav, AVSOX, FC2Club, FreeJAVBT, GIGA, IQQTV, Jav321, JavDB (movie and actor), JavLibrary, and Minnano (actor).
- **SDK patches:** 11 unique patch files target existing SDK providers. The maintained workflow applies the entire set to the pinned SDK, runs focused deterministic tests, and checks formatting; latest pass: [run 38932602151](https://github.com/strange-dragon/metaX/actions/runs/38932602151).
- **Still outstanding:** finish source-by-source behavior comparison and decisions across the 24 movie and 6 actor sources; implement only justified gaps; design actor image/profile enrichment and identity resolution; run the full pinned SDK test suite; verify provider import/registration in the actual MetaTube engine; and conduct selective live-site checks plus Jellyfin/MetaTube integration validation.
- **Do not interpret the counts as a simple percentage complete.** Some inventory rows are already covered by an SDK provider plus a hardening patch, some have native Go implementations, and others need a design decision or are blocked by site access. The acceptance checklist and per-source evidence determine completion.


## Progress snapshot (2026-10-17, XCity)

- Added native Go Provider package `provider/xcity/` with search/detail parsing, XCity's `adc=1` age-confirmation cookie, same-origin redirect restrictions, HTTPS production origin validation, bounded response reads, proxy/timeout settings, and SDK registration.
- Search treats results as candidates only and validates each detail page's `span.kopin` against the requested catalog number after normalization. This avoids the Amane reference crawler's first-result false-positive behavior.
- Added deterministic local HTTP tests for exact-vs-near number matching, field mapping, URL validation, cross-origin redirect blocking, cookie behavior, and registration/configuration.
- Updated README and compatibility matrix. Go formatting has passed CI; full deterministic Go test workflow is still running. No live-site validation or runtime engine import has been claimed.


## Progress snapshot (2026-10-17, KIN8 ID validation)

- Added SDK patch `0012-kin8-validate-and-normalize-movie-id.patch` to reject unrelated/invalid KIN8 IDs before network access and normalize accepted numeric IDs to four digits.
- Added deterministic SDK migration tests for valid prefixes, normalization, zero/overlong values, foreign catalog numbers, and path-like input.
- Added the patch and focused tests to the permanent SDK patch-validation workflow, including gofmt coverage.
- The old SDK route and selectors remain unchanged; this patch deliberately does not claim full current-site compatibility. CI status pending at the time of this update.


## KIN8 patch validation result (2026-10-17)

The new KIN8 ID normalization patch applies cleanly to the pinned SDK. The permanent workflow passed patch application, gofmt, and all focused deterministic patch tests in [run 38978044278](https://github.com/strange-dragon/metaX/actions/runs/38978044278). The metaX Go tests and formatting also passed on the same branch head in [run 38978044363](https://github.com/strange-dragon/metaX/actions/runs/38978044363) and [run 38978044255](https://github.com/strange-dragon/metaX/actions/runs/38978044255). This is not live-site verification and does not resolve KIN8's legacy route drift.


## Source-contract audit: DAHLIA/FALENO REST API (2026-10-17)

- Confirmed that Amane's shared WordPress works crawler uses the WordPress REST API and exact returned slug matching, whereas the pinned SDK shared core still uses legacy HTML search/detail selectors.
- Confirmed an additional field-mapping difference for FALENO: publisher/label depends on slug prefixes, while studio remains FALENO. Existing URL/slug patches do not close this behavior gap.
- Recorded the evidence and implementation criteria in `docs/COMPATIBILITY_MATRIX.md`.
- Next step: design the smallest SDK patch that keeps public work-page URLs in `MovieInfo.Homepage`, fetches the JSON endpoint for data, and tests exact-slug selection, null/missing ACF fields, images, and publisher routing with independent fixtures. Do not change production endpoints without deterministic coverage; live-site behavior remains unverified.


## DAHLIA/FALENO REST metadata patch validated (2026-10-18)

- Added `patches/metatube-sdk-go/0013-dahlia-faleno-wordpress-metadata.patch` after the existing exact-slug and URL-origin patches.
- Detail retrieval now reads the exact REST work record and maps title, actors, plot, runtime, release date, poster, thumbnail, gallery images, and FALENO publisher labels. The public work-page URL remains `MovieInfo.Homepage`.
- Added independent local HTTP/JSON tests for metadata mapping and rejection of non-exact slugs. Existing redirect identity tests remain enabled.
- The pinned SDK patch workflow, focused tests, Go formatting, and metaX Go tests passed on commit `0b3086e95c8153259e939180a4c2ef9d87e3ce36`: [SDK patch validation](https://github.com/strange-dragon/metaX/actions/runs/39134683828), [Go tests](https://github.com/strange-dragon/metaX/actions/runs/39134683778), [formatting](https://github.com/strange-dragon/metaX/actions/runs/39134683752).
- Search remains on the legacy HTML path; no live-site validation or full engine integration is claimed.

## Progress snapshot (2026-10-18, after SDK regression validation)

- **Approximate overall progress: 40%.** This is a stage-weighted engineering estimate, not a count of files or a claim that 40% of sites work live.
- **Workspace and licensing baseline:** substantially complete; Amane source and fixtures remain behavioral references rather than copied GPL-3.0 implementation.
- **Native provider work:** 11 Go Provider packages exist. They have varying levels of deterministic test coverage; live availability and engine discovery have not been established.
- **Existing SDK corrections:** 13 maintained patches apply to the pinned SDK. The permanent workflow now runs patch application, formatting, focused deterministic tests, and the full upstream SDK test suite. All steps passed for commit `3eecaaf76a6b14e26d72a9803e3f2190fc4d7012` in [SDK patch validation run 39135970713](https://github.com/strange-dragon/metaX/actions/runs/39135970713); metaX Go tests and formatting also passed in [run 39135970715](https://github.com/strange-dragon/metaX/actions/runs/39135970715) and [run 39135970712](https://github.com/strange-dragon/metaX/actions/runs/39135970712).
- **Source comparisons:** focused reviews now include FC2 and Getchu, and the six registered Amane actor-source contracts have been audited. This is still not a completed source-by-source review of all 24 movie crawlers and 6 actor crawlers.
- **Engine integration:** not complete. Provider packages in this workspace do not register themselves in a separately built MetaTube process unless the engine imports them or they are incorporated into the SDK.
- **Runtime validation:** no current live-site verification or complete Jellyfin → MetaTube API → migrated-provider end-to-end test is claimed.

### Remaining work, in priority order

1. Identify and inspect the actual MetaTube engine repository and its provider-discovery/configuration path; verify the repository and supported integration point before editing it.
2. Finish per-source comparisons and decide which gaps justify implementation, especially actor-source aggregation, JavBus search behavior, and deferred catalog/region-specific sources.
3. Integrate provider packages through the supported engine/SDK boundary, preserving the existing Jellyfin plugin API contract.
4. Add or verify integration-level tests for provider discovery, configuration, error handling, and field-level merge behavior.
5. Run selective live-site checks where access permits, then verify actual Jellyfin plugin search/metadata display against the running MetaTube service.
6. Update user-facing migration and licensing documentation, review the complete diff, and only then assess release readiness.

The 40% estimate should increase materially only after the engine integration path and at least one end-to-end request are verified; passing SDK regression tests alone is necessary but not sufficient.

## Engine registration boundary verified (2026-10-18)

Inspection of the pinned SDK source confirms the concrete integration point: `engine/register.go` blank-imports each built-in provider package, which triggers each package's `init()` registration; `engine/init.go` then enumerates the movie and actor factory registries. A provider's `init()` in the separate metaX module is therefore insufficient by itself.

The current module boundary matters. metaX is module `github.com/strange-dragon/metaX` and its new providers import `github.com/metatube-community/metatube-sdk-go`. Making the SDK import those metaX packages would create an import cycle (SDK → metaX provider → SDK). The safe integration route is to merge or port the independently authored provider packages into the SDK module's own `provider/` tree, then add blank imports in `engine/register.go`, or maintain a deliberate SDK fork that contains both changes. Do not add a direct SDK-to-metaX import.

Before changing the SDK integration surface, build a repeatable integration overlay that:
1. copies the intended provider source and tests into matching SDK package paths without importing Amane GPL-3.0 source/fixtures;
2. adds only the required imports to `engine/register.go`;
3. runs the pinned SDK's full tests and verifies the new names appear in `engine.Default().GetMovieProviders()` / `GetActorProviders()` (or an equivalent deterministic registry test);
4. records licensing notices and keeps the public Jellyfin plugin/API contract unchanged.

The integration overlay has now been prototyped with the native Jav321 provider. The permanent SDK validation workflow copies the provider package into the disposable pinned SDK checkout, adds the required blank import, and adds a deterministic engine registration test. This avoids an SDK → metaX → SDK import cycle and does not change the public Jellyfin plugin/API contract.

## First engine-registration prototype validated (2026-10-18)

- Added `.github/scripts/validate-provider-integration.sh` and wired it into `.github/workflows/sdk-patch-validation.yml`.
- The overlay copies `provider/jav321` into the temporary SDK checkout, registers it through `engine/register.go`, and adds a test that constructs the engine and asserts `Jav321` is discoverable as a movie provider.
- The first run exposed a workflow invocation issue (the script lacked executable permission); the workflow now explicitly invokes it with `bash`.
- On commit `39374f5a555f0ed3f17799e1009a7792ad2b6ed3`, the integration overlay and provider-registration test passed in [SDK validation run 39136860932](https://github.com/strange-dragon/metaX/actions/runs/39136860932). The metaX Go tests passed in [run 39136861054](https://github.com/strange-dragon/metaX/actions/runs/39136861054), and formatting passed in [run 39136860939](https://github.com/strange-dragon/metaX/actions/runs/39136860939).
- **What this proves:** the Jav321 package can be placed inside the pinned SDK tree and discovered by the SDK engine using the proposed registration pattern.
- **What it does not prove:** live Jav321 site access, correctness of all scraped fields against current pages, integration of the other ten native packages, or a full Jellyfin → MetaTube API → provider end-to-end request.

### Next implementation steps

1. Generalize the overlay to a small provider batch only after checking each package's SDK imports, dependencies, tests, and license provenance.
2. Add a deterministic discovery test for each integrated provider, including actor providers where applicable.
3. Inspect the actual MetaTube server/API repository and its supported provider configuration path before changing deployment integration.
4. Continue the remaining source-by-source comparison; prioritize JavBus behavior and actor enrichment/identity matching.
5. Raise the overall progress estimate only after broader engine discovery and at least one end-to-end API request are verified.

## Jellyfin API contract inspection (2026-10-18)

Inspected the public `metatube-community/jellyfin-plugin-metatube` client implementation at its `main` branch. The plugin sends movie and actor searches to `/v1/movies/search` and `/v1/actors/search`, and detail requests to `/v1/movies/{provider}/{id}` and `/v1/actors/{provider}/{id}`; requests use the configured MetaTube server URL and optionally a bearer token. The plugin's provider argument is passed through to the API rather than being a client-side crawler registry.

Implications for this migration:

- The plugin's HTTP contract can remain unchanged if the backend recognizes the migrated provider names, returns the existing response schema, and exposes their search/detail flows through these routes.
- Adding a package only to the separate metaX module does not make a running server discover it. The backend build must include the provider in the SDK engine or an equivalent supported extension mechanism.
- The public plugin repository establishes the client-side route contract, but does not by itself prove the deployed server implementation's provider wiring. Do not assume a server repository or edit an unofficial fork without verifying provenance and the actual deployment source.
- The dual-provider overlay (Jav321 + Airav) now applies cleanly and both registration assertions passed in the latest SDK validation run; the full pinned SDK regression suite was still running when this note was prepared. See [run 39137797585](https://github.com/strange-dragon/metaX/actions/runs/39137797585). metaX unit tests passed on that commit in [run 39137797591](https://github.com/strange-dragon/metaX/actions/runs/39137797591), and formatting passed in [run 39137797581](https://github.com/strange-dragon/metaX/actions/runs/39137797581).

### Next steps

1. Wait for the full SDK regression result on the two-provider overlay; fix any compile or regression failure before expanding the batch.
2. Inspect the SDK's movie/actor factory interfaces and decide the next small batch based on import dependencies and test coverage. Actor providers need separate discovery assertions, not movie-provider checks.
3. Verify the backend distribution/build path that the user actually runs. If only released server binaries are available, determine whether a maintained SDK/server fork is required; do not imply that the existing plugin alone can load external Go packages.
4. Only after the server-side inclusion path is established, run a real API request through the documented routes and verify response fields and plugin display.


## Four-provider engine overlay validated (2026-10-18)

- Expanded `.github/scripts/validate-provider-integration.sh` to overlay four native packages into a disposable checkout of the pinned SDK: movie providers `Jav321` and `Airav`, actor providers `JavDBActor` and `MinnanoActor`.
- The overlay adds blank imports in `engine/register.go` and a deterministic engine test that verifies all four names are discoverable through the correct movie/actor registry.
- On commit `be37f37916e28f556d1c3194d17df79e37f860e9`, the overlay, focused package/registration tests, and full upstream SDK regression suite passed in [run 39138091636](https://github.com/strange-dragon/metaX/actions/runs/39138091636). metaX tests passed in [run 39138091625](https://github.com/strange-dragon/metaX/actions/runs/39138091625); formatting passed in [run 39138091638](https://github.com/strange-dragon/metaX/actions/runs/39138091638).
- **Proven:** the four packages compile in the pinned SDK tree and register with its engine. **Not proven:** current live-site behavior or a real request through a built/deployed MetaTube server and Jellyfin plugin.

## Server distribution and integration boundary follow-up (2026-10-18)

- The official MetaTube organization identifies `metatube-community/metatube-sdk-go` as “MetaTube SDK & API Server in Golang”; its separate `metatube-server-releases` repository publishes binary releases rather than the server source tree. The project documentation also describes deployment from released binaries or a container image. See the [official organization repositories](https://github.com/metatube-community) and [server deployment guide](https://metatube-community.github.io/wiki/server-deployment/).
- The SDK is therefore the appropriate source integration point for this migration. A separate public `metatube-server` source repository was not found in the official organization; do not invent one or assume the release binary can load external Provider packages dynamically.
- Practical next step: prepare a clean, reproducible SDK fork/patch series that includes selected native provider packages and the required engine blank imports, then build/test the server binary from that source. Before replacing or deploying the user's current service, verify its exact image/tag or binary version and whether its build provenance points to the official SDK source. The four-provider overlay is currently CI proof only; it does not yet alter a published server release.


## Reproducible SDK integration build (2026-10-18)

A single build entry point now applies the pinned SDK patch set, overlays the currently integration-tested native providers, runs API/registration tests, runs the full SDK regression suite, and builds the actual server executable:

```bash
# Start from a clean checkout of metatube-sdk-go at c62584675c593816cc07e7b5b977d14c7eb505a3.
bash .github/scripts/build-metatube-server.sh /path/to/metatube-sdk-go /path/to/output/metatube-server
```

The script intentionally rejects a dirty SDK checkout or a different SDK commit so that patches cannot silently apply to an unreviewed baseline. The original validated overlay covered four providers. The integration has now been expanded to all eleven native provider packages in this repository: ten movie providers (`Airav`, `AVSOX`, `FC2Club`, `Freejavbt`, `GIGA`, `IQQTV`, `Jav321`, `JavDB`, `JavLibrary`, `XCity`) and two actor providers (`JavDBActor`, `MinnanoActor`). The expanded build passed in [run 39139794806](https://github.com/strange-dragon/metaX/actions/runs/39139794806): all 13 SDK patches applied, all eleven packages were overlaid and registered, the `/v1/providers` endpoint exposed the expected movie/actor provider names, focused tests and the full pinned SDK regression suite passed, and the server binary built successfully. Artifact `metatube-server-cd8ba9f73e2409a9dce7efd73a8266fc2a295a8c` is available until 2026-10-25. This still does not establish live-site scraping or Jellyfin end-to-end behavior.

The CI workflow now invokes this same build entry point and uploads the generated server as a short-lived artifact. A green run proves patch application, the covered provider registration and `/v1/providers` discovery contract, the full pinned SDK test suite, and compilation of the integrated server. It does not prove live-site scraping or a running Jellyfin deployment.


## API search/detail contract validation (2026-10-18)

The SDK integration overlay now includes route-level contract tests backed by in-memory SQLite and deterministic fake providers. It exercises the actual SDK engine and Gin routes for all four plugin-facing flows:

- `GET /v1/movies/search?q=...&provider=...` — verifies list response shape and movie search fields.
- `GET /v1/movies/{provider}/{id}?lazy=false` — verifies movie detail response shape and metadata fields.
- `GET /v1/actors/search?q=...&provider=...` — verifies list response shape and actor search fields.
- `GET /v1/actors/{provider}/{id}?lazy=false` — verifies actor detail response shape and metadata fields.
- Both search routes reject a missing required `q` with HTTP 400 and an `error` envelope.

Validation passed on commit [940d62e](https://github.com/strange-dragon/metaX/commit/940d62ef95fd23d953187826490affa96b6201c0): [integrated server build and API tests](https://github.com/strange-dragon/metaX/actions/runs/39140651962), [metaX Go tests](https://github.com/strange-dragon/metaX/actions/runs/39140651888), and [format check](https://github.com/strange-dragon/metaX/actions/runs/39140651869). The build workflow produced artifact `metatube-server-940d62ef95fd23d953187826490affa96b6201c0`, available until 2026-10-25.

These route tests validate the SDK/plugin wire contract with controlled providers. They do not establish that each live provider website currently returns parseable pages; live scraping and an actual Jellyfin instance remain separate validation steps.
