# MAR Hybrid Simplification — Phase 0 Baseline

**Date:** 2026-09-21  
**Source HEAD:** `dfb29b4c5355413822a05494253864e60b865a6c`  
**Architecture decision:** `docs/architecture/MAR_HYBRID_SIMPLIFICATION_DECISION.md`  
**Roadmap:** `docs/roadmap/MAR_V2_HYBRID_SIMPLIFICATION.md`

## Scope

Read-only measurement of the current local MAR state before Phase 1 changes.

Two pre-existing activation-hotfix files remained dirty during measurement and are not part of this baseline commit:

- `cmd/mar/runtime_activation_script_test.go`
- `scripts/activate-current-head.ps1`

## 1. Data-root footprint

Measured `D:\MAR\.mar` total:

- **18,127,188,304 bytes**
- **16.882 GiB**

Largest top-level components:

| Path | Bytes | GiB |
|---|---:|---:|
| `.mar/w` | 7,559,765,281 | 7.041 |
| `.mar/runtime` | 5,650,860,101 | 5.263 |
| `.mar/selfdev` | 1,131,557,726 | 1.054 |
| `.mar/recovery` | 983,471,581 | 0.916 |
| `.mar/remote-full-goal-final` | 647,835,358 | 0.603 |
| `.mar/selfhost-v4` | 552,268,618 | 0.514 |
| `.mar/final-uat-f01731e-v2` | 544,635,946 | 0.507 |
| `.mar/go` | 537,766,414 | 0.501 |
| `.mar/mar.db` | 109,346,816 | 0.102 |
| `.mar/staging` | 74,288,968 | 0.069 |

This is after a prior safe deletion of several rebuildable Go build caches, so it is already lower than the earlier ~19.76 GiB pressure observation.

## 2. Workspace/task baseline

SQLite schema version: **15**.

Durable task counts:

| Task state | Count |
|---|---:|
| BLOCKED | 118 |
| COMPLETE | 66 |
| CANCELLED | 43 |
| RETRY_WAIT | 1 |

Workspace table rows: **219**.  
Workspace paths with live bytes: **114**.

Measured sum of live workspace-path bytes: **7,670,433,508 bytes** (~7.14 GiB; this sum follows the workspace paths recorded in SQLite and is slightly broader than the direct `.mar/w` top-level scan).

Dominant live groups:

| Task / workspace / authority / result | Count | GiB |
|---|---:|---:|
| BLOCKED / READY / PHYSICALLY_TERMINATED / UNVERIFIED / NOT_INTEGRATED | 14 | 3.649 |
| BLOCKED / READY / PHYSICALLY_TERMINATED / VERIFIED / integration BLOCKED | 7 | 1.565 |
| BLOCKED / READY / PHYSICALLY_TERMINATED / no result | 72 | 1.290 |
| BLOCKED / READY / PHYSICALLY_TERMINATED / VERIFICATION_FAILED | 11 | 0.549 |
| BLOCKED / READY / LOGICALLY_FENCED / no result | 9 | 0.087 |
| RETRY_WAIT / READY / PHYSICALLY_TERMINATED | 1 | 0.003 |

Therefore **104 BLOCKED workspaces are already physically terminated yet retain about 7.05 GiB of live filesystem state**.

These workspaces are not authorized for deletion in Phase 1 because resumable source state has not yet been converted into the Phase 2 checkpoint/rehydrate representation.

Attempt-authority rows:

- `PHYSICALLY_TERMINATED`: 331
- `LOGICALLY_FENCED`: 9

## 3. Runtime footprint

Largest measured `.mar/runtime` entries:

| Runtime entry | Bytes | MiB |
|---|---:|---:|
| `audit-20260915` | 1,162,811,814 | 1108.9 |
| `owner_realuse_aacb67f_final6` | 539,617,440 | 514.6 |
| `owner_realuse_e42a32f_final5` | 539,581,363 | 514.6 |
| `owner_realuse_42195f8_final4` | 328,002,942 | 312.8 |
| `gomodcache` | 324,340,058 | 309.3 |
| `gomodcache_probe_host` | 324,340,058 | 309.3 |
| `go-portable` | 246,896,065 | 235.5 |
| `go-build-cache` | 231,633,300 | 220.9 |
| tunnel-client download cache | 89,670,392 | 85.5 |
| `archive` | 72,194,560 | 68.9 |
| `go-download` | 66,090,690 | 63.0 |
| tunnel-client runtime | 61,837,836 | 59.0 |
| `cloudflared.exe` | 54,841,128 | 52.3 |
| `verify` | 49,341,952 | 47.1 |
| `runtime/staging` | 25,053,271 | 23.9 |
| current `mar-v1-stable.exe` | 25,052,672 | 23.9 |

Not every large runtime directory is currently classified as safe/rebuildable. Phase 1 must use a positive allowlist and must not infer deletability from age/size alone.

## 4. Resource-governor baseline

Current production configuration in `cmd/mar/main.go`:

`MaxMARDiskBytes: 20 << 30`

The governor currently evaluates:

`projected MAR bytes = observed MAR bytes + existing disk reservations + new claim disk reservation`

and denies with `MAR_DISK_BUDGET` when the projected value exceeds the configured maximum.

The current Windows sensor counts all files below configured MAR roots into `MARDiskUsedBytes`; rebuildable cache and irreproducible durable state are not distinguished in the budget.

Scheduler behavior on denial is to leave the selected task in `WAITING_RESOURCE`.

Existing daemon retention removes bounded terminal workspaces before scheduling each tick, but:

- BLOCKED workspaces are intentionally excluded;
- runtime pressure cleanup is not part of scheduler admission;
- activation retention only removes old marked activation backups and allowed staging files;
- rebuildable runtime cache can therefore contribute directly to `MAR_DISK_BUDGET`.

## 5. Phase 0 conclusion

The independent-review hypothesis is confirmed on the live host.

The dominant storage problem is **not source code or the MAR executable**. It is durable execution materialization:

1. ~7.05 GiB in physically terminated BLOCKED workspaces;
2. multi-GiB runtime/history/probe material whose retention classes are not explicit;
3. rebuildable cache counted identically to durable facts for admission.

Phase 1 should **not** delete BLOCKED workspaces. That belongs to Phase 2 after checkpoint/rehydrate exists.

Phase 1 should first establish:

- positive retention classes;
- cleanup-before-disk-denial;
- safe pressure pruning of explicitly rebuildable runtime state;
- bounded terminal cleanup escalation under disk pressure;
- before/after telemetry that reports reclaimed bytes and final denial reasons.

## 6. Phase 1 acceptance oracle

A synthetic/fixture admission test must prove:

1. the first governor decision is denied only for host/MAR disk pressure;
2. scheduler invokes bounded safe reclamation;
3. governor takes a fresh snapshot;
4. if enough bytes are safely reclaimed, the same task is admitted without owner intervention;
5. if not enough bytes are safely reclaimable, the task remains `WAITING_RESOURCE` with the original truth-preserving denial;
6. no BLOCKED workspace or authoritative evidence is removed by Phase 1;
7. existing final verification/integration semantics remain unchanged.
