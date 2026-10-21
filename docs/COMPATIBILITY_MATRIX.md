# Upstream compatibility and Provider inventory

This matrix is the evidence log for the Amane → MetaTube SDK migration. A blank field means **not yet verified**, not “not applicable”.

## Pinned upstream snapshots

| Component | Repository | Commit SHA | Toolchain / license | Status |
|---|---|---|---|---|
| Amane | [sqzw-x/amane](https://github.com/sqzw-x/amane) | `a2d45880e83df7047d461452965919692701e8c9` | Python >=3.14; GPL-3.0 | Source inventory started; Amane is a behavioral reference only; do not copy or adapt GPL-3.0 code |
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
| Airav | `sites/airav.py` | `provider/airav` (new native Go provider in metaX) | Tightened detail URL parsing to require the canonical `/video/{id}` route and reject query/fragment ambiguity; deterministic route tests added. Live-site verification pending; SDK engine/API integration and server build passed in [run 39139794806](https://github.com/strange-dragon/metaX/actions/runs/39139794806) |
| AVBase | `sites/avbase.py` | `provider/avbase` plus patch `0002` | Source comparison completed: search lacks exact normalized `work_id` filtering, alternate query forms, and deterministic collision resolution; retain SDK source-provider fallback; patch `0010` adds exact normalized work identity, original/compact/dashed query fallback, and deterministic prefix-collision resolution; patch application, formatting, and deterministic tests passed in [CI run 38921478829](https://github.com/strange-dragon/metaX/actions/runs/38921478829) |
| AVSox | `sites/avsox.py` | `provider/avsox` (new native Go API provider in metaX) | Compared API session lifecycle: native Go client previously discarded cookies between CSRF bootstrap and JSON POST; added a shared cookie jar and deterministic cookie-propagation test. Exact/folded number matching and localized metadata are covered by local tests; SDK engine/API integration and server build passed in [run 39139794806](https://github.com/strange-dragon/metaX/actions/runs/39139794806); live-site verification pending |
| Dahlia | `sites/dahlia.py` | `provider/dahlia` | Compared shared DAHLIA/FALENO core; patches `0003` and `0011` validate exact work slug, final redirect identity, HTTPS origin, port, base path, and exact detail route; deterministic tests passed in [CI run 38922533782](https://github.com/strange-dragon/metaX/actions/runs/38922533782); live-site behavior remains unverified |
| DMM | `sites/dmm.py` | `provider/fanza` (FANZA/DMM legacy and video catalogs) | Compared on 2026-10-16; patch `0009` validates DMM detail URL origins/routes and content IDs; deterministic patch-application, formatting, and URL/ID tests passed in [CI run 38901273948](https://github.com/strange-dragon/metaX/actions/runs/38901273948); current live catalog behavior remains unverified |
| Faleno | `sites/faleno.py` | `provider/faleno` + shared Dahlia core | Patch `0013` now maps the WordPress REST work payload, preserves the exact-slug identity check, and routes publisher labels from slug prefixes (`mgold` → maryGOLD, `jimmy` → JimmyScandal; default FALENOstar). Patch `0014` uses exact REST search. Deterministic fixture tests cover metadata mapping, non-exact slugs, publisher routing, and malformed responses. Live verification was blocked on 2026-10-20 by an HTTP 403 from the site's hosting provider; full parity is not claimed. |
| FC2 | `sites/fc2.py` | `provider/fc2` | Compared on 2026-10-18: Amane uses the numeric article ID to construct canonical detail URLs, requires a title, and maps the first sample image as a thumbnail/cover fallback; SDK has broader fields (score, runtime, iframe summary, product-ID consistency check) and keeps richer existing extraction. Keep SDK metadata mapping; retain patch `0004` URL/ID hardening. No parser rewrite justified without current-page fixtures/live validation. |
| FC2Club | `sites/fc2club.py` | Native `provider/fc2club` added in metaX | Deterministic search/detail, ID normalization, URL-origin and redirect tests passed; SDK engine/API integration and server build passed in [run 39139794806](https://github.com/strange-dragon/metaX/actions/runs/39139794806); live-site verification pending |
| FC2PPVDB | `sites/fc2ppvdb.py` | `provider/fc2ppvdb` | Compared on 2026-10-16; patch `0006` hardens article URL/ID validation; temporary deterministic verification workflow reported passing by user; live HTML mapping remains unverified |
| Freejavbt | `sites/freejavbt.py` | No equivalent in the pinned SDK; `provider/javfree` targets a different FC2-focused site | Native `provider/freejavbt` added in metaX; deterministic search/detail, metadata, URL, redirect, and registration tests passed in [CI run 38901990362](https://github.com/strange-dragon/metaX/actions/runs/38901990362); live-site behavior remains unverified; SDK engine/API integration and server build passed in [run 39139794806](https://github.com/strange-dragon/metaX/actions/runs/39139794806) |
| Getchu | `sites/getchu.py` | `provider/getchu` | Compared on 2026-10-18: Amane searches the legacy `www.getchu.com` catalog and returns the first `soft.phtml` result without identity verification; SDK targets digital-download detail pages on `dl.getchu.com` and has no equivalent searcher. Do not port first-result behavior or conflate catalog IDs. Patch `0005` hardens detail ID/URL validation. Live check on 2026-10-20: exact `GETCHU-<digits>` search works through the SDK engine's non-searcher direct-ID fallback; fuzzy `www.getchu.com` catalog search remains deferred. |
| Giga | `sites/giga.py` | No direct equivalent in the pinned SDK | Native `provider/giga` added in metaX; deterministic age-gate, exact-number search, metadata, redirect, and registration tests passed in [CI run 38902620528](https://github.com/strange-dragon/metaX/actions/runs/38902620528); live-site behavior remains unverified; SDK engine/API integration and server build passed in [run 39139794806](https://github.com/strange-dragon/metaX/actions/runs/39139794806) |
| Iqqtv | `sites/iqqtv.py` | No direct equivalent in the pinned SDK | Native `provider/iqqtv` added in metaX; deterministic exact-number matching, excluded-title, metadata, URL, redirect, and registration tests passed in [CI run 38903214624](https://github.com/strange-dragon/metaX/actions/runs/38903214624); multi-language parity and live-site behavior remain unverified; SDK engine/API integration and server build passed in [run 39139794806](https://github.com/strange-dragon/metaX/actions/runs/39139794806) |
| Jav321 | `sites/jav321.py` | No Provider in SDK tree; metaX now has `provider/jav321` | Deterministic tests cover route, metadata mapping, not-found behavior and registration; live-site verification pending; SDK engine/API integration and server build passed in [run 39139794806](https://github.com/strange-dragon/metaX/actions/runs/39139794806) |
| JavBus | `sites/javbus.py` | `provider/javbus` | Compared against pinned SDK source; findings and follow-up decision recorded below |
| JavDB | `sites/javdb.py` | No Provider in SDK tree; metaX now has `provider/javdb` | Deterministic tests cover search, details, number variants, foreign hosts, and registration; SDK engine/API integration and server build passed in [run 39139794806](https://github.com/strange-dragon/metaX/actions/runs/39139794806). Live check on 2026-10-20: `ABP-123` search/detail and FC2 search succeeded; anonymous FC2 detail redirected to JavDB login. |
| JavLibrary | `sites/javlibrary.py` | No direct equivalent in pinned SDK; native `provider/javlibrary` added in metaX | Deterministic search/detail metadata and URL/redirect tests passed in [CI run 38919512864](https://github.com/strange-dragon/metaX/actions/runs/38919512864); live-site availability and current selectors remain unverified; SDK engine/API integration and server build passed in [run 39139794806](https://github.com/strange-dragon/metaX/actions/runs/39139794806) |
| Kin8 | `sites/kin8.py` | `provider/kin8tengoku` + patch `0012` | Added SDK-side KIN8 ID normalization/validation: accepts numeric provider IDs and KIN8/KIN8TENGOKU prefixes, pads to four digits, rejects zero/overlong/foreign identifiers before network access. Legacy `/moviepages/{id}/index.html` route and selectors remain unchanged because current-page behavior is not yet fixture/live-verified. Partial compatibility, not full equivalence |
| MGStage | `sites/mgstage.py` | `provider/mgstage` | Compared on 2026-10-16; patch `0007` validates detail URL origins/paths and rejects malformed IDs; temporary patch-application, formatting, and deterministic test workflow reported passing by user |
| Official | `sites/official.py` | No direct equivalent; this is a 29-manufacturer routing layer, not one site scraper | Source audit completed; defer implementation until SDK/config integration can pass route overrides and prior-studio context without hard-coding a partial solution |
| Prestige | `sites/prestige.py` | No direct equivalent in pinned SDK | Source audit: uses exact SKU JSON endpoints (`/api/sku/item/{sku}` → `parentProduct.uuid` → `/api/product/{uuid}`), tries dashed/compact and GOOE special SKUs; `/api/search` is fuzzy and unsuitable for identity lookup. Amane reports CloudFront 403/region restriction outside Japan; defer native implementation until API behavior can be tested from an allowed region or reliable fixtures become available |
| R18Dev | `sites/r18dev.py` | No HTTP Provider equivalent | Compared on 2026-10-17: this is a query adapter over a user-supplied offline PostgreSQL mirror, not a website/API crawler. Porting it as a normal HTTP Provider would change its data-source contract; defer unless metaX explicitly supports an optional local database-backed metadata source |
| ThePornDB | `sites/theporndb.py` | `provider/theporndb` | Compared on 2026-10-16; patch `0008` adds HTTPS origin, collection-path, and ID validation for scenes, movies, and performers. Actor audit on 2026-10-20 found that `SetConfig(map[string]string)` never satisfied `provider.ConfigSetter`, so the engine never applied the access token; patch `0016` fixes actor and scene/movie config plumbing, accepts `ACCESS_TOKEN`/`api_token`, and applies shared cookie transport settings. Live API/site behavior remains unverified |
| XCity | `sites/xcity.py` | Native `provider/xcity` added | Native Go Provider added on 2026-10-17 with exact normalized catalog-number matching against detail-page `span.kopin`, canonical `/titles/{slug}` route validation, same-origin redirect restrictions, response-size limits, `adc=1` cookie, metadata mapping, and deterministic HTTP-fixture tests. SDK engine/API integration and server build passed in [run 39139794806](https://github.com/strange-dragon/metaX/actions/runs/39139794806); no live-site verification claimed |

Amane's actor registry is separate at `src/amane/crawlers/actor/sites/`; inventory it independently and compare with SDK `ActorProvider` / `ActorSearcher` implementations. The pinned SDK tree has a direct actor implementation for ThePornDB; metaX now adds native JavDB and Minnano actor providers. Do not infer that a movie crawler also covers actor profiles or images.


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
- **URL origin/path validation — SDK was weaker.** `ParseMovieIDFromURL` previously accepted any syntactically valid URL and used its final path segment as the ID. Even after the REST slug check, `GetMovieInfoByURL` could then visit the caller-supplied URL. Patch `0011` now requires the configured origin and scheme/port, disallows userinfo/query/fragment, and enforces the provider base path followed by exactly `/works/{slug}/`; the FALENO `/top` base path is preserved.
- **Metadata mapping differences.** Amane uses the exact REST post's ACF fields and purpose-specific image URLs; SDK scrapes the HTML and extracts actors, runtime, release date, cover, preview video, gallery, and summary. The SDK may currently expose fields not represented in Amane's model, so replacing the whole parser is not justified by this finding alone.
- **Decision.** Applied a targeted correctness fix rather than rewriting the metadata parser. The proposed SDK patch checks the WordPress REST API for the requested slug before scraping, then validates the final detail URL after redirects. Existing actor/runtime/release/cover/gallery/summary parsing remains unchanged.
- **Patch status (2026-10-16).** Patch `0003` verifies the WordPress REST slug and final redirect identity; patch `0011` adds strict detail URL origin/path validation with deterministic tests for valid DAHLIA/FALENO URLs, wrong host/scheme/port, incorrect base path, extra path segments, query/fragment, and userinfo. Combined patch application, gofmt, and all focused JavBus, AVBase, DAHLIA/FALENO, FC2, and Getchu tests passed in [CI run 38922533782](https://github.com/strange-dragon/metaX/actions/runs/38922533782). These remain proposed SDK patches in the metaX migration workspace; no live-site requests, full SDK regression suite, or end-to-end MetaTube integration were run.

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


## Focused comparison: ThePornDB (2026-10-16)

Compared Amane `src/amane/crawlers/sites/theporndb.py` with the pinned SDK's `provider/theporndb/video.go` and `actor.go` at `c62584675c593816cc07e7b5b977d14c7eb505a3`.

- **Architecture and metadata — preserve the SDK implementation.** Amane uses GraphQL while the SDK uses authenticated REST endpoints with separate scene/movie providers. The SDK maps title, description, poster/image, rating, trailer, duration, site, release date, tags, performers, and directors. Replacing the REST implementation with Amane's GraphQL flow would be a broader architectural change and is not part of this hardening patch.
- **URL validation gap.** SDK movie/scene and actor URL parsers previously accepted any parseable URL and returned its last path segment. The patch requires absolute HTTPS URLs on `theporndb.net`, rejects userinfo and non-default ports, and accepts only the expected `/scenes/{id}`, `/movies/{id}`, or `/performers/{id}` paths.
- **ID validation gap.** Movie/scene and actor detail IDs were interpolated into REST API URLs without validation. The patch permits alphanumeric slugs/IDs separated by single hyphens, trims surrounding whitespace, and rejects malformed IDs before making a request. Existing metadata mapping and search behavior are unchanged.
- **Verification status.** After an earlier log reported `provider/theporndb/validation_test.go` missing, the temporary workflow was hardened to assert all patched files exist before formatting/tests. The corrected workflow passed patch application, file-existence assertions, gofmt, and deterministic URL/ID tests in [CI run 38899502066](https://github.com/strange-dragon/metaX/actions/runs/38899502066); the full metaX Go test workflow also passed in [CI run 38899502013](https://github.com/strange-dragon/metaX/actions/runs/38899502013). The one-time workflow was then deleted. No live ThePornDB API calls have been made; live API behavior remains unverified.

### Actor audit (2026-10-20)

Compared Amane's `src/amane/crawlers/actor/sites/theporndb.py` with the pinned SDK `provider/theporndb/actor.go` and scene/movie config code at `c62584675c593816cc07e7b5b977d14c7eb505a3`.

- **Concrete config gap — fixed.** The SDK's ThePornDB actor and scene/movie providers declared `SetConfig(config map[string]string) error`. That legacy signature no longer satisfies `provider.ConfigSetter`, which is declared as `SetConfig(provider.Config) error` and is the only path used by `engine/init.go` to apply full provider configuration. The documented `ACCESS_TOKEN` setting was therefore silently ignored, and the token-gated actor provider could not be enabled through configuration. Patch `0016` changes both providers to the current interface, reads `ACCESS_TOKEN` (and Amane's `api_token` name, case-insensitively), trims surrounding whitespace, and delegates cookie transport settings to `Scraper.SetConfig`. Compile-time interface assertions and deterministic token/cookie-config tests guard the behavior.
- **Architecture — intentionally different.** Amane uses a stash-box GraphQL `searchPerformer`/`findPerformer` flow. The SDK uses authenticated REST endpoints and separate scene/movie providers. Rewriting the REST provider into a GraphQL client would be an architecture change, not an actor-config fix, so it was not done.
- **Search identity — not fully equivalent.** Amane requires an exact normalized canonical-name or alias match and follows deleted/merged performer records. The SDK returns REST search results and the engine applies its existing similarity threshold for search selection. The actor provider itself does not implement Amane's merge-chain or exact-match rules; this remains a search-quality difference, not a proven defect for explicit actor IDs.
- **Field mapping — partial.** Both map names, aliases, birthday, height, measurements, cup, images, and profile text where available. Amane additionally emits provider IDs, a disambiguation/tagline field, and images ranked by pixel area; the SDK `ActorInfo` model does not expose all of those fields, and no model expansion was justified by this audit. Country/nationality and image ordering also differ.
- **Verification.** Patch `0016` applies cleanly after `0008`, `provider/theporndb` tests pass, and the final bundled SDK CI job passed patch application, focused tests, full pinned-SDK regression, formatting, and server build in [run 39420345234](https://github.com/strange-dragon/metaX/actions/runs/39420345234). No live ThePornDB API call was made, so token acceptance and live actor metadata remain unverified.


## Focused comparison: DMM / FANZA (2026-10-16)

Compared Amane `src/amane/crawlers/sites/dmm.py` and its DMM API/CDN helpers with the pinned SDK's `provider/fanza/fanza.go`, `provider/fanza/internal/graphql`, and `provider/fanza/internal/searchparse` at `c62584675c593816cc07e7b5b977d14c7eb505a3`.

- **Provider coverage — overlapping but not identical.** SDK FANZA already supports the legacy `www.dmm.co.jp` digital/mono detail routes and the `video.dmm.co.jp` GraphQL catalog, with broad metadata mapping for titles, identifiers, actors, maker/label/series, genres, runtime, reviews, preview images/video, and thumbnails. Amane additionally routes FANZA TV and DMM TV through separate GraphQL endpoints and has more explicit category-specific HTML fallbacks for legacy pages. These are not one-to-one implementations; do not replace the SDK parser or assume all Amane categories are covered by the SDK.
- **Concrete correctness/security gap.** SDK `IsDigitalVideoURL` previously used `strings.HasPrefix(url, "https://video.dmm.co.jp/")`, which can classify lookalike hostnames such as `video.dmm.co.jp.evil.invalid` as DMM video URLs. `ParseMovieIDFromURL` also extracted IDs from arbitrary hosts/paths, and `GetMovieInfoByID` interpolated unvalidated IDs into candidate URLs.
- **Targeted patch.** Added `patches/metatube-sdk-go/0009-fanza-validate-detail-url.patch`: constrain detail URLs to HTTPS on `www.dmm.co.jp` or `video.dmm.co.jp`, reject userinfo and non-default ports, validate legacy `/digital/.../cid=...` and modern `/{av,amateur,anime,cinema}/content/?id=...` route shapes, and reject malformed IDs before URL construction. Existing parsing, search semantics, GraphQL requests, and metadata mapping remain unchanged.
- **Verification.** The one-off workflow passed patch application, file-existence checks, gofmt, and deterministic URL/ID tests in [CI run 38901273948](https://github.com/strange-dragon/metaX/actions/runs/38901273948). The full metaX Go test workflow passed in [run 38901273971](https://github.com/strange-dragon/metaX/actions/runs/38901273971), and metaX formatting passed in [run 38901273993](https://github.com/strange-dragon/metaX/actions/runs/38901273993). No live DMM/FANZA requests were made, and the SDK's existing live-oriented provider tests were not run.
- **Remaining gap.** Amane's FANZA TV / DMM TV GraphQL category support is not confirmed to be covered by the pinned SDK provider. Treat it as a separate capability review rather than expanding this validation patch without live or deterministic API evidence.


## Focused comparison: Freejavbt (2026-10-16)

Compared Amane `src/amane/crawlers/sites/freejavbt.py` with the pinned SDK provider inventory and `provider/javfree/javfree.go` at `c62584675c593816cc07e7b5b977d14c7eb505a3`.

- **No direct SDK equivalent.** The SDK's `JAVFREE` provider targets `javfree.me` and FC2-specific paths/IDs; it is not the `freejavbt.com` catalog. Reusing it as a substitute would conflate unrelated sites and lookup semantics.
- **Search semantics.** Amane treats the final detail-URL segment as the catalog number and accepts only an exact normalized match. The new native Go provider follows exact matching, rejects near-number and external-origin results, and falls back to direct detail lookup when search returns no candidate.
- **Metadata mapping.** The new provider maps title/number, performers, maker, label, series, release date, runtime, genres, cover/thumbnail, and sample images to the existing SDK `model.MovieInfo` / `MovieSearchResult` types. It does not introduce model changes or an Amane runtime.
- **Request hardening.** Detail and search requests are constrained to the configured origin, redirects are limited to the same origin and 10 hops, response bodies are capped, and malformed IDs are rejected.
- **Deterministic verification.** Local HTTP tests cover exact vs near-number results, field mapping, direct-detail fallback, invalid origins/routes, same-origin redirects to a different work, cross-origin redirect rejection, registration, proxy, and timeout behavior. Full metaX Go tests passed in [run 38901990362](https://github.com/strange-dragon/metaX/actions/runs/38901990362); formatting passed in [run 38901990351](https://github.com/strange-dragon/metaX/actions/runs/38901990351).
- **Limits.** No live Freejavbt requests were made. The site domain, current selectors, permalink shape, and whether the site redirects to a canonical alias remain unverified. The new provider is implemented in the metaX migration workspace but has not been imported into the pinned upstream SDK's registration package.


## Focused comparison: GIGA (2026-10-16)

Compared Amane `src/amane/crawlers/sites/giga.py` with the pinned SDK provider inventory at `c62584675c593816cc07e7b5b977d14c7eb505a3`. No direct GIGA provider exists in the SDK snapshot.

- **Main implementation constraint — age-gate session state.** Unlike a straightforward HTML scraper, GIGA can return an age-confirmation page with HTTP 200 and mutate `PHPSESSID` during `/cookie_set.php` calls. Amane serializes both session initialization and page requests because concurrent requests can overwrite the confirmed session. The native Go implementation likewise serializes requests, uses a shared cookie jar, retries the age-gate flow at most once, and returns a distinct age-verification error rather than reporting a false no-result.
- **Search correctness.** Amane selects the first `/product/index.php` link in a `thumBox` card. The new provider checks candidate detail pages and accepts only an exact normalized match between the requested catalog number and the detail page's `作品番号`; near-number candidates are skipped.
- **Metadata mapping.** The provider maps product ID, title, catalog number, performers, GIGA maker name, DVD release date, runtime, cover/thumbnail, and sample images into the existing SDK models. No model changes or Amane source/fixture reuse were introduced.
- **Request hardening.** URLs are constrained to the configured origin and product-detail route; redirects are limited to the same origin and ten hops; response bodies are capped; malformed product IDs are rejected.
- **Deterministic verification.** Local HTTP tests cover the two-step session issuance pattern, cookie propagation, near-number rejection, detail metadata, invalid URLs, redirects to another product, cross-origin redirect blocking, age-gate failure, registration, proxy, and timeout behavior. Full metaX Go tests passed in [run 38902620528](https://github.com/strange-dragon/metaX/actions/runs/38902620528), and formatting passed in [run 38902620464](https://github.com/strange-dragon/metaX/actions/runs/38902620464).
- **Limits.** No live GIGA requests were made. The current age-gate cookie behavior, product route parameters, and selectors are based on the pinned Amane behavior and remain unverified against the live site. The new provider is in the metaX workspace and is not yet imported by the pinned upstream SDK's provider registration package.


## Focused comparison: IQQTV (2026-10-16)

Compared Amane `src/amane/crawlers/sites/iqqtv.py` with the pinned SDK provider inventory at `c62584675c593816cc07e7b5b977d14c7eb505a3`. No direct IQQTV provider exists in the pinned SDK.

- **Search correctness.** Amane only accepts a result whose title contains a whole catalog-number token matching the query and excludes known broken/re-encoded releases. The new Go provider uses exact normalized token matching, rejects near-number results, and applies the same exclusion categories without copying Amane code or fixtures.
- **Metadata mapping.** The native provider maps title/number, actors, studio, series, release date, runtime, tags/genres, Open Graph cover, sample images, and plot into existing SDK models. Runtime parsing rounds seconds to the nearest minute and metadata collections are deduplicated.
- **URL/request hardening.** Detail IDs preserve the path needed for lookup, while origin, scheme, port, userinfo, traversal, and search-route checks prevent unrelated URLs from being treated as details. Redirects are constrained to the configured origin, capped at ten hops, and response bodies are capped.
- **Deterministic verification.** Local HTTP tests cover exact vs near-number results, excluded titles, foreign hosts, field mapping, redirect-to-different-work rejection, cross-origin redirect blocking, registration, proxy, and timeout configuration. Full metaX Go tests passed in [run 38903214624](https://github.com/strange-dragon/metaX/actions/runs/38903214624), and formatting passed in [run 38903214733](https://github.com/strange-dragon/metaX/actions/runs/38903214733).
- **Remaining gap.** Amane exposes `zh_cn`, `zh_tw`, and `jp` search prefixes. The current SDK `MovieSearcher` interface does not pass Amane's per-request language option, so the native provider currently searches the default Simplified Chinese route; details can be parsed from supported language-prefixed URLs, but full multi-language search parity needs a deliberate interface/config decision. No live IQQTV requests were made, and the provider is not yet imported into the pinned upstream SDK registration package.


## Focused comparison: Official manufacturer router (2026-10-16)

Compared Amane `src/amane/crawlers/sites/official.py` with the pinned SDK's provider interfaces and inventory at `c62584675c593816cc07e7b5b977d14c7eb505a3`.

- **This is not a single-site crawler.** Amane routes catalog numbers to 29 manufacturer domains. Resolution order is explicit `official_routes` configuration, series-prefix mapping, then aliases inferred from a prior provider's studio metadata. The domains do not all share one CMS, and a connectivity probe to one manufacturer cannot establish cluster-wide availability.
- **SDK integration gap.** A plain `MovieSearcher.SearchMovie(keyword)` call does not carry Amane's `partial_result.studio` context, and a generic provider instance cannot infer per-user `official_routes` unless configuration is passed through `ConfigSetter` or a higher-level routing service. A static `official` provider with a copied series table but no configuration/context support would silently lose two of the three routing priorities.
- **Metadata scope.** Once routed, Amane extracts title, catalog number, actors, release date, runtime, genres, series, and a horizontal cover from manufacturer-specific sites that share a Works-like HTML structure. It does not have a single base URL or universal detail route.
- **Decision (2026-10-21): not planned.** metaX uses the plugin/library model instead: each Jellyfin library maps to one content category, each category maps to a provider list, and the backend fans out and merges results. This does not require Amane's `official_routes`, series-prefix routing, studio-alias fallback, or manufacturer priority. The source comparison above remains evidence for why a hidden static `Official` provider would be incomplete; it is not an implementation task.
- **Limits.** This is a source-level architecture assessment only; no manufacturer websites were requested. No Amane code or route fixtures were copied into the project.


## Focused comparison: JavLibrary (2026-10-16)

Compared Amane `src/amane/crawlers/sites/javlibrary.py` with the pinned SDK provider tree at `c62584675c593816cc07e7b5b977d14c7eb505a3`. No direct JavLibrary provider exists in the pinned SDK.

- **Search behavior.** Amane queries the catalog-number endpoint and prefers a title containing the requested number, but falls back to the first result when none matches. The native Go provider deliberately rejects non-exact catalog-number candidates to avoid assigning a neighboring title to the requested work.
- **Metadata coverage.** The new provider maps number, title, release date, runtime, director, studio, label, cast, genres, cover image, and score to the existing SDK model. It supports the site's query-parameter detail IDs and relative image URLs.
- **Request safety.** Detail URLs must use the configured origin, an allowed language path, and a valid `v` query parameter. Redirects are restricted to the same origin and at most ten hops; response bodies are capped at 8 MiB. Proxy and request timeout settings follow the local Provider conventions.
- **Deterministic tests.** Local HTTP fixtures cover exact-number search vs near matches, metadata mapping, malformed/foreign URLs, cross-origin redirect rejection, registration, proxy validation, and timeout configuration. Initial CI runs stopped at the formatting gate; the formatter workflow applied a formatting-only commit. The subsequent full Go test workflow passed, including the deterministic JavLibrary tests, in [CI run 38919512864](https://github.com/strange-dragon/metaX/actions/runs/38919512864). Formatting passed in [run 38919462195](https://github.com/strange-dragon/metaX/actions/runs/38919462195).
- **Limits.** No live JavLibrary requests have been made. Current selectors, route behavior, score formatting, and availability require live-site verification. This implementation is independently written and does not include Amane code or fixtures.


## Focused comparison: AVBase search semantics (2026-10-16)

Compared Amane `src/amane/crawlers/sites/avbase.py` with the pinned SDK `provider/avbase` implementation at `c62584675c593816cc07e7b5b977d14c7eb505a3`, including existing metaX patch `0002-avbase-preserve-prefixed-detail-url.patch`.

- **Existing fix retained.** Patch `0002` corrects prefixed canonical detail URLs by constructing `/works/{prefix}:{work_id}`. This addresses a real 404 risk and has its own deterministic test.
- **Search false-positive risk.** SDK `SearchMovie` currently emits all results returned by a single fuzzy `/works?q=` query without verifying that normalized `work_id` matches the requested catalog number. Amane checks normalized identity, tries original/compact/dash-inserted query forms, and handles duplicate `work_id` values caused by prefixes.
- **Collision handling.** When multiple work records share a normalized ID, the desired deterministic policy is to prefer a candidate whose products contain the requested number; otherwise choose an unprefixed canonical record only if unique; unresolved ambiguity should produce no match rather than silently selecting the first result.
- **Metadata differences.** Amane extracts runtime from product volume and a director from product iteminfo, while the SDK's fallback work mapping does not currently populate these fields. The SDK's multi-source provider lookup can supply richer metadata and should remain the preferred source, with work-level data as fallback. The work-level actor schema does not expose the same gender note as Amane's casts structure, so do not infer gender where the SDK data has no reliable signal.
- **Next implementation.** Add patch `0010` for exact work identity, query-form fallback, and deterministic collision resolution, with tests for normalized separators/leading zeros, ambiguous prefixed works, and fallback query forms. Keep metadata enhancements separate unless the pinned JSON contract and test data support them. No live AVBase requests were made; this is a source-level comparison.
## Focused comparison: Prestige (2026-10-16)

Compared Amane's `sites/prestige.py` with the pinned SDK provider inventory. No direct Prestige provider exists in the pinned SDK.

- **Lookup semantics.** Amane deliberately avoids the site's fuzzy `/api/search` endpoint. It probes exact SKU IDs in a fixed order: the submitted number, its compact form, then `GOOE` special-edition variants; it resolves the product through the SKU response's `parentProduct.uuid`, then fetches `/api/product/{uuid}`.
- **Metadata mapping.** The API response carries the normal SKU number, title, release date, runtime, actresses, directors, genres, maker/label/series, plot, poster/package image paths, media gallery, and product UUID. Image paths may be relative to `/api/media/`; poster and landscape thumbnail slots are intentionally mapped from different API fields.
- **Important access constraint.** The pinned Amane implementation documents a region restriction: requests outside Japan may receive CloudFront 403 or a regional restriction response. No live requests were made as part of this comparison, and the API schema has not been independently validated here.
- **Decision.** Do not create a speculative native provider yet. First obtain permitted-region test responses or stable, independently sourced API fixtures; then implement exact SKU matching, special SKU handling, field mapping, URL-origin validation, response limits, and deterministic tests. Do not substitute the fuzzy search endpoint as a fallback because it can return unrelated products.


## Focused comparison: AVSOX API session lifecycle (2026-10-16)

Compared Amane `src/amane/crawlers/sites/avsox.py` with metaX `provider/avsox` against the pinned source snapshots.

- **API flow.** Both clients first GET the `/cn/` shell to retrieve a CSRF token, then POST JSON to `/javu/data/api/{search|getMovie}`. Amane uses a shared crawler HTTP client for both requests.
- **Gap found in native Go provider.** The Go client did not configure an HTTP cookie jar. Cookies set by the CSRF bootstrap response were therefore not retained for the following API POST, which can break session-bound CSRF validation or other session state.
- **Fix.** The native provider now creates a shared cookie jar for its HTTP client, including the test-client path. The API fixture server sets a session cookie during the CSRF bootstrap and asserts that both JSON API handlers receive it.
- **Verification.** Full Go tests passed in [CI run 38924134237](https://github.com/strange-dragon/metaX/actions/runs/38924134237), and formatting passed in [run 38924134183](https://github.com/strange-dragon/metaX/actions/runs/38924134183). This is deterministic local HTTP coverage only; no live AVSOX requests were made.


## Focused comparison: Airav detail route validation (2026-10-16)

Compared Amane `src/amane/crawlers/sites/airav.py` with the native Go provider in metaX.

- **Search identity.** Both implementations accept search cards only when the leading catalog token exactly matches the requested number after normalization; near-number matches are rejected.
- **URL validation gap.** The native Go parser validated origin but previously treated the final path segment of any same-origin URL as a movie ID. That allowed unrelated paths such as `/not-video/{id}` to be used as detail pages; query and fragment components were also accepted even though they are not part of the canonical detail identity.
- **Fix.** Detail URLs now require exactly the `/video/{id}` route (an optional trailing slash is tolerated) and reject query/fragment components. Redirect validation still requires the final path's ID to match the originally requested ID, in addition to the same-origin redirect policy.
- **Verification.** Added deterministic parser tests for wrong route, extra path segments, query, and fragment. Go tests and formatting passed in [CI run 38924333135](https://github.com/strange-dragon/metaX/actions/runs/38924333135) and [format run 38924333132](https://github.com/strange-dragon/metaX/actions/runs/38924333132). No live Airav requests were made.


## Focused comparison: JavDB detail URL origin validation (2026-10-16)

Compared Amane `src/amane/crawlers/sites/javdb.py` with native metaX `provider/javdb`.

- **Search behavior.** Amane retries alternate Western date-number forms and selects an exact or uniquely accepted leading-digit variant. The native provider already implements number variants and rejects ambiguous leading-digit collisions; this work leaves search behavior unchanged.
- **Gap found.** The native provider checked JavDB's hostname but did not require the configured scheme or effective port and accepted userinfo and fragments. Consequently, URLs such as `http://javdb.com/v/id`, `https://javdb.com:8443/v/id`, or `https://user@javdb.com/v/id` could pass detail URL parsing even though they do not represent the configured origin.
- **Fix.** Detail URL parsing now requires the configured scheme, the configured host or documented `www` alias, the same effective port, and no userinfo or fragment. Query parameters remain allowed because JavDB detail links can legitimately include locale parameters. The configured test-server scheme is honored so local HTTP tests remain deterministic while production's configured origin is HTTPS.
- **Verification.** Added deterministic rejection cases for HTTP downgrade, non-default port, userinfo, lookalike host, and fragment. Full Go tests passed in [CI run 38926808936](https://github.com/strange-dragon/metaX/actions/runs/38926808936), and formatting passed in [run 38926808957](https://github.com/strange-dragon/metaX/actions/runs/38926808957). No live JavDB requests were made.


## Focused comparison: JavDB actor search and profile (2026-10-16)

Compared Amane's JavDB actor crawler with the pinned SDK actor interfaces and ThePornDB actor implementation.

- Coverage gap: the pinned SDK exposes ActorProvider / ActorSearcher, but its provider tree has no JavDB actor implementation. Amane's JavDB actor crawler provides exact canonical-name/alias matching, profile IDs, aliases, and avatar extraction.
- Implementation: added provider/javdb/actor.go as a separate registered JavDBActor provider. It searches JavDB's actor filter, rejects non-exact names rather than accepting substring hits, parses actor profile slugs, collects aliases while dropping gender/film-count labels, and maps profile/avatar data to existing SDK actor models. It reuses the native JavDB HTTP client and does not add SDK model fields.
- Safety: actor detail URLs require the configured scheme, host/alias, effective port, a canonical /actors/{slug} path, and no userinfo, query, or fragment. Redirects continue to use the shared same-origin policy.
- Verification: deterministic local tests cover exact search selection, metadata mapping, malformed/foreign origins, and actor factory registration. Full Go tests passed in [CI run 38927152300](https://github.com/strange-dragon/metaX/actions/runs/38927152300), and formatting passed in [run 38927152279](https://github.com/strange-dragon/metaX/actions/runs/38927152279). No live JavDB actor requests were made.



### Actor source audit: implementation scope

| Amane actor source | SDK/metaX status | Decision |
|---|---|---|
| AVBase | SDK has an AVBase movie provider but no actor provider | AVBase actor pages are addressed by talent name, while the payload also exposes a separate numeric talent ID. Defer until the native provider can define and test a stable ID-to-canonical-name resolution contract rather than pretending the numeric ID is directly routable. |
| GFriends | SDK actor provider plus built-in image enrichment | Gfriends is an image index backed by a repository `Filetree.json`, not a biography/profile crawler. The pinned SDK already prepends Gfriends images to Japanese actor profiles in `engine/actor.go`, so no separate enrichment provider is required. Do not replace profile fields with image-only hits. |
| JavDB | Native `provider/javdb/JavDBActor` added | Exact name/alias matching, actor slug validation, profile metadata and avatar mapping are covered by deterministic tests; live-site behavior remains unverified. |
| Minnano | Native `provider/minnano/MinnanoActor` added | Exact-name search, numeric profile IDs, age-gate cookie and profile fields are covered by deterministic tests; SDK actor registration and API discovery passed in [run 39139794806](https://github.com/strange-dragon/metaX/actions/runs/39139794806); live-site behavior remains unverified. |
| Wikipedia | Native `provider/wikipediaactor/WikipediaActor` added | Exact label/alias identity, AV occupation/description filtering, Wikidata birthday/image fields, Wikipedia biography extraction, and deterministic mocked API tests; live-site behavior remains unverified. |
| ThePornDB | SDK already provides `ThePornDBActor`; patch `0016` fixes config plumbing | Retain the existing REST actor provider. Patch `0016` makes the actor and scene/movie providers implement `provider.ConfigSetter`, so `ACCESS_TOKEN`/`api_token` and cookie transport settings are actually applied; GraphQL merge/exact-search behavior and missing model fields remain documented differences. |


## Focused comparison: Minnano actor search and profile (2026-10-16)

Compared the behavior of Amane's Minnano actor crawler with the pinned SDK `ActorProvider` / `ActorSearcher` interfaces. The SDK had no Minnano actor provider.

- **Search identity.** The native provider uses the actress-scoped search endpoint and only accepts exact normalized display-name matches; it does not return substring or near-name results. Actor details use the site's numeric `/actress{id}.html` route.
- **Profile mapping.** Added a separate registered `MinnanoActor` provider mapping aliases and name readings, birthday, height, measurements, cup size, and profile image into existing SDK actor fields. The age-verification cookie is sent for search and detail requests.
- **Input and response hardening.** Detail URLs must use the configured origin and canonical numeric actress route; query strings, fragments, userinfo, alternate ports, and foreign origins are rejected. Redirects are bounded and constrained to the configured origin, and response bodies are capped at 8 MiB.
- **Verification.** Deterministic local HTTP tests cover exact search matching, age-cookie delivery, profile mapping, URL-origin/path rejection, and factory registration. Full Go tests passed in [CI run 38928899145](https://github.com/strange-dragon/metaX/actions/runs/38928899145), and formatting passed in [run 38928899279](https://github.com/strange-dragon/metaX/actions/runs/38928899279). Live Minnano site behavior and current selectors remain unverified.


## SDK patch validation workflow audit (2026-10-16)

The maintained workflow previously applied only a subset of the patch set, so several patch files were not reproducibly validated by the long-term CI. Expanded `.github/workflows/sdk-patch-validation.yml` to apply every unique patch against the pinned SDK in dependency order and run focused deterministic tests for JavBus, AVBase, DAHLIA/FALENO, FC2, Getchu, FC2PPVDB, MGStage, ThePornDB, and FANZA/DMM. Formatting coverage now includes all changed SDK source and test files.

The audit also found two competing files numbered `0005` for Getchu. Kept the version used by the workflow (`0005-getchu-normalize-and-validate-detail-url.patch`) and removed the redundant `0005-getchu-validate-detail-url.patch`.

- All maintained patches applied successfully to the pinned SDK; formatting and all focused patch tests passed in [CI run 38932602151](https://github.com/strange-dragon/metaX/actions/runs/38932602151).
- The metaX Go tests and Go formatting workflows also passed on the same commit: [tests](https://github.com/strange-dragon/metaX/actions/runs/38932602115), [format](https://github.com/strange-dragon/metaX/actions/runs/38932602108).
- This validates patch application and targeted regression cases, not the entire upstream SDK test suite or live-site behavior.


## Focused comparison: KIN8, R18Dev, and XCity (2026-10-17)

### KIN8

The pinned SDK has `provider/kin8tengoku`, but its behavior is not equivalent to Amane current crawler.

- **Route drift.** Amane constructs canonical detail URLs at `https://www.kin8tengoku.com/movie/{four-digit-id}`; the pinned SDK constructs legacy `/moviepages/{four-digit-id}/index.html` URLs and parses legacy DOM selectors.
- **Identity normalization.** Amane accepts only `KIN8-` / `KIN8TENGOKU-`-style identifiers, validates the numeric portion, pads it to four digits, and rejects zero and overlong IDs. The SDK accepts bare numeric IDs, but its prefix regex does not cover the full `KIN8TENGOKU-` form and does not impose the same four-digit/nonzero constraints.
- **Decision.** Do not change the SDK detail route based only on the Amane source: the current site full HTML and all relevant metadata fields need deterministic fixtures or live confirmation first. Mark the SDK provider as partial/stale rather than equivalent.

### R18Dev

Amane R18Dev crawler is not an HTTP scraper. It queries a user-configured offline PostgreSQL mirror through `R18Repository`, translates the query number into candidate IDs, maps the database result to metadata, and degrades database errors to a miss so other sources can continue.

- There is no equivalent provider in the pinned SDK tree, but implementing an HTTP Provider would be the wrong abstraction.
- **Decision.** Defer unless metaX product scope explicitly includes an optional local PostgreSQL metadata source. If added later, design its connection/configuration and schema contract independently from web crawlers; do not port Amane code or assume its database schema.

### XCity

Amane strips hyphens from the requested number, queries `/result_published/?q=...`, and takes the first result link containing `/titles/`. Detail parsing then extracts the actual catalog number from `span.kopin`, but the search flow does not verify that it matches the requested number.

- The pinned SDK has no XCity provider; metaX now has a native Go implementation in `provider/xcity/`.
- **Correctness fix.** The provider treats the search page as a candidate list only, fetches each candidate detail page, and accepts it only when the normalized `span.kopin` value exactly equals the requested catalog number. It rejects off-origin URLs, noncanonical detail paths, query/fragment-bearing detail URLs, and cross-origin redirects.
- **Coverage.** Deterministic local HTTP fixtures cover exact-vs-near number matching, metadata mapping, age-confirmation cookie, URL rejection, redirect rejection, and provider registration. All deterministic metaX Go tests and formatting passed on [test run 38978044363](https://github.com/strange-dragon/metaX/actions/runs/38978044363) and [format run 38978044255](https://github.com/strange-dragon/metaX/actions/runs/38978044255); no live-site compatibility is claimed.


### KIN8 ID normalization patch (2026-10-17)

Added `patches/metatube-sdk-go/0012-kin8-validate-and-normalize-movie-id.patch`. The patch validates KIN8 IDs before URL construction and normalizes numeric IDs to the four-digit form expected by the current Amane reference. It accepts bare numeric IDs for compatibility with the SDK's existing direct-ID tests, plus `KIN8-` and `KIN8TENGOKU-` prefixes; it rejects unrelated catalog prefixes, zero, overlong values, and path-like input before any network request.

The patch is applied by `.github/workflows/sdk-patch-validation.yml`; patch application, formatting, and all focused deterministic SDK tests passed in [run 38978044278](https://github.com/strange-dragon/metaX/actions/runs/38978044278). This does not fix the legacy-vs-current route difference; that remains deferred until the current site's HTML and field mappings are verified.


## DAHLIA/FALENO WordPress REST contract gap (2026-10-17)

A deeper source comparison found that the Amane `WordPressWorksCrawler` is not merely a different HTML parser. It uses the WordPress REST endpoint `/wp-json/wp/v2/works?slug={slug}` as the source of truth for both lookup and metadata extraction. It rejects fuzzy/redirect-based substitutes by requiring a returned post whose `slug` exactly matches the normalized catalog slug. This is important because the HTML site can redirect an unrecognized prefix match to another work.

The pinned SDK's shared `provider/dahlia/core` still searches legacy HTML and extracts fields from legacy selectors. The existing patches `0003` and `0011` improve exact identity and URL safety on that implementation, but do not close the REST API/source-contract gap.

Output mapping differences to resolve:
- REST ACF fields supply title, actors, plot, runtime, and release date; custom image fields supply main, sub, and gallery images.
- The public work page URL should remain the returned metadata homepage, even if JSON is used for retrieval.
- FALENO derives publisher/label from slug prefixes (`mgold` → `maryGOLD`, `jimmy` → `JimmyScandal`) while keeping studio as FALENO; Dahlia uses DAHLIA for both.
- Missing optional ACF fields should degrade individually rather than fail the whole record; non-exact slug results should be treated as misses.

Implementation update (2026-10-18): patch `0013-dahlia-faleno-wordpress-metadata.patch` now fetches the exact WordPress REST work record once and enriches the existing SDK `MovieInfo`, while retaining the public work-page URL as `Homepage`. It maps title (with rendered-title fallback and actor-suffix trimming), actors, plot, runtime, release date, poster/thumbnail/gallery images, and FALENO's `maryGOLD` / `JimmyScandal` label prefixes while keeping the studio as FALENO. Missing optional fields preserve values already parsed from HTML.

The permanent patch workflow applies the new patch and tests exact-slug matching, redirect identity, metadata mapping, image handling, runtime, and FALENO label routing. Patch application, formatting, focused SDK tests, and metaX Go tests passed on commit `0b3086e95c8153259e939180a4c2ef9d87e3ce36` in [SDK patch run 39134683828](https://github.com/strange-dragon/metaX/actions/runs/39134683828), [Go test run 39134683778](https://github.com/strange-dragon/metaX/actions/runs/39134683778), and [format run 39134683752](https://github.com/strange-dragon/metaX/actions/runs/39134683752).

Remaining limitation: search results still use the SDK's legacy HTML search path, and no live-site validation has been performed. Treat this as improved detail metadata parity, not complete DAHLIA/FALENO migration.


## Focused comparison: official FC2 article metadata (2026-10-18)

Compared Amane `src/amane/crawlers/sites/fc2.py` with the pinned SDK `provider/fc2/fc2.go` at `c62584675c593816cc07e7b5b977d14c7eb505a3`.

- **Lookup contract.** Amane accepts FC2-prefixed or bare numeric identifiers, strips the prefix, and constructs the canonical `/article/{numeric-id}/` URL. The SDK uses the shared `fc2util.ParseNumber` normalizer and constructs the same article route. Existing patch `0004` hardens the SDK boundary by rejecting malformed IDs and noncanonical/untrusted detail URLs before fetching.
- **Required identity/title.** Amane returns no metadata if the page title is absent and derives the reported number from the numeric article path. The SDK also checks the page's Product ID against the requested ID, which is a useful additional identity safeguard, but can return a partially populated `MovieInfo` if selectors stop matching. Do not change this behavior without deterministic fixtures and a clear downstream validity contract.
- **Metadata coverage.** Amane maps title, studio, release date, tags, description, poster, and sample images. The SDK additionally maps score, runtime, iframe-based summary, and validates Product ID consistency. Preserve the SDK's broader mapping rather than replacing it with Amane's narrower parser.
- **Image semantics.** Amane uses the first sample image as its thumbnail and all sample images as extrafanart, while the poster comes from the main thumbnail element. The SDK maps the main thumbnail separately and uses the first preview image as the cover fallback because it may have better resolution. These are distinct field semantics; do not collapse them into one URL.
- **Decision.** No additional FC2 metadata patch is justified by this source comparison. Keep patch `0004` and the SDK's existing richer field extraction. Revisit title-required behavior only with independent fixtures showing a real false-positive/partial-result problem.
- **Verification limits.** This is a source-level comparison only. No live FC2 requests, current HTML fixture capture, full upstream SDK regression suite, or end-to-end MetaTube/Jellyfin test was run.


## Focused comparison: Getchu search/catalog contract (2026-10-18)

Compared Amane `src/amane/crawlers/sites/getchu.py` with the pinned SDK `provider/getchu/getchu.go` at `c62584675c593816cc07e7b5b977d14c7eb505a3`.

- **Different catalogs.** Amane queries `www.getchu.com/php/nsearch.phtml` and follows `soft.phtml` links. The SDK's provider is explicitly based on `https://dl.getchu.com/i/item{digits}`, a digital-download catalog. A shared provider name does not establish equivalent IDs, routes, or content coverage.
- **Amane's current selection is not safe to copy.** It returns the first matching `soft.phtml` link without checking the detail page's catalog number against the requested number. Porting that logic would reproduce a known false-positive risk.
- **Detail metadata.** Amane extracts title, cover, release date, brand, genres, and a leading token as the number. The SDK additionally maps circle/label, summary, preview images, and Open Graph fallbacks. Keep the SDK's richer existing detail parser.
- **Decision.** Do not add a `MovieSearcher` implementation in this task. First define whether the desired feature is legacy retail catalog search, digital-download search, or both; then implement only with deterministic fixtures and exact identity checks. Patch `0005` remains limited to safe detail ID normalization and URL validation.
- **Verification limits.** This is a source-level comparison only. No live Getchu requests, current catalog fixtures, full upstream SDK regression suite, or end-to-end MetaTube/Jellyfin test was run.

## Focused comparison: actor-source contracts (2026-10-18)

Audited the six registered Amane actor crawlers at commit `a2d45880e83df7047d461452965919692701e8c9`. These sources do not form one interchangeable "actor scraper" category; their capabilities and data contracts differ.

- **AVBase** is a name-to-profile adapter for `/talents/{name}`, not a conventional search endpoint. It reads Next.js `talent` data and maps canonical name, alternate identities, gender, birthday, prefecture, body measurements, image, and AVBase ID. A native implementation should preserve 404-as-not-found semantics and accept both male and female profiles; a generic fuzzy searcher would not be equivalent.
- **GFriends** is an actor-image index over a GitHub-hosted `Filetree.json`, with optional local index caching. It supplies image URLs and a content path, not a biographical profile. The index's deterministic folder ordering and duplicate filename policy affect which image is selected. The pinned SDK already uses it as an image enrichment source for Japanese actor profiles in `engine/actor.go`; do not replace profile fields with image-only hits.
- **JavDB** is an actor profile source; metaX already has a native JavDB actor package. It still needs its own field-by-field comparison and tests against the pinned SDK actor interfaces before parity can be claimed.
- **Minnano** performs a name search, handles exact-hit redirects, and otherwise selects only a normalized exact-name result. Its detail mapping includes aliases/readings, birthday, birthplace, height, measurements, cup size, and image. The native metaX package exists, but current-site selectors and live availability remain unverified.
- **ThePornDB** is the actor implementation already present in the pinned SDK and should be compared before duplicating it. Actor profile fields and movie-scene metadata are separate contracts.
- **Wikipedia/Wikidata** is an enrichment source rather than a conventional studio catalog: it searches Wikidata in Japanese, Chinese, and English, filters candidates for AV-industry relevance, and extracts localized biography text from Wikipedia. Its occupation/gender/entity disambiguation and text cleanup are substantial behavior; it should not be approximated by a simple title search or folded into a site-specific profile parser.

### Actor migration decision

The native actor packages currently present in metaX cover JavDB, Minnano, AVBase, and Wikipedia/Wikidata. Gfriends image enrichment does **not** require a new provider: the pinned SDK already injects Gfriends images into Japanese actor profiles in `engine/actor.go`. The ThePornDB comparison remains a distinct work item and should produce a patch only if a concrete SDK gap is identified. Before adding actor profile aggregation, verify how the MetaTube engine consumes `ActorProvider` results and whether it can merge profile, image, and biography sources without discarding populated fields.

### Verification limits

This pass is a source-contract audit, not a runtime test. No live actor site/API request, captured-site fixture test, actor-source aggregation test, MetaTube engine registration check, or Jellyfin end-to-end test was performed.


## Airav FC2 catalog query fallback (2026-10-19)

A focused follow-up to the Airav source comparison found a behavior gap in the native Go provider: Amane recognizes that Airav indexes FC2 articles under the canonical `FC2-PPV-{id}` form and tries that form before the caller's original number. The Go provider previously queried only the original input, so searches using `FC2-{id}` could miss a record that Airav indexed under the PPV spelling.

- **Change.** The Go provider now recognizes FC2 article IDs across hyphenated, compact, and supported underscore-separated spellings, tries the canonical `FC2-PPV-{id}` query first when it differs from the input, then falls back to the original query. It stops at the first exact semantic match, and if the canonical request fails it still attempts the fallback query before returning an error.
- **Identity safety.** FC2 variants compare equal only when both parse as FC2 article numbers with the same numeric article ID. Near-number matches remain rejected; non-FC2 matching behavior is unchanged.
- **Tests.** Added deterministic local HTTP tests for canonical-first ordering, empty-result fallback to the original query, and matching `FC2-{id}`, compact `FC2PPV{id}`, and underscore-separated variants against a canonical Airav result.
- **Limits.** No live Airav requests were made. Current website behavior, current selectors, and runtime metadata coverage remain unverified; this closes the source-derived FC2 query-variant gap only.


## AVBase actor profile provider (2026-10-19)

Compared Amane `src/amane/crawlers/actor/sites/avbase.py` with the pinned SDK actor interface and the existing metaX actor providers.

- **Lookup contract.** AVBase has no usable actor search endpoint; `/talents?q=` returns 404. The native `AVBaseActor` provider resolves names directly through `/talents/{name}`, treats HTTP 404 or missing embedded talent data as a miss, and accepts same-origin redirects to canonical talent names.
- **Metadata mapping.** The provider reads the Next.js `props.pageProps.talent` payload and maps canonical name, aliases, birthday, height, available bust/waist/hip measurements, cup size, and profile image into existing `model.ActorInfo` fields. It does not mis-map Amane's gender or prefecture fields because the pinned SDK model has no semantically equivalent fields.
- **Identity and request handling.** Actor IDs use the canonical talent path segment; URL parsing requires HTTPS on the configured AVBase origin and exactly one `/talents/{name}` segment. Requests enforce same-origin redirects, bounded response reads, timeout, and proxy configuration.
- **Deterministic coverage.** Local HTTP tests cover alias redirects, canonical identity, field mapping, missing data, 404 misses, malformed URLs, cross-origin redirect blocking, registration, and request configuration. Go tests and formatting passed on commit `dc8b7d5` in [runs 39207291535](https://github.com/strange-dragon/metaX/actions/runs/39207291535) and [39207291570](https://github.com/strange-dragon/metaX/actions/runs/39207291570). The twelve-package engine registration/API discovery test, full pinned SDK regression suite, and server compilation passed in [run 39207291492](https://github.com/strange-dragon/metaX/actions/runs/39207291492).
- **Limits.** No live AVBase requests were made. Current Next.js payload shape, redirects, selectors, and the live profile's actual metadata completeness remain unverified. Actor gender and birthplace remain unavailable in the pinned SDK's `ActorInfo` model.

## JavDB actor uncensored-name normalization (2026-10-20)

Compared Amane's JavDB actor parser with the native Go actor provider. Amane removes trailing `(無碼)`, `(无码)`, and `(Uncensored)` markers from actor names and aliases before exact-name matching. The Go provider previously retained those suffixes, which could cause duplicate aliases or failed exact-name matches between censored and uncensored listings.

The native actor parser now removes these trailing markers (including full-width parentheses) when splitting canonical names and aliases. A deterministic unit test covers all three marker forms and duplicate-name de-duplication. This is a source-derived compatibility fix only; no live JavDB actor page was requested, and tests/formatting were not run in this task.

## JavDB actor canonical profile identity (2026-10-20)

Amane derives the actor provider ID and source URL from the profile's canonical link when available, falling back to the requested page URL. The native Go parser previously always retained the requested ID and reconstructed its homepage, which could store a non-canonical identity when the site resolves an alias URL to a canonical actor profile.

The Go parser now accepts a canonical URL only after normalizing it against JavDB's base URL and validating it with the existing actor URL/origin rules. Invalid or off-origin canonical links are ignored; the requested identity remains the fallback. A deterministic HTTP fixture test covers a requested actor URL whose page declares a different canonical actor ID. No live requests or Go test/format commands were run.

## Automatic category inference and routed search (2026-10-20)

The API's optional category parameter alone was insufficient for the existing Jellyfin plugin contract, which sends movie number/provider but does not supply an Amane content type. Added conservative inference for identifier forms that strongly imply a category: FC2 PPV forms (FC2-PPV3954834, FC2PPV3954834, FC2-PPV-3954834) and date/sequence forms such as 101926_01 use the project's combined FC2/uncensored route; KIN8 identifiers use the western route.

Explicit provider overrides remain unchanged, explicit categories still take precedence, URL lookups remain direct detail lookups, and ambiguous numbers retain the SDK's broad search behavior. If an inferred category has no installed route providers, the API falls back to broad search instead of turning an optimization into a hard failure. Deterministic unit and API/fan-out regression tests were added, but Go tests, formatting, integrated build, and live-site requests were not run.

## Integrated live API and provider coverage (2026-10-20)

Environment: local macOS, Go 1.27.3, pinned SDK `c62584675c593816cc07e7b5b977d14c7eb505a3`, public network. The server was built with the maintained patches/overlays and exercised through the HTTP API with `.github/scripts/live-metax-api-smoke.sh`; the script passed (`LIVE_API_SMOKE_OK`).

| Surface | Observed result | Limit / interpretation |
|---|---|---|
| Censored search | `GET /v1/movies/search?q=ABP-123&category=censored` returned HTTP 200; `MetaX` was first, followed by JavBus and JavDB results | Point-in-time result; search merge filled the `MetaX` candidate from later route results |
| `MetaX` detail | `GET /v1/movies/MetaX/ABP-123?category=censored` returned merged title, actors, genres, cover, release date, and `sources[]`; JavDB and JavBus succeeded | FANZA failed through a region/age-gate path in this environment |
| `MetaX` image | `GET /v1/images/primary/MetaX/ABP-123?category=censored` returned HTTP 200 JPEG (359×539) | Image identity used the route-first provider |
| FC2 search/detail | `FC2-PPV-3954834` search returned a merged candidate; `MetaX/FC2-3954834` detail merged the FC2 source with summary, genres, cover, and release date | JavDB detail redirected to login, JavBus rejected the FC2 keyword, AVSOX/Airav were not found |
| Amateur route | `200GANA-123` search returned HTTP 200 with `MetaX` first; JavDB results followed | Explicit MGS search returned HTTP 403; MGS is registered, but live access is blocked |
| Hentai route | `GETCHU-12345` search returned HTTP 200 with `MetaX` first, then Getchu | Exact `GETCHU-<digits>` fallback works; fuzzy catalog search remains deferred |
| Actor search | `三上悠亜` returned three results, Gfriends first | Gfriends is live; `WikipediaActor` now adds biography enrichment with deterministic tests, but its live-site path has not been rechecked |
| Error/config behavior | Unknown category returned HTTP 400; `METAX_ROUTES_FINGERPRINT=deadbeef` aborted startup with `fingerprint mismatch` | Startup coverage/fingerprint checks are active |

Provider-specific limits observed on the same date:

- **JavDB:** search worked for censored and FC2 numbers; anonymous FC2 detail redirected to JavDB's login page. The native provider now accepts `MT_MOVIE_PROVIDER_JAVDB__COOKIES` for an authenticated header.
- **FANZA/DMM:** requests were redirected to a region/age-gate page. Search/detail in an allowed region remains unverified, and the FANZA TV/DMM TV category gap remains separate. The digital GraphQL endpoint is reachable for some IDs, so the failure is regional HTML access rather than a proven parser defect.
- **Freejavbt:** returned HTTP 503 with a login page; the native provider now accepts `MT_MOVIE_PROVIDER_FREEJAVBT__COOKIES`. **FC2PPVDB:** returned Cloudflare `error code: 1005` (IP-level block). **FALENO:** home page returned 200 but search/work paths returned 403 from XSERVER WAF. These are availability/anti-bot results, not parser proofs; TLS-fingerprint probes with Chrome 131/133 and Firefox 135 did not change the FC2PPVDB, MGS, or FALENO work-page results.
- **Kin8:** sample `KIN8-1234` returned 404. Chinese `MD-1234` and Western `KIN8-1234` route searches also returned 404/empty; the samples may be invalid catalog entries.
- **MGStage/MGS:** registered and present in the amateur route, but live requests returned 403.
- **ThePornDB:** token-gated; disabled without a configured token. Patch `0016` makes `ACCESS_TOKEN`/`api_token` reach the actor and scene/movie providers, but no live token-backed request was made in this pass. Startup coverage logs it as unavailable in a default build.
- **Official:** intentionally deferred; startup coverage logs it as unavailable in the censored route.
- **Prestige/R18Dev:** remain deferred for the reasons recorded above.

The route table now aliases Amane names that differ from SDK package names: `DMM` → `FANZA`, `MGStage` → `MGS`, and `ThePornDB` → `ThePornDBMovie`/`ThePornDBScene`. Startup fails only when a category has no available provider; individual gaps are logged because `Official` and token-gated `ThePornDB` are intentional.

## Provider access follow-up (2026-10-20)

Amane's per-site `cookie` and `proxy` settings are the only general-purpose
workarounds for the live failures above; its TLS impersonation, retry, and
browser fallback do not bypass IP bans, regional redirects, or login-only
pages. The Go runtime now exposes equivalent configuration:

- Native JavDB and Freejavbt accept `COOKIES`/`COOKIE` through `SetConfig`.
- SDK scraper providers receive the same settings through
  `patches/metatube-sdk-go/0015-scraper-provider-cookies.patch`.
- The planned Jellyfin-plugin cookie dictionary is applied per request through
  `X-MetaX-Provider-Cookies`; the server creates request-local provider
  instances and does not persist or replay the values.
- Existing per-provider `PROXY` and `TIMEOUT` settings remain unchanged.

See [Provider access, cookies, and proxy configuration](PROVIDER_ACCESS.md) for
the failure-by-failure matrix and environment examples. This is configuration
parity, not evidence that a blocked source now succeeds from this network.

## Request resilience and observability (2026-10-21)

Big Task 7 adds bounded retry behavior and a non-secret operational status
surface without changing the MetaTube/Jellyfin provider contract.

- **Shared retry transport.** `provider/httpretry` wraps native provider
  `net/http` transports. It retries only idempotent `GET`/`HEAD`/`OPTIONS`
  requests whose bodies can be replayed, and only for HTTP 408, 429, 503, 504
  or transient network errors. The policy is capped at three attempts, uses
  200 ms exponential base delay with a 2 s cap and ±20% jitter, and honors
  numeric or HTTP-date `Retry-After` within the same cap. Permanent 4xx
  responses and unsafe methods are returned immediately.
- **Per-provider isolation.** Each provider transport has its own retry policy
  and backoff state. There is no global retry mutex, token bucket, or
  cross-provider queue; one provider waiting for `Retry-After` does not block
  another provider. The existing `METAX_AGGREGATE_FANOUT` bound remains
  request-scoped. This is a deliberate limit definition rather than a global
  rate limiter, because provider quotas are not known and serializing unrelated
  providers would regress the fan-out contract.
- **Documented gaps.** JavLibrary's actual fetch path uses a separate
  browser-fingerprint `fhttp` client with 403/406 profile rotation and a
  one-second pause; the shared transport is not applied there. AVSOX's search
  POST is not retried because it is not an idempotent HTTP method. Upstream
  scraper providers use their own transport and are not wrapped by this
  package. These are explicit limits, not evidence that those paths are
  resilient.
- **Status endpoint.** `GET /v1/metax/status` returns registered movie/actor
  provider counts, route category count and fingerprint, route provider gaps,
  and cookie-transport version `1`. It does not return cookies, tokens, proxy
  credentials, provider configuration values, or URLs. The endpoint is in the
  existing unauthenticated `/v1` system group, like `/v1/providers`.
- **Log redaction.** The upstream `engine/init.go` proxy log now removes URL
  userinfo and query data before logging, and `SetProxy` failures no longer
  echo the raw proxy value. Malformed scraper cookie settings no longer include
  the configured cookie text in the returned error. Deterministic tests cover
  credential/query redaction, invalid proxy values, and cookie-value
  non-disclosure.
- **Cancellation decision.** Gin request cancellation is not propagated into
  provider calls because the provider interfaces do not accept
  `context.Context`. Adding a setter would require request-scoped provider
  cloning and an interface change; it was evaluated and deferred. Per-client
  timeouts remain the outer bound, and the retry transport stops when its own
  request context is canceled.
- **Verification.** Local metaX `go test ./... -count=1` and formatting pass.
  On a clean pinned SDK checkout, the focused `engine`, `route`,
  `provider/httpretry`, and all thirteen native provider tests pass, and
  `go build ./cmd/server` succeeds. The status endpoint test is included in the
  same focused run. Full pinned-SDK live regression remains subject to the
  regional and anti-bot limits recorded above and is not claimed as a pass.
  CI on commit `2f1805d` passed the SDK integration/build job in
  [run 39534885960](https://github.com/strange-dragon/metaX/actions/runs/39534885960),
  Go tests in [39534885829](https://github.com/strange-dragon/metaX/actions/runs/39534885829),
  and formatting in [39534885871](https://github.com/strange-dragon/metaX/actions/runs/39534885871).
  The live FC2 and MOON-023 smoke jobs also passed; they do not exercise retry
  behavior or blocked sources. Follow-up log-redaction commit `1060ccc` passed
  SDK integration/build in [39536369716](https://github.com/strange-dragon/metaX/actions/runs/39536369716),
  Go tests in [39536369702](https://github.com/strange-dragon/metaX/actions/runs/39536369702),
  and formatting in [39536369734](https://github.com/strange-dragon/metaX/actions/runs/39536369734).
