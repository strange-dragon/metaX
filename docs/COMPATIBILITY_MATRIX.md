# Upstream compatibility and Provider inventory

This matrix is the evidence log for the Amane → MetaTube SDK migration. A blank field means **not yet verified**, not “not applicable”.

## Pinned upstream snapshots

| Component | Repository | Commit SHA | Toolchain / license | Status |
|---|---|---|---|---|
| Amane | [sqzw-x/amane](https://github.com/sqzw-x/amane) | `a2d45880e83df7047d461452965919692701e8c9` | Python >=3.14; GPL-3.0 | Source inventory started; code reuse blocked pending licensing decision |
| MetaTube SDK | [metatube-community/metatube-sdk-go](https://github.com/metatube-community/metatube-sdk-go) | `c62584675c593816cc07e7b5b977d14c7eb505a3` | Go 1.25+ per README (go.mod says 1.26.0); Apache-2.0 | Provider interfaces and representative implementations inspected |
| Jellyfin plugin | [metatube-community/jellyfin-plugin-metatube](https://github.com/metatube-community/jellyfin-plugin-metatube) | `f7c1f336fc2bd3b35b82af7c6e69da09a196e3d2` | C#/.NET; Apache-2.0 license file present, exact target framework to verify | Repository structure inspected |

These are the `main` commit SHAs observed on 2026-10-16. Recheck them before a release or after upstream changes.

## SDK integration points confirmed from source

- `provider.Provider`: name, priority, language, base URL.
- `provider.MovieProvider`: movie ID normalization, URL-to-ID parsing, detail lookup by ID and URL.
- `provider.MovieSearcher`: keyword normalization and movie search.
- `provider.ActorProvider` / `provider.ActorSearcher`: actor detail and search capabilities.
- Optional interfaces include `Fetcher`, `RequestTimeoutSetter`, `ProxySetter`, and `ConfigSetter`.
- `provider.Register` registers factories by name; follow the SDK's actual package and registration conventions.
- Existing implementations use shared fetcher/scraper helpers, language tags, SDK model types, and provider-specific tests. Reuse them only when comparison shows they meet the requirement.

## Amane registered movie crawlers

The current Amane `src/amane/crawlers/sites/__init__.py` exports these 24 crawler classes. This is a capability inventory only; do not copy source files or fixture data into this Apache-2.0 project.

| Amane crawler | Source file | SDK equivalent observed in tree | Next comparison |
|---|---|---|---|
| Airav | `sites/airav.py` | `provider/airav` (new native Go provider in metaX) | Exact catalog-number matching, detail IDs, metadata fields, URL/redirect handling; live-site verification pending |
| AVBase | `sites/avbase.py` | `provider/avbase` | Compare field coverage and multi-source fallback |
| AVSox | `sites/avsox.py` | `provider/avsox` (new native Go API provider in metaX) | CSRF/API flow, exact/folded number matching, localized metadata, URL and redirect handling; live-site verification pending |
| Dahlia | `sites/dahlia.py` | `provider/dahlia` | Compare output, headers, ID rules |
| DMM | `sites/dmm.py` | DMM/FANZA-related providers exist; exact mapping to verify | API/site behavior, ID normalization, images |
| Faleno | `sites/faleno.py` | `provider/faleno` | Compare output and error behavior |
| FC2 | `sites/fc2.py` | `provider/fc2` | Compare ID normalization and metadata |
| FC2Club | `sites/fc2club.py` | Not confirmed | Site-specific ID and result semantics |
| FC2PPVDB | `sites/fc2ppvdb.py` | `provider/fc2ppvdb` | Compared on 2026-10-16; patch `0006` hardens article URL/ID validation; temporary deterministic verification workflow reported passing by user; live HTML mapping remains unverified |
| Freejavbt | `sites/freejavbt.py` | Not confirmed | Search/detail and anti-bot behavior |
| Getchu | `sites/getchu.py` | `provider/getchu` | Compared on 2026-10-16; patch `0005` hardens ID/URL validation; temporary deterministic verification workflow reported passing by user; search behavior/catalog mismatch remains open |
| Giga | `sites/giga.py` | Not confirmed | Search/detail and ID rules |
| Iqqtv | `sites/iqqtv.py` | Not confirmed | Search/detail, API behavior |
| Jav321 | `sites/jav321.py` | No Provider in SDK tree; metaX now has `provider/jav321` | Deterministic tests cover route, metadata mapping, not-found behavior and registration; live-site and full server integration pending |
| JavBus | `sites/javbus.py` | `provider/javbus` | Compared against pinned SDK source; findings and follow-up decision recorded below |
| JavDB | `sites/javdb.py` | No Provider in SDK tree; metaX now has `provider/javdb` | Deterministic tests cover search, details, number variants, foreign hosts, and registration; live-site and full server integration pending |
| JavLibrary | `sites/javlibrary.py` | Not confirmed | Login/session, parsing, source availability |
| Kin8 | `sites/kin8.py` | `provider/kin8tengoku` likely related; mapping must be verified | Confirm canonical domain, IDs, and output |
| MGStage | `sites/mgstage.py` | `provider/mgstage` | Compared on 2026-10-16; patch `0007` validates detail URL origins/paths and rejects malformed IDs; temporary patch-application, formatting, and deterministic test workflow reported passing by user |
| Official | `sites/official.py` | No direct equivalent confirmed | Inspect multi-hop/brand routing and fallback |
| Prestige | `sites/prestige.py` | Not confirmed | Search/detail, IDs, images |
| R18Dev | `sites/r18dev.py` | Not confirmed | API schema and identifier mapping |
| ThePornDB | `sites/theporndb.py` | `provider/theporndb` | Compare movie/actor APIs, IDs, optional fields |
| XCity | `sites/xcity.py` | Not confirmed | Search/detail and ID rules |

Amane's actor registry is separate at `src/amane/crawlers/actor/sites/`; inventory it independently and compare with SDK `ActorProvider` / `ActorSearcher` implementations. Do not infer that a movie crawler also covers actor profiles or images.


## Focused comparison: JavBus (2026-10-16)

Compared Amane `sites/javbus.py` and its shared number-matching helpers at Amane commit `a2d45880e83df7047d461452965919692701e8c9` with SDK `provider/javbus/javbus.go` at SDK commit `c62584675c593816cc07e7b5b977d14c7eb505a3`.

- **Search correctness — Amane is stronger.** Amane tries number variants, first attempts a direct detail URL, then searches and selects only an exact normalized number (or one unique match differing only by the known leading digit segment). It avoids returning the first fuzzy result. The SDK implementation runs normal and uncensored search pages and appends every matching card without checking that the result number matches the query. This risks false-positive results for ambiguous/similar numbers.
- **Number variants — Amane is stronger.** Amane accounts for JavBus omitting leading numeric segments in some amateur-series identifiers. SDK `NormalizeMovieKeyword` filters certain special numbers, but it does not implement the same number-variant / candidate-selection flow.
- **Detail fields — broadly similar, with differences.** Both extract title, cover, actors, release, runtime, studio/maker, label/publisher, director, series, genres/tags, and sample images. SDK additionally probes for a thumbnail URL with two HEAD requests; Amane derives poster/thumbnail URLs directly. These behaviors are not equivalent and should be validated before choosing one.
- **Required-field validation — Amane is stronger.** Amane returns no metadata if title or the page's identifier is missing. SDK initializes an info object and may return partially populated data when selectors no longer match.
- **HTTP behavior — different and needs deliberate review.** SDK config disables automatic redirects, sets a Referer and cookies `dv=1` and `existmag=all`; Amane sets `dv=1`, a Chinese Accept-Language header, and same-origin Referer behavior. Do not blindly merge headers/cookies or enable redirects without deterministic tests.
- **URL validation — SDK gap to address in the SDK source/fork.** SDK `ParseMovieIDFromURL` parses any syntactically valid URL and takes its final path segment without checking scheme/host/port. A caller passing a lookalike or unrelated origin can therefore produce a JavBus ID from that URL. The detail fetcher is configured to disable redirects, but that does not validate the initial URL.
- **Decision.** Treat Amane's exact candidate selection and required-field validation as behavior worth porting into the native SDK JavBus provider; add deterministic tests for near-match rejection, leading-digit variants, malformed/missing detail fields, and URL-origin validation first. Keep SDK thumbnail probing and existing HTTP settings unless tests or live behavior demonstrate a reason to change them. This comparison is source-based only; no live JavBus requests were made.
- **Patch status (2026-10-16).** The migration patch now implements HTTPS origin/path validation, bounded number variants, exact-first candidate selection with unique leading-digit-only fallback, duplicate-URL deduplication, and required title/number validation. Patch application, formatting, and focused deterministic tests passed in [CI run 38878430661](https://github.com/strange-dragon/metaX/actions/runs/38878430661). This patch remains a proposed SDK change in the metaX migration workspace; it has not been merged upstream, and the full SDK regression suite and live-site behavior are still unverified.


## Focused comparison: FC2Club (2026-10-16)

Compared Amane `src/amane/crawlers/sites/fc2club.py` at commit `a2d45880e83df7047d461452965919692701e8c9` with the native Go implementation in `provider/fc2club`. The pinned SDK has FC2HUB and FC2PPVDB providers, but they target different sites and are not drop-in FC2Club implementations.

- **Lookup semantics — broadly aligned.** Both normalize FC2-prefixed numeric IDs into a canonical `FC2-{digits}` identifier and construct the direct `/html/FC2-{digits}.html` detail URL. Both reject pages without the expected content marker/title.
- **Metadata — Go implementation is more defensive.** It handles lazy-loaded cover attributes and Open Graph fallback, deduplicates actors/tags, strips a leading catalog number from the display title, caps response size, and maps not-found responses into a predictable error. Amane's simpler parser extracts the title, actors, cover, tags, and release date.
- **Correctness trade-off.** The Go implementation currently treats a missing cover as an error, while Amane can return metadata without a cover. Keep this stricter behavior only if downstream validity requirements justify the possible false negatives.
- **Security gap found and fixed.** Detail URL parsing previously accepted non-default ports and userinfo on the expected hostname, and the default HTTP client could follow redirects off-site. The native provider now requires the configured scheme/host/effective port, rejects userinfo and spoofed hosts, and limits redirects to the same origin with a 10-hop cap.
- **Deterministic coverage.** Added tests for HTTPS downgrade, unusual port, userinfo, lookalike host, allowed `www` alias, and a cross-origin redirect that must not reach its target. The initial test run stopped at the formatting gate; after gofmt normalized the tracked Go files, the full metaX Go test workflow passed on the updated branch head in [CI run 38884644252](https://github.com/strange-dragon/metaX/actions/runs/38884644252). Formatting also passed in [run 38884644417](https://github.com/strange-dragon/metaX/actions/runs/38884644417). No live FC2Club requests were made.


## Focused comparison: official FC2 article pages (2026-10-16)

Compared Amane `src/amane/crawlers/sites/fc2.py` with the pinned SDK's `provider/fc2/fc2.go` at `c62584675c593816cc07e7b5b977d14c7eb505a3`.

- **Metadata coverage — SDK is broader.** Amane extracts title, sample images, cover/poster, release date, seller/studio, tags, and description. The SDK additionally parses actor/genre information, runtime, score, iframe summary, and checks Product ID consistency. Preserve those existing fields.
- **URL and ID correctness — SDK gap found.** SDK `ParseMovieIDFromURL` previously accepted any URL and used its final path segment; `GetMovieInfoByID` interpolated the caller's ID directly into the URL. This allowed unrelated origins and malformed IDs to enter the detail-fetch path.
- **Decision and patch.** Added `patches/metatube-sdk-go/0004-fc2-validate-detail-url.patch`: validate HTTPS, exact official host, default HTTPS port, no userinfo, and the `/article/{numeric-id}` path; normalize numeric IDs before constructing URLs; reject redirects unless they remain on the same FC2 article, with a 10-hop cap. Existing metadata extraction remains unchanged.
- **Verification.** Combined patch application, formatting, and deterministic JavBus, AVBase, DAHLIA/FALENO, and FC2 tests passed in [CI run 38885581605](https://github.com/strange-dragon/metaX/actions/runs/38885581605). The full metaX Go test workflow also passed in [run 38885581539](https://github.com/strange-dragon/metaX/actions/runs/38885581539). No live FC2 requests or full upstream SDK regression suite were run.


## Focused comparison: MGStage (2026-10-16)

Compared Amane `src/amane/crawlers/sites/mgstage.py` with the pinned SDK's `provider/mgstage/mgstage.go` at `c62584675c593816cc07e7b5b977d14c7eb505a3`.

- **Metadata coverage — SDK is broader.** Amane extracts title, actors, studio, publisher, series, runtime, release date, tags, cover, sample images, and plot. SDK additionally exposes thumbnail variants, preview video, score, label, and movie reviews. Keep the existing SDK mapping; replacing it with Amane's narrower metadata extraction would lose fields.
- **Lookup behavior — both try direct detail lookup and use MGStage's product-detail URL shape.** Amane tries known number variants and verifies a direct page by requiring a title, then searches and selects a matching catalog number rather than returning the first result. SDK search returns the provider's result list; do not replace its search semantics with Amane's single-result resolver.
- **URL/ID validation gap.** SDK previously uppercased any input and used `path.Base(URL.Path)` without validating origin or route. Both detail and review-by-ID methods interpolate IDs into request URLs, so malformed input could reach the fetcher.
- **Targeted patch.** Added `patches/metatube-sdk-go/0007-mgstage-validate-detail-url.patch`: constrain IDs to alphanumeric segments separated by hyphens, normalize accepted IDs to uppercase, require HTTPS on `www.mgstage.com` with the default port and no userinfo, and accept only `/product/product_detail/{id}/` routes. Invalid IDs are rejected before detail or review requests. Existing metadata parsing and search behavior remain unchanged.
- **Limits / verification.** No live MGStage requests were made. The temporary workflow checking patch application, gofmt, and deterministic MGStage tests was reported passing by the user and was deleted afterward.

## Per-site acceptance checklist

- [ ] Site registry, aliases, and supported roles confirmed.
- [ ] Existing SDK provider located and behavior compared (or absence confirmed).
- [ ] License and fixture provenance reviewed.
- [ ] ID and URL normalization cases covered.
- [ ] Search and detail results mapped to SDK models.
- [ ] Empty/not-found behavior covered.
- [ ] Missing optional fields and malformed responses covered.
- [ ] Headers/cookies, redirects, proxy, timeout, retry and rate-limit behavior reviewed.
- [ ] Actor and image behavior verified where applicable.
- [ ] Focused tests and format checks pass.
- [ ] SDK regression suite run, or exact reason for not running recorded.
- [ ] Status updated with test evidence and known limitations.


## Focused comparison: AVBase (2026-10-16)

Compared Amane `src/amane/crawlers/sites/avbase.py` and the pinned SDK `provider/avbase/avbase.go` at `c62584675c593816cc07e7b5b977d14c7eb505a3`.

- **Detail URL correctness — SDK search result bug found.** Amane explicitly builds canonical detail routes as `/works/{prefix}:{work_id}` when a prefix exists, and notes that `/works/{number}` is not a valid direct route. SDK `SearchMovie` returned a prefixed ID via `JoinPrefixID`, but built `Homepage` from `work.WorkID` alone, dropping the prefix. This can make a result's detail URL point at the wrong/nonexistent route.
- **Metadata strategy — SDK is broader.** Amane reads the embedded Next.js `work` payload and selects fields from products. SDK also tries source-specific providers (DUGA, FANZA, Getchu, MGStage, Pcolle), then falls back to AVBase's own work payload and supplements missing fields. Do not replace this fallback with Amane's simpler extraction.
- **Search semantics differ.** Amane's `_search` is a single exact-number resolver: it tries query forms, normalizes work IDs, and rejects ambiguous candidates. SDK `SearchMovie` is a search-results API and should retain its list semantics; blindly porting Amane's single-result selection would be a regression. Exact matching can be considered separately only if observed SDK false positives warrant it.
- **Decision and patch.** Added `patches/metatube-sdk-go/0002-avbase-preserve-prefixed-detail-url.patch`, which uses `JoinPrefixID` when building search-result `Homepage` and adds deterministic tests for prefixed and unprefixed works. Combined patch application, gofmt, JavBus focused tests, and the AVBase URL test passed in [CI run 38880331157](https://github.com/strange-dragon/metaX/actions/runs/38880331157). No live AVBase requests were made; full SDK regression and end-to-end MetaTube integration remain unverified.


## Focused comparison: DAHLIA / FALENO shared WordPress Works theme (2026-10-16)

Compared Amane's `WordPressWorksCrawler` implementation and the pinned SDK's `provider/dahlia/core/core.go` (shared by DAHLIA/FALENO).

- **Exact existence check — Amane is safer.** Amane calls `/wp-json/wp/v2/works?slug={normalized_slug}` and requires a returned post whose `slug` exactly matches the requested slug before treating the number as found. Its source comment documents a site behavior where an unknown slug can 302 to a different work (example: `/works/fns1/` → `/works/fns108/`), which can produce incorrect metadata if the redirected HTML page is accepted.
- **SDK risk.** SDK `GetMovieInfoByID` constructs `/works/{id}/` and `GetMovieInfoByURL` scrapes the resulting HTML without checking that the returned work identifier matches the requested one. Because this is shared core code, the risk applies to both DAHLIA and FALENO.
- **Metadata mapping differences.** Amane uses the exact REST post's ACF fields and purpose-specific image URLs; SDK scrapes the HTML and extracts actors, runtime, release date, cover, preview video, gallery, and summary. The SDK may currently expose fields not represented in Amane's model, so replacing the whole parser is not justified by this finding alone.
- **Decision.** Applied a targeted correctness fix rather than rewriting the metadata parser. The proposed SDK patch checks the WordPress REST API for the requested slug before scraping, then validates the final detail URL after redirects. Existing actor/runtime/release/cover/gallery/summary parsing remains unchanged.
- **Patch status (2026-10-16).** Added `patches/metatube-sdk-go/0003-dahlia-verify-exact-work-slug.patch` with deterministic local HTTP tests for exact slug success, missing slug, mismatched slug, and redirect to another work. Combined patch application, gofmt, JavBus/AVBase tests, and DAHLIA/FALENO identity tests passed in [CI run 38883877346](https://github.com/strange-dragon/metaX/actions/runs/38883877346). This remains a proposed SDK patch in the metaX migration workspace; no live-site requests, full SDK regression suite, or end-to-end MetaTube integration were run.

## Focused comparison: Airav (2026-10-16)

Compared Amane's `sites/airav.py` behavior with the native MetaTube SDK interfaces at the pinned SDK snapshot. The implementation in metaX was written independently from the observed behavior; Amane source files and fixtures were not copied.

- **SDK coverage.** No Airav provider was found in the pinned SDK provider inventory, so this is a new provider rather than a patch to an existing SDK scraper.
- **Search correctness.** Amane accepts a search card only when its leading title token matches the requested catalog number. The Go implementation follows exact normalized-number matching and rejects near matches such as an extra trailing digit; it does not select the first fuzzy result.
- **Detail extraction.** The Go provider maps the detail page's number/title, Open Graph cover, actors, studio, release date, tags, and plot into `model.MovieInfo`. It retains the SDK's existing model contract and supports both catalog-number lookup and opaque IDs parsed from detail URLs.
- **Request handling.** The provider validates detail origins, caps response bodies, limits redirects to the configured origin, and rejects a same-origin redirect that resolves to a different detail path.
- **Deterministic coverage.** Local HTTP tests cover exact vs near-number results, required detail identity, field mapping, opaque detail IDs, malformed/unsafe origins, cross-origin redirects, redirected work identity, provider registration, and proxy/timeout configuration. Full metaX Go tests and formatting passed in [run 38890986424](https://github.com/strange-dragon/metaX/actions/runs/38890986424) and [format run 38890986411](https://github.com/strange-dragon/metaX/actions/runs/38890986411).
- **Limitations.** No live Airav requests were made. Current production HTML selectors, domain availability, redirects, and metadata coverage remain unverified; the new provider is not yet registered in the pinned upstream SDK's `engine/register.go`, so it is not automatically available in an unmodified MetaTube server.

## Focused comparison: AVSOX (2026-10-16)

Compared Amane's AVSOX crawler behavior with the pinned SDK provider inventory. No `provider/avsox` package exists in the SDK snapshot, so metaX now has an independent native implementation; no Amane source or fixture files were copied.

- **Transport model.** AVSOX is a Vue SPA; useful movie data comes from its Yii JSON API rather than the initial HTML shell. The Go provider follows the observed sequence: GET the localized home page, read the CSRF token, then POST JSON to `/javu/data/api/search` or `/javu/data/api/getMovie` with the expected AJAX headers and referer.
- **Number selection.** Search prioritizes case-insensitive exact numbers, then falls back to separator-folded matching where hyphens and spaces are ignored but underscores remain significant. Near-number matches are not accepted.
- **Metadata mapping.** The provider maps movie ID/number, localized title and description, cover/poster, sample images, actors, studio, label, series, director, genres, release date, and runtime into the SDK model. Localized entity-name suffixes are handled without assuming every API object has the same shape.
- **Request hardening.** Requests and redirects are constrained to the configured origin; detail URLs require the supported movie path and an alphanumeric ID; response bodies are capped.
- **Deterministic coverage.** Local API tests cover CSRF propagation, search selection, underscore distinction, metadata mapping, mismatched API IDs, invalid URL origins/paths, cross-origin redirects, provider registration, and proxy/timeout configuration. Full metaX Go tests passed in [run 38892023957](https://github.com/strange-dragon/metaX/actions/runs/38892023957), and formatting passed in [run 38892023918](https://github.com/strange-dragon/metaX/actions/runs/38892023918).
- **Limitations.** No live AVSOX requests were made. The configured domain, CSRF policy, API payload/response schema, and current field names remain source-derived assumptions pending live verification. The provider is not yet imported by the pinned upstream SDK's provider registration package.

## Focused comparison: Getchu (2026-10-16)

Compared Amane's `sites/getchu.py` with the pinned SDK's `provider/getchu/getchu.go` at `c62584675c593816cc07e7b5b977d14c7eb505a3`.

- **Search behavior is not equivalent.** Amane searches `www.getchu.com/php/nsearch.phtml`, supplies Getchu cookies, and returns the first `soft.phtml` link without checking that the result matches the requested number. The pinned SDK provider targets `dl.getchu.com` digital-download item pages and does not implement `MovieSearcher`. Do not copy Amane's first-result behavior into the SDK without an exact-match rule and a clear decision about which Getchu catalog is intended.
- **Detail metadata — SDK is broader.** Amane extracts title, cover, release date, brand, genres, and a leading token as the number. The SDK also maps circle/label, release date, genres, summary, preview images, and Open Graph title/cover fallbacks. Keep the existing richer SDK field mapping.
- **ID/URL correctness — SDK gap found.** `GetMovieInfoByID` previously interpolated the caller-provided ID directly into a detail URL. `ParseMovieIDFromURL` used `strings.TrimLeft(path.Base(...), "item")`, which trims any leading characters in the set `i/t/e/m` rather than removing the literal `item` prefix, and it accepted unrelated hosts and paths. This could derive malformed IDs from non-Getchu URLs.
- **Targeted patch.** Added `patches/metatube-sdk-go/0005-getchu-validate-detail-url.patch`: normalize only numeric IDs with optional `GETCHU-` / `GETCHU_` prefixes; reject invalid IDs before URL construction; require HTTPS, the exact `dl.getchu.com` host, default HTTPS port, no userinfo, and the exact `/i/item{digits}` path. Deterministic tests cover valid IDs, malformed IDs, URL origin/path rejection, and invalid-ID short-circuiting.
- **Limits and verification.** No live Getchu requests were made. This patch does not add search support or change the existing metadata parser. The temporary SDK regression workflow was reported passing by the user and was deleted afterward.

## Focused comparison: FC2PPVDB (2026-10-16)

Compared Amane's `sites/fc2ppvdb.py` with the pinned SDK's `provider/fc2ppvdb/fc2ppvdb.go` at `c62584675c593816cc07e7b5b977d14c7eb505a3`.

- **Lookup behavior.** Both implementations derive an article URL from the numeric part of an FC2 number. Amane checks that the response contains a title before accepting the URL; the SDK parses the detail page directly. Amane's numeric extraction removes optional `FC2` / `PPV` prefixes, while the SDK delegates normalization to shared `fc2util.ParseNumber`.
- **Metadata mapping.** Amane extracts title, actresses, article cover, tags, and release date from its current selectors. The SDK additionally maps maker, runtime, and score, and has a more specific Japanese-label parser. It is not safe to replace that parser wholesale without fixtures or a live-site check; current HTML compatibility remains unverified.
- **URL/ID validation gap.** The SDK previously passed arbitrary IDs into `/articles/%s` and extracted IDs from `path.Base(URL.Path)` without validating scheme, host, or article path. That could construct malformed URLs or accept a non-FC2PPVDB URL as a detail page.
- **Targeted patch.** Added `patches/metatube-sdk-go/0006-fc2ppvdb-validate-detail-url.patch`: trim input before shared FC2 normalization, reject invalid IDs before constructing a URL, and require HTTPS on `fc2ppvdb.com` with a numeric `/articles/{id}` path (optional trailing slash allowed). Deterministic tests cover valid/invalid IDs and URL origin/path rejection.
- **Limits / verification status.** No live site requests were made. A malformed regex hunk in the patch file was found and repaired in commit `b44d87c7c6db6e6bf17b92e87fba3064ddc14613`. The temporary workflow's patch-application, formatting, and deterministic FC2PPVDB tests were reported passing by the user; the temporary workflow was deleted afterward. Current HTML compatibility remains unverified.
