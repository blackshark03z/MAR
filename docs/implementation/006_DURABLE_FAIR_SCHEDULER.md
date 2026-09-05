# Slice 006 — Durable Fair Scheduler

**Architecture:** FROZEN
**Status:** COMPLETE / VERIFIED

## Goal

Turn durable `WAITING_RESOURCE` tasks into automatically admitted isolated workspaces without introducing a separate queue service or in-memory source of truth.

## Design

- `tasks.state = WAITING_RESOURCE` is the durable queue.
- Scheduler executes one serialized authoritative decision at a time; admitted workers may later execute concurrently.
- Waiting tasks are grouped by project, so a project backlog contributes only one candidate per scheduling decision.
- Within each project: highest effective priority, then oldest wait wins.
- Priority aging is computed from durable `task.updated_at`; it needs no hidden scheduler state.
- Across projects: effective priority first, then never/least-recently-dispatched project, then oldest candidate.
- `project_scheduler_state` stores only `last_dispatched_at` + dispatch count so fairness survives daemon restart.
- Resource Governor admission occurs before workspace provisioning.
- Resource denial keeps the task in `WAITING_RESOURCE` with explicit denial reasons.
- Workspace preparation failure moves the task to `BLOCKED` rather than retrying an unsafe/permanent side effect forever.

## Resource boundary

This slice claims only configurable lightweight RAM/disk headroom for workspace preparation. Heavy build/test/browser resource claims belong to the later worker/tool scheduler; a whole autonomous task is not treated as one permanently-heavy resource slot.

## Fairness boundary

This is not a distributed queue. One MAR daemon owns the scheduler. SQLite is durable truth; the Scheduler's mutex merely serializes decisions inside that daemon.

## Acceptance

1. Resource denial leaves task durably `WAITING_RESOURCE` and does not create a workspace.
2. Successful admission creates/prepares one workspace and advances task readiness.
3. One project backlog does not starve a never/less-recently-dispatched project.
4. Fairness survives Scheduler reconstruction/restart via SQLite state.
5. Priority aging eventually elevates old low-priority work without another database queue structure.
6. Concurrent `Step` calls do not dispatch the same task twice.
7. Workspace failure blocks the task safely.
8. Schema migration through v4 preserves older data.
9. Existing Slice 001–005 tests remain green.
10. `go test ./...`, `go vet ./...`, build, and `git diff --check` pass.

## Evidence

- `WAITING_RESOURCE` is used as the durable queue; no separate queue truth exists: PASS.
- Resource denial preserves durable `WAITING_RESOURCE` and does not invoke workspace provisioning: PASS.
- Scheduler reconstruction preserves cross-project fairness via SQLite `last_dispatched_at`: PASS.
- One-project backlog yields to a never/less-recently-dispatched project at equal effective priority: PASS.
- Priority aging elevates old low-priority work without new durable queue state: PASS.
- Concurrent `Step()` calls do not double-dispatch the same task: PASS.
- Workspace provisioning failure moves task to `BLOCKED`: PASS.
- Vertical integration using real Git repo + Resource Governor + Isolated Workspace Manager advances `WAITING_RESOURCE → WORKSPACE_READY`: PASS.
- Workspace resource lease is released after provisioning; no admission lease leak: PASS.
- Schema migration through v4: PASS via full store regression.
- `go test -count=1 ./...`: PASS.
- `go vet ./...`: PASS.
- Windows `cmd/mar` build: PASS.
- `git diff --check`: PASS.

Final implementation revision is recorded by the Git commit containing this document.
