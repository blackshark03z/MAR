# Slice 002 — Execution Attempt & Logical Fencing

**Architecture:** FROZEN
**Status:** COMPLETE / VERIFIED

## Goal

Implement durable execution-attempt identity and logical authority fencing without violating the R3 physical-fencing invariant.

## Scope

- Versioned SQLite schema migrations.
- Task `run_epoch`.
- Durable execution-attempt records.
- Active/logically-fenced/physically-terminated authority states.
- Heartbeat + lease deadline metadata.
- Orchestrator pre-execution CAS transitions.
- Attempt-bound task transitions.
- Replacement admission blocked until previous attempt is `PHYSICALLY_TERMINATED`.
- Recovery back to `WORKSPACE_READY` only after physical termination is recorded.

## Important boundary

Slice 002 does **not** claim to prove OS process termination.

The durable store contains the physical-termination transition primitive, but Slice 002 exposes no trusted OS proof by itself. Slice 003 is the only production service path that may invoke that transition, and it requires a Windows Job Object-backed `TerminationProof`.

`run_epoch` is logical fencing only.

## Acceptance

1. Initial attempt advances epoch from 0 to 1 and task to `RUNNING`.
2. Logical fence immediately rejects further heartbeat/MAR-mediated actions.
3. Logical fence alone does not permit same-workspace replacement.
4. Replacement becomes eligible only after prior attempt is recorded `PHYSICALLY_TERMINATED`.
5. Replacement receives a higher epoch.
6. Delayed actions from old epoch are rejected.
7. Concurrent begin attempts admit only one active writer.
8. Existing Slice 001 tests continue to pass.
9. `go test ./...` and `go vet ./...` pass.

## Evidence

- `go test ./...`: PASS, including logical-vs-physical fencing and concurrent-attempt tests.
- `go vet ./...`: PASS.
- `go build ./cmd/mar`: PASS.
- Pre-versioned Slice 001 database migration to schema v2: PASS.
- Real Slice 001 smoke DB upgraded successfully with existing task preserved at `run_epoch=0`.
- Final implementation revision is recorded by the Git commit containing this document.
