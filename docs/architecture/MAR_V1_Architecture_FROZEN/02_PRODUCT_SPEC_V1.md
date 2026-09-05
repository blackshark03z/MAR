# MAR V1 — Product Specification

**Status:** ARCHITECTURE FROZEN

## 1. Scope

MAR V1 is a **single-owner, single-host autonomous coding runtime exposed through MCP**.

It supports:

- multiple MCP clients/tabs;
- multiple local projects;
- multiple concurrent tasks;
- autonomous model/tool loops;
- durable task state;
- isolated mutable workspaces;
- local Git operations;
- test/build/verification workflows;
- task steering and cancellation;
- recovery after process/runtime interruption;
- CPU/RAM/process/disk governance;
- serialized project integration.

---

## 2. Non-goals

V1 does not implement:

- multiple human users;
- organizations;
- RBAC;
- SaaS tenant isolation;
- cloud worker pools;
- managed billing;
- deployment automation;
- unrestricted remote shell;
- global vector database;
- generalized swarm orchestration;
- plugin marketplace;
- automatic push/merge/deploy by default.

---

## 3. Core entities

### Project
A registered local repository with execution policy, resource profile and verification profile.

### Goal Contract
Immutable task intent.

Required fields:

- `goal`
- `acceptance`
- `boundaries`
- `non_goals`
- `project_id`
- `base_revision`
- `authority`
- `verification_profile`
- `priority`
- `contract_hash`

### Task
Durable execution instance created from one Goal Contract.

### Workspace
Execution filesystem assigned to a task.

Mutable task → isolated Git worktree.

Read-only task → may use shared immutable project snapshot.

### Worker
Autonomous agent runtime executing the inner model/tool loop.

### Execution Attempt
One durable execution generation of a task.

Required identity:

- `attempt_id`
- `run_epoch` (monotonically increasing per task)
- `worker_id`
- `supervisor_id`
- `started_at`
- `heartbeat_at`
- `lease_deadline`
- `attempt_state`

Only the execution attempt matching the task's current `run_epoch` may perform MAR-mediated mutation-producing actions or lifecycle transitions.

A stale attempt is logically fenced even if its OS process is still alive.

**However, logical fencing does not revoke physical filesystem write capability.**

For a mutable task whose replacement attempt would use the same mutable workspace, a higher `run_epoch` may not begin mutable execution until the previous attempt's entire mutation-capable process tree is confirmed terminated, or OS enforcement has conclusively revoked its write authority to that workspace.

If neither condition can be proven, task recovery enters `BLOCKED / RECOVERY_REQUIRED`.

### Effect Intent
Durable identity for a mutation or non-trivial side effect whose outcome may become uncertain across crash/retry.

Minimum fields:

- `operation_id`
- `task_id`
- `attempt_id`
- `run_epoch`
- `effect_type`
- `expected_precondition`
- `effect_state`
- `observed_result`

Effect states:

`PREPARED → DISPATCHED → OBSERVED`

MAR does not promise generic exactly-once execution. It promises deterministic observation/reconciliation before retry when an outcome may be uncertain.

### Evidence
Revision-bound proof that acceptance conditions were checked.

### Integration Item
Verified task result waiting to be reconciled with the project integration branch.

---

## 4. MCP control surface

The public ChatWeb-facing surface should remain small.

Required operations:

- `submit`
- `status`
- `steer`
- `input`
- `cancel`
- `result`
- `inspect`

ChatWeb must not need direct `read_file`, `write_file`, or unrestricted `run_shell` calls for normal autonomous coding workflow.

Low-level primitives belong inside the worker runtime.

---

## 5. Required task states

```text
SUBMITTED
PREFLIGHT
WAITING_RESOURCE
WORKSPACE_READY
RUNNING
VERIFYING
REVIEWING
READY_TO_INTEGRATE
INTEGRATING
VERIFIED
COMPLETE
```

Exceptional states:

```text
INPUT_REQUIRED
BLOCKED
RETRY_WAIT
FAILED
CANCELLED
```

Task state must be durable.

State semantics:

- `VERIFIED` means the task candidate satisfies its technical verification contract for a specific evidence identity.
- `READY_TO_INTEGRATE` means the verified candidate is eligible for project integration.
- `INTEGRATING` means a durable integration attempt is active.
- `COMPLETE` means MAR has no remaining runtime work for the task. Integration status is reported separately and must never be inferred from `COMPLETE` alone.


---

## 6. Submit behavior

A valid `submit` must:

1. identify the project;
2. validate Goal Contract;
3. verify base revision;
4. assign an idempotency key;
5. persist task before execution;
6. perform impact/preflight analysis;
7. calculate initial resource class;
8. either dispatch or enter `WAITING_RESOURCE`.

A retry of the same idempotent submission must not create duplicate work.

---

## 7. Autonomous execution behavior

Once dispatched, a worker may autonomously:

- inspect repository context;
- search code;
- read files;
- edit files;
- run allowed commands;
- run tests/builds;
- inspect failures;
- repair;
- checkpoint;
- self-review.

The worker must not:

- change the Goal Contract;
- widen project authority;
- push/deploy by default;
- mutate unrelated projects;
- bypass resource or security policy.

---

## 8. Steering

A client may steer an active task.

Steering may:

- add factual context;
- clarify priority;
- choose among explicitly blocked alternatives;
- request cancellation;
- request additional verification.

Steering must not silently rewrite the original Goal Contract.

If steering materially changes goal/acceptance/boundary, MAR creates a **new task** with an immutable Goal Contract linked by `supersedes`.

The superseded task remains preserved for provenance but becomes ineligible for integration unless explicitly reauthorized.

---

## 9. Disconnect behavior

Loss of ChatWeb connection must not terminate an admitted autonomous task.

The task continues while:

- MAR daemon is running;
- required model/provider remains available;
- local machine remains powered;
- task is not blocked for user input.

---

## 10. Restart behavior

After MAR daemon restart:

- durable tasks are reconstructed;
- running tasks are reconciled against actual process/workspace state;
- clean resumable tasks continue from semantic checkpoint;
- uncertain tasks enter a recovery/block state rather than blindly continuing;
- an old execution attempt must be logically fenced;
- for mutable reuse of the same workspace, every previous mutation-capable process must also be confirmed terminated (or its workspace write authority conclusively revoked by OS enforcement) before replacement mutable execution is admitted;
- if physical mutation authority cannot be disproven, recovery blocks rather than starting a second writer.

---


## 10A. Side-effect recovery policy

All mutation-producing effects are classified before execution:

### Recoverable/observable local effect
Examples: file mutation, task-local Git working state, local test artifacts.

Recovery inspects actual state before deciding whether retry is safe.

### Process effect
Owned by exactly one execution attempt and its process container.

### Integration effect
Must use a durable integration attempt with expected-head validation.

### External/non-idempotent effect
Denied by default in V1 or executed only through an explicitly brokered/idempotent capability.

A crash between dispatch and durable observation must produce an `UNKNOWN/RECONCILE_REQUIRED` condition, never an automatic blind retry.

## 11. Cancellation behavior

Cancellation must:

1. stop new agent actions;
2. request graceful interruption;
3. terminate the entire task process tree after timeout;
4. persist final task state;
5. retain sufficient evidence/logs for diagnosis;
6. leave workspace in a known recoverable or disposable state.

No orphan child processes are acceptable.

---

## 12. Completion behavior

A task cannot reach `COMPLETE` solely because the model says “done”.

Completion requires:

- acceptance evaluation;
- verification result;
- final revision identification;
- evidence manifest bound to `revision + verification_profile_hash + relevant environment/toolchain identity`;
- remaining-risk declaration;
- clean task process state.

---

## 13. Result contract

A result should include:

- task ID;
- Goal Contract hash;
- base revision;
- final task revision;
- files/areas changed;
- verification executed;
- pass/fail evidence;
- unresolved risks;
- integration status;
- workspace disposition;
- resource summary;
- verdict.

---

## 14. Multi-client semantics

Multiple ChatWeb tabs may observe or steer the same task.

A browser tab is not task ownership.

Task identity is explicit and independent from transport/session identity.

---

## 15. Multi-project semantics

Inactive projects should consume minimal resident memory.

Project state and indexes are loaded lazily.

Global scheduling must prevent one project from starving others.

---

## 16. Definition of Done for V1

V1 is not accepted until the benchmark plan in `07_ACCEPTANCE_BENCHMARK_PLAN.md` passes.

Feature count is not a completion metric.
