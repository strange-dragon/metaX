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
5. Build the MetaTube server and verify `/v1/providers` lists JavDB and Jav321; then use the existing Jellyfin MetaTube plugin to search and retrieve a real known title.

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
