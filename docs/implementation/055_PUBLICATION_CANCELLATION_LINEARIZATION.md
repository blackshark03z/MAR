# Kernel Closure — Publication / Cancellation Linearization

**Date:** 2026-09-23  
**Status:** IMPLEMENTED CANDIDATE  
**Scope:** governed integration safety; no lifecycle redesign.

## Reproduced gap

The governed runner checked durable cancellation immediately before calling `Integrate()`, but integration dispatch and cancellation were not yet linearized against each other.

The pre-fix race was:

1. verification completes and the runner observes no cancel request;
2. integration atomically marks the integration attempt `DISPATCHED` and task `INTEGRATING`;
3. an Owner cancellation arrives after dispatch;
4. because the execution attempt is already physically terminated, cancellation could move the task `INTEGRATING -> CANCELLED`;
5. `driveAttempt()` could still execute the expected-head Git CAS before finalization noticed the task-state conflict.

That allowed the undesirable outcome “task is CANCELLED but the canonical ref still advances.”

## Decision

The existing integration-dispatch transaction is the publication linearization point.

- cancellation is accepted through `READY_TO_INTEGRATE`;
- once the task is `INTEGRATING`, cancellation is rejected with `ErrStateConflict`;
- `MarkIntegrationDispatched` already changes integration `PREPARED -> DISPATCHED` and task `READY_TO_INTEGRATE -> INTEGRATING` in the same serializable SQLite transaction.

Therefore concurrent dispatch/cancel transactions have one finite ordering:

- **cancel wins first:** task becomes `CANCELLED`; dispatch cannot transition it to `INTEGRATING`, so no publication CAS occurs;
- **dispatch wins first:** task becomes `INTEGRATING`; later cancellation is rejected before a cancel control is persisted, and publication owns completion/recovery.

No new durable state, lock service, mutex, or coordination database is added.

## Change

`internal/store/control.go::RequestTaskCancellation` now treats `TaskIntegrating` as non-cancellable, alongside terminal states, before inserting the cancel control.

## Acceptance

1. cancellation in `READY_TO_INTEGRATE` is accepted and moves the task to `CANCELLED`;
2. cancellation in `INTEGRATING` returns `ErrStateConflict`;
3. rejected post-dispatch cancellation does not persist a cancel control;
4. the task remains `INTEGRATING` after rejected post-dispatch cancellation;
5. existing integration dispatch remains the atomic publication boundary;
6. focused store regressions pass;
7. full repository test/vet/build/diff-check must pass before qualification.

## Focused evidence

`TestTaskCancellationLinearizesBeforeIntegrationDispatch` covers both sides of the boundary.

Current candidate:
- managed `gofmt` — PASS;
- `go test -count=1 ./internal/store` — PASS;
- `git diff --check` — PASS.

## Non-goals

- no change to fast-path Git operations;
- no cancellation of already-dispatched publication;
- no new task state;
- no new integration state;
- no distributed lock or queue;
- no weakening of expected-head CAS, fresh-evidence gating, or owner-work protection.
