# Movie metadata aggregation and source records

## API contract

The existing detail endpoint remains the entry point:

```http
GET /v1/movies/:provider/:id
```

Aggregation is enabled by default. The response keeps the existing `data` envelope and the existing `MovieInfo` fields at the top level of `data`, then adds:

- `category`: the confidently inferred route category, or `unknown` when the number does not match a supported classifier.
- `sources`: one record per route provider, plus the selected provider when it is outside the route.
  - `provider`, `id`: source identity.
  - `status`: `success`, `error`, `not_found`, `identity_mismatch`, or `baseline_only`.
  - `error`: diagnostic text when a source fails or cannot be safely used.
  - `metadata`: that provider's independent, normalized SDK `MovieInfo` record, when available.

Use `?aggregate=false` to request the stock detail response without the added `category` and `sources` fields. The public identity at the top level remains the originally requested provider + ID so existing detail URLs and plugin identity semantics are not rewritten.

## Merge rules

1. The selected provider's detail must succeed before the endpoint can return a result.
2. Cross-provider lookup is enabled only when the catalog number can be classified confidently. Unknown formats return the selected provider's record and an explanatory `baseline_only` source entry instead of guessing a category.
3. Supplemental details are eligible only when their normalized catalog number matches the selected record. Normalization is conservative: case, spaces, hyphens, and underscores are ignored; different digit sequences do not match.
4. Scalar descriptive fields use the first non-empty value in configured route order. Empty supplemental values never erase populated values.
5. Actor, genre, and preview-image lists are stably deduplicated in route order. Image URL fields use the first non-empty value per field.
6. Runtime and release date use the first available value in route order. Scores are not averaged across providers; the selected provider's score is retained when present, with a supplemental score used only if the selected score is empty.
7. A failed source is recorded independently and does not discard successful source records or the aggregate. A candidate whose detail number fails the identity gate is recorded as `identity_mismatch` and is not merged.

## What “source data” means here

The current MetaTube `MovieProvider` contract returns `model.MovieInfo`, after each provider has parsed its website or upstream API. Therefore `sources[].metadata` preserves each provider's complete independent **structured SDK record**, not raw HTML, HTTP headers, response bytes, or the source site's original JSON document. Those wire-level payloads are not available consistently through the current interface and must not be implied by this API.

## Current limits

- Automatic classification currently recognizes the high-confidence formats already implemented by metaX routing (including FC2 number forms and KIN8 formats). It deliberately does not guess a category for ambiguous identifiers.
- Field-level preferences per individual metadata field, source/language variants, Amane's two-phase partial-source protocol, actor aggregation, and persistent raw HTTP payload capture are not implemented by this first pass.
- This is an integration overlay for the pinned MetaTube SDK, not a standalone Amane service. The SDK integration script applies the engine method, detail-route wiring, and deterministic tests.
- Tests and integrated build must be run in a local SDK checkout after applying the overlay; this documentation alone is not verification.
