# Movie metadata aggregation and source records

## API contract

The existing detail endpoint remains the entry point:

```http
GET /v1/movies/:provider/:id
```

Aggregation is enabled by default. The response keeps the existing `data` envelope and the existing `MovieInfo` fields at the top level of `data`, then adds:

- `category`: the confidently inferred route category, or `unknown` when the number does not match a supported classifier. FC2 is merged into `uncensored`, so FC2 numbers report `uncensored`.
- `sources`: one record per route provider, plus the selected provider when it is outside the route. The top-level `MovieInfo` is the merged view; each record keeps that provider's original, unmerged metadata.

  - `provider`, `id`: source identity.
  - `status`: `success`, `error`, `not_found`, `identity_mismatch`, or `baseline_only`.
  - `error`: diagnostic text when a source fails or cannot be safely used.
  - `metadata`: that provider's independent, normalized SDK `MovieInfo` record, when available.

Use `?aggregate=false` to request the stock detail response without the added `category` and `sources` fields. The public identity at the top level remains the originally requested provider + ID so existing detail URLs and plugin identity semantics are not rewritten.

## Merge rules

1. The selected provider's detail must succeed before the endpoint can return a result.
2. Cross-provider lookup is enabled only when the catalog number can be classified confidently. Unknown formats return the selected provider's record and an explanatory `baseline_only` source entry instead of guessing a category.
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
- This is an integration overlay for the pinned MetaTube SDK, not a standalone Amane service. The SDK integration script applies the engine method, detail-route wiring, and deterministic tests.
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

Full SDK regression (`go test $(go list ./... | grep -Ev .../translate)`) had two pre-existing environment failures only: `engine/dbengine` requires a Docker daemon, and `imageutil` expects Go 1.26 JPEG behavior while this run used Go 1.27.3. CI sets `GITHUB_ACTIONS=true` to skip live provider tests and pins Go 1.26.x.
