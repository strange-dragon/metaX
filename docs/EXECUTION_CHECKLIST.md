# Amane → MetaTube migration execution checklist

This file is the durable handoff for small, independently reviewable work units. Update it after each task so work can resume without relying on a long chat session.

## Working rules

- Work only on `feat/amane-provider-migration`; do not merge to `main` without explicit approval.
- One task per commit. Keep each change narrow; inspect the diff and record verification before starting the next task.
- Do not mark tests as passing unless they were actually run. Distinguish test failures, tests not run, and live-site checks.
- Compare Amane behavior with the SDK implementation before porting. Prefer the best implementation based on correctness, coverage, resilience, maintainability, and runtime cost.
- The intended runtime remains Jellyfin → existing MetaTube Jellyfin plugin → MetaTube API → native SDK Providers. Do not introduce a standalone Amane service.
- Preserve Apache-2.0 only for code that can properly be distributed under it. MetaTube SDK-derived code retains required upstream notices. Amane attribution alone does not grant permission to copy GPL-3.0 source or fixtures; resolve licensing before any such reuse.

## Task queue

### Task 0 — Recover and audit repository state
- [x] Confirm repository and feature branch.
- [x] Read contributor instructions, migration plan, compatibility matrix, licensing notes, and initial findings.
- [x] Confirm existing JavDB and Jav321 Provider implementations and tests.
- [x] Inspect the SDK Provider interface and explicit `engine/register.go` registration imports.
- [ ] Local tests, code formatting, and server integration were not run as part of this audit.

### Task 1 — Establish a trustworthy test baseline for the existing JavDB Provider
- [x] Inspect the complete JavDB implementation and existing deterministic HTTP tests.
- [x] Review found that the default HTTP client followed redirects without constraining the destination; this could escape the configured origin or port.
- [x] Add a redirect policy that restricts redirects to the configured host/port, permits the canonical www alias on the same effective port, blocks HTTPS downgrade, and caps redirect hops.
- [x] Add a deterministic regression test that redirects to a different origin/port and asserts the destination is never requested.
- [x] GitHub Actions deterministic test job passed on commit `ec2b52fd7d656c3748114972d5727f330e8bbb16` (run [38869568850](https://github.com/strange-dragon/metaX/actions/runs/38869568850)); CI formatting check passed. Race checks and live-site checks were not run.
- [x] No live-site support is claimed; live JavDB compatibility remains unverified.
- [x] CI exposed an unrelated `provider/fc2club` compile error (`appendUnique` was referenced but undefined in that package); added the package-local helper in commit `ec2b52f` so the repository test job can build.

### Task 2 — Verify the integration path before expanding providers
- [x] Verified the stock integration path in `metatube-community/metatube-sdk-go` at upstream commit `c62584675c593816cc07e7b5b977d14c7eb505a3`: `cmd/server/main.go` imports `engine`; `engine/register.go` blank-imports provider packages; `engine/init.go` initializes providers from the factory registries.
- [x] Confirmed that adding metaX as a normal module dependency does not register its providers. The correct delivery path is to transplant reviewed provider packages and registration imports into an SDK fork or propose them upstream; do not make the SDK depend on metaX.
- [x] Existing Jellyfin plugin contract can remain unchanged; no plugin-side change is indicated by the current architecture.
- [x] Documented the verified path in `docs/INTEGRATION.md` (commit `9d34a3e`).

### Task 3 — Validate Jav321 independently
- [ ] Review implementation and deterministic tests.
- [ ] Check ID/URL normalization, search semantics, field extraction, malformed/not-found responses, and registration.
- [ ] Run focused tests and update findings only with evidence.

### Task 4 — Compare existing SDK providers against Amane, one site per task
- [ ] Start with the highest-value uncovered behavior in an existing provider (initial candidates: JavBus, AVBase, MGStage).
- [ ] Record observed differences and a decision before changing code.
- [ ] Reuse or improve existing SDK code where it is better; avoid duplicate provider stacks.
- [ ] Update the compatibility matrix after each completed comparison.

### Task 5 — Add uncovered providers in small increments
- [ ] Prioritize sites with a clear mapping to existing SDK interfaces and manageable network behavior.
- [ ] Each provider gets focused tests, registration verification, and a separate commit.
- [ ] Defer browser-only/anti-bot-heavy sites until feasibility is assessed.

### Task 6 — Actor-provider inventory and migration
- [ ] Inventory Amane actor crawlers separately from movie crawlers.
- [ ] Compare with SDK ActorProvider/ActorSearcher interfaces and preserve actor IDs, images, and field semantics.
- [ ] Migrate one actor site per task where feasible.

### Task 7 — Aggregation, full regression, and release readiness
- [ ] Compare Amane field-level aggregation with MetaTube's existing orchestration.
- [ ] Implement a shared aggregation change only if a demonstrated gap exists and the behavior is tested independently.
- [ ] Run the available SDK tests, formatting/lint checks, and integration checks.
- [ ] Document known gaps and verification limits. Do not merge to `main` without explicit approval.

## Current verified status

- Repository: `strange-dragon/metaX`
- Branch: `feat/amane-provider-migration`
- Existing providers: `provider/javdb`, `provider/jav321`
- Current blocker for production use: the new providers are not automatically loaded by MetaTube merely because this repository exists; the runtime registration/import path must be verified and integrated.
- Latest audit: 2026-10-16. JavDB redirect restrictions and regression coverage are in commits `d3b80d0` and `4dc8e86`; CI found and the follow-up commit `ec2b52f` fixed the `fc2club` compile error. GitHub Actions deterministic tests and formatting check passed on `ec2b52f` (run [38869568850](https://github.com/strange-dragon/metaX/actions/runs/38869568850)). Integration path was verified against SDK commit `c62584675c593816cc07e7b5b977d14c7eb505a3`; details are in `docs/INTEGRATION.md`. Race checks and live-site checks remain unrun.
