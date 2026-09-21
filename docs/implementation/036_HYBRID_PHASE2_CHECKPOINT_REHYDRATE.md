# MAR Hybrid Simplification — Phase 2 Checkpoint/Rehydrate

**Date:** 2026-09-21
**Base:** `2612d9029a83a280b5aeefac2fefeacd6d8d4481`
**Status:** candidate; exact-head qualification required before integration/activation.

## Goal

Reclaim storage held by safely terminated BLOCKED workspaces without discarding resumable source state.

Phase 2 follows the architecture rule:

> persist facts and irreproducible intent; reconstruct execution materializations.

SQLite remains the durable coordination truth. Git owns reconstructible source snapshots. A task worktree is materialized only while mutation/resume needs it.

## Baseline motivating this slice

Phase 0 measured 104 BLOCKED + physically terminated live workspaces retaining about 7.05 GiB.

The pre-implementation safety audit additionally found two classes that must not be auto-compacted:

- 6 workspaces where the actual worktree HEAD differed from durable `workspaces.head_revision`;
- 7 BLOCKED workspaces whose latest result was VERIFIED with integration status BLOCKED.

These remain fail-closed. Phase 2 must not use compaction to hide an unresolved Git identity or integration problem.

## Durable representation

Schema v16 adds `workspace_checkpoints`.

A checkpoint records:

- task/workspace/project identity;
- original durable HEAD;
- immutable Git snapshot revision;
- private `refs/mar/checkpoints/...` anchor;
- hash of the pre-checkpoint porcelain status;
- clean/dirty flag;
- lifecycle state and timestamps.

Workspace lifecycle gains:

`READY -> CHECKPOINTING -> CHECKPOINTED -> PREPARING -> READY`

Checkpoint lifecycle:

`CAPTURED -> COMPACTED -> REHYDRATING -> REHYDRATED`

## Capture semantics

A BLOCKED workspace is eligible only when:

1. workspace state is READY;
2. every execution attempt is PHYSICALLY_TERMINATED;
3. actual registered worktree HEAD equals durable `head_revision`;
4. latest VERIFIED/integration-BLOCKED result does not protect the workspace;
5. ignored material is absent except for the explicit reconstructible task-local scratch allowlist: `.mar/go/{build,mod,tmp}` and `.mar/runtime/{profile,tmp,python-cache}`.

Clean workspaces use the existing HEAD as the snapshot.

Dirty workspaces use an isolated temporary Git index:

1. `read-tree <actual-head>`;
2. `git add -A -- .`;
3. `write-tree`;
4. `commit-tree` with the actual HEAD as parent;
5. create a private `refs/mar/checkpoints/...` anchor.

This preserves tracked changes plus normal untracked WIP without changing the user's branch, current worktree index, or authoritative branch ref.

Ignored material is deliberately not inferred to be disposable. Only ACI-owned task-local scratch paths with direct source provenance are classified reconstructible: `.mar/go/{build,mod,tmp}` and `.mar/runtime/{profile,tmp,python-cache}`. They are not checkpointed and are recreated on demand. Any other ignored material blocks automatic compaction.

## Compaction

After the private ref and DB checkpoint record are durable, MAR removes only the task's managed worktree and prunes stale worktree metadata.

The task remains BLOCKED. No result, verification evidence, authority record, or project branch is rewritten.

When the scheduler is truly idle (no waiting task and the resource governor can take its idle-exclusive gate), MAR also backfills at most two safe BLOCKED workspaces per scheduler tick. This migrates historical materializations without delaying queued/running work. Candidate scanning is wider than the mutation batch so a small number of fail-closed HEAD-drift workspaces cannot permanently starve later safe candidates.

Disk-pressure admission order becomes:

1. bounded terminal workspace reclaim;
2. positive-allowlist rebuildable cache reclaim;
3. bounded safe BLOCKED-workspace checkpoint/compaction;
4. invalidate disk telemetry and retry the same admission claim.

## Resume / rehydrate

A blocked-choice resume against a CHECKPOINTED workspace moves the task to WAITING_RESOURCE.

`EnsureMutable` then reconstructs the managed worktree at the checkpoint snapshot revision. Successful rehydrate finalizes:

- workspace -> READY;
- task -> WORKSPACE_READY;
- checkpoint -> REHYDRATED.

The reconstructed worktree is a clean immutable representation of the preserved WIP snapshot. Staging-area distinctions are intentionally not preserved; file/tree content is.

## Crash recovery

The scheduler reconciles bounded CHECKPOINTING transactions before normal admission.

Two crash windows are covered:

- crash before durable snapshot record: restore READY only when the original registered worktree still matches durable HEAD;
- crash after CAPTURED: verify the private checkpoint ref, complete worktree removal if needed, then finalize CHECKPOINTED.

A task-local unsafe/stale checkpoint remains fail-closed but does not fail the entire scheduler step. This prevents one damaged workspace from starving unrelated admission.

## Proven candidate evidence

Targeted tests prove:

- dirty tracked + untracked WIP survives checkpoint -> compact -> blocked-choice resume -> rehydrate;
- rehydrated content matches the checkpoint and is clean;
- durable/actual HEAD drift blocks compaction;
- crash before snapshot record restores READY safely;
- crash after CAPTURED finishes compaction safely;
- an unsafe checkpoint transaction does not cause global scheduler starvation;
- scheduler retries disk admission after successful BLOCKED-workspace compaction.

Store/service/workspace/scheduler targeted regression, vet and build have passed during implementation. These are development checks, not the final release claim.

## Non-goals

This slice does not:

- compact mutation-capable attempts;
- compact VERIFIED/integration-BLOCKED workspaces;
- overwrite branch refs;
- push checkpoint refs remotely;
- classify arbitrary ignored files as disposable;
- delete checkpoint refs before the task lifecycle makes them unnecessary;
- replace final verification/integration semantics;
- change the currently activated production runtime until exact-candidate qualification passes.

## Release gate

Before integration:

1. `git diff --check`;
2. exact scoped candidate commit;
3. clean detached worktree at that commit;
4. full repository test split into watchdog-safe groups;
5. `go vet -p 1 ./...`;
6. `go build -p 1 ./...`;
7. exact HEAD/status identity proof.

Only after those pass may Phase 2 be integrated to `master`, pushed, and considered for activation.
