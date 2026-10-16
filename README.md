# metaX

metaX is a migration workspace for integrating Amane's scraping capabilities into MetaTube's native Go Provider ecosystem, so the existing MetaTube Jellyfin plugin can use them through the MetaTube service.

**Target architecture:** Jellyfin → existing MetaTube Jellyfin plugin → MetaTube API → existing and migrated native Go Providers.

Amane is the capability/reference implementation, not a separate service that Jellyfin should call. Migration work compares Amane and existing SDK implementations and preserves whichever behavior is demonstrably more correct, robust, maintainable, and well-tested.

## Project status

Initial repository reconnaissance and migration planning are in progress. No site Provider migration or runtime test has been claimed complete yet. A licensing gate is recorded because Amane is GPL-3.0 while this repository and the SDK are Apache-2.0; do not copy source or fixtures until the reuse strategy is resolved.

## Engineering documents

- [Contributor and implementation rules](AGENTS.md)
- [Migration plan and acceptance criteria](docs/MIGRATION_PLAN.md)
- [Compatibility and Provider inventory matrix](docs/COMPATIBILITY_MATRIX.md)
- [Licensing and source reuse gate](docs/LICENSING.md)

## Working principles

- Preserve the existing MetaTube SDK and Jellyfin plugin contract unless a verified compatibility gap requires a change.
- Inventory all Amane movie and actor sources; compare each against existing SDK Providers before deciding whether to reuse, replace, or add implementation.
- Use deterministic fixtures and focused tests. Never treat compilation alone as proof that a site works.
- Record upstream revisions, test commands, and unresolved site/network constraints.
- Work on a feature branch; do not merge to `main` without explicit direction.
