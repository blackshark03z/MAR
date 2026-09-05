# MAR V1 — Execution & Concurrency Model

## 1. Core rule

> One mutable task = one isolated workspace.

This is the primary concurrency safety rule.

---

## 2. Types of concurrency

MAR distinguishes:

### Agent concurrency
Number of logically active autonomous tasks.

### Compute concurrency
Number of tasks simultaneously consuming substantial local CPU/RAM/I/O.

These numbers are intentionally different.

Example:

```text
8 active agent tasks
2 heavy local compute jobs
```

Tasks waiting for remote model inference need little local compute and should not occupy a heavy-compute slot.

---

## 3. Read-only concurrency

Read-only work may share:

- project base revision;
- immutable file content;
- Git object database;
- context index;
- symbol graph;
- dependency cache.

Read-only work should be cheap and highly parallel.

---

## 4. Mutable concurrency

Each mutable task gets:

- dedicated worktree;
- dedicated mutable temp/build area;
- task-scoped process tree;
- task-scoped logs;
- task-scoped checkpoint.

Shared immutable caches remain allowed.

---


## 4A. Replacement-attempt physical fencing

Worktree isolation protects different tasks. Recovery of the **same mutable task/workspace** requires an additional rule.

A replacement attempt must not become mutation-capable while any previous attempt or child process can still write to that workspace.

Required sequence:

```text
mark old attempt logically stale
        ↓
stop new MAR-mediated actions
        ↓
terminate old mutation-capable process tree
        ↓
confirm termination
        ↓
advance/admit replacement epoch
        ↓
start replacement mutable execution
```

If termination cannot be confirmed:

`BLOCKED / RECOVERY_REQUIRED`

V1 does not rely on logical `run_epoch` fencing as a substitute for physical writer removal.

A future implementation may allow a stale process to remain alive only if OS enforcement can conclusively revoke all write authority to the active workspace; V1 need not support that optimization.

---

## 5. Impact analysis

Worktrees solve physical file collision, not semantic conflict.

Before dispatch, MAR should estimate impacted:

- modules;
- symbols;
- interfaces;
- schemas;
- public APIs;
- configuration;
- tests.

Tasks with strong overlapping impact may be:

- serialized;
- dependency-linked;
- marked competing intentionally;
- allowed in parallel with mandatory reconciliation.

Impact analysis is advisory/scheduling input, not a claim that semantic conflict can be perfectly predicted.

Compatibility outcomes are:

- `COMPATIBLE`
- `INCOMPATIBLE`
- `UNCERTAIN`

`UNCERTAIN` must block or require review; it must never silently auto-integrate.

---

## 6. Base drift

Every task records a base revision.

If integration base changes before task integration:

1. task result is considered stale relative to integration head;
2. merge/rebase/cherry-pick is attempted in controlled integration;
3. affected assumptions and tests are re-evaluated;
4. invalidated evidence is discarded;
5. repair may be required.

“Tests passed on old base” is not sufficient.

---

## 7. Single integration writer

Single-writer serialization is necessary but not sufficient.

Every integration candidate is processed through a durable integration attempt with:

- `expected_head`;
- candidate revision;
- current evidence identity;
- compare-and-advance validation;
- crash recovery state.



For each project:

> exactly one integration authority mutates the authoritative integration branch/state at a time.

Workers may commit locally within their worktrees.

Workers do not bypass the integration lane.

---

## 8. Multi-client behavior

Multiple clients may submit independent tasks.

Clients do not own filesystem sessions.

```text
Tab A → Task 1
Tab B → Task 2
Phone → Task 1 status
```

Transport loss does not change task ownership.

---

## 9. Duplicate submission protection

Every submission receives or derives an idempotency key.

The runtime must distinguish:

- retry of the same submit;
- intentional duplicate/competing task.

---

## 10. Task dependencies

A task may declare:

- `depends_on`;
- `blocks`;
- `supersedes`;
- `competes_with`.

The scheduler must not dispatch a task whose required dependency is unresolved.

---

## 11. Multi-project fairness

One project cannot consume all heavy worker capacity.

Global scheduling should enforce:

- per-project concurrency caps;
- fair access to heavy slots;
- priority without starvation;
- explicit starvation prevention such as aging/fair-share/deficit accounting.

---

## 12. Workspace cleanup

Task workspace lifecycle:

```text
CREATE
→ EXECUTE
→ VERIFY
→ INTEGRATE or RETAIN_FOR_REVIEW
→ RETENTION WINDOW
→ DESTROY / SAFE RECYCLE
```

A resumable or unreviewed workspace cannot be silently removed.

A completed integrated workspace cannot be retained indefinitely without an explicit retention reason.

Retention is bounded by both **time** and **aggregate storage budget**.

MAR must preserve a configured host free-space reserve and deny/stop further growth before that reserve is threatened.

---

## 13. Warm worktree pool

Optional after correctness is proven.

A project may keep a small number of clean reusable worktrees.

Recycle requires proof of:

- clean Git state;
- no active process;
- no task-owned temp state;
- expected base revision/reset.

Uncertain workspace → destroy, do not recycle.

---

## 14. Concurrency anti-patterns

Forbidden:

- multiple writers on one working tree;
- task identity tied to browser tab;
- worker directly merging main;
- two mutation-capable execution attempts for the same mutable task workspace;
- “max concurrent tasks = N” as the only resource policy;
- treating atomic file write as semantic concurrency safety;
- reusing stale test evidence after base drift.


---

## 15. Goal supersession

A material change to Goal/Acceptance/Boundary never mutates an executing task in place.

It creates a new immutable task linked by:

`new_task.supersedes = old_task_id`

The superseded task remains available for provenance but is not integration-eligible unless explicitly reauthorized.
