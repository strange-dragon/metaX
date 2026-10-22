# Historical upstream patches

These patches were originally applied to a pinned checkout of
`metatube-community/metatube-sdk-go` at build time. They are **not** applied by
the current build.

The same changes are already merged into the source tree in this repository,
which is now a standalone server. The files are retained for provenance and to
make the upstream-derived changes reviewable.

Build the server with:

```bash
bash scripts/build-server.sh ./dist/metax-server
```

See [`docs/INTEGRATION.md`](../docs/INTEGRATION.md) for the current architecture.
