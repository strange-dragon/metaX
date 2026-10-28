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
export MT_MOVIE_PROVIDER_JAVLIBRARY__COOKIES='cf_clearance=...; other_cookie=...'

# Optional: use a current JavLibrary language-site origin, not the javlib.com address portal
export MT_MOVIE_PROVIDER_JAVLIBRARY__BASE_URL='https://www.f101w.com'

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
- `JavLibrary` (native Go provider): accepts `BASE_URL` to select an HTTPS site
  origin and `COOKIES`/`COOKIE` for its browser-fingerprint requests. The
  `javlib.com` domain is an address portal and should not be configured as the
  scraping origin. Browser verification on 2026-10-27 confirmed that the
  `www.f101w.com` search and current slug detail routes are accessible after
  the browser's JavaScript challenge completes. Go requests still receive the
  Cloudflare `Just a moment...` challenge, even with the newest Chrome 146 TLS
  profile available in pinned tls-client v1.14.0. Ordinary headers alone did
  not remove it; the provider now falls back to an in-container Chromium
  instance to execute the challenge and retain its clearance cookies. The
  Docker runtime image includes Chromium; `METAX_CHROMIUM_BIN` can override its
  path. This fallback compiles and is covered by the provider's ordinary parser
  tests, but live headless-Chromium challenge success still needs deployment
  verification. `COOKIES`/`COOKIE` remains an optional fallback for restricted
  environments.
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
| Official | Not planned under the category fan-out model | Amane's manufacturer router | No `official_routes` or manufacturer priority; add a manufacturer only as a normal provider if needed. |
| Prestige | CloudFront 403 from `SIN2-P9`; region-not-available page | Prestige JSON API; Amane uses browser TLS but documents the same regional limit | Native `Prestige` provider is implemented and deterministic-tested against synthetic schema fixtures. Live support is not claimed; use a permitted-region proxy and re-run the SKU/product flow before treating it as verified. |
| R18Dev | Out of scope (2026-10-21) | Offline PostgreSQL mirror | r18.dev publishes a PostgreSQL dump, not a per-title HTTP API; metaX will not add an optional PostgreSQL/import contract for a source absent from the default routes. |
| Getchu fuzzy search | `www.getchu.com/php/nsearch.phtml` returned HTTP 403 with `Content-Length: 0` and `Vary: User-Agent` | Amane sends age cookies and uses the first `soft.phtml` result | Patch `0017` adds exact numeric/title identity matching over the existing `dl.getchu.com` detail provider. Deterministic synthetic tests pass; live catalog HTML remains unverified. |
| Wikipedia/Wikidata actor biography | Implemented as `WikipediaActor` | Wikidata entity filtering + Wikipedia extraction | Exact label/alias identity, AV occupation/description filtering, and deterministic API fixtures are covered locally; live-site verification remains pending. |

### Chromium fallback applicability (2026-10-27)

A browser-session spot check distinguishes JavLibrary's executable JavaScript
challenge from other observed blocks. The browser could complete JavLibrary's
challenge and load its search/detail pages; the Go TLS client received the
Cloudflare challenge HTML. By contrast, Chromium itself received immediate
HTTP 403 responses for FC2PPVDB (`Cloudflare Access denied`), MGStage
(`ERROR: The request could not be satisfied`), FALENO search/detail paths, the
Getchu legacy search (blank 403), and Prestige. Those responses indicate an IP,
region, or hosting-provider rule rather than a browser-rendering requirement;
adding Chromium is not expected to fix them. This is a point-in-time check from
the connected browser egress, not a guarantee for Koyeb's region. No other
provider currently has evidence that it requires Chromium specifically.

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

## Shared anti-bot fetch layer (`internal/antibot`)

Providers whose live sites reject plain Go HTTP clients (Jav321 today, JavLibrary
partially) go through one shared fetcher:

1. Browser TLS fingerprints via `tls-client` (Chrome 146 first, then older
   Chrome/Firefox), rotating on 403/challenge responses.
2. A **headless** Chromium fallback for JavaScript challenges.

Browser policy, because scraping must never disturb the operator's desktop:

- Chromium always starts headless (`--headless=new`). No window is created, no
  focus is stolen, and the run works the same on a laptop or a headless server.
- Every launch uses a throwaway `--user-data-dir` created by chromedp under the
  system temp directory, so the local Chrome profile, cookies and logins are
  never read or written.
- The browser process is stopped on `Close()`; long-lived providers must
  implement `io.Closer`, otherwise a headless Chrome can outlive the request.
- `METAX_CHROMIUM_HEADED=1` is the only way to get a visible window, for
  debugging a challenge by hand. On Linux without `DISPLAY` that path starts
  Xvfb so the window still stays off a real screen.
- `METAX_ANTIBOT_DEBUG=1` prints which strategy answered a request.

### How a Cloudflare challenge is cleared headlessly

The working recipe, verified against f101w.com (JavLibrary), JavDB and JavDB's
search pages on 2026-10-28:

1. Real Chrome, `--headless=new`, GPU left enabled.
2. The UA is passed as a **launch flag** (`--user-agent=`), never through CDP.
   `Emulation.setUserAgentOverride` is detectable: under it the challenge reloads
   every ~7 seconds and never clears, while the same browser with the launch flag
   clears it in 5-8 seconds. The UA version must match the binary because Chrome
   derives `Sec-CH-UA` from its build.
3. `--lang=ja-JP` (override with `METAX_BROWSER_LANG`), so `navigator.language`,
   `Accept-Language` and the page content agree.
4. No in-page JavaScript patches. They are unnecessary once 1-3 hold, and a
   patched `navigator.plugins` is itself a fingerprint.
5. The challenge reloads the document while validating, so the wait loop must
   re-evaluate from scratch; `chromedp.PollFunction` aborts with
   "Inspected target navigated or closed (-32000)" on the first reload.

### Hosts that need a proxy, not a parser

These answer a real browser with a firewall or geo block. No amount of browser
automation helps; they need a residential/Japanese egress.

| Site | Answer | Why |
| --- | --- | --- |
| prestige-av.com | 403 "not available in your region" | Hard geo block |
| faleno.jp | 403 on `/top/works/*` and on any REST call | Host WAF; the homepage alone loads |
| fc2ppvdb.com | 403 / "Access denied" | Cloudflare firewall rule (error 1020) |
| freejavbt.com | 503 "Web Page Blocked" | Upstream network block |
| fc2club.top | TLS reset | Upstream network block |
| iqqr5.org | CF `urlblock` | Cloudflare firewall rule |

`DAHLIA` and `FALENO` additionally expose a WordPress REST API. The pretty
`/wp-json/wp/v2/works` path is blocked by the host WAF, but the equivalent
`?rest_route=/wp/v2/works&slug=...` query form is served normally — that is what
`dahlia/core` uses now.

### Live status (2026-10-28, macOS, this network, no proxy)

`go run ./cmd/metax-livecheck -timeout 50s`: **13 ok, 18 failed, 12 skipped**.
Skipped means the harness has no sample number or the sample uses a format the
provider never accepts, so it says nothing about the provider. Of the failures
most are the proxy-only hosts above, plus wrong sample numbers on the amateur
labels (C0930/H0930/H4610/Gcolle/Pcolaco/PACOPACOMAMA/MURAMURA return 404 for
the shared sample).

## Container and cloud deployment (Koyeb, Cloud Run, Kubernetes)

Two things break a containerised deployment that work fine on a laptop.

**Chromium cannot use its sandbox.** The default seccomp profile denies
unprivileged user namespaces and `/dev/shm` is capped at 64 MiB, so the fallback
browser dies with `No usable sandbox!` or a SIGBUS renderer. `internal/antibot`
detects a container (`/.dockerenv`, `/proc/1/cgroup`, `KUBERNETES_SERVICE_HOST`,
`KOYEB_APP_NAME`) and adds `--no-sandbox --disable-dev-shm-usage --disable-gpu`.
The shipped `Dockerfile` also sets `METAX_CHROMIUM_NO_SANDBOX=1` because Koyeb
runs on microVMs where none of those markers are reliable. Dropping the sandbox
is acceptable: the container is the isolation boundary.

**Resources.** A 512 MB instance is the practical floor for one Chromium plus the
server; 256 MB will be OOM-killed mid-challenge. Expect 200-400 MB RSS right
after a challenge clears.

### Which cloud region fixes what

Changing region only helps the hard geo blocks, and only if the region matches
the site's allowed country. It does **not** help Cloudflare, which scores the
ASN rather than the country, and datacenter ranges score worse than residential
ones. Verified egress pool for Koyeb `was` (Washington D.C.) on 2026-10-28:

```
dig +short outbound-ips-region-was.infra.prod.koyeb.com   # 17 addresses
135.148.55.84    AS16276  OVH SAS        Reston, VA
15.204.161.117   AS16276  OVH SAS        Reston, VA
64.34.81.129     AS396356 Latitude.sh    Ashburn, VA
67.213.115.39    AS396356 Latitude.sh    Ashburn, VA
```

Seventeen shared addresses for every customer in that region, all on hosting
ASNs. `fc2ppvdb` (error 1020) and `iqqr5` (`urlblock`) are ASN/IP rules, so this
is exactly the pool they reject; OVH in particular is heavily challenged. The
`socks5`/HTTP proxy is still the only answer for those.

### Environment variables for a container deployment

```
METAX_PROVIDER_PROXY=socks5://user:pass@host:1080   # applied to every provider
METAX_CHROMIUM_NO_SANDBOX=1                          # set by the Dockerfile
METAX_BROWSER_LANG=ja-JP                             # default already
METAX_ANTIBOT_DEBUG=1                                # log which strategy ran
```

`engine.init` applies `METAX_PROVIDER_PROXY` to every provider that implements
`mt.ProxySetter`, unless that provider already has an explicit `proxy` key in its
database config (the explicit value wins). Before this, the variable was only
read by `metax-livecheck`, so setting it on a deployed server silently did
nothing.

Check where the container actually exits from before blaming a parser:

```
metax-livecheck -egress
# egress  : 135.148.55.84 (AS16276 OVH SAS, Reston, VA, US)
# network : cloud/hosting ASN - challenges and 1020 blocks are expected here
```

