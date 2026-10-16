# Initial implementation comparison notes

Review date: 2026-10-16. Source revisions are pinned in `COMPATIBILITY_MATRIX.md`. These notes describe observable implementation differences from source inspection; they are not live-site validation and no site is marked migrated.

## JavBus: existing SDK Provider vs Amane crawler

**SDK implementation:** `provider/javbus/javbus.go` implements `MovieProvider`, `MovieSearcher`, and `Fetcher`. It uses the SDK's shared fetcher/scraper, configures a base referer and cookies (`dv=1`, `existmag=all`), normalizes IDs to uppercase, parses IDs from URLs, and has detail/search parsing plus preview and thumbnail handling.

**Amane implementation:** `src/amane/crawlers/sites/javbus.py` uses the shared Amane client and explicitly sets `dv=1` plus a Chinese-preferred Accept-Language header. Search tries number variants, tries a direct detail URL before the search page, and ranks candidates against the requested number. Detail extraction includes actors, cover/poster URL handling, release date, and additional normalized metadata.

**Comparison decision:** do not add a second JavBus Provider before a field-by-field diff. The SDK already has substantial coverage and may be the better foundation for the native Provider contract. Test the behavior gaps explicitly: number variants, candidate ranking, language/locale, detail URL discovery, missing-number handling, poster/thumb semantics, and the resulting model fields. Reuse only independently licensed/compatible material; do not copy Amane implementation code under the current unresolved license gate.

## AVBase: existing SDK Provider vs Amane crawler

**SDK implementation:** `provider/avbase/avbase.go` reads the site's Next.js data API, derives movie metadata from the work response, queries source-specific providers for richer details, supplements empty fields from the work response, and applies deterministic product-source ordering. This is already a multi-source aggregator within the SDK.

**Amane implementation:** `src/amane/crawlers/sites/avbase.py` queries `/works?q=...`, tries normalized query forms, checks work IDs against a normalized key, and builds metadata from the work/cast/product structures. It extracts actor gender conservatively: only the explicit male-performer note is treated as male; other free-text values remain unknown. The source also contains specialized query-form and work-key helpers.

**Comparison decision:** this is not a simple file-for-file port. Compare the two implementations' current endpoint/response assumptions, exact-number matching, variant handling, metadata completeness, actor gender semantics, product-source precedence, and fallback rules. The SDK's cross-provider source enrichment is valuable; Amane's query normalization and conservative actor gender handling are potential behavior requirements to validate. If these requirements are missing from the SDK, implement them in the smallest tested SDK change rather than duplicating a parallel AVBase aggregation stack.

## JavDB: potential new Provider

Amane's `src/amane/crawlers/sites/javdb.py` supports search and detail extraction, including alternate western-style number handling and explicit actor-gender parsing from HTML classes. No `provider/javdb` source file was present in the inspected SDK provider tree.

**Decision:** first confirm the SDK factory/registration and model requirements, then assess current site accessibility and create deterministic fixtures from legitimate captured responses. A new native Go Provider is a candidate only after the license path is resolved and the target's network behavior is testable. If the site relies on anti-bot or browser-only behavior, document that constraint instead of claiming full support.

## MGStage: existing SDK Provider vs Amane crawler

**SDK implementation:** `provider/mgstage/mgstage.go` implements movie metadata and search along with review methods. It uses the SDK scraper and a cookie, parses the site's detail page, and includes thumbnail/image URL helpers.

**Amane implementation:** `src/amane/crawlers/sites/mgstage.py` tries multiple number variants, attempts a direct detail URL first, then falls back to site search and ranks candidates. Detail parsing reads actors, release date, cover image, and number from the URL.

**Comparison decision:** prefer the SDK Provider as the starting point if its interface coverage and parser remain correct. Add regression cases for number variants, direct-URL fallback, candidate matching, cover image selection, and the difference between Amane's metadata schema and `model.MovieInfo`. Reviews should remain part of the SDK implementation if still supported; they should not be lost in a rewrite.

## Limits of this review

- This review inspected repository source, not a local checkout.
- No Go formatter, linter, unit tests, Python tests, or live site requests were executed in this environment.
- Existing SDK tests and Amane fixtures must be inspected before code changes.
- Site availability and anti-bot behavior can change independently of source code.
- The licensing gate in `LICENSING.md` remains open; these notes are architectural comparison only and do not authorize source reuse.
