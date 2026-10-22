# metaX

metaX is a self-contained, MetaTube-compatible metadata backend for Jellyfin.
It includes the server, engine, routes, and providers in this repository, and
adds category-aware provider routing, cross-provider metadata aggregation,
request-scoped provider credentials, and a suite of native Go providers. The
server code is derived from
[metatube-sdk-go](https://github.com/metatube-community/metatube-sdk-go) under
the Apache-2.0 license; see [NOTICE.md](NOTICE.md).

The companion Jellyfin plugin is
[metaX-jellyfin](https://github.com/strange-dragon/metaX-jellyfin).

```text
Jellyfin
  └─ metaX Jellyfin plugin
       └─ metaX backend (MetaTube-compatible /v1 API)
            └─ metaX engine + native and upstream-derived providers
                 └─ source sites and APIs
```

## Features

- **Native Go providers** — additional movie and actor providers implemented
  against the MetaTube SDK Provider interfaces.
- **Category-aware search** — requests are routed to an ordered provider list
  for censored, uncensored, Chinese-subtitle, amateur, western, and hentai
  content.
- **Aggregated metadata** — a merged `MetaX` candidate combines fields from
  multiple providers while preserving each provider's original result and
  structured detail record in `sources[]`.
- **Request-scoped provider credentials** — the Jellyfin plugin sends provider
  cookies per request. The backend applies them only to that request and does
  not persist or replay them.
- **Operational controls** — API token, configurable route tables, route
  fingerprints, bounded provider fan-out, in-process caching, SQLite or
  PostgreSQL, and Docker deployment.
- **HTTP client hardening** — bounded retries for supported native providers,
  optional per-provider proxies, and strict URL/identity validation.

### Provider coverage

The integrated server contains the pinned MetaTube SDK provider set plus the
following native metaX provider names:

| Type | Native providers |
| --- | --- |
| Movie | Airav, AVSOX, FC2Club, FreeJAVBT, GIGA, IQQTV, Jav321, JavDB, JavLibrary, Prestige, XCity |
| Actor | AVBaseActor, JavDBActor, MinnanoActor, WikipediaActor |

`JavDB` and `JavDBActor` are registered by the same native package.

The SDK also contributes providers such as FANZA, MGS, FC2, Getchu, JavBus, and
ThePornDB. Availability depends on the build, provider credentials, region, and
the source site's current behavior. A compiled provider is not by itself a
claim that every live site is reachable.

### Default category routes

Routes are ordered provider chains. The default table is:

| Category | Default route |
| --- | --- |
| `censored` | FANZA, JavDB, JavBus |
| `uncensored` | JavDB, JavBus, AVSOX, Freejavbt, FC2PPVDB, FC2, Airav |
| `chinese` | IQQTV, JavDB, Airav, Freejavbt |
| `amateur` | MGS, FANZA, JavDB, JavBus |
| `western` | ThePornDB (movie/scene), JavDB, Freejavbt |
| `hentai` | Getchu, FANZA, JavDB |

`fc2` is accepted as an alias for `uncensored`; `animation` is accepted as an
alias for `hentai`. The route table can be replaced with `METAX_ROUTES` or
`METAX_ROUTES_FILE`.

## Requirements

### Runtime

- Jellyfin `12.x` with the
  [metaX Jellyfin plugin](https://github.com/strange-dragon/metaX-jellyfin).
- A reachable metaX server. The plugin and backend should communicate over
  HTTPS, especially when provider cookies are used.
- One of:
  - Docker Engine with Docker Compose v2 (recommended), or
  - a Linux host with systemd for the binary install.

### Building from source

- Go `1.26.0` or newer.
- Git (used to derive the version and commit).
- Network access to the Go module proxy for the first build.

The server source is already in this repository. The build does not clone or
patch an upstream checkout. The Docker image uses the same build script.

## Quick start with Docker

The Docker image is the recommended deployment method. It builds the complete
server from this repository, without cloning or patching an upstream checkout.
The first build downloads Go modules, so it can take a few minutes.

```bash
git clone https://github.com/strange-dragon/metaX.git
cd metaX

cp .env.example .env
# Edit .env and set METAX_TOKEN to a long random value.

docker compose up -d --build
docker compose logs -f metax
```

The default service listens on port `18080` and stores SQLite data in the
`metax-data` volume:

```bash
curl http://127.0.0.1:18080/v1/metax/status
```

The image runs as UID/GID `1000`. If you use a bind mount for `/data`, make the
host directory writable by that UID/GID.

See [docs/DOCKER.md](docs/DOCKER.md) for:

- authentication and network exposure,
- route, cookie, proxy, and ThePornDB token configuration,
- PostgreSQL,
- bind-mount permissions,
- build arguments and operations.

## Binary installation with systemd

Use this method when Docker is not available or when the binary should run
directly on the host.

### 1. Build the server

```bash
git clone https://github.com/strange-dragon/metaX.git
cd metaX

bash scripts/build-server.sh ./dist/metax-server
```

The build script derives the version and commit automatically (see
[Versioning](#versioning)), injects them into the binary, and compiles
`./cmd/metax-server`. To override the derived values, set `METAX_VERSION` and
`METAX_COMMIT` before invoking it.

### 2. Install the binary and service account

```bash
sudo install -m 0755 ./dist/metax-server /usr/local/bin/metax-server

sudo useradd --system --user-group \
  --home-dir /var/lib/metax \
  --shell /usr/sbin/nologin \
  metax

sudo install -d -o metax -g metax -m 0750 /var/lib/metax
sudo install -d -o root -g root -m 0750 /etc/metax
```

If the `metax` user already exists, skip the `useradd` command.

### 3. Configure the token

Create `/etc/metax/metax.env`:

```bash
sudo tee /etc/metax/metax.env >/dev/null <<'EOF'
TOKEN=replace-with-a-long-random-token
# Optional route override:
# METAX_ROUTES={"routes":{"censored":["FANZA","JavDB"],"uncensored":["JavDB","FC2","Airav"]}}
EOF

sudo chmod 0600 /etc/metax/metax.env
```

The server reads `TOKEN` from the environment and uses it as the bearer token
for private API routes.

### 4. Create the systemd unit

Create `/etc/systemd/system/metax.service`:

```ini
[Unit]
Description=metaX metadata backend
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=metax
Group=metax
WorkingDirectory=/var/lib/metax
EnvironmentFile=-/etc/metax/metax.env
ExecStart=/usr/local/bin/metax-server \
  --bind 127.0.0.1 \
  --port 18080 \
  --dsn /var/lib/metax/metatube.db
Restart=on-failure
RestartSec=5s
StateDirectory=metax
StateDirectoryMode=0750

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/metax

[Install]
WantedBy=multi-user.target
```

Start and verify:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now metax
sudo systemctl status metax

curl http://127.0.0.1:18080/v1/metax/status
```

The example binds to loopback only. Put an HTTPS reverse proxy in front of it
for Jellyfin access. If Jellyfin runs directly on the same host, loopback is
still sufficient; if it runs on another host, terminate TLS at the proxy or
bind to the appropriate private interface and restrict access with a firewall.
If Jellyfin itself runs in a container on the same host, loopback is not
reachable from that container; use a private interface or the host gateway and
restrict access accordingly.

## Jellyfin configuration

1. Install and enable the
   [metaX Jellyfin plugin](https://github.com/strange-dragon/metaX-jellyfin).
2. Open **Dashboard → Plugins → MetaX**.
3. Set **Server** to the backend URL, for example:

   ```text
   https://metax.example.com
   ```

   For a local Docker deployment, use `http://127.0.0.1:18080` from the host or
   `http://metax:18080` from another container on the same Docker network.
4. Set **Token** to the same `TOKEN` value configured on the backend.
5. Configure provider cookies if a provider requires a login or anti-bot cookie.
   Leave a provider blank to disable its cookie entry.
6. Map each Jellyfin library to a metaX category.
7. Enable MetaX in the library's metadata downloaders and image fetchers, then
   run a library refresh.

Provider cookies remain in the plugin. The backend applies the
`X-MetaX-Provider-Cookies` header only to the request that carries it.

## Configuration reference

The most useful server settings are:

| Setting | Purpose |
| --- | --- |
| `TOKEN` / `--token` | Bearer token for private API routes |
| `METAX_ROUTES` | Inline JSON route table; takes precedence over the file |
| `METAX_ROUTES_FILE` | Path to a JSON route table |
| `METAX_ROUTES_FINGERPRINT` | Optional pinned route fingerprint for configuration drift detection |
| `METAX_AGGREGATE_CACHE_TTL` | Aggregated detail/image cache TTL; default `10m` |
| `METAX_AGGREGATE_CACHE_MAX` | Maximum cache entries per cache; default `256` |
| `METAX_AGGREGATE_FANOUT` | Concurrent provider calls; default `16` |
| `--request-timeout` | Per-provider HTTP deadline; default `1m` |
| `--dsn` | SQLite path or PostgreSQL DSN |

SQLite is the default database and auto-migrates on startup. For PostgreSQL,
use a `postgresql://` DSN and enable `--db-auto-migrate` when the schema should
be created automatically.

Provider credentials use the MetaTube environment convention:

```bash
export MT_MOVIE_PROVIDER_JAVDB__COOKIES='session=...; cf_clearance=...'
export MT_MOVIE_PROVIDER_FREEJAVBT__COOKIES='session=...'
export MT_MOVIE_PROVIDER_FALENO__PROXY='http://proxy.example:18081'
export MT_MOVIE_PROVIDER_MGSTAGE__PROXY='socks5://proxy.example:1080'
export MT_MOVIE_PROVIDER_THEPORNDBSCENE__ACCESS_TOKEN='...'
```

These environment values are optional fallbacks. The Jellyfin plugin is the
normal owner of provider cookies. See
[docs/PROVIDER_ACCESS.md](docs/PROVIDER_ACCESS.md) for the full list and the
live-access limitations.

## API overview

| Endpoint | Authentication | Description |
| --- | --- | --- |
| `GET /v1/metax/status` | Public | Route fingerprint, category routes, provider gaps, cookie transport version |
| `GET /v1/providers` | Public | Registered movie and actor providers |
| `GET /v1/movies/search?q=ABC-123&category=censored` | Bearer | Category-aware movie search |
| `GET /v1/movies/{provider}/{id}` | Bearer | Stock provider detail |
| `GET /v1/movies/{provider}/{id}?aggregate=true&category=censored` | Bearer | Aggregated detail for a known provider identity |
| `GET /v1/movies/MetaX/{number}?category=censored` | Bearer | Resolve a catalog number through the category route |
| `GET /v1/actors/search?q=...` | Bearer | Actor search |
| `GET /v1/images/{primary,thumb,backdrop}/{provider}/{id}` | Public | Provider or aggregated images |

`MetaX` is a reserved provider name, not a registered provider package. It is
used by the merged search candidate and the number-based detail/image path.
See [docs/METADATA_AGGREGATION.md](docs/METADATA_AGGREGATION.md) for the merge
rules, source-record schema, error semantics, and caching behavior.

## Security

- Terminate TLS before exposing the backend outside a trusted network.
- Set a strong `TOKEN`; without one, private metadata routes are unauthenticated.
- Treat provider cookies and ThePornDB tokens as credentials. Do not commit them
  to the repository or put them in URLs.
- The backend does not persist provider cookies received in
  `X-MetaX-Provider-Cookies`.
- The Jellyfin plugin stores provider cookies in its own XML configuration in
  plaintext. Protect the Jellyfin configuration directory and use HTTPS.
- Restrict `METAX_ROUTES` changes to trusted operators; malformed route tables
  abort startup rather than silently broadening provider selection.

## Development and verification

Repository tests:

```bash
go test ./...
```

Server build:

```bash
bash scripts/build-server.sh ./dist/metax-server
```

The deterministic routing, engine, route, and native-provider suites are safe
to run in CI. Some provider tests make live-site requests and can fail because
of region blocks, login walls, or anti-bot responses unrelated to the server
code.

## Versioning

`scripts/version.sh` resolves the build version in this order:

1. `METAX_VERSION` / `METAX_COMMIT` environment variables (release builds).
2. Local Git tags and `HEAD`.
3. The repository `VERSION` file plus the current commit of the deployed ref
   resolved from GitHub (Docker builds without `.git`).

The binary reports `v<version>-<commit>` when a commit is known. Tag a release
with a semantic version such as `v0.2.0` to make the version advance
automatically; `VERSION` is the fallback for environments without Git.

## Documentation

- [Docker deployment](docs/DOCKER.md)
- [Migration plan](docs/MIGRATION_PLAN.md)
- [Compatibility matrix](docs/COMPATIBILITY_MATRIX.md)
- [Metadata aggregation contract](docs/METADATA_AGGREGATION.md)
- [Provider access, cookies, and proxies](docs/PROVIDER_ACCESS.md)
- [Licensing and attribution](docs/LICENSING.md)
- [Contributor instructions](AGENTS.md)

## Status and limitations

The deterministic integration suite and reproducible server build pass. Live
site coverage is inherently dependent on region, account state, cookies,
proxies, and upstream changes. In particular:

- FANZA may redirect to a region/age-gate page.
- JavDB FC2 detail may require a login cookie.
- Freejavbt, FC2PPVDB, MGS, and FALENO may return login pages or anti-bot
  responses.
- Prestige and Getchu live catalog behavior has not been fully verified.

These are access constraints, not claims that the parsers are correct for every
site response. See the compatibility matrix and provider access notes before
enabling a provider in production.

## License and attribution

metaX keeps Apache-2.0 for project-authored code and MetaTube-derived code,
subject to the original notices. The project was informed by Amane as a
functional reference; Amane's GPL-3.0 source and fixtures are not relicensed or
copied by attribution alone. See [LICENSE](LICENSE), [NOTICE.md](NOTICE.md),
and [docs/LICENSING.md](docs/LICENSING.md).
