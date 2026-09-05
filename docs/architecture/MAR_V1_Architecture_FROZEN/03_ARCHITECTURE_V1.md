# MAR V1 — Architecture

**Status:** ARCHITECTURE FROZEN

## 1. Topology

```text
                    ChatWeb / MCP Clients
                            │
                            ▼
                      ┌───────────┐
                      │ MCP Edge  │
                      └─────┬─────┘
                            │
                            ▼
                 ┌────────────────────┐
                 │ Durable Orchestrator│
                 ├────────────────────┤
                 │ Task State          │
                 │ Scheduler           │
                 │ Project Coordinators      │
                 │ Resource Governor   │
                 │ Integration Manager │
                 └───────┬────────────┘
                         │
                ┌────────┴────────┐
                ▼                 ▼
         Context Services     Worker Supervisor
                                  │
                           ┌──────┴──────┐
                           ▼             ▼
                        Worker A      Worker B
                           │             │
                        Model↔Tools   Model↔Tools
                           │             │
                        Worktree A    Worktree B
                           │             │
                           └──────┬──────┘
                                  ▼
                              Repository
```

---

## 2. Architectural invariants

### I1 — MCP is control plane
The external MCP layer does not host the inner coding loop.

### I2 — Mutable task isolation
One mutable task owns one isolated workspace.

### I3 — Parallel execution, serialized integration
Tasks may execute concurrently; project integration has one authoritative writer.

### I4 — Durable state
Task state outlives ChatWeb transport/session and individual worker processes.

### I5 — Revision-bound evidence
Verification is valid only for the specific revision it checked.

### I6 — Resource-aware admission
Dispatch decisions consider workload, CPU, RAM, I/O and project fairness.

### I7 — Goal immutability
Workers may change implementation, never task intent/acceptance/boundaries.

### I8 — Bounded public tool surface
Outer MCP does not expose unrestricted machine mutation primitives by default.

### I9 — Process-tree ownership
Every spawned process belongs to an owning task lifecycle.

### I10 — Deterministic workspace disposition
Every workspace must eventually be retained for a bounded reason, recycled safely, or destroyed.

### I11 — Execution fencing
Every mutable execution belongs to a durable `attempt_id` and monotonically increasing `run_epoch`.

`run_epoch` provides **logical authority fencing** for MAR-mediated tool dispatch, callbacks, checkpoints, lifecycle transitions and delayed events.

It does **not** by itself revoke physical filesystem write capability from a stale OS process.

For replacement mutable execution on the same workspace, MAR must not activate a higher epoch until the prior attempt's entire mutation-capable process tree is confirmed terminated, or OS enforcement has conclusively revoked that tree's write authority to the active workspace.

If neither can be proven, recovery blocks.

### I12 — Effect reconciliation
A side effect with uncertain crash/retry outcome must be observed/reconciled before it can be repeated.

### I13 — Crash-safe integration
Integration advances the authoritative project head only through a durable integration attempt using an `expected_head` precondition.

### I14 — Hard resource envelope
MAR must apply backpressure before host CPU/RAM/disk/process safety reserves are exhausted.

### I15 — Single coordination truth
SQLite-backed Orchestrator state is the only durable coordination authority. Project Coordinators and Integration Manager state are reconstructable projections/serialization mechanisms, never independent sources of truth.

### I16 — Semantic uncertainty blocks
If compatibility of a parallel integration candidate cannot be established with sufficient confidence, MAR returns `REVIEW_REQUIRED/BLOCKED` rather than silently integrating.

---

## 3. MCP Edge

Responsibilities:

- MCP protocol handling;
- authentication/connection identity;
- request validation;
- idempotency;
- translation into durable internal commands;
- response/status projection.

It must not:

- own long-running task truth;
- keep essential state only in transport memory;
- execute arbitrary shell commands;
- become the scheduler.

---

## 4. Orchestrator

The orchestrator is the single authoritative owner of runtime coordination state.

SQLite-backed durable state is the **only coordination source of truth**. Any in-memory project coordinator, scheduler cache, integration mutex, or worker-supervisor projection must be reconstructable from durable state + observable machine/repository state.

Responsibilities:

- task lifecycle;
- project queues;
- resource admission;
- dependency tracking;
- impact conflict handling;
- retry policy;
- recovery;
- integration queue;
- cleanup eligibility.

The orchestrator persists durable state before side effects where practical.

---

## 5. Project Coordinator

One lightweight, evictable in-memory coordinator per active project.

Responsibilities:

- project queue projection;
- project execution-policy cache;
- base-revision projection;
- impact-map cache;
- workspace-pool handles;
- verification-profile cache;
- integration serialization;
- per-project resource accounting.

A Project Coordinator is **not** an actor-system requirement and must not contain unreconstructable truth.

Inactive coordinators are evictable and rebuilt from SQLite + Git/project state.

---

## 6. Worker Supervisor

Creates and owns worker process trees.

Responsibilities:

- sandbox/process setup;
- resource assignment;
- worker lifecycle;
- graceful stop;
- forced kill;
- crash detection;
- state reconciliation.

A worker crash must not corrupt global orchestration state.

Before dispatch, Worker Supervisor receives the task's current `attempt_id/run_epoch`.

Every MAR-mediated mutation-producing tool dispatch and lifecycle transition must validate the expected epoch.

Recovery distinguishes:

1. **Logical fencing** — stale messages/tool dispatches/lifecycle events are rejected by epoch.
2. **Physical mutation authority** — stale OS processes may still hold file handles or continue already-dispatched work.

For a mutable task reusing the same workspace, replacement execution may become active only after the old attempt's entire mutation-capable process tree is confirmed terminated, or its workspace write authority is conclusively revoked at the OS boundary.

Merely declaring an old attempt non-authoritative in durable state is insufficient.

If termination/revocation cannot be proven, the task enters `BLOCKED / RECOVERY_REQUIRED` and no replacement mutable worker is started.

---

## 7. Agent Worker

The Agent Worker is the Codex-like runtime loop.

Core components:

- model adapter;
- prompt/context manager;
- planner;
- tool dispatcher;
- filesystem/search tools;
- patch/edit engine;
- command/test runner;
- verification helper;
- checkpoint manager;
- token/context budget manager;
- completion evaluator.

The agent loop is local to the worker runtime.

---

## 8. Model Gateway

Model access is abstracted from task orchestration.

A model profile should pin, for a task phase:

- provider;
- model;
- tool schema;
- base instructions;
- sandbox profile;
- workspace identity.

Avoid unnecessary model switching inside one continuous phase because it weakens context/prompt-cache efficiency and behavior consistency.

V1 may support one provider first while retaining a provider-neutral interface.

---

## 9. Durable state

Recommended V1 store: SQLite in WAL mode.

Store metadata only:

- tasks;
- task state;
- projects;
- workspaces;
- checkpoints;
- dependencies;
- integration queue;
- resource accounting;
- artifact pointers.

Large logs/transcripts belong in filesystem-backed append-only files.

---

## 10. Context architecture

V1 uses layered retrieval:

```text
task intent
   ↓
repo metadata / git
   ↓
lexical search
   ↓
symbol/dependency analysis
   ↓
optional semantic retrieval
   ↓
context pack
```

Do not require a global always-loaded vector database.

Indexes should be:

- content-hash reusable;
- incremental;
- lazy-loaded;
- shareable across worktrees where content is identical.

---

## 11. Workspace architecture

Mutable tasks use Git worktrees sharing one object database.

Read-only tasks may use:

- base repository snapshot;
- immutable index;
- no private worktree until mutation is required.

No two mutable tasks may share the same mutable filesystem.

---

## 11A. Side-effect protocol

MAR classifies effects into:

1. recoverable/observable local effects;
2. process effects;
3. integration effects;
4. external/non-idempotent effects.

For effects whose crash outcome can be ambiguous, MAR persists an `EffectIntent`:

```text
PREPARED
   ↓
DISPATCHED
   ↓
OBSERVED
```

If recovery finds `DISPATCHED` without durable observation, it inspects the actual world (Git/files/process/external broker state) before retry.

Generic exactly-once execution is not assumed.

---

## 12. Integration architecture

Workers never directly write authoritative integration state.

Verified outputs enter an integration lane.

Integration manager uses a durable `integration_attempt`:

Required fields:

- `integration_attempt_id`
- `task_id`
- `expected_head`
- `task_result_revision`
- `candidate_revision`
- `evidence_id`
- `status`

Protocol:

```text
PREPARE candidate from expected_head
        ↓
VERIFY candidate
        ↓
actual head == expected_head ?
   NO → invalidate evidence / rebuild
   YES
        ↓
CAS_ADVANCE(expected_head → candidate)
        ↓
durably finalize integration result
```

Recovery compares durable attempt state with observable Git truth.

If actual head equals `candidate_revision` after a crash, MAR may finalize/reverify as required.

If actual head is neither `expected_head` nor `candidate_revision`, integration enters deterministic reconciliation/block.

Only the integration authority may advance authoritative project refs.

---


## 12A. Worker authority boundary

A Git worktree is isolation for mutable source state; it is **not** a security sandbox.

The V1 worker authority contract is:

- read: registered project/worktree plus explicitly granted runtime paths;
- write: task workspace and task-owned runtime/artifact paths;
- task-local Git operations: allowed through bounded tooling;
- authoritative integration refs: worker denied;
- Git push: denied by default;
- deploy: denied;
- secrets: not generally readable; brokered capability preferred;
- network: denied or explicitly restricted by sandbox profile where enforceable;
- child processes: inherit task process/resource authority and must remain owned by the task.

The exact Windows implementation may evolve, but the enforcement boundary may not depend on model obedience.

Worker authority must be strictly weaker than MAR daemon authority.

---

## 13. Deployment boundary

MAR V1 assumes:

> one trusted OS user on one host.

Multiple clients, projects and tasks all act for the same owner.

MAR V1 is not a hostile multi-user isolation system.
