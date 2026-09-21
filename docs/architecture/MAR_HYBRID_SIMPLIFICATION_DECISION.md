# MAR Hybrid Simplification Architecture Decision

**Status:** ACCEPTED — CANONICAL ARCHITECTURE AMENDMENT  
**Date:** 2026-09-21  
**Basis:** independent adversarial architecture review of MAR at/around `b99e0e3c12a1984839374c131aeaa6402438759b`, current repository SoT, release/research evidence, and observed local runtime/storage behavior.  
**Decision type:** architecture evolution, not a rewrite.

## 1. Decision

MAR is **over-engineered in representation/lifecycle machinery for the current single-owner, single-host deployment**, while several safety guarantees remain justified and must be preserved.

The accepted target is:

> **Hybrid MAR — a compact durable safety kernel whose persistent state is proportional to active or irreproducible work, not to the number and age of historical tasks.**

Canonical simplification principle:

> **Persist facts and irreproducible intent; reconstruct execution materializations.**

This decision does not authorize removal of safety guarantees merely to reduce code or disk. It changes how those guarantees are represented.

## 2. Evidence that triggered the amendment

The review identified architecture-generated operational failures, not merely aesthetic complexity:

- observed `.mar` pressure near ~19.76 GiB against a ~20 GiB MAR budget;
- `.mar/w` around ~7.04 GiB with many retained BLOCKED workspaces;
- representative retained workspaces around ~400–530 MiB;
- rebuildable Go caches alone recovered roughly ~3.25 GiB when removed;
- scheduler admission could enter `WAITING_RESOURCE` because retained MAR state plus workspace reservation exceeded MAR's own budget;
- current retention intentionally treats ordinary BLOCKED workspaces as resumable/live and therefore does not reclaim them;
- replace-in-place promotion of `mar-v1-stable.exe` produced a Windows executable-lock failure class;
- attempt authority lease required special keepalive handling while Web capacity was reacquired.

The review estimated that workspace compaction plus rebuildable-cache reclamation could remove roughly 44–50% of the observed pressure-point footprint. This is a hypothesis to benchmark, not a release claim.

## 3. Safety kernel to preserve

The following remain architectural invariants unless later evidence explicitly overturns them:

1. one durable coordination authority on the current host (SQLite/WAL);
2. task identity and idempotency;
3. bounded Goal/acceptance identity;
4. attempt/run-epoch stale-write fencing;
5. logical revocation distinct from confirmed physical termination;
6. OS process-tree containment / Windows Job Object semantics;
7. scoped project and dangerous-action permissions;
8. owner-checkout protection;
9. isolated mutation while mutation is active;
10. exact candidate revision identity;
11. authoritative final verification bound to that candidate;
12. criterion-specific evidence where required;
13. expected-head/CAS-style integration;
14. fail-closed handling of uncertain external effects;
15. compact semantic checkpoints;
16. provable runtime/artifact revision identity.

## 4. Representation changes accepted

### 4.1 Workspace

Old assumption:

> task resumability requires keeping a live isolated worktree.

New invariant:

> **A simultaneously mutating attempt owns one isolated mutable workspace while active. The workspace is an execution materialization, not durable task truth.**

Read-only work should not require a per-task worktree.

After safe physical fencing, a BLOCKED/NEEDS_INPUT task may be serialized to Git-native source checkpoint + compact SQLite metadata, its disposable caches removed, and its live worktree reclaimed. Resume rehydrates a workspace from the checkpoint under a new valid attempt/run epoch.

### 4.2 Git versus MAR state

Git owns reconstructible source state.

SQLite owns durable coordination/authority/result facts.

The filesystem workspace is not a second version-control authority.

### 4.3 Task lifecycle

The current 16-state lifecycle is no longer a long-term invariant.

Target durable outcome status:

- `QUEUED`
- `RUNNING`
- `NEEDS_INPUT`
- `BLOCKED`
- `SUCCEEDED`
- `FAILED`
- `CANCELLED`

Execution phase such as `PREFLIGHT / PREPARING / EXECUTING / VERIFYING / INTEGRATING` is diagnostic state and must not become correctness authority.

`VERIFIED` is a verification/result fact, not a required top-level lifecycle status.

Migration must be backward-compatible and incremental; no flag-day schema rewrite is authorized.

### 4.4 Resource governance

Resource governance remains required, but policy changes to:

> **reclaim safely first; deny admission second.**

Rebuildable/disposable cache must not consume durable budget until useful work is denied when safe automatic reclamation is available.

Resource telemetry should distinguish active/irreproducible bytes from checkpointed/history/rebuildable bytes.

### 4.5 Authority lease and capacity

Attempt authority lifetime and resource-slot ownership are separate concerns.

A parked/reacquiring resource slot must not by itself cause a valid attempt authority lease to expire. The supervisor owns authority heartbeat/fencing independently from capacity wait mechanics.

### 4.6 Runtime activation

Replace-in-place mutation of a running stable executable is not the target architecture.

Target model:

- immutable versioned runtime binaries;
- artifact manifest bound to revision/hash;
- small current-version pointer/launcher;
- promotion by pointer switch + restart;
- rollback by pointer reversal;
- bounded retention of prior qualified versions.

Running binaries are never overwritten.

### 4.7 Evidence and retention

Retention classes:

- **hot:** active workspace, recent detailed output, task-local disposable caches;
- **warm:** compact checkpoint + bounded failure/input evidence, normally no live worktree;
- **cold:** final Goal/revision/verification/integration/feedback facts plus artifact hashes/references;
- **rebuildable:** caches, staging binaries, derived indexes and temporary materializations; automatically reclaimable.

## 5. What this decision does not do

This is not authorization for:

- a from-scratch MAR rewrite;
- weaker final verification;
- weaker stale-worker fencing;
- destructive synchronization of owner work;
- removal of dangerous-action permission boundaries;
- replacing SQLite with a distributed coordination platform;
- adding a generic DAG/actor/event-sourcing system;
- turning MAR into an internal AI planner/multi-agent framework.

## 6. Migration rule

Each simplification phase must prove both:

1. the old safety outcome remains true; and
2. measurable complexity/resource/failure-surface is reduced.

Prefer deletion over new compensating machinery.

Temporary compatibility fixes may be kept only while the legacy path remains active and must not silently become new long-term architecture.

## 7. Acceptance direction

The migration succeeds when representative evidence shows:

- inactive historical tasks no longer cause linear growth in live-workspace bytes;
- read-only/no-change tasks allocate no isolated mutable workspace;
- ordinary BLOCKED/NEEDS_INPUT tasks can be checkpointed, reclaimed and rehydrated without losing source/authority identity;
- rebuildable cache cannot be the sole reason a safe new task is denied when automatic reclaim can make room;
- final candidate verification and expected-head integration retain current correctness;
- runtime promotion no longer overwrites a running executable;
- the owner workflow requires less operational cleanup, not more;
- no accepted safety/recovery invariant regresses.

## 8. Precedence

This document is the explicit evidence-backed architecture review required by `MAR_ARCHITECTURE_CONSTITUTION.md` governance.

Where older forward-looking documents imply that a live workspace, the 16-state lifecycle, or replace-in-place activation is permanently frozen, this decision supersedes that representation while preserving the safety outcomes listed above.

Historical release records remain historical truth for their exact releases.
