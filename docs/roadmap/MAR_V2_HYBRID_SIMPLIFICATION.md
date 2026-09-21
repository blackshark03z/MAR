# MAR V2 — Hybrid Simplification Migration

**Status:** ACTIVE ROADMAP  
**Date:** 2026-09-21  
**Mode:** incremental migration; **NO FROM-SCRATCH REWRITE**  
**Architecture authority:** `docs/architecture/MAR_HYBRID_SIMPLIFICATION_DECISION.md`

## Goal

Reduce MAR's persistent state, lifecycle cross-product, recovery surface and owner friction while preserving the proven safety kernel.

Primary optimization objective:

> reliable verified software delivery with less durable machinery.

## Phase 0 — Baseline and migration guardrails

Measure the current authoritative system before behavior changes:

- `.mar` bytes by runtime/workspaces/recovery/selfdev/database/cache/staging;
- workspace count and bytes by task/result/attempt state;
- rebuildable/cache bytes;
- blocked/needs-input age distribution;
- resource-denial reasons and reclaimable bytes at denial time;
- task-state and attempt-authority transition inventory;
- activation artifacts/processes and rollback footprint;
- representative resume/recovery fixtures.

Deliver a machine-readable baseline plus a human summary.

**Gate:** no storage semantics migration without a reproducible baseline and rollback snapshot.

## Phase 1 — Cleanup-first resource lifecycle

Implement storage classes and safe automatic reclaim before task denial.

Priority:

1. rebuildable build/test caches;
2. stale staging/candidate artifacts;
3. bounded activation/recovery generations;
4. already-terminal reclaim-safe execution materializations;
5. expose active / warm / cold / rebuildable bytes separately.

Resource admission must attempt bounded reclaim before `MAR_DISK_BUDGET` denial.

**Acceptance:** reproduce the prior disk-pressure scenario and prove safe reclaim occurs before denial; no authoritative state/evidence loss.

## Phase 2 — BLOCKED checkpoint / rehydrate

Introduce feature-flagged suspension:

1. physically fence mutation;
2. persist WIP source as Git-native hidden checkpoint/ref or equivalent Git object identity;
3. persist compact required metadata;
4. preserve only explicitly irreproducible external artifacts;
5. delete task-local disposable caches;
6. remove the live worktree;
7. rehydrate on resume under a valid new attempt/run epoch.

Start with ordinary Git-backed tasks only.

**Acceptance:** source identity and required task facts before suspend == rehydrated identity after resume; stale workers cannot mutate; workspace bytes are reclaimed.

## Phase 3 — Lazy workspace materialization

Read/context/research paths operate against bounded project/Git reads without allocating a task worktree.

Create isolated workspace on first mutation intent, not task submission.

**Acceptance:** read-only and no-change representative tasks allocate zero mutable task worktrees.

## Phase 4 — Lifecycle simplification

Introduce a backward-compatible projection:

`status = QUEUED | RUNNING | NEEDS_INPUT | BLOCKED | SUCCEEDED | FAILED | CANCELLED`

with optional diagnostic `phase`.

Move VERIFIED/integration truth to their existing evidence/result/integration facts.

Migrate consumers gradually, stop writing obsolete states only after compatibility coverage proves equivalent recovery behavior.

## Phase 5 — Decouple authority from resource capacity

Supervisor heartbeat/fencing becomes independent from parked/reacquiring execution capacity.

Resource slots express scheduling capacity only.

**Acceptance:** capacity contention/Web waits cannot expire an otherwise valid attempt; stale/revoked attempts still fail closed.

## Phase 6 — Immutable runtime promotion

Introduce versioned binaries and current-pointer launcher alongside the legacy activation path.

Example target:

`runtime/bin/mar-<revision>-<hash>.exe`

`runtime/current.json -> qualified artifact identity`

Promotion switches pointer and restarts; rollback switches pointer back. Never overwrite a running executable.

**Acceptance:** promotion/rollback on Windows succeeds while prior binary remains immutable; exact runtime identity remains `HEALTHY / ALIGNED / trusted_for_release=true`.

## Phase 7 — Evidence/history compaction

Preserve final authoritative receipts while bounding non-authoritative raw logs/output.

Use content identity/deduplication/compression where justified.

Derived Project Brain/index data remains rebuildable.

## Stop rule

Do not open the next phase automatically.

After every phase, remeasure:

- steady-state and pressure-point disk;
- workspace count/bytes;
- task admission delay/denial;
- verified-result latency;
- resume success;
- recovery correctness;
- code/schema/recovery-path growth;
- owner intervention count.

Stop when the dominant architecture-induced friction has been removed and additional simplification has poor complexity-adjusted return.

## Release rule

A phase is not complete merely because code exists.

It must have:

- bounded Goal/acceptance;
- focused regression evidence;
- full applicable release verification;
- exact revision identity;
- safe integration;
- runtime activation where runtime behavior changed;
- before/after measurement.

## Immediate authorized scope

The first implementation scope is **Phase 0 + Phase 1 only**.

Phases 2–7 are architecturally accepted targets but must be opened one at a time after the previous phase has representative evidence.

The existing activation-process-stop patch may be treated only as a bounded compatibility hotfix for the legacy activation path; do not expand it into the long-term solution in place of Phase 6.
