# metaX contributor instructions

## Project objective

Integrate Amane's scraping capabilities into MetaTube's native Go Provider ecosystem so the existing MetaTube Jellyfin plugin can consume the resulting metadata without a separate Amane runtime or a custom Jellyfin plugin.

This repository is a migration workspace. Do not turn it into a parallel scraper service or treat deploying Amane beside MetaTube as the target solution.

## Before implementation

1. Read `docs/MIGRATION_PLAN.md` and `docs/COMPATIBILITY_MATRIX.md`.
2. Inspect the exact upstream revisions and licenses of:
   - [Amane](https://github.com/sqzw-x/amane)
   - [metatube-sdk-go](https://github.com/metatube-community/metatube-sdk-go)
   - [MetaTube Jellyfin plugin](https://github.com/metatube-community/jellyfin-plugin-metatube)
3. Confirm the current source tree, Go/Python versions, public APIs, and test commands. Never infer these from an old plan.
4. Record upstream commit SHAs and the date of each compatibility review.
5. Do not assume a file, script, permission, or test exists until verified.

## Implementation principles

- Extend the SDK's existing Provider interfaces and factory/registration conventions; preserve the public MetaTube/Jellyfin plugin contract unless evidence proves an API change is required.
- Compare equivalent behavior on both sides before porting. Prefer the implementation with better correctness, coverage, resilience, maintainability, and runtime cost—not simply the newer or shorter implementation.
- Port behavior, not framework scaffolding. Amane is Python-based and currently declares Python >=3.14; the target SDK is Go. Do not introduce a Python runtime or a separate Amane service merely to reuse scraping code unless a documented blocker justifies revisiting the architecture.
- Reuse SDK HTTP/fetch, parsing, common normalization, error handling, and test utilities when they meet requirements. Improve shared SDK mechanisms only when a demonstrated cross-provider benefit outweighs regression risk.
- Keep site-specific behavior inside its Provider or a well-scoped internal package. Avoid unnecessary global state and cross-provider coupling.
- Preserve meaningful source behavior: ID normalization, URL parsing, empty-result semantics, alternate URL handling, locale, cookies/headers, proxy and timeout support, retries/rate limits, pagination, actor extraction, images, and field-level fallbacks where applicable.
- Do not claim a site works based only on compilation. Prefer deterministic fixture tests for parsing and explicit mocked tests for HTTP/error behavior; live-site tests are optional and must not be the only regression coverage.
- Never fabricate captured site responses or expected metadata. Mark missing fixtures and blocked live checks clearly.
- Do not commit credentials, cookies, tokens, personal media, or other secrets.
- Respect each upstream project's license and attribution requirements. Resolve compatibility before copying substantial code or fixtures.

## Required verification

For each change:
1. Add or update focused tests.
2. Run formatting and the narrow test target.
3. Run the full SDK test suite when the environment allows.
4. Record exact commands and outcomes; distinguish tests not run from tests that failed.
5. Review the diff for API compatibility, accidental dependency growth, duplicated helpers, and unrelated changes.

## Git workflow

- Work on a feature branch, not directly on `main`.
- Make small, reviewable commits with descriptive messages.
- Never force-push or rewrite shared history.
- Do not merge into `main` without explicit user direction.
