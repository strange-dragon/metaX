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
| Airav | `sites/airav.py` | Not confirmed | Search/detail, IDs, image URLs, errors |
| AVBase | `sites/avbase.py` | `provider/avbase` | Compare field coverage and multi-source fallback |
| AVSox | `sites/avsox.py` | Not confirmed | Search/detail, site status and parsing |
| Dahlia | `sites/dahlia.py` | `provider/dahlia` | Compare output, headers, ID rules |
| DMM | `sites/dmm.py` | DMM/FANZA-related providers exist; exact mapping to verify | API/site behavior, ID normalization, images |
| Faleno | `sites/faleno.py` | `provider/faleno` | Compare output and error behavior |
| FC2 | `sites/fc2.py` | `provider/fc2` | Compare ID normalization and metadata |
| FC2Club | `sites/fc2club.py` | Not confirmed | Site-specific ID and result semantics |
| FC2PPVDB | `sites/fc2ppvdb.py` | `provider/fc2ppvdb` | Compare source mapping and field coverage |
| Freejavbt | `sites/freejavbt.py` | Not confirmed | Search/detail and anti-bot behavior |
| Getchu | `sites/getchu.py` | `provider/getchu` | Compare source mapping and field coverage |
| Giga | `sites/giga.py` | Not confirmed | Search/detail and ID rules |
| Iqqtv | `sites/iqqtv.py` | Not confirmed | Search/detail, API behavior |
| Jav321 | `sites/jav321.py` | No Provider in SDK tree; metaX now has `provider/jav321` | Deterministic tests cover route, metadata mapping, not-found behavior and registration; live-site and full server integration pending |
| JavBus | `sites/javbus.py` | `provider/javbus` | Compared against pinned SDK source; findings and follow-up decision recorded below |
| JavDB | `sites/javdb.py` | No Provider in SDK tree; metaX now has `provider/javdb` | Deterministic tests cover search, details, number variants, foreign hosts, and registration; live-site and full server integration pending |
| JavLibrary | `sites/javlibrary.py` | Not confirmed | Login/session, parsing, source availability |
| Kin8 | `sites/kin8.py` | `provider/kin8tengoku` likely related; mapping must be verified | Confirm canonical domain, IDs, and output |
| MGStage | `sites/mgstage.py` | `provider/mgstage` | Compare field coverage and request behavior |
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
