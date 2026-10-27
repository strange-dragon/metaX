# Docker deployment

metaX is a standalone Go server. The Docker image builds only from this
repository:

1. copy the source and download Go modules;
2. derive the version and commit with `scripts/version.sh`;
3. build the `metax-server` binary with `scripts/build-server.sh`;
4. copy that binary into a Debian runtime image with Chromium for JavLibrary's
   on-demand browser challenge fallback.

The build needs network access to the Go module proxy. No upstream SDK checkout
is cloned and no patches are applied at build time. Image builds do not run
live-site provider tests; run the deterministic suites separately before a
release.

## Quick start

```sh
cp .env.example .env
# Optional: edit .env for route overrides, provider cookies, or proxies.
docker compose up -d --build
docker compose logs -f metax
```

The default service listens on port `18080` and persists SQLite data in the
`metax-data` volume at `/data/metax.db`.

Chromium is started only when JavLibrary's lightweight Go request receives a
Cloudflare JavaScript challenge. It is not a separate service, but it adds
image size and temporary memory/CPU use during that fallback. The executable
defaults to `/usr/bin/chromium`; set `METAX_CHROMIUM_BIN` only if your image
installs it elsewhere.

Check the non-secret routing status:

```sh
curl http://127.0.0.1:18080/v1/metax/status
```

In Jellyfin, set the MetaX plugin server URL to the address of this container,
for example `http://docker-host:18080` (or an HTTPS reverse-proxy URL). Leave
the plugin **Token** blank. The plugin sends provider cookies per request; the
`MT_*` environment variables are optional fallback values.

## Authentication and exposure

metaX has no authentication. All `/v1` routes are public by design because the
backend only serves publicly scraped metadata. There is no backend token to
configure.

The server itself does not terminate TLS. Put it behind HTTPS (a reverse proxy
or a private network) when the Jellyfin plugin sends provider cookies. The
Compose file publishes the port on all host interfaces by default; restrict it
with firewall rules or change the port mapping to
`127.0.0.1:${METAX_PORT:-18080}:18080` when Jellyfin runs on the same host.

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

```text
GO_VERSION=1.26.0
METAX_VERSION=          # optional semantic version override
METAX_COMMIT=           # optional commit override
METAX_GIT_REF=main      # deployed ref resolved when .git is unavailable
METAX_GIT_URL=https://github.com/strange-dragon/metaX.git
```

`scripts/version.sh` uses local Git tags and `HEAD` when available. In a Docker
build context without `.git`, it falls back to `VERSION` and resolves the
current commit of `METAX_GIT_REF` from GitHub. Pass explicit values for a
reproducible release build:

```sh
docker build \
  --build-arg METAX_VERSION=0.2.0 \
  --build-arg METAX_COMMIT="$(git rev-parse --short=7 HEAD)" \
  -t metax-backend:0.2.0 .
```

The resulting version is available at `GET /` and through
`metax-server --version`.

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
