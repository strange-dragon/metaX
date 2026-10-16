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
- [ ] Record exact upstream SHAs for Amane, SDK, and Jellyfin plugin in the compatibility matrix.
- [ ] Inspect all existing metaX files, scripts, workflow permissions, and prior branches/commits for handoff material.
- [ ] Confirm source and fixture licenses and determine what code/data can be ported or must be independently reimplemented.
- [ ] Identify SDK's actual Go toolchain, lint, unit-test, and CI commands.

**Exit criteria:** the migration is legally and technically scoped; the baseline can be reproduced.

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
- Read the relevant Amane crawler, parser, aggregation, configuration, and test code for each selected site.
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
