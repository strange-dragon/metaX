# Server architecture

metaX is a standalone Go server. The server, engine, routes, and both native
and upstream-derived providers live in this repository.

```text
cmd/metax-server
  └─ cmd.Router(engine.DefaultEngineName)
       ├─ engine/register.go  (blank imports all providers)
       ├─ routing/            (category-aware route tables)
       ├─ route/              (MetaTube-compatible /v1 API)
       └─ provider/           (native and upstream-derived providers)
```

## Provider registration

The engine discovers providers through package `init()` registration. Native
providers are registered alongside the upstream-derived providers by blank
imports in [`engine/register.go`](../engine/register.go). Adding a provider
requires two steps:

1. Add the implementation under `provider/<name>/`.
2. Add a blank import in `engine/register.go`.

There is no build-time provider transplant step and no separate SDK checkout.

## Upstream provenance

The engine, routes, and upstream providers are derived from
[`metatube-community/metatube-sdk-go`](https://github.com/metatube-community/metatube-sdk-go)
at commit `c62584675c593816cc07e7b5b977d14c7eb505a3`, then modified in-tree for
metaX category routing, aggregation, request-scoped credentials, and the
vendored validation fixes. The upstream Apache-2.0 license and attribution are
retained in [`LICENSE`](../LICENSE) and [`NOTICE.md`](../NOTICE.md).

The historical patch series under [`patches/metatube-sdk-go`](../patches/metatube-sdk-go)
is kept for provenance. It is **not** applied during the build; those changes
are already part of the source tree.

## API compatibility

The public `/v1` API and data model remain compatible with the Jellyfin
MetaTube plugin. metaX-specific aggregation and category behavior is exposed
through the existing endpoints and the `/v1/metax/*` routes.
