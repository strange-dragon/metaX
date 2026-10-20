# Provider access, cookies, and proxy configuration

This document records why several live Provider checks fail and which controls
are actually available in the MetaTube runtime. It is not a claim that blocked
sites can be scraped from every network.

## Amane comparison

Amane's HTTP stack contains several mechanisms that are easy to notice in a
source comparison but are not interchangeable code that can simply be ported
to the Go SDK:

| Mechanism | What it helps with | What it does not fix |
| --- | --- | --- |
| `curl_cffi` browser TLS impersonation with rotation on 403/406 | Sites that reject the default Go TLS fingerprint | Cloudflare IP bans, regional WAF rules, or login-only pages |
| Retries on 408/429/503/504 with backoff | Transient rate limits and upstream failures | Permanent 403/404/503 responses |
| Cloudflare challenge detection and browser fallback | Interactive JS challenges | A site that returns a hard 403/1005 block before a challenge |
| Per-site proxy and per-site cookies | Regional restrictions, account-only content, anti-bot cookies | Missing accounts, invalid samples, or removed works |
| Region-specific FANZA TV / DMM TV GraphQL endpoints | Some FANZA/DMM catalog categories | FANZA's regional HTML redirect or works absent from the digital catalog |

The practical parity for MetaTube is therefore **configuration** (proxy and
cookies), not a headless browser or a GPL Python runtime. The Go SDK already
supports per-provider proxy and timeout configuration; this branch adds
per-provider Cookie configuration for the native JavDB and Freejavbt providers
and for scraper-based SDK providers.

## Configuration

Provider settings use the MetaTube environment convention:

```bash
# Cookie header value (do not commit real values)
export MT_MOVIE_PROVIDER_JAVDB__COOKIES='session=...; cf_clearance=...'
export MT_MOVIE_PROVIDER_FREEJAVBT__COOKIES='session=...'
export MT_MOVIE_PROVIDER_FALENO__COOKIES='...'
export MT_MOVIE_PROVIDER_MGSTAGE__COOKIES='...'
export MT_MOVIE_PROVIDER_FC2PPVDB__COOKIES='...'

# Per-provider proxy (HTTP(S) or SOCKS5, depending on the provider client)
export MT_MOVIE_PROVIDER_FALENO__PROXY='http://127.0.0.1:8080'
export MT_MOVIE_PROVIDER_MGSTAGE__PROXY='socks5://127.0.0.1:1080'
```

`COOKIE` is accepted as an alias for `COOKIES`. CR/LF characters are rejected
so a malformed environment value cannot inject additional HTTP headers.

Coverage details:

- `JavDB` (native Go provider): sends the configured header on search, detail,
  and fetch requests. This is the supported workaround for the anonymous FC2
  detail page redirecting to the login page.
- `Freejavbt` (native Go provider): sends the configured header on all requests.
  The site currently serves a login page with HTTP 503 to anonymous clients.
- SDK scraper providers such as `FALENO`, `MGSTAGE`, and `FC2PPVDB`: patch
  `0015-scraper-provider-cookies.patch` installs the header for the provider's
  base URL through the shared `Scraper.SetConfig`.
- Providers that override `SetConfig` with their own signature, or that do not
  use the SDK `Scraper`, are not covered by patch `0015`; configure them
  individually if support is added later.

## Failure-by-failure status

These observations were made from the migration environment on 2026-10-20 and
are point-in-time network evidence, not parser tests.

| Source | Observed result | Amane mechanism | MetaTube status |
| --- | --- | --- | --- |
| FANZA/DMM | HTML search redirects to `special.fanza.jp/not-available-in-your-region` | Region-aware HTML + GraphQL; no bypass | Use an allowed-region proxy. The digital GraphQL endpoint itself remains reachable for some IDs (`ssis00123` returned data; `ABP-123` was absent), so this is not a parser failure. |
| JavDB FC2 detail | Anonymous detail redirects to JavDB login | Per-site cookie configuration | `MT_MOVIE_PROVIDER_JAVDB__COOKIES` supported. |
| Freejavbt | HTTP 503 with a login page | Cookie configuration, retries | `MT_MOVIE_PROVIDER_FREEJAVBT__COOKIES` supported; retries do not turn a login page into content. |
| FC2PPVDB | Cloudflare `error code: 1005` | Browser/cookies/proxy; TLS rotation alone does not help | Use a different egress IP and valid cookies. Browser TLS profiles still returned 403 in this environment. |
| MGStage/MGS | CloudFront 403 | Proxy/cookies | Use a proxy with access; browser TLS profiles still returned 403. |
| FALENO | Home page 200, search/work paths 403 from XSERVER WAF | Proxy/cookies | Use a proxy. Browser TLS profiles still returned 403 on work pages. |
| Kin8 | HTTP 404 for the sample | Same miss semantics | The sample may be invalid or removed; verify with a known current catalog entry before changing the provider. |
| Official / Prestige / R18Dev | Not implemented or intentionally deferred | Official router / Prestige API / offline PostgreSQL | Still deferred for the reasons in the compatibility matrix. |
| Getchu fuzzy search | Exact `GETCHU-<digits>` fallback works; fuzzy catalog search deferred | Amane uses the first search result | Do not copy the weaker first-result behavior; add an exact identity check first. |
| Wikipedia/Wikidata actor biography | Implemented as `WikipediaActor` | Wikidata entity filtering + Wikipedia extraction | Exact label/alias identity, AV occupation/description filtering, and deterministic API fixtures are covered locally; live-site verification remains pending. |

## Runtime cookie update API

The backend now exposes an authenticated runtime update endpoint for expiring
cookies:

```bash
curl -X PUT http://127.0.0.1:8080/v1/providers/config \
  -H "Authorization: Bearer $METATUBE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"type":"movie","provider":"JavDB","config":{"cookies":"session=..."}}'
```

- `type` is `movie` or `actor`; `provider` uses the registered provider name.
- Only `cookies` / `cookie` keys are accepted in this task.
- The endpoint requires the existing MetaTube bearer token; without a token the
  server's other private `/v1` routes are equally open, so deploy behind TLS and
  configure a token.
- The provider must implement `provider.ConfigSetter`. This covers JavDB,
  Freejavbt, and scraper-based providers through patch `0015`.
- Updates are applied immediately and are **in memory only**. A restart restores
  the startup environment configuration. Persistence/startup replay is a
  separate follow-up task.
- The API never logs cookie values. It rejects CR/LF and unsupported keys.

The intended user-facing location is still the Jellyfin plugin configuration,
so a cookie can be refreshed without rebuilding the container. The pinned
upstream plugin (`f7c1f336`) currently exposes only the server URL/token and has
no provider cookie fields or pass-through protocol. The plugin change is a
separate project task; until it exists, call this endpoint directly or use the
server-side environment variables as the fallback.

## Security notes

- Never commit cookies, tokens, or proxy credentials. Pass them through the
  Jellyfin plugin configuration once that path exists, or through the
  deployment environment/secret manager as the current fallback.
- Logs should record that a cookie/proxy was configured, not the secret value.
- Cookies expire; a failed scrape after a previously successful run should be
  re-checked with a fresh value before treating it as a parser regression.
