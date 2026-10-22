# Docker deployment

The metaX repository is a Provider workspace, not a standalone server. The
Docker image therefore uses the same reproducible build path as
`.github/scripts/build-metax-server.sh`:

1. clone the pinned `metatube-community/metatube-sdk-go` revision;
2. apply the maintained SDK patches and native Provider overlay;
3. run the focused metaX integration tests;
4. build the `metax-server` binary;
5. copy that binary into a small Debian runtime image.

The build needs network access to GitHub and the Go module proxy. It is slower
than a normal application image because it builds and validates the integrated
MetaTube server.

The Docker build sets `METAX_SKIP_UPSTREAM_TESTS=1` for the build script. That
keeps image builds deterministic: focused metaX integration tests still run,
while the upstream SDK suite that makes live-site requests is skipped. A normal
source build without that variable still runs the full upstream suite.

## Quick start

```sh
cp .env.example .env
# Edit .env: at minimum set METAX_TOKEN if the backend should be token-protected.
docker compose up -d --build
docker compose logs -f metax
```

The default service listens on port `18080` and persists SQLite data in the
`metax-data` volume at `/data/metax.db`.

Check the non-secret routing status:

```sh
curl http://127.0.0.1:18080/v1/metax/status
```

In Jellyfin, set the MetaX plugin server URL to the address of this container,
for example `http://docker-host:18080` (or an HTTPS reverse-proxy URL). If
`METAX_TOKEN` is set, enter the same token in the plugin. The plugin sends
provider cookies per request; the `MT_*` environment variables are optional
fallback values.

## Authentication and exposure

The server itself does not terminate TLS. Do not expose port `18080` directly to
an untrusted network. Put it behind HTTPS (a reverse proxy or a private
network), especially when the Jellyfin plugin sends provider cookies.

`METAX_TOKEN` is optional. When set, it is passed to the server's `--token`
flag through the `TOKEN` environment variable. The Compose file publishes the
port on all host interfaces by default; restrict it with firewall rules or
change the port mapping to `127.0.0.1:${METAX_PORT:-18080}:18080` when Jellyfin
runs on the same host.

## Data and configuration

- `/data` is the persistent volume. Back it up if SQLite is used.
- The default command is:
  `--bind 0.0.0.0 --port 18080 --dsn /data/metax.db`
- To use PostgreSQL instead, override the command with a PostgreSQL DSN:
  `--dsn 'postgresql://user:password@postgres:5432/metax'`. The server
  auto-migrates SQLite; for PostgreSQL, keep `--db-auto-migrate` enabled or
  run the migration deliberately.
- Route configuration uses `METAX_ROUTES` (inline JSON) or
  `METAX_ROUTES_FILE` (path in the container, normally under `/data`).
  `METAX_ROUTES_FINGERPRINT` optionally pins the expected route hash.
- Provider cookie fallback and ThePornDB token names are documented in
  [`PROVIDER_ACCESS.md`](PROVIDER_ACCESS.md).

If you use a bind mount instead of the named volume, make the host directory
writable by UID/GID `1000` (the image's runtime user):

```sh
mkdir -p ./metax-data
sudo chown -R 1000:1000 ./metax-data
# then mount ./metax-data:/data
```

## Build arguments

The defaults pin the SDK revision used by the source build (compatibility
reviewed on 2026-10-22):

```text
GO_VERSION=1.26.0
SDK_COMMIT=c62584675c593816cc07e7b5b977d14c7eb505a3
SDK_REPOSITORY=https://github.com/metatube-community/metatube-sdk-go.git
```

Override them only when intentionally testing another compatible revision:

```sh
docker build \
  --build-arg SDK_COMMIT=<revision> \
  -t metax-backend:test .
```

The build script rejects a mismatched or dirty SDK checkout, so a changed
`SDK_COMMIT` must still be compatible with the patches in `patches/metatube-sdk-go`.

## Operations

```sh
docker compose ps
docker compose logs -f metax
docker compose restart metax
docker compose down
# Keep the named volume:
docker compose down
# Also delete the database:
docker compose down -v
```

The container runs as a non-root user. The Compose service enables
`no-new-privileges` and uses an init process for signal forwarding.
