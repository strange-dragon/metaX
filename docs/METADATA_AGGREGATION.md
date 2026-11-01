# Movie metadata aggregation and source records

## Jellyfin interaction model

End users only see the Jellyfin plugin, not provider names. The plugin sends a configured category plus a movie name or catalog number, renders the candidates the backend returns, and then requests detail for the candidate the user selects. metaX therefore exposes both one merged candidate and the original result from each provider.

## Search contract

```http
GET /v1/movies/search?q=:keyword&category=:category
```

When a category is supplied, or a catalog number is confidently classified, the response keeps every provider's original search result and prepends one merged candidate:

```json
[
  {"provider": "MetaX", "id": "FC2-PPV-123456", "number": "FC2-PPV-123456"},
  {"provider": "FC2",    "id": "123456",         "number": "FC2-PPV-123456"},
  {"provider": "JavDB",  "id": "abc123",         "number": "FC2-PPV-123456"}
]
```

- `MetaX` is a reserved provider name. It is not a registered provider package.
- The merged candidate uses the first valid result as its identity (catalog number, homepage), then fills detail fields from the configured detail provider and artwork from the configured image provider, each falling back to route order. Actor names are deduplicated case-insensitively. This is a display merge only: selecting the candidate runs the full detail aggregation, so search stays cheap and does not fetch every provider's detail page.
- An explicit `provider` parameter remains a manual override and does not add the merged candidate.
- The merged candidate is only added when a category route is used. Broad, unclassified search keeps the stock SDK response.
- If the plugin enables provider filtering, its allowlist must include `MetaX`.

## Detail contract

```http
GET /v1/movies/:provider/:id?category=:category&aggregate=:bool
```

The endpoint has three modes:

1. **Real provider, default (`aggregate` omitted or `false`)** — stock MetaTube detail response for that provider. Selecting an original provider candidate returns exactly that provider's unmerged metadata.
2. **Real provider, `aggregate=true`** — merged `MovieInfo` at the top level plus `category` and `sources`. This is the explicit opt-in path for callers that already hold a provider + ID.
3. **Reserved provider `MetaX`** — `id` is treated as a catalog number. The backend resolves the category route, searches each provider, uses the route-first candidate whose detail succeeds as the underlying identity, and returns the merged view plus `category` and `sources`.

For `MetaX`, the response keeps `provider=MetaX` and `id=<catalog number>` so later refreshes continue to resolve through aggregation. The per-provider records below `sources` still expose each provider's original identity and metadata.

`category` takes precedence over provider/number inference. An explicit but unknown category returns HTTP 400 whenever the category participates in routing: category search without an explicit provider, `MetaX` detail/image, and `aggregate=true`. The explicit-provider search override and stock (`aggregate=false`) detail keep their existing behavior and ignore `category` for compatibility. FC2 is folded into `uncensored` in responses, while `category=fc2` remains accepted as an input alias.

When aggregation runs, the response keeps the existing `data` envelope and the existing `MovieInfo` fields at the top level, then adds:

- `category`: the resolved route category, or `unknown` when the number does not match a supported classifier.
- `sources`: one record per route provider, plus the selected provider when it is outside the route. The top-level `MovieInfo` is the merged view; each record keeps that provider's original, unmerged metadata.

  - `provider`, `id`: source identity.
  - `status`: `success`, `error`, `not_found`, `identity_mismatch`, or `baseline_only`.
  - `error`: diagnostic text when a source fails or cannot be safely used.
  - `metadata`: that provider's independent, normalized SDK `MovieInfo` record, when available.

### Error and partial-failure semantics

| Situation | Result |
| --- | --- |
| Invalid or empty keyword, malformed query, unknown category used for routing | 400 |
| Category route exists but no route provider is registered | 400 (`ErrNoAvailableProvider`) |
| `MetaX` detail or category search has no matching candidate | 404 (`ErrInfoNotFound`) |
| All category providers fail or return no valid results | 404, matching stock MetaTube search behavior |
| Some category providers fail or return nothing | 200; successful provider results are kept, failures are logged |
| Aggregation baseline detail fails | baseline error (404 for not found, 500 for incomplete metadata) |
| A non-baseline source detail fails | 200; `sources[]` records `error`, `not_found`, or `identity_mismatch` while successful sources still merge |
| An inferred category cannot be resolved on detail | 200 with a `baseline_only` source record |
| `MetaX` image with an invalid category | 400 |

#### How a provider reports a failure

The distinction that drives those statuses is who says "no" and how:

| Provider outcome | Engine result |
| --- | --- |
| Returns `provider.ErrMovieNotFound` (wrapped or not) | 404 as the baseline, `not_found` as a source |
| Answers a bare 404 status | 404 as the baseline, `not_found` as a source |
| Returns a record that fails `MovieInfo.IsValid()` | 500 `ERR_INCOMPLETE_METADATA` as the baseline, `error` as a source |
| Returns `nil, nil` | 404 as the baseline, `not_found` as a source |
| Times out, answers 403/429/5xx, or cannot parse the page | 500 as the baseline, `error` as a source, message preserved |

A missing record and a broken page are deliberately different answers. The first is a fact about the catalogue and is stable; the second means the scraper no longer understands the markup and someone needs to look. Reporting the second as 404 hides a regression behind a plausible "not in this source", which is how several providers used to answer with an empty 200 or a raw `Not Found` string.

`provider/internal/scraper` supplies the shared machinery so a provider does not have to re-derive it:

- `TrackNotFound` observes status codes and a fingerprint list of "this item is gone" copy (`お探しの商品が見つかりません`, `page not found`, `頁面不存在`, …). `Resolve` then maps the visit error to `ErrMovieNotFound` only when one of those was actually seen — a timeout or an unparsed page keeps its own error.
- `ClonedCollector` relabels a response that looks like markup but arrives as `text/plain`. Proxies and CDNs do this, and colly silently skips every `OnHTML`/`OnXML` callback when the header does not mention html or xml, so such a source used to parse to nothing without an error.
- `LastPathSegment` is the single way to take an identifier out of a detail URL. `path.Base(path.Dir(p))` returns the parent segment (`moviepages`) whenever the URL has neither a trailing slash nor an index file.
- The default collector also negotiates charset and wraps the transport in `httpretry`, so a transient reset does not surface as a metadata failure.

## Image contract

```http
GET /v1/images/{primary|thumb|backdrop}/:provider/:id?category=:category&url=:url
```

Images requested with `provider=MetaX` are resolved to a real provider before the stock image path runs. The resolution prefers the configured image provider and then falls back to route order, so the artwork returned by `primary`/`thumb`/`backdrop` matches the artwork in the merged detail. The candidates are tried in that order and the sharpest result wins, so a provider that has no image for the title, or whose image host refuses the request, does not break the image. The plugin normally passes the image URL from the selected metadata; the `category` parameter is optional and is inferred from the catalog number when omitted.

## Field source preference

Merged fields are filled in two independent passes, because artwork and metadata fail independently:

| Group | Fields | Env override | Default |
| --- | --- | --- | --- |
| Detail | `title`, `summary`, `director`, `actors`, `genres`, `maker`, `label`, `series`, `runtime`, `release_date`, `score`, preview-video URLs | `METAX_MERGE_DETAIL_PROVIDER` | `JavDB` |
| Image | `cover_url`, `big_cover_url`, `thumb_url`, `big_thumb_url`, `preview_images` | `METAX_MERGE_IMAGE_PROVIDER` | `Jav321` |

- Both passes fall back to route order for any field the preferred provider leaves empty or cannot answer at all. A provider that returns `not_found` or `error` simply does not appear, so the preference is never a hard requirement.
- Artwork order is nudged by a size signal before it is applied: a source that reports `big_cover_url` different from `cover_url` (or the same for the thumb) demonstrably has a larger copy, and it moves ahead of sources that only ever report one URL. This is what stops a 356 px thumbnail from becoming the cover of a record another source holds at full size. When no source in the group has a distinct large variant there is no signal, so the preference order is used unchanged. The judgment is made per image group; metadata still follows the detail pass untouched.
- `JavDB` is excluded from artwork unless nothing else has it (`METAX_ARTWORK_LAST_RESORT`, default `JavDB`; comma-separated, `off`/`none` disables). It is the best source for text and the worst for images on the same record: its covers go through a hotlink-protected host that answers a placeholder to many clients, and it publishes a single size, so a merged record could end up carrying a JavDB cover while a sibling source held the full-size original. The demotion runs *after* the size nudge — otherwise a large-variant reorder would pull it straight back to the front, which is exactly how it kept winning. "Demoted is not dropped": if every other source in the chain answers without artwork, JavDB still supplies one. The rule applies to artwork only; `JavDB` keeps its `METAX_MERGE_DETAIL_PROVIDER` default for text.
- Set either variable to `off`, `none`, `route`, `default`, or an empty value to restore pure route-order merging for that group.
- The preference also drives `MetaX` image resolution, so the same provider supplies both the merged `cover_url` and the bytes behind `/v1/images/primary/MetaX/:number`.
- Image bytes are chosen by resolution, not only by preference: the image route walks up to 4 candidates, keeps the largest source image, and stops as soon as one is at least 180 000 pixels (between JavDB's 292×438 covers and the 800×538 artwork DMM and JavBus serve). A preferred provider therefore wins whenever it has artwork worth showing, and a title it does not carry — or artwork it cannot serve — falls through to the sharpest remaining source instead of returning nothing. A last-resort provider rides behind that window as a single extra candidate, resolved from the local catalog only (no search fan-out), so it is reached only after every budgeted candidate answered without artwork.
- The default pair exists because the two sources fail in opposite ways in practice: Jav321's artwork comes from the DMM picture CDN, while JavDB's image host answers 403 to datacenter clients that are not in its allowlist; JavDB in turn has the more complete catalogue metadata. Provider names are matched case-insensitively; unregistered names are ignored.
- `Jav321` fetches artwork from `pics.dmm.co.jp` and `p.dmm.co.jp` in addition to its own host. Detail URLs remain restricted to jav321 itself.
- The preference can only reorder providers the category route already contains, so the default image provider must also be routed. `Jav321` is listed in the `censored` and `uncensored` routes; an `uncensored` route without it silently served JavDB covers for every title. The `amateur` route still omits it, so amateur titles keep route-order artwork.

## FC2 entries

An FC2 number (`FC2-4861886`, `FC2PPV4861886`, `FC2_PPV_4861886`) classifies as `fc2`, which canonicalises to `uncensored`, so it resolves through the `uncensored` chain.

- `FC2` (the publisher catalogue at `adult.contents.fc2.com`) is the source that answers them: it returns the work number, title, maker, release date, cover and sample images from a plain address.
- A bare publisher ID (`5001937`) also resolves. Files taken straight from the storefront are named after the numeric product ID, so the aggregate would compare `5001937` against the `FC2-5001937` every provider returns and match nothing. Seven-digit runs are FC2's shape alone, so the number gains the prefix before routing (`routing.InferCategory` already classifies it as `fc2`). Eight-digit runs are left unclassified because they are usually dates.
- A product the publisher has removed answers **HTTP 200** with `お探しの商品が見つかりませんでした` and no article markup. `provider/fc2` checks for that page and returns `ErrMovieNotFound`, so the caller sees 404 instead of the `ERR_INCOMPLETE_METADATA` (500) it used to get.
- FC2 also rewrites the heading for requesters it reads as scrapers, overwriting characters with asterisks (`おじさんの精*の勢い…素人**`) as well as hiding one span inside it. The span is removed while parsing. The asterisks cannot be: the characters are replaced, not appended. The merge step therefore prefers a sibling provider's unmarked spelling of the same number when one exists, which covers titles `FC2PPVDB` has archived. A title the archive does not carry keeps the marks — the alternative is waiting on an egress FC2 does not treat as a scraper, which no header or cookie changes.
- `FC2PPVDB` is routed. The host it is named after, `fc2ppvdb.com`, answers 526 to everything, but the catalogue lives on at `ppvdatabank.com`, which a plain datacenter IP can read. It archives titles the publisher has since delisted, so it sits directly after `FC2` in the `uncensored` chain — and never before it, because it carries neither genres nor performers and its cover is a single 356px thumbnail.
- JavDB indexes FC2 titles, but its FC2 detail pages redirect anonymous clients to `/login`. The search card still exists, so the failure looked like "JavDB does not have this title" while the catalogue did. `provider/javdb` now detects the login page and reports `ErrLoginRequired`, which still unwraps to `provider.ErrMovieNotFound` (the record cannot be described without credentials, so the status stays 404) but says in the message that a signed-in cookie is required. Supply one through `MT_MOVIE_PROVIDER_JAVDB__COOKIES`, or per request via `X-MetaX-Provider-Cookies`.
- `FC2Club` is gone and `fc2hub` redirects to a host that answers 403, so neither is routed.

## Caching and request coalescing

Aggregated detail responses and `MetaX/<number>` image identity resolution are cached in-process and coalesced with `singleflight`:

- `METAX_AGGREGATE_CACHE_TTL` controls the TTL (default `10m`; `0`, `off`, or `disabled` turns caching off).
- `METAX_AGGREGATE_CACHE_MAX` controls the maximum number of entries per cache (default `256`).
- `METAX_AGGREGATE_FANOUT` bounds concurrent provider calls from category search and detail source fan-out (default `16`; `0`, `off`, `disabled`, or `none` disables the bound). The server's `--request-timeout` (default `1m`) remains the per-provider HTTP deadline; the limiter only bounds concurrency and never abandons a running provider call.
- `METAX_SEARCH_BUDGET` bounds how long a category search waits for its slowest provider (default `10s`). Answers that arrived are returned; providers still running are logged and skipped for that request. `0`, `off`, `disabled`, or `none` restores waiting for every provider. This is what keeps a library refresh from queueing behind one provider whose own timeout is measured in seconds.

Only successful results are cached; failures are returned to the caller and retried on the next request. Cache keys include the route-table fingerprint, category, lazy flag, and provider/number identity. FC2 and uncensored share a cache key because they resolve to the same route. Returned values are cloned so the `MetaX` identity rewrite cannot corrupt the cached entry. The cache is per-process and is cleared on restart; it is not shared across server instances.

For multi-instance deployments, the in-process cache is intentionally not a distributed cache: instances may fetch the same metadata independently, and a restart clears only that instance's entries. Route-table drift is the more important consistency risk, so `METAX_ROUTES_FINGERPRINT` pins the expected `FingerprintHash()` value. Set the same pin on every instance to make a mismatched route table abort startup instead of serving results from a different route configuration. The hash is also logged at startup for comparison.

## DB-first catalog-number resolution

`MetaX/<number>` detail requests with `lazy=true` and image identity resolution consult the local `movie_metadata` table before searching providers:

- The lookup is restricted to providers in the resolved category route, keeps configured route order when several providers have the same number, and applies `routing.SameNumber` in Go after a bounded SQL pre-filter.
- The SQL pre-filter mirrors number folding (case-insensitive, hyphens and spaces removed, underscores preserved) and includes the common FC2 spellings (`FC2-...`, `FC2PPV...`, `FC2_PPV_...`).
- Catalog numbers that carry a site name (`GCOLLE-847256`, `HEYDOUGA-4037-479`, `H0930-ori1643`, …) fold the same way a file name does, so a bare ID matches a provider that stores the prefixed one. The lookup keys list both spellings, because rows written before the prefix was folded out keep it and SQL folding cannot remove it.
- A miss, query error, or incomplete row falls back to the live route search. A stale record at worst yields a stale lazy detail or a broken image until the next non-lazy refresh; it never blocks live fallback for a different request.
- `lazy=false` deliberately skips the DB-first path so a refresh still observes provider changes immediately.

## Route configuration

The server route table is a category -> provider list. It is not Amane's
Official manufacturer table, but its provider-list order is used as the
Amane-style result priority: real-provider candidates are ordered by their
position in the category route, then by match quality within the same provider.
The SDK's numeric `Provider.Priority()` value does not control this ordering.
The merged `MetaX` candidate is always prepended as the first result.

The built-in `routing.DefaultConfig()` routes every movie provider compiled into
the binary, in quality-first order. The experimental provider `AVBASE` is listed
in the table but only resolves in a `-tags experimental` build, so a default
build reports it under `route_provider_gaps`. `MadouQu` used to be experimental
and disabled; it now ships in the default build and is routed under `chinese`,
because that route was the thinnest one and the provider is covered by live
search and detail tests. `7MMTV` is routed under `uncensored` and `chinese`; it
sits behind the DMM-backed sources because its artwork lives on its own CDN, so
it is there for its Chinese titles and for the titles the manufacturer sites do
not index. Override the table with the variables below to narrow
the fan-out or to change the priority order.

Two providers stay registered but are deliberately unrouted, because the sites
themselves are gone: `FC2Club` (`fc2club.com` does not resolve, `fc2club.top`
redirects to a parked host) and `fc2hub` (301 to `javten.com`, which answers 403).
They remain in the binary so an operator can route one again through
`METAX_ROUTES` if it comes back. `IQQTV` and `Freejavbt` were on the unreachable
list and are not any more: `iqq5.xyz` redirects to `iqqab.com`, and
`freejavbt.com` serves parseable cards, both from a plain IP.

### How a site is judged dead

A curl probe from one machine is not evidence, and reading it as evidence has
produced two wrong verdicts in this table (both `IQQTV` and `Freejavbt` were
declared dead while serving traffic, and `AVSOX` was dropped for "answering 200
with an empty shell" when that shell is its Vue SPA by design — the metadata
moved to a JSON API the provider already calls, and which answers with real
records from the same IP).

Judge liveness by two independent signals instead:

1. **A peer project still routes it.** Amane's `_DEFAULT_CONTENT_ROUTES` is the
   reference: it lists `AVSOX`, `Freejavbt` and `FC2PPVDB` as live, and carries
   `FC2Club` only as a registered crawler that no default route uses — the same
   split this table now has. Presence in a maintained project's default route is
   strong evidence of life; absence is weak evidence of death, because a project
   may simply not cover a site, so never drop on absence alone.
2. **The provider's own data path answers.** Not the HTML: the endpoint the
   provider actually calls. `avsox.click/cn/search/...` returns an empty 1.5 kB
   shell while `POST /javu/data/api/search` returns `{"code":200,"data":[...]}`.

A site is only unroutable when its *data* is unreachable **and** no peer routes
it. `FC2Club` fails both; `fc2hub` fails both; `AVSOX` fails neither and is
routed again.

The table is configurable at startup:

- `METAX_ROUTES` contains inline JSON and takes precedence over `METAX_ROUTES_FILE`, which points at a JSON file. With neither set, `routing.DefaultConfig()` is used.
- Both `{"routes":{"censored":["DMM","JavDB"]}}` and the bare `{"censored":["DMM","JavDB"]}` shape are accepted.
- Category keys must be canonical: `censored`, `uncensored`, `chinese`, `amateur`, `western`, or `hentai`. Legacy aliases (`fc2`, `chinese_subtitles`, `animation`) are rejected in configuration so they cannot silently shadow the canonical route; query values still canonicalize through `Category.Canonical()`.
- Unknown categories, empty routes, empty or duplicate provider names, malformed JSON, and extra top-level fields abort server startup with a nonzero exit.
- `METAX_ROUTES_FINGERPRINT` optionally pins the expected `Config.FingerprintHash()` value (SHA-256 of the canonical route table; a `sha256:` prefix is accepted). A mismatch aborts startup before the database is opened. The startup log prints the actual fingerprint.
- Provider availability is resolved against the providers registered in the running binary. Amane route names that differ from SDK package names are bridged explicitly: `DMM` → `FANZA`, `MGStage` → `MGS`, and `ThePornDB` → `ThePornDBMovie`/`ThePornDBScene`. Startup fails if any configured category has zero available providers; individual gaps are logged because some entries are intentionally deferred or token-gated (currently `ThePornDB` is built into the default image but returns no data until a token is configured). `Official` is not part of the category fan-out model.
- A route whose providers are all unavailable returns HTTP 400 (`ErrNoAvailableProvider`), and unregistered entries are skipped.
- The stock search/detail wrappers read `routing.ActiveConfig()`; explicit `...WithConfig` engine methods remain available for tests and embedders. The cache key includes the route fingerprint, so changing the active table never reuses results from a previous table.

## Merge rules

1. For a real-provider aggregate request, the selected provider's detail must succeed before the endpoint can return a result. For `MetaX`, the first route candidate whose detail succeeds becomes the baseline identity; if no provider matches the catalog number, the endpoint returns not found.
2. Cross-provider lookup is enabled when `category` is explicit or the catalog number can be classified confidently. Unknown formats on the real-provider path return the selected provider's record and an explanatory `baseline_only` source entry instead of guessing a category. `MetaX` with an explicit but unknown category returns HTTP 400.
3. Supplemental details are eligible only when their normalized catalog number matches the selected record. Normalization follows Amane's `fold_number`: case, spaces, and hyphens are ignored, underscores remain significant, and leading zeros are preserved. On top of that, the FC2 family is canonicalized because providers spell the same product differently (`FC2-123456`, `FC2PPV123456`, `FC2-PPV-123456`, `FC2_PPV_123456` all match). Bare numeric provider IDs do not match FC2 catalog numbers.

4. Scalar descriptive fields use the first non-empty value in *detail order*: the configured detail provider first, then configured route order. Empty supplemental values never erase populated values.
5. Actor, genre, and preview-image lists are stably deduplicated in the order of their pass. Image URL fields use the first non-empty value per field in *image order*: the configured image provider first, then route order.
6. Runtime and release date use the first available value in detail order. Scores are not averaged across providers; the selected provider's score is retained when present, with a supplemental score used only if the selected score is empty.
7. A failed source is recorded independently and does not discard successful source records or the aggregate. A candidate whose detail number fails the identity gate is recorded as `identity_mismatch` and is not merged.

## What “source data” means here

The current MetaTube `MovieProvider` contract returns `model.MovieInfo`, after each provider has parsed its website or upstream API. Therefore `sources[].metadata` preserves each provider's complete independent **structured SDK record**, not raw HTML, HTTP headers, response bytes, or the source site's original JSON document. Those wire-level payloads are not available consistently through the current interface and must not be implied by this API.

## Live verification (2026-10-20)

A local macOS run with Go 1.27.3, the in-tree server source, and public network access built the server and ran:

```bash
bash .github/scripts/live-metax-api-smoke.sh /tmp/metax-server 18086
# -> OK: censored-search, censored-detail, censored image, fc2-search, fingerprint pin
# -> LIVE_API_SMOKE_OK
```

Point-in-time observations:

- `GET /v1/movies/search?q=ABP-123&category=censored&fallback=false` returned HTTP 200 with `MetaX` first, followed by JavBus and JavDB results. `MetaX` used `ABP-123` as its ID and filled its display fields from the route results.
- `GET /v1/movies/MetaX/ABP-123?category=censored` returned merged metadata with title, actors, genres, cover, release date, and `sources[]`; JavDB and JavBus were `success`, while FANZA failed in this region/age-gate path.
- `GET /v1/images/primary/MetaX/ABP-123?category=censored` returned HTTP 200 `image/jpeg` (359×539).
- `GET /v1/movies/search?q=FC2-PPV-3954834&category=uncensored&fallback=false` returned HTTP 200 with a merged `MetaX` candidate and FC2/JavDB results. `GET /v1/movies/MetaX/FC2-3954834?category=uncensored` merged the FC2 source successfully, including a non-empty summary, genres, cover, and release date.
- Amateur (`200GANA-123`), hentai (`GETCHU-12345`), and actor (`三上悠亜`) searches returned HTTP 200. The actor search returned a Gfriends result first.
- `GET /v1/movies/search?q=ABP-123&category=not-a-category` returned HTTP 400, and an incorrect `METAX_ROUTES_FINGERPRINT` aborted startup with `fingerprint mismatch`.

Live limitations observed on the same date:

- JavDB search works for FC2 numbers, but FC2 detail redirects to the JavDB login page; anonymous detail verification is not possible.
- FANZA requests were redirected to a region/age-gate page from this network. FANZA search/detail behavior remains unverified outside an allowed region.
- Freejavbt returned HTTP 503; FC2PPVDB returned HTTP 403; FALENO returned HTTP 403 from its hosting provider. These are external availability/anti-bot results, not proofs of parser correctness.
- Kin8 sample `KIN8-1234`, Chinese sample `MD-1234`, and Western sample `KIN8-1234` returned 404/empty. The samples may not be valid catalog entries; they do not establish provider failure.
- No Jellyfin runtime or plugin-side scrape was exercised here.

Live results are evidence for the tested numbers only. Deterministic tests remain the regression baseline.

## Current limits

- Automatic classification follows the Amane-style number families implemented in metaX (censored catalog numbers, known uncensored prefixes and six-digit sequence IDs, FC2, Chinese MD/MKY, amateur prefixes, KIN8/Western studio-date IDs, and Getchu identifiers). For non-catalog identifiers, a small number of source-specific hints are used (ThePornDB, Getchu, FC2 marketplaces). Directory-only overrides and ambiguous identifiers are not guessed; the selected record is returned with a `baseline_only` source status.
- Per-field preferences finer than the two groups above (metadata vs artwork), source/language variants, Amane's two-phase partial-source protocol, actor aggregation, and persistent raw HTTP payload capture are not implemented by this first pass.
- The aggregation engine is part of this standalone server, not a separate Amane service.
- The deterministic tests and server build run directly from this repository; this documentation alone is not verification.

## Verification

Run from the repository root with Go 1.26+:

```bash
# 1. Deterministic engine, route, and routing tests
go test ./routing ./engine ./route -count=1
# -> ok routing, ok engine, ok route

# 2. Native provider tests
go test ./provider/avbaseactor ./provider/airav ./provider/avsox ./provider/fc2club \
  ./provider/freejavbt ./provider/giga ./provider/iqqtv ./provider/jav321 ./provider/javdb \
  ./provider/javlibrary ./provider/minnano ./provider/prestige ./provider/wikipediaactor \
  ./provider/xcity -count=1

# 3. Build the self-contained server with automatic versioning
bash scripts/build-server.sh /tmp/metax-server
# -> build succeeds
```

The test suite covers the merged search candidate, default stock detail,
`aggregate=true`, the reserved `MetaX` detail path, and rejection of explicit
unknown categories. The image route is built and its `MetaX` resolution path is
covered by the engine resolver tests; it is not exercised with live image bytes.

A full `go test ./...` includes upstream live-site provider tests and can fail because of region blocks, login walls, or anti-bot responses. `engine/dbengine` also requires a Docker daemon. Use the deterministic suites above for CI, and set `GITHUB_ACTIONS=true` when a run should skip live provider tests.
