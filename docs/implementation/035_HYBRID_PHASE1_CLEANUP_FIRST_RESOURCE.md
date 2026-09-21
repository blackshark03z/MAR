# MAR Hybrid Simplification — Phase 1 Cleanup-First Resource Lifecycle

**Date:** 2026-09-21  
**Architecture:** `docs/architecture/MAR_HYBRID_SIMPLIFICATION_DECISION.md`  
**Roadmap:** `docs/roadmap/MAR_V2_HYBRID_SIMPLIFICATION.md`  
**Baseline:** `docs/research/034_HYBRID_SIMPLIFICATION_PHASE0_BASELINE.md`

## Goal

Prevent safely reclaimable MAR storage from being the sole reason a new task remains in `WAITING_RESOURCE`.

Phase 1 deliberately does **not** implement BLOCKED checkpoint/rehydrate and does not delete BLOCKED workspaces. That remains Phase 2.

## Implementation

### Positive-allowlist pressure cache reclamation

`internal/retention` now exposes a pressure-only cleanup path for explicitly classified rebuildable caches:

- `runtime/go-build-cache`
- legacy `runtime/gocache`
- legacy `runtime/diag-gocache`

The cache root itself is preserved so sandbox ACL identity remains stable. Module caches, audit/evidence trees, portable toolchains, unknown runtime directories and historical workspaces are excluded.

Symlinked cache roots fail closed; symlink/non-regular descendants are never traversed as deletion authority.

### Cleanup-before-denial

When scheduler admission is denied specifically by:

- `HOST_DISK_RESERVE`, or
- `MAR_DISK_BUDGET`

the scheduler performs one bounded safe reclaim pass before returning `WAITING_RESOURCE`:

1. existing terminal-safe workspace reclaim;
2. shared rebuildable-cache cleanup only when MAR has zero active resource claims;
3. invalidate cached MAR disk usage;
4. take a fresh governor admission decision for the same task/claim;
5. admit if the refreshed truth is now safe, otherwise preserve the final denial.

RAM/CPU/I/O/capacity-only denial does not trigger disk cleanup.

The scheduler result carries reclaimed terminal-workspace count and reclaimed cache bytes for direct execution evidence. A separate durable telemetry subsystem was intentionally **not** added in this phase: Phase 0 already provides the storage-class baseline, and building a new historical metrics authority would conflict with the simplification objective. Broader hot/warm/cold observability should reuse later checkpoint/storage truth rather than create a parallel state model.

### Fresh disk observation

The Windows resource sensor now supports explicit MAR-disk cache invalidation. A generation guard prevents an older in-flight recursive scan from repopulating the cache after reclamation invalidated it.

## Safety boundaries

Phase 1 preserves the existing safety kernel:

- BLOCKED workspaces remain untouched.
- Existing store-level terminal-removal eligibility remains authoritative.
- Cache deletion is a positive allowlist, not an age/name heuristic over arbitrary runtime state.
- Shared rebuildable cache is not pruned while another MAR resource claim is active.
- `GOMODCACHE` is not pressure-pruned because it participates in offline module availability.
- Failed/insufficient reclamation leaves the task resource-blocked rather than bypassing the governor.
- Final verification, integration, attempt fencing and owner-checkout semantics are unchanged.

## Acceptance evidence

Focused gate:

`go test ./internal/retention ./internal/resourcegov ./internal/scheduler ./internal/workspace -count=1 -v` — **PASS**

Includes:

- allowlisted cache contents removed while cache roots remain;
- module cache/audit evidence preserved;
- symlinked cache root rejected;
- sensor invalidation forces fresh lower disk observation;
- disk-pressure denial invokes reclaim and retries the same task;
- recovered capacity advances to `WORKSPACE_READY`;
- non-disk denial does not invoke disk reclaim;
- shared cache cleanup is disabled while another MAR resource claim is active;
- existing workspace isolation/removal tests remain green.

Orchestrator regression:

`go test ./internal/orchestrator -count=1` — **PASS** (~78.1 s).

Full working-tree repository gate before sealing:

- `go test -p 1 -count=1 -timeout 180s ./...` — **PASS** (~274.3 s);
- `go vet -p 1 ./...` — **PASS**;
- `go build -p 1 ./...` — **PASS**;
- `git diff --check` — **PASS**.

The full working-tree gate also contained the pre-existing uncommitted legacy-activation compatibility patch. Therefore it is supporting regression evidence, **not the final exact-candidate proof for this Phase 1 commit**. After the scoped Phase 1 commit is created, exact-candidate qualification must run in a detached worktree at that commit with the activation hotfix absent.

## Baseline relevance

Phase 0 measured:

- `.mar` total: 16.882 GiB;
- `.mar/w`: 7.041 GiB;
- 104 BLOCKED + physically terminated live workspaces retaining ~7.05 GiB;
- current `runtime/go-build-cache`: ~220.9 MiB at measurement time.

Phase 1 can reclaim the known rebuildable cache class and already-terminal reclaim-safe state before admission denial. It intentionally cannot solve the dominant ~7.05 GiB BLOCKED-workspace retention yet; that is the reason Phase 2 exists.

## Stop / next rule

Do not open Phase 2 merely because Phase 1 code exists.

First:

1. seal the scoped Phase 1 commit without the unrelated activation patch;
2. run exact-candidate full test/vet/build/diff gate;
3. record a clean qualification outcome;
4. only then consider Phase 2 checkpoint/rehydrate.
