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
- The merged candidate carries the route-first provider's display fields and the catalog number as its ID. Selecting it runs the full aggregation in the detail endpoint, so search stays cheap.
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

`category` takes precedence over provider/number inference. An explicit but unknown category returns HTTP 400 instead of silently broadening the search. FC2 is folded into `uncensored` in responses, while `category=fc2` remains accepted as an input alias.

When aggregation runs, the response keeps the existing `data` envelope and the existing `MovieInfo` fields at the top level, then adds:

- `category`: the resolved route category, or `unknown` when the number does not match a supported classifier.
- `sources`: one record per route provider, plus the selected provider when it is outside the route. The top-level `MovieInfo` is the merged view; each record keeps that provider's original, unmerged metadata.

  - `provider`, `id`: source identity.
  - `status`: `success`, `error`, `not_found`, `identity_mismatch`, or `baseline_only`.
  - `error`: diagnostic text when a source fails or cannot be safely used.
  - `metadata`: that provider's independent, normalized SDK `MovieInfo` record, when available.

## Image contract

```http
GET /v1/images/{primary|thumb|backdrop}/:provider/:id?category=:category&url=:url
```

Images requested with `provider=MetaX` are resolved to the route-first real provider before the stock image path runs. The plugin normally passes the image URL from the selected metadata; the `category` parameter is optional and is inferred from the catalog number when omitted.

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

## Current limits

- Automatic classification follows the Amane-style number families implemented in metaX (censored catalog numbers, known uncensored prefixes and six-digit sequence IDs, FC2, Chinese MD/MKY, amateur prefixes, KIN8/Western studio-date IDs, and Getchu identifiers). For non-catalog identifiers, a small number of source-specific hints are used (ThePornDB, Getchu, FC2 marketplaces). Directory-only overrides and ambiguous identifiers are not guessed; the selected record is returned with a `baseline_only` source status.
- Field-level preferences per individual metadata field, source/language variants, Amane's two-phase partial-source protocol, actor aggregation, and persistent raw HTTP payload capture are not implemented by this first pass.
- This is an integration overlay for the pinned MetaTube SDK, not a standalone Amane service. The SDK integration script applies the engine methods, search/detail/image-route wiring, and deterministic tests.
- Tests and integrated build must be run in a local SDK checkout after applying the overlay; this documentation alone is not verification.

## Verification (2026-10-20)

Run from this repository with Go 1.27.3 and a clean SDK checkout at `c62584675c593816cc07e7b5b977d14c7eb505a3`:

```bash
# 1. Repository unit tests
go test ./... -count=1
# -> all packages pass

# 2. Apply maintained patches to the disposable SDK checkout
#    (same ordered list as .github/scripts/build-metatube-server.sh)

# 3. Provider overlay + routing/aggregation overlay
bash .github/scripts/validate-provider-integration.sh /path/to/sdk
bash .github/scripts/integrate-category-routing.sh /path/to/sdk

# 4. Focused integration tests
cd /path/to/sdk
go get github.com/bogdanfinn/tls-client@v1.14.0
go test ./routing ./engine ./route -count=1
# -> ok routing, ok engine, ok route

go test ./provider/avbaseactor ./provider/airav ./provider/avsox ./provider/fc2club \
  ./provider/freejavbt ./provider/giga ./provider/iqqtv ./provider/jav321 ./provider/javdb \
  ./provider/javlibrary ./provider/minnano ./provider/xcity -count=1
# -> all 12 package test targets pass

go build -o /tmp/metatube-server ./cmd/server
# -> build succeeds
```

The latest run on 2026-10-20 added endpoint coverage for the merged search candidate, default stock detail, `aggregate=true`, the reserved `MetaX` detail path, and rejection of explicit unknown categories. The image route is built and its `MetaX` resolution path is covered by the engine resolver tests; it is not exercised with live image bytes.

Full SDK regression (`go test $(go list ./... | grep -Ev .../translate)`) had two pre-existing environment failures only: `engine/dbengine` requires a Docker daemon, and `imageutil` expects Go 1.26 JPEG behavior while this run used Go 1.27.3. CI sets `GITHUB_ACTIONS=true` to skip live provider tests and pins Go 1.26.x.
