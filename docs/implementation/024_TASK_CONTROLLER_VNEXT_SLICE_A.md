# MAR Task Controller vNext — Slice A

**Status:** IMPLEMENTATION CANDIDATE  
**Scope:** phase-aware blocked recovery + WORKSPACE_READY reconciliation

## Goal
Make durable tasks converge from observed SQLite truth instead of assuming every `BLOCKED` task is a failed worker replacement.

## Safety kernel preserved
SQLite durable truth, isolated mutable workspaces, `run_epoch`, attempt authority, logical fencing, physical-termination proof before replacement, resource-governor admission, verification sealing/freshness, and integration CAS remain unchanged. No second orchestrator, queue, database, or worker framework is added.

## Reconciliation decision table
| Observed durable reality | Safe controller action |
| --- | --- |
| `BLOCKED`, `run_epoch=0`, no attempt, no workspace | return to `PREFLIGHT`; never jump to `WORKSPACE_READY` |
| `BLOCKED`, READY workspace, prior attempt `PHYSICALLY_TERMINATED` | replacement may return to `WORKSPACE_READY` |
| `BLOCKED`, READY workspace, no attempt | post-workspace recovery may return to `WORKSPACE_READY` |
| `BLOCKED`, missing/non-READY workspace with execution history | remain `BLOCKED`; persist typed workspace/invariant blocker |
| verified integration block | existing integration-only retry runs first; no new coding worker |

A `blocked_choice` resolves an owner decision; it never authorizes skipping lifecycle phases.

## WORKSPACE_READY invariant
`WORKSPACE_READY` requires one durable workspace record in `READY` state. `launchReady` performs reconciliation before heavy execution admission. A legacy impossible state with `run_epoch=0`, no attempt, and no workspace safely repairs to `PREFLIGHT`; unsafe mismatches fail closed into a typed blocker.

## Durable blocker
Slice A stores one current blocker per task in the existing SQLite coordination database with `phase`, `code`, `detail`, and `recovery`. This metadata is diagnostic/recovery truth only and grants no execution authority.

## Regression anchor
The motivating real failure is preserved generically: preflight blocks before workspace creation, owner supplies `blocked_choice`, and the controller must re-enter preflight rather than produce `WORKSPACE_READY` without a workspace.

## Deferred slices
Adaptive project capability/verification discovery, approved dirty-input snapshots, general runtime-child self-healing, provider/tunnel changes, and Owner Console visual redesign are explicitly deferred.

## Verification gate
Focused store/service/orchestrator regressions and the canonical `go-standard` profile must pass before integration.
