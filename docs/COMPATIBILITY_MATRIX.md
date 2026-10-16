# Upstream compatibility and Provider inventory

This matrix is the evidence log for the Amane → MetaTube SDK migration. Update it as code is inspected and sites are ported. A blank field means **not yet verified**, not “not applicable”.

## Upstream snapshots

| Component | Repository | Ref / SHA | Language / toolchain | License review | Status |
|---|---|---|---|---|---|
| Amane source | [sqzw-x/amane](https://github.com/sqzw-x/amane) | Initial tree observed at `a2d45880e83df7047d461452965919692701e8c9`; pin exact commit before copying code | Python; `pyproject.toml` declares Python >=3.14 | Required before porting implementation or fixtures | Inspected at high level |
| MetaTube SDK | [metatube-community/metatube-sdk-go](https://github.com/metatube-community/metatube-sdk-go) | Initial tree observed at `c62584675c593816cc07e7b5b977d14c7eb505a3`; pin exact commit before changes | Go; current `go.mod` declares Go 1.26.0 | Required | Provider interfaces inspected |
| Jellyfin plugin | [metatube-community/jellyfin-plugin-metatube](https://github.com/metatube-community/jellyfin-plugin-metatube) | Initial tree observed at `f7c1f336fc2bd3b35b82af7c6e69da09a196e3d2`; pin exact commit before integration verification | C# / .NET; exact target framework to verify | Required | Repository structure inspected |

The tree SHAs above identify snapshots seen during initial reconnaissance; where a tree SHA differs from a commit SHA, do not treat it as a commit pin. Record the actual commit SHA from the repository's commit endpoint before using it as a reproducible baseline.

## SDK integration points confirmed from source

- `provider.Provider`: name, priority, language, base URL.
- `provider.MovieProvider`: movie ID normalization, URL-to-ID parsing, detail lookup by ID and URL.
- `provider.MovieSearcher`: keyword normalization and movie search.
- `provider.ActorProvider` / `provider.ActorSearcher`: actor detail and search capabilities.
- Optional interfaces include `Fetcher`, `RequestTimeoutSetter`, `ProxySetter`, and `ConfigSetter`.
- `provider.Register` registers factories by name; providers should follow the repository's existing package and registration conventions.
- Representative providers use shared fetcher/scraper utilities, language tags, standard SDK models, and provider-specific tests. Prefer these utilities when they satisfy the behavior requirements.

## Site-level migration matrix

Statuses: `not-started`, `inspecting`, `blocked`, `implemented`, `tested`, `verified`.

| Amane site / capability | Amane source path | Existing SDK equivalent | Target interface/model | Behavior differences / decision | Fixtures/tests | Status |
|---|---|---|---|---|---|---|
| AVBase | `src/amane/crawlers/sites/avbase.py` | `provider/avbase` | Movie search/detail; model comparison required | Compare field coverage, HTTP behavior, ID rules, errors, image handling before choosing implementation | Pending | inspecting |
| Dahlia | `src/amane/crawlers/sites/dahlia.py` | `provider/dahlia` | Movie search/detail; model comparison required | Compare source and existing provider; do not overwrite a better SDK implementation | Pending | inspecting |
| Faleno | `src/amane/crawlers/sites/faleno.py` | `provider/faleno` | Movie search/detail; model comparison required | Compare source and existing provider | Pending | inspecting |
| FC2 | `src/amane/crawlers/sites/fc2.py` | `provider/fc2` | Movie search/detail; model comparison required | Check related FC2 sub-sites and canonical ID behavior | Pending | inspecting |
| GFriends | `src/amane/crawlers/sites/avbase.py` / actor crawler as applicable | `provider/gfriends` | Movie/actor capabilities to verify | Confirm exact Amane site mapping before implementation | Pending | not-started |
| JavBus | `src/amane/crawlers/sites/javbus.py` | `provider/javbus` | Movie search/detail | Compare normalizing, special-ID rejection, redirects/cookies, preview images, and tests | Pending | inspecting |
| JavDB | `src/amane/crawlers/sites/javdb.py` | `provider/javdb` | Movie search/detail; model comparison required | Compare HTML/API handling and anti-bot constraints | Pending | inspecting |
| MGStage | `src/amane/crawlers/sites/mgstage.py` | `provider/mgstage` | Movie search/detail | Compare metadata coverage and source resilience | Pending | inspecting |
| ThePornDB | `src/amane/crawlers/sites/theporndb.py` | `provider/theporndb` | Movie and actor capabilities | Compare API schema, actor metadata, IDs, and missing-field behavior | Pending | inspecting |
| Actor sources | `src/amane/crawlers/actor/sites/` | Several SDK actor providers; inventory required | `ActorProvider` / `ActorSearcher` | Keep actor capability inventory separate from movie sources | Pending | not-started |
| Remaining Amane sites | `src/amane/crawlers/sites/` | Inventory against SDK `provider/` tree | Depends on capability | Enumerate every site; do not assume table is exhaustive | Pending | not-started |

**Note:** the site rows are an initial comparison shortlist based on matching filenames, not a claim that the implementations are equivalent. Correct any path or mapping after reading each site's registry and source.

## Per-site acceptance checklist

- [ ] Site registry, aliases, and supported roles confirmed.
- [ ] Existing SDK provider located and behavior compared.
- [ ] License and fixture reuse reviewed.
- [ ] ID and URL normalization cases covered.
- [ ] Search and detail results mapped to SDK models.
- [ ] Empty/not-found behavior covered.
- [ ] Missing optional fields and malformed responses covered.
- [ ] Headers/cookies, redirect, proxy, timeout, retry and rate-limit behavior reviewed.
- [ ] Actor and image behavior verified where applicable.
- [ ] Focused tests and format checks pass.
- [ ] SDK regression suite run or exact reason for not running recorded.
- [ ] Status updated with test evidence and known limitations.
