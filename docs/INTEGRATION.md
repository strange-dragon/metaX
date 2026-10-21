# MetaTube SDK integration

## Why this step is required

MetaTube discovers Providers through package `init()` registration and blank imports in `engine/register.go`. A standalone dependency on the metaX module does not automatically load `provider/javdb` into an existing MetaTube server.

This repository keeps the new Provider in a testable migration workspace. To enable it in a MetaTube build, bring the Provider into the SDK source tree or apply the equivalent changes in a fork of `metatube-community/metatube-sdk-go`.

## Integration patch

1. Copy `provider/javdb/` and `provider/jav321/` into the SDK's `provider/` directory, retaining their tests.
2. Add the following import in the SDK's `engine/register.go` alongside existing blank imports:

   ```go
   _ "github.com/metatube-community/metatube-sdk-go/provider/javdb"
   _ "github.com/metatube-community/metatube-sdk-go/provider/jav321"
   ```

3. Run `gofmt -w provider/javdb/*.go provider/jav321/*.go engine/register.go`.
4. Run `go test ./provider/javdb ./provider/jav321` and then `go test ./...` in the SDK checkout.
5. Build the MetaTube server and verify `/v1/providers` lists JavDB and Jav321; verify `/v1/metax/status` returns the route fingerprint, provider counts, gaps, and cookie-transport version without secrets; then use the existing Jellyfin MetaTube plugin to search and retrieve a real known title.

## Design constraints

- Do not create another Jellyfin plugin or make the existing plugin call Amane directly.
- Do not import metaX as a runtime extension from inside the SDK while metaX itself depends on the SDK module; merge/transplant the Provider into the SDK source tree instead.
- Keep the SDK model and existing API response format unchanged.
- The current metaX tests validate the Provider against the pinned SDK API; integration against the full SDK and the actual server binary remains a separate required verification step.


## Verified upstream integration path (2026-10-16)

Reviewed `metatube-community/metatube-sdk-go` at commit
[`c62584675c593816cc07e7b5b977d14c7eb505a3`](https://github.com/metatube-community/metatube-sdk-go/commit/c62584675c593816cc07e7b5b977d14c7eb505a3).

- `cmd/server/main.go` imports `engine` and builds the API router with `engine.DefaultEngineName`.
- `engine/register.go` blank-imports the built-in provider packages.
- `engine/init.go` initializes movie and actor providers by iterating the factory registries from package `provider`; `provider/factory.go` implements those registries.
- Therefore, a provider package must be part of the SDK module and imported from `engine/register.go` (or the relevant experimental registration file) to be discovered by the stock server. Adding metaX as a normal dependency does not register it.

**Recommended delivery path:** keep metaX as the migration/test workspace, then transplant the reviewed provider package(s) and registration import(s) into an SDK fork or propose them to the upstream SDK. Do not add a runtime dependency from the SDK back to metaX; that reverses the dependency direction and does not fit the current registration design. The Jellyfin plugin contract does not need to change for provider discovery.


## Upstream contribution constraints (2026-10-16)

Repository reviewed: [metatube-community/metatube-sdk-go](https://github.com/metatube-community/metatube-sdk-go).

- The upstream `LICENSE` is Apache-2.0.
- The root `README.md` documents Go 1.25+, while the current `go.mod` declares `go 1.26.0`; use the module directive as the authoritative toolchain requirement.
- No root `CONTRIBUTING.md` was found in the inspected default branch. This does not prove there are no contribution expectations in issues, PR templates, or maintainer guidance.
- No user-owned SDK fork has been supplied. Do not attempt to push a branch to the upstream repository or open a PR from metaX as if it were a fork.

**Current delivery plan:** prepare a reviewable patch against the pinned SDK source in this repository's migration workspace, including source changes and deterministic tests. Validate that the patch applies cleanly to the pinned upstream revision before treating it as ready. To submit it as a normal GitHub PR, the user will need a fork (or upstream write access); then the same patch can be applied to that fork and CI run there.
