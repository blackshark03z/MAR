# Slice 005 — Isolated Git Workspace Manager

**Architecture:** FROZEN
**Status:** COMPLETE / VERIFIED

## Goal

Enforce the frozen invariant:

> one mutable task = one isolated mutable workspace.

## Design

- Registered project root must be the Git toplevel.
- Goal `base_revision` is resolved to an exact commit before workspace creation.
- Mutable workspace path is deterministic under MAR's managed data root.
- Workspace intent is persisted as `PREPARING` before `git worktree add`.
- Worktree is created **detached** at the exact resolved base commit; no task branch is created in V1 workspace preparation.
- Creation outcome is verified against `git worktree list --porcelain -z` and observed HEAD before task state advances.
- `MarkWorkspaceReady` and `WAITING_RESOURCE → WORKSPACE_READY` occur in one SQLite transaction.
- Repeated/concurrent ensure calls reconcile/return the same task workspace.
- Project-local Git worktree mutation is serialized inside the manager.
- All workspace-manager Git subprocess trees execute inside Windows Job Objects; daemon/process cancellation cannot leave an uncontained Git child mutating repository/worktree state.

## Crash/retry semantics

If Git reports an error after a possible side effect, MAR inspects observable Git worktree truth before marking failure. A persisted `PREPARING` workspace that is already correctly registered is reconciled to `READY` instead of blindly creating a second worktree.

Unexpected/unregistered pre-existing paths are not overwritten.

## Cleanup

V1 cleanup in this slice is intentionally conservative:

- only `FAILED` or `CANCELLED` tasks are eligible;
- every execution attempt must already be `PHYSICALLY_TERMINATED`;
- removal path must remain under MAR's managed data root;
- Git worktree registration is removed first;
- residual managed directory can then be removed;
- durable workspace state becomes `REMOVED` only after observable cleanup.

Completed-but-not-yet-integrated workspaces are retained; integration/retention cleanup is implemented with the integration lane later.

## Acceptance

1. Exact base commit produces a detached worktree with expected content.
2. Task and workspace become ready atomically in durable state.
3. Concurrent/idempotent ensure calls create only one task worktree.
4. Different mutable tasks never share a worktree.
5. Invalid base does not create a worktree.
6. Pre-attempt cancellation can safely remove a worktree.
7. Cleanup refuses non-terminal or mutation-capable tasks.
8. Schema upgrades from previous slices remain valid.
9. Existing Slice 001–004 tests remain green.
10. `go test ./...`, `go vet ./...`, build, and `git diff --check` pass.

## Evidence

- Exact-base detached worktree creation: PASS.
- Atomic durable `WAITING_RESOURCE → WORKSPACE_READY`: PASS.
- Concurrent/idempotent ensure creates one task worktree: PASS.
- Two tasks in one project receive separate mutable worktrees: PASS.
- Invalid base creates no worktree: PASS.
- Crash-window reconciliation (`PREPARING` + already-created Git worktree): PASS.
- Pre-existing unregistered managed path is preserved and workspace becomes safely failed/blocked: PASS.
- Cancelled pre-attempt workspace cleanup: PASS.
- Cleanup with mutation-capable attempt is rejected: PASS.
- Generic contained-command parent+child termination: PASS.
- Parent-exits-first/orphan-child timeout cleanup: PASS.
- Schema migration through v3: PASS via full store regression.
- `go test -count=1 ./...`: PASS.
- `go vet ./...`: PASS.
- Windows `cmd/mar` build: PASS.
- `git diff --check`: PASS.

Final implementation revision is recorded by the Git commit containing this document.
