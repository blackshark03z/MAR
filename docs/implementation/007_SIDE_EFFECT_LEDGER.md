# Slice 007 — Side-Effect Ledger / Crash Reconciliation

**Architecture:** FROZEN
**Status:** COMPLETE / VERIFIED

## Goal

Close acceptance T15:

> a crash after side-effect dispatch but before durable observation must never cause a blind duplicate dispatch.

## Protocol

Generic attempt-bound effects use a durable immutable `operation_id` + intent hash:

```text
PREPARED
   ↓ durable AuthorizeDispatch
DISPATCHED
   ↓ physical effect happens or does not happen
OBSERVED(APPLIED | NOT_APPLIED)
```

The crucial ordering is:

> persist `DISPATCHED` **before** starting the physical side effect.

Therefore any restart that finds `DISPATCHED` has an uncertain physical outcome and returns `RECONCILE`; it never returns `DISPATCH`.

### Reconciliation

- If actual-world observation proves the effect **applied**, record `OBSERVED/APPLIED`; future plans return `ALREADY_APPLIED`.
- If observation proves the effect **did not apply**, record `OBSERVED/NOT_APPLIED`; retry remains forbidden until an explicit `RearmAfterNotApplied` transitions it back to `PREPARED` and increments `reconciliation_count`.
- If the actual outcome cannot be determined, leave the effect `DISPATCHED` and block/review; there is no generic exactly-once claim.

## Authority

- Preparing and authorizing an attempt-bound effect requires the referenced execution attempt to match the task's current `run_epoch` and remain `ACTIVE`.
- A stale/logically-fenced attempt cannot dispatch a new effect.
- Reconciliation/observation intentionally may happen after the original attempt is fenced, because recovery must inspect and settle its uncertain effects.

## Scope boundary

The generic ledger is for worker/agent side effects that do not already have a stronger specialized transaction state machine.

Existing specialized protocols remain valid:

- workspace creation/removal uses durable `workspaces.PREPARING/REMOVING` + Git observation;
- process termination uses OS-backed `TerminationProof`;
- future integration uses its own `integration_attempt` + expected-head CAS.

We do not duplicate those protocols into this generic ledger.

## Acceptance

1. Same `operation_id` + identical intent is idempotent.
2. Same `operation_id` + changed payload/precondition is rejected.
3. `DISPATCHED` survives database restart and requires reconciliation rather than redispatch.
4. `OBSERVED/APPLIED` suppresses future dispatch.
5. `OBSERVED/NOT_APPLIED` requires explicit rearm before retry.
6. Stale attempt cannot authorize a prepared effect.
7. Concurrent prepare produces one immutable effect record.
8. Schema migration through v5 preserves older data.
9. Existing Slice 001–006 tests remain green.
10. `go test ./...`, `go vet ./...`, build, and `git diff --check` pass.

## Evidence

- Targeted effect/store tests: PASS.
- Fenced `DISPATCHED` effect remains readable as `RECONCILE`; fenced `PREPARED` effect cannot become dispatchable.
- Real T15 local-file crash window: physical effect occurs after durable `DISPATCHED`, database is closed/reopened before observation, restart returns `RECONCILE`, actual file state is observed, and no second physical write occurs: PASS.
- Concurrent immutable `operation_id` prepare: PASS.
- Existing pre-versioned database migration through schema v5: PASS via store regression suite.
- `go test -count=1 ./...`: PASS.
- `go vet ./...`: PASS.
- Windows `go build ./cmd/mar`: PASS.
- `git diff --check`: PASS.
- Final implementation revision is the Git commit containing this document.
