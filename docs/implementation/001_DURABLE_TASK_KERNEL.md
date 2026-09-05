# Slice 001 — Durable Task Kernel

**Architecture:** FROZEN
**Status:** COMPLETE / VERIFIED

## Goal

Establish the smallest durable kernel required before MCP, workers, scheduling or recovery are added.

## Scope

- Go module bootstrap.
- SQLite/WAL durable source of truth.
- Project registration.
- Immutable/hashable Goal Contract.
- Task creation in `SUBMITTED` state.
- Idempotent submit semantics.
- Durable status lookup after process/store reopen.
- Minimal CLI for local verification.

## Explicit non-goals

- MCP transport.
- worker execution.
- run epochs/attempt fencing.
- worktrees.
- model providers.
- scheduler/resource governor.
- integration.

Those belong to subsequent vertical slices and must preserve the frozen architecture.

## Acceptance

1. First submit creates exactly one task.
2. Same idempotency key + same Goal Contract returns the original task.
3. Same idempotency key + different Goal Contract is rejected.
4. Concurrent duplicate submits produce exactly one task identity.
5. Task survives database close/reopen.
6. Unknown project is rejected.
7. Coordination DB uses one authoritative SQLite connection in V1 to keep transaction/PRAGMA behavior deterministic under concurrent submit.
8. `go test ./...` passes.

## Evidence

- `go test ./...`: PASS, including concurrent duplicate-submit test.
- `go vet ./...`: PASS.
- `go build ./cmd/mar`: PASS.
- CLI smoke: PASS (`init -> project-add -> submit -> idempotent retry -> status`).
- Runtime smoke artifacts stay under ignored `.mar/`.
- Final implementation revision is recorded by the Git commit containing this document.
