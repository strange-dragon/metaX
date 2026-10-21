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

This branch also adds bounded retries (408/429/503/504 and transient network
errors, at most three attempts, 2 s cap, `Retry-After` support) for native
provider `net/http` clients. Retries do not change authentication or regional
access: JavLibrary's browser-fingerprint path, AVSOX's POST search, and
upstream scraper providers are not covered by the shared retry transport. A
retry cannot make a permanently blocked site succeed and must not be treated as
a substitute for cookies, a permitted-region proxy, or valid identifiers.

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

# ThePornDB API token (patch 0016; API_TOKEN is accepted as an alias)
export MT_ACTOR_PROVIDER_THEPORNDBACTOR__ACCESS_TOKEN='...'
export MT_MOVIE_PROVIDER_THEPORNDBSCENE__ACCESS_TOKEN='...'
export MT_MOVIE_PROVIDER_THEPORNDBMOVIE__ACCESS_TOKEN='...'
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
- ThePornDB scene/movie/actor providers: patch `0016` replaces the obsolete
  `SetConfig(map[string]string)` signature with `provider.ConfigSetter`, applies
  `ACCESS_TOKEN` (or `API_TOKEN`), and delegates cookie transport settings to
  the shared scraper. API authentication still depends on the token; a cookie
  alone does not enable the provider.
- Providers that override `SetConfig` with an incompatible signature, or that
  do not use the SDK `Scraper`, are not covered by these patches; configure them
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

## Request-scoped cookies from the Jellyfin plugin

The Jellyfin plugin is the only persistent owner of provider cookies. Its
configuration should store a dictionary keyed by provider name, for example:

```json
{
  "JavDB": "session=...; cf_clearance=...",
  "Freejavbt": "login=..."
}
```

On every API request the plugin sends the dictionary in one request header:

```http
X-MetaX-Provider-Cookies: {"JavDB":"session=...; cf_clearance=...","Freejavbt":"login=..."}
```

The header value is a JSON object (`map<string,string>`). The server applies
the matching entry only to that request, then discards it. There is no
server-side cookie persistence and no startup replay; restarting the server
does not lose plugin-managed cookies. Requests without the header continue to
use the environment-variable fallback described above.

Implementation rules:

- Provider names are matched case-insensitively. Empty names and empty values
  are ignored. Unknown provider names are ignored for forward compatibility.
- The documented route aliases are expanded before lookup: `DMM` → `FANZA`,
  `MGStage` → `MGS`, and `ThePornDB` → `ThePornDBMovie`/`ThePornDBScene`. If the
  same request supplies conflicting cookies for an alias and its canonical
  name, the request is rejected with HTTP 400.
- A known provider must implement `provider.ConfigSetter`; otherwise the
  request is rejected with HTTP 400. JavDB, Freejavbt, and scraper providers
  covered by patch `0015` satisfy this.
- The raw header is limited to 16 KiB. Malformed JSON, trailing JSON, non-string
  values, empty provider names, and CR/LF are rejected with HTTP 400.
- Provider instances are recreated per request with the request cookie overlaid
  on the provider's startup configuration. The shared provider instance is not
  mutated, so concurrent requests with different cookie dictionaries cannot
  leak into each other.
- Cookie values are never logged or returned in an error message.
- The same header can be sent to image endpoints when a provider's images also
  require authentication.
- The plugin integration is a separate project task. Until that change exists,
  send the header directly or use the environment-variable fallback.

## Security notes

- Never commit cookies, tokens, or proxy credentials. Pass them through the
  Jellyfin plugin configuration once that path exists, or through the
  deployment environment/secret manager as the current fallback.
- Logs should record that a cookie/proxy was configured, not the secret value.
- Cookies expire; a failed scrape after a previously successful run should be
  re-checked with a fresh value before treating it as a parser regression.
