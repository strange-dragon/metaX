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
- The merged candidate uses the first valid result's catalog number as its ID, then fills empty title, homepage, thumbnail, cover, score, release date, and actor fields from later results in route order. Actor names are deduplicated case-insensitively. This is a display merge only: selecting the candidate runs the full detail aggregation, so search stays cheap and does not fetch every provider's detail page.
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

## Image contract

```http
GET /v1/images/{primary|thumb|backdrop}/:provider/:id?category=:category&url=:url
```

Images requested with `provider=MetaX` are resolved to the route-first real provider before the stock image path runs. The plugin normally passes the image URL from the selected metadata; the `category` parameter is optional and is inferred from the catalog number when omitted.

## Caching and request coalescing

Aggregated detail responses and `MetaX/<number>` image identity resolution are cached in-process and coalesced with `singleflight`:

- `METAX_AGGREGATE_CACHE_TTL` controls the TTL (default `10m`; `0`, `off`, or `disabled` turns caching off).
- `METAX_AGGREGATE_CACHE_MAX` controls the maximum number of entries per cache (default `256`).
- `METAX_AGGREGATE_FANOUT` bounds concurrent provider calls from category search and detail source fan-out (default `16`; `0`, `off`, `disabled`, or `none` disables the bound). The server's `--request-timeout` (default `1m`) remains the per-provider HTTP deadline; the limiter only bounds concurrency and never abandons a running provider call.

Only successful results are cached; failures are returned to the caller and retried on the next request. Cache keys include the route-table fingerprint, category, lazy flag, and provider/number identity. FC2 and uncensored share a cache key because they resolve to the same route. Returned values are cloned so the `MetaX` identity rewrite cannot corrupt the cached entry. The cache is per-process and is cleared on restart; it is not shared across server instances.

For multi-instance deployments, the in-process cache is intentionally not a distributed cache: instances may fetch the same metadata independently, and a restart clears only that instance's entries. Route-table drift is the more important consistency risk, so `METAX_ROUTES_FINGERPRINT` pins the expected `FingerprintHash()` value. Set the same pin on every instance to make a mismatched route table abort startup instead of serving results from a different route configuration. The hash is also logged at startup for comparison.

## DB-first catalog-number resolution

`MetaX/<number>` detail requests with `lazy=true` and image identity resolution consult the local `movie_metadata` table before searching providers:

- The lookup is restricted to providers in the resolved category route, keeps configured route order when several providers have the same number, and applies `routing.SameNumber` in Go after a bounded SQL pre-filter.
- The SQL pre-filter mirrors number folding (case-insensitive, hyphens and spaces removed, underscores preserved) and includes the common FC2 spellings (`FC2-...`, `FC2PPV...`, `FC2_PPV_...`).
- A miss, query error, or incomplete row falls back to the live route search. A stale record at worst yields a stale lazy detail or a broken image until the next non-lazy refresh; it never blocks live fallback for a different request.
- `lazy=false` deliberately skips the DB-first path so a refresh still observes provider changes immediately.

## Route configuration

The server route table is a category -> provider list. It is not Amane's
Official manufacturer table, but its provider-list order is used as the
Amane-style result priority: real-provider candidates are ordered by their
position in the category route, then by match quality within the same provider.
The SDK's numeric `Provider.Priority()` value does not control this ordering.
The merged `MetaX` candidate is always prepended as the first result.

The table is configurable at startup:

- `METAX_ROUTES` contains inline JSON and takes precedence over `METAX_ROUTES_FILE`, which points at a JSON file. With neither set, `routing.DefaultConfig()` is used.
- Both `{"routes":{"censored":["DMM","JavDB"]}}` and the bare `{"censored":["DMM","JavDB"]}` shape are accepted.
- Category keys must be canonical: `censored`, `uncensored`, `chinese`, `amateur`, `western`, or `hentai`. Legacy aliases (`fc2`, `chinese_subtitles`, `animation`) are rejected in configuration so they cannot silently shadow the canonical route; query values still canonicalize through `Category.Canonical()`.
- Unknown categories, empty routes, empty or duplicate provider names, malformed JSON, and extra top-level fields abort server startup with a nonzero exit.
- `METAX_ROUTES_FINGERPRINT` optionally pins the expected `Config.FingerprintHash()` value (SHA-256 of the canonical route table; a `sha256:` prefix is accepted). A mismatch aborts startup before the database is opened. The startup log prints the actual fingerprint.
- Provider availability is resolved against the providers registered in the running binary. Amane route names that differ from SDK package names are bridged explicitly: `DMM` → `FANZA`, `MGStage` → `MGS`, and `ThePornDB` → `ThePornDBMovie`/`ThePornDBScene`. Startup fails if any configured category has zero available providers; individual gaps are logged because some entries are intentionally deferred (currently token-gated `ThePornDB`). `Official` is not part of the category fan-out model.
- A route whose providers are all unavailable returns HTTP 400 (`ErrNoAvailableProvider`), and unregistered entries are skipped.
- The stock search/detail wrappers read `routing.ActiveConfig()`; explicit `...WithConfig` engine methods remain available for tests and embedders. The cache key includes the route fingerprint, so changing the active table never reuses results from a previous table.

## Merge rules

1. For a real-provider aggregate request, the selected provider's detail must succeed before the endpoint can return a result. For `MetaX`, the first route candidate whose detail succeeds becomes the baseline identity; if no provider matches the catalog number, the endpoint returns not found.
2. Cross-provider lookup is enabled when `category` is explicit or the catalog number can be classified confidently. Unknown formats on the real-provider path return the selected provider's record and an explanatory `baseline_only` source entry instead of guessing a category. `MetaX` with an explicit but unknown category returns HTTP 400.
3. Supplemental details are eligible only when their normalized catalog number matches the selected record. Normalization follows Amane's `fold_number`: case, spaces, and hyphens are ignored, underscores remain significant, and leading zeros are preserved. On top of that, the FC2 family is canonicalized because providers spell the same product differently (`FC2-123456`, `FC2PPV123456`, `FC2-PPV-123456`, `FC2_PPV_123456` all match). Bare numeric provider IDs do not match FC2 catalog numbers.

4. Scalar descriptive fields use the first non-empty value in configured route order. Empty supplemental values never erase populated values.
5. Actor, genre, and preview-image lists are stably deduplicated in route order. Image URL fields use the first non-empty value per field.
6. Runtime and release date use the first available value in route order. Scores are not averaged across providers; the selected provider's score is retained when present, with a supplemental score used only if the selected score is empty.
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
- Field-level preferences per individual metadata field, source/language variants, Amane's two-phase partial-source protocol, actor aggregation, and persistent raw HTTP payload capture are not implemented by this first pass.
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
