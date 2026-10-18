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
