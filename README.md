# metaX

MetaTube-compatible metadata backend for Jellyfin. It provides category-aware
search, metadata aggregation, movie/actor metadata, and images through the
existing MetaTube plugin API.

Companion Jellyfin plugin:
[metaX-jellyfin](https://github.com/strange-dragon/metaX-jellyfin).

[![Deploy to Koyeb](https://www.koyeb.com/static/images/deploy/button.svg)](https://app.koyeb.com/deploy?type=git&builder=dockerfile&repository=github.com/strange-dragon/metaX&branch=main&name=metax&ports=18080;http;/&env[TOKEN]=change-me-before-deploy)

## One-click deploy on Koyeb

1. Click the button above.
2. Replace the placeholder `TOKEN` with a long random value **before**
   clicking Deploy.
3. Keep the service port at `18080`.
4. Deploy.
5. If you need persistent metadata, add a Koyeb volume mounted at `/data`, or
   configure an external PostgreSQL database with `--dsn`.

After deployment, open:

```text
https://<your-service>.koyeb.app/
```

You should see:

```json
{"data":{"app":"metaX","version":"v0.1.0-..."}}
```

The default command uses SQLite at `/data/metax.db`. Without a volume, Koyeb's
ephemeral filesystem loses that database on redeploy.

## Docker Compose

```bash
git clone https://github.com/strange-dragon/metaX.git
cd metaX

cp .env.example .env
# Edit .env and set METAX_TOKEN to a long random value.

docker compose up -d --build
docker compose logs -f metax
```

Check the server:

```bash
curl http://127.0.0.1:18080/
curl http://127.0.0.1:18080/v1/metax/status
```

The default host port is `18080`. Change `METAX_PORT` in `.env` if needed. The
default SQLite database is stored in the `metax-data` volume at
`/data/metax.db`.

## Docker run

Build the image:

```bash
docker build -t metax-backend .
```

Run it:

```bash
docker run -d \
  --name metax \
  --restart unless-stopped \
  -p 18080:18080 \
  -v metax-data:/data \
  -e TOKEN='replace-with-a-long-random-token' \
  metax-backend
```

Check it:

```bash
curl http://127.0.0.1:18080/
curl http://127.0.0.1:18080/v1/metax/status
```

Use `--dsn` to switch to PostgreSQL:

```bash
docker run -d \
  --name metax \
  --restart unless-stopped \
  -p 18080:18080 \
  -e TOKEN='replace-with-a-long-random-token' \
  metax-backend \
  --bind 0.0.0.0 \
  --port 18080 \
  --dsn 'postgresql://user:password@postgres:5432/metax'
```

## Build and run with Go

Requirements: Go `1.26.0` or newer.

Recommended build script. It injects the version and commit automatically:

```bash
bash scripts/build-server.sh ./dist/metax-server
./dist/metax-server --bind 0.0.0.0 --port 18080 --dsn ./metax.db
```

Or build directly with `go build`. This produces an `unknown` version unless
you pass the version with `-ldflags`:

```bash
go build -o ./dist/metax-server ./cmd/metax-server
./dist/metax-server --bind 0.0.0.0 --port 18080 --dsn ./metax.db
```

Check the version:

```bash
./dist/metax-server --version
```

Example with Go's toolchain and SQLite only:

```bash
go run ./cmd/metax-server --bind 127.0.0.1 --port 18080 --dsn ./metax.db
```

## Environment variables

The most commonly used settings:

| Variable or flag | Default | Description |
| --- | --- | --- |
| `TOKEN` / `--token` | empty | Bearer token for protected API routes. Set this on any public deployment. |
| `METAX_ROUTES` | built-in table | Inline JSON route table. Takes precedence over `METAX_ROUTES_FILE`. |
| `METAX_ROUTES_FILE` | empty | Path to a JSON route table. |
| `METAX_ROUTES_FINGERPRINT` | empty | Optional expected route fingerprint; startup fails on mismatch. |
| `METAX_AGGREGATE_CACHE_TTL` | `10m` | Aggregated detail/image cache TTL. |
| `METAX_AGGREGATE_CACHE_MAX` | `256` | Maximum cache entries per cache. |
| `METAX_AGGREGATE_FANOUT` | `16` | Concurrent provider requests during aggregation. |
| `--bind` | empty | Bind address. Docker uses `0.0.0.0`. |
| `--port` | `8080` | Listen port. Docker/Koyeb images use `18080`. |
| `--dsn` | empty | SQLite path or PostgreSQL DSN. |
| `--request-timeout` | `1m` | Per-provider HTTP timeout. |
| `--db-auto-migrate` | false | Enable database migration. SQLite is auto-migrated automatically. |
| `METAX_PORT` | `18080` | Docker Compose host port only. |

Provider credentials use the MetaTube environment convention. They are optional
fallbacks; the Jellyfin plugin normally sends provider cookies per request.

```bash
# Provider cookies
MT_MOVIE_PROVIDER_JAVDB__COOKIES='session=...; cf_clearance=...'
MT_MOVIE_PROVIDER_FREEJAVBT__COOKIES='session=...'

# Per-provider proxies
MT_MOVIE_PROVIDER_FALENO__PROXY='http://proxy.example:18081'
MT_MOVIE_PROVIDER_MGSTAGE__PROXY='socks5://proxy.example:1080'

# ThePornDB tokens
MT_MOVIE_PROVIDER_THEPORNDBSCENE__ACCESS_TOKEN='...'
MT_ACTOR_PROVIDER_THEPORNDBACTOR__ACCESS_TOKEN='...'
```

See [docs/PROVIDER_ACCESS.md](docs/PROVIDER_ACCESS.md) for the full provider
credential list.

## Jellyfin setup

1. Install the
   [metaX Jellyfin plugin](https://github.com/strange-dragon/metaX-jellyfin).
2. Open **Dashboard → Plugins → MetaX**.
3. Set **Server** to the backend URL:

   ```text
   http://127.0.0.1:18080
   ```

   For Docker, use the service name if both containers share a network:

   ```text
   http://metax:18080
   ```

4. Set **Token** to the same `TOKEN` value configured on the backend.
5. Map each Jellyfin library to a category: `censored`, `uncensored`,
   `chinese`, `amateur`, `western`, or `hentai`.
6. Enable MetaX under the library's metadata downloaders and image fetchers.
7. Run **Scan library** or **Refresh metadata**.

For provider cookies, configure them in the plugin settings. They are sent only
to the configured metaX backend and are not stored by the backend.

## Default category routes

| Category | Default providers, in order |
| --- | --- |
| `censored` | FANZA, JavDB, JavBus |
| `uncensored` | JavDB, JavBus, AVSOX, Freejavbt, FC2PPVDB, FC2, Airav |
| `chinese` | IQQTV, JavDB, Airav, Freejavbt |
| `amateur` | MGS, FANZA, JavDB, JavBus |
| `western` | ThePornDB (movie/scene), JavDB, Freejavbt |
| `hentai` | Getchu, FANZA, JavDB |

`fc2` is an alias for `uncensored`; `animation` is an alias for `hentai`.
Override the table with `METAX_ROUTES` or `METAX_ROUTES_FILE`.

## Native providers

| Type | Providers |
| --- | --- |
| Movie | Airav, AVSOX, FC2Club, FreeJAVBT, GIGA, IQQTV, Jav321, JavDB, JavLibrary, Prestige, XCity |
| Actor | AVBaseActor, JavDBActor, MinnanoActor, WikipediaActor |

The server also includes the upstream-derived providers such as FANZA, MGS,
FC2, Getchu, JavBus, and ThePornDB. Live availability depends on region, login
state, cookies, proxies, and the source site.

## API checks

```bash
# Server identity and version
curl http://127.0.0.1:18080/

# Public status: routes, provider gaps, cookie transport version
curl http://127.0.0.1:18080/v1/metax/status

# Registered providers
curl http://127.0.0.1:18080/v1/providers

# Category search
curl -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:18080/v1/movies/search?q=SSIS-001&category=censored'

# Aggregated detail
curl -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:18080/v1/movies/MetaX/SSIS-001?category=censored'
```

`MetaX` is a reserved provider name used for the merged search candidate and
number-based detail/image lookup. It is not listed in `/v1/providers`.

## Versioning

The Docker image and `scripts/build-server.sh` derive the version automatically:

- `METAX_VERSION` and `METAX_COMMIT` override the detected values.
- Local builds use Git tags and `HEAD`.
- Docker builds without `.git` use `VERSION` and resolve the deployed commit
  from GitHub.

The server reports the result at `GET /` and with `--version`. Create a release
tag to advance the semantic version:

```bash
git tag v0.2.0
git push origin v0.2.0
```

## Security

- Set `TOKEN` on any deployment reachable outside a trusted network.
- Terminate TLS in front of the backend when Jellyfin is remote.
- Treat provider cookies and ThePornDB tokens as credentials.
- Do not expose PostgreSQL or the SQLite data directory publicly.
- Back up `/data` if you use SQLite.

## Common issues

- **FANZA returns an error**: region/age-gate restrictions are common. Use a
  permitted region or proxy.
- **JavDB FC2 detail fails**: the source may require a login cookie.
- **Freejavbt, FC2PPVDB, MGS, or FALENO return login/anti-bot pages**: configure
  cookies or a proxy for that provider.
- **Images work but metadata does not**: check `TOKEN`, the plugin Server URL,
  and the backend logs.

## Documentation

- [Docker deployment details](docs/DOCKER.md)
- [Metadata aggregation](docs/METADATA_AGGREGATION.md)
- [Provider access, cookies, and proxies](docs/PROVIDER_ACCESS.md)
- [Compatibility matrix](docs/COMPATIBILITY_MATRIX.md)
- [Licensing and attribution](docs/LICENSING.md)

## License

metaX is Apache-2.0. Server code is derived from
[metatube-sdk-go](https://github.com/metatube-community/metatube-sdk-go); see
[LICENSE](LICENSE) and [NOTICE.md](NOTICE.md).
