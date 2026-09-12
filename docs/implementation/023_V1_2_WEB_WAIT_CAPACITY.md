# MAR V1.2 — Web-wait capacity

**Status:** IMPLEMENTED / ACCEPTANCE_PASS  
**Scope:** R-028 D1 / C-04

## Goal

A durable Web Brain wait must not monopolize scarce heavy execution capacity. The waiting worker process and exact attempt authority stay alive, but useful compute slots/heavy resource claims may be yielded while waiting.

## Implementation

- `TaskRunner` passes a parent-owned `worker.WaitCapacity` controller into `ProcessRunner`.
- Once a Web turn is durably pending, `ProcessRunner` asks the daemon to park capacity.
- Parking releases the task's heavy `resourcegov` lease while preserving the exact worker process, task, attempt, run epoch and turn identity.
- When a response arrives, a parked worker must reacquire a daemon slot and the same heavy claim before the response is returned to the worker.
- `resuming` counts as occupied capacity so a new READY task cannot steal a slot after a waiter starts reacquiring.
- Resident parked workers are bounded to one cohort of `MaxConcurrentWorkers`; total resident workers are bounded to `2 * MaxConcurrentWorkers`.
- If the parked cohort is already full, an additional waiter keeps its existing slot/lease rather than expanding resident-worker count without bound.

No task-state, attempt-authority, fencing, Web-turn binding, verification or integration semantics were weakened.

## Acceptance evidence

1. Deterministic daemon acceptance: with `MaxConcurrentWorkers=2`, two workers occupy both slots, both park, capacity/heavy claims fall to zero, and a third READY task is admitted. A parked waiter reports resumed only after its heavy claim is reacquired.
2. ProcessRunner acceptance: a durable response becoming available does not return to the worker while `Resume()` is blocked; it returns only after reacquire completes.
3. Real Windows sandbox E2E: two independent tasks reached `INPUT_REQUIRED` and yielded capacity; a third task submitted afterward also reached `INPUT_REQUIRED`. Responding to the first parked task produced the next exact Web turn and the requested workspace mutation under the same active epoch/attempt.
4. Existing single Web Brain E2E control remained PASS.
5. Full `go test ./internal/worker ./internal/orchestrator -count=1 -timeout=180s` PASS.
6. `go vet ./internal/worker ./internal/orchestrator ./cmd/mar` PASS.
7. `go build ./cmd/mar` to a non-canonical verification artifact PASS.

## Safety notes

A parked child is synchronously blocked on the parent RPC response and cannot receive the successful cognition response until capacity has been reacquired. Attempt heartbeats and cancellation monitoring continue while parked. Physical/logical mutation authority remains unchanged; this change only separates scarce compute admission from the lifetime of a durable external-cognition wait.

## Decision

`WEB_WAIT_CAPACITY_STARVATION_CLOSED`
