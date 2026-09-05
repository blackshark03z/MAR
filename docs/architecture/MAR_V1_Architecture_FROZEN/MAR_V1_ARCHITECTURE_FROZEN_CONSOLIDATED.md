# MAR V1 — ARCHITECTURE FROZEN



---

## Source: `00_ARCHITECTURE_FREEZE_RECORD.md`

# MAR V1 — Architecture Freeze Record

**Freeze status:** `ARCHITECTURE_FROZEN`  
**Frozen baseline:** MAR V1 Architecture Freeze Candidate R3  
**Final independent review verdict:** `ARCHITECTURE_READY_FOR_FREEZE`  
**Remaining blockers:** `NONE`  
**Contradictions:** `NONE`  
**Freeze decision:** `YES`

---

## 1. Frozen product boundary

MAR V1 is:

> A single-owner, single-machine, MCP-native autonomous coding runtime that supports multiple clients, projects and concurrent tasks while keeping the inner model↔tool loop local to the runtime.

MAR V1 is not a multi-user SaaS, distributed workflow platform, generic remote shell, cloud worker fabric or agent-swarm framework.

---

## 2. Frozen architectural topology

```text
ChatWeb / MCP Client
        ↓
Stable MCP Control Plane
        ↓
Durable Orchestrator
        ↓
Project Coordinator + Resource Governor
        ↓
Autonomous Agent Worker
        ↓
Isolated Mutable Worktree
        ↓
Verification
        ↓
Serialized Crash-Safe Integration
        ↓
Repository
```

---

## 3. Frozen invariants

The architecture is considered correct only while these remain true:

1. MCP is the control plane, not the inner coding loop.
2. One mutable task owns one isolated mutable workspace.
3. Parallel execution is allowed; authoritative project integration is serialized.
4. Task truth is durable and independent of ChatWeb transport/session.
5. Verification evidence is bound to the exact verified revision and relevant verification environment/profile identity.
6. Resource admission is based on workload plus CPU/RAM/disk/process pressure.
7. Goal Contract is immutable; material change creates a superseding task.
8. Public MCP surface remains bounded and task-oriented.
9. Every spawned process tree belongs to one task lifecycle.
10. Workspace retention/cleanup is deterministic and bounded.
11. `run_epoch` provides logical fencing only.
12. Replacement mutable execution on the same workspace cannot start until every prior mutation-capable process tree is confirmed terminated, or OS-level write authority is conclusively revoked; otherwise recovery blocks.
13. Effects with uncertain crash outcomes must be observed/reconciled before retry.
14. Integration uses a durable attempt with `expected_head` and compare-and-advance/CAS semantics.
15. MAR applies backpressure before host CPU/RAM/disk/process safety reserves are exhausted.
16. SQLite-backed Orchestrator state is the only durable coordination truth.
17. Semantic compatibility may be `COMPATIBLE`, `INCOMPATIBLE`, or `UNCERTAIN`; `UNCERTAIN` blocks/requires review.

---

## 4. Architecture reopen rule

Architecture is **closed**.

It may be reopened only if implementation or benchmark evidence proves at least one frozen invariant false or insufficient in a way that cannot be corrected locally.

Valid reopen evidence includes:

- lost work;
- duplicate physical mutation;
- stale worker mutation;
- unrecoverable crash ambiguity;
- false completion;
- unsafe authority escape;
- incorrect parallel integration;
- unbounded host resource consumption;
- structural inability to sustain the required autonomous coding loop.

The following do **not** reopen architecture by themselves:

- search latency;
- prompt quality;
- model quality;
- token cost;
- UI quality;
- test-selection tuning;
- context-ranking quality;
- worktree startup time;
- ordinary implementation defects;
- provider choice;
- parser/index optimization.

These belong to implementation/optimization.

---

## 5. Next phase

The next phase is:

`IMPLEMENTATION → T1–T17 BENCHMARK/ACCEPTANCE`

No additional broad architecture research is authorized before implementation.

The benchmark plan is the evidence gate for whether implementation satisfies the frozen architecture.

---

## 6. Review history summary

### R1
Verdict: `ARCHITECTURE_READY_WITH_REQUIRED_CORRECTIONS`

Required corrections:
- execution fencing/recovery transaction;
- enforceable autonomous-shell boundary;
- crash-safe integration transaction;
- hard resource/disk bounds.

### R2
Verdict: `ARCHITECTURE_NOT_READY`

Remaining blocker:
- logical `run_epoch` fencing did not itself revoke physical write capability of a stale OS process sharing the same mutable workspace.

### R3
Correction:
- logical fencing explicitly separated from physical mutation authority;
- same-workspace replacement requires kill-and-confirm or conclusive OS-level write revocation;
- otherwise recovery blocks;
- T14 strengthened to test physical mutation isolation.

Final verdict:
`ARCHITECTURE_READY_FOR_FREEZE`

---

## 7. Canonical documents

The frozen architecture is defined by:

- `01_PRODUCT_BRIEF.md`
- `02_PRODUCT_SPEC_V1.md`
- `03_ARCHITECTURE_V1.md`
- `04_EXECUTION_CONCURRENCY_MODEL.md`
- `05_RESOURCE_RELIABILITY_MODEL.md`
- `06_SECURITY_TRUST_MODEL.md`
- `07_ACCEPTANCE_BENCHMARK_PLAN.md`
- `08_ARCHITECTURE_DECISIONS.md`
- `09_RISKS_AND_OPEN_QUESTIONS.md`

This Freeze Record controls interpretation when review-history wording differs from the final frozen state.


---

## Source: `01_PRODUCT_BRIEF.md`

# MAR V1 — Product Brief

## 1. Problem

ChatWeb models can reason well about software, but naïve MCP integration creates a slow and fragile loop:

`ChatWeb → MCP call → repository → MCP result → ChatWeb reasoning → repeat`

For non-trivial coding tasks this causes:

- excessive network/tool round trips;
- dependence on an open browser conversation;
- poor long-task durability;
- weak recovery after disconnect;
- unsafe same-worktree concurrency;
- uncontrolled CPU/RAM/process growth;
- duplicated work across tabs;
- semantic conflicts between parallel tasks;
- repository/worktree bloat;
- too much human forwarding between Tech Lead and coding worker.

Native coding agents avoid much of this by keeping the inner model↔tool loop close to the repository.

---

## 2. Product thesis

MAR moves the autonomous agent loop behind MCP.

ChatWeb should submit a high-level bounded Goal, not remote-control individual files.

```text
ChatWeb
   ↓
MCP Control Plane
   ↓
MAR Durable Runtime
   ↓
Autonomous Agent Loop
   ↕
Filesystem / Shell / Git / Tests
   ↓
Verified Result
```

The external MCP conversation may disconnect without invalidating the task.

---

## 3. Primary user

One developer operating one workstation.

The same owner may use:

- multiple ChatWeb tabs;
- multiple MCP-capable clients;
- multiple local projects;
- multiple concurrent autonomous tasks.

All clients act on behalf of the same trusted OS user.

---

## 4. Primary job-to-be-done

> “I give MAR a bounded software goal with acceptance criteria. MAR executes autonomously against the correct repository, verifies the result, survives interruptions, and returns evidence without requiring me to micromanage the coding loop.”

---

## 5. Core user experience

1. User discusses intent with ChatWeb.
2. ChatWeb produces a Goal Contract.
3. ChatWeb submits the Goal through MCP.
4. MAR performs preflight and resource admission.
5. MAR creates or assigns an isolated workspace.
6. Local agent worker executes the model↔tool loop.
7. MAR runs verification.
8. MAR produces a result with revision-bound evidence.
9. For parallel work, MAR serializes integration.
10. ChatWeb reviews the result and escalates only when human judgment is needed.

---

## 6. Product principles

### P1 — Goal-level control
ChatWeb operates at goal/decision level, not file-operation level.

### P2 — Durable autonomy
Task state survives browser disconnect and runtime restart.

### P3 — Isolation before parallelism
Parallel mutable tasks never share the same mutable workspace.

### P4 — Verification over self-assertion
“Done” is based on evidence, not only model output.

### P5 — Bounded machine impact
MAR must preserve workstation usability and prevent runaway process, RAM, CPU and disk growth.

### P6 — Simple local product
V1 optimizes for one owner and one machine. Multi-user SaaS concerns are excluded.

### P7 — Architecture stops when acceptance is met
No open-ended feature accumulation.

---

## 7. Success definition

MAR V1 succeeds when it can complete representative coding tasks with:

- minimal external MCP turns;
- no lost updates;
- no orphan processes;
- correct recovery;
- bounded resource usage;
- clean worktree lifecycle;
- revision-bound verification;
- low human intervention;
- acceptance quality competitive with a native autonomous coding workflow.


---

## Source: `02_PRODUCT_SPEC_V1.md`

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


---

## Source: `03_ARCHITECTURE_V1.md`

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


---

## Source: `04_EXECUTION_CONCURRENCY_MODEL.md`

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


---

## Source: `05_RESOURCE_RELIABILITY_MODEL.md`

# MAR V1 — Resource & Reliability Model

## 1. Goal

MAR must maximize autonomous throughput **without making the workstation unusable**.

---

## 2. Resource classes

Example workload classes:

| Workload | CPU | RAM | I/O | Parallelism |
|---|---:|---:|---:|---|
| Search/read/context | low | low | medium | high |
| Waiting remote model | very low | low | low | high |
| Static analysis | medium | medium | medium | moderate |
| Unit test | medium | medium | medium | moderate |
| Build | high | high | high | limited |
| Browser/UI runtime | medium | high | medium | limited |
| Integration test | high | high | high | 1–2 |

Scheduler policy uses class + live machine state.

---

## 3. Resource governor inputs

Required signals:

- available RAM;
- memory pressure;
- CPU utilization;
- user-interactive load;
- I/O pressure if available;
- number of active heavy jobs;
- per-project usage;
- task priority;
- host free-disk reserve;
- total MAR disk consumption;
- task/workspace/log/artifact/cache consumption.

---

## 4. Admission rules

Examples:

### High memory pressure
- block new heavy jobs;
- evict optional context caches;
- pause background indexing;
- allow lightweight status/model-wait tasks.

### CPU saturation
- reduce background worker priority;
- allow network/model-waiting tasks;
- postpone new builds/tests.


### Disk reserve threatened
- deny new disk-producing/heavy jobs;
- stop optional indexing/cache growth;
- rotate/truncate bounded logs according to policy;
- request graceful stop for task growth;
- terminate/block a task whose uncontrolled output threatens the host reserve.

### User actively using machine
- preserve interactive responsiveness;
- lower background compute weight.

### Idle machine
- permit higher background throughput within hard limits.

---


## 4A. Hard resource envelope

Configuration defines:

- minimum host free-space reserve;
- maximum total MAR disk budget;
- per-task/workspace output budget;
- log/artifact/cache budgets;
- maximum active heavy-process capacity;
- bounded in-memory log/output buffers.

Exact numeric thresholds are configuration/benchmark decisions.

The architectural invariant is that MAR applies backpressure **before** host exhaustion, not only cleanup afterward.

---

## 5. OS-level process governance

Each task owns its process tree.

Windows target:
- Windows Job Objects;
- process-group termination;
- CPU rate/priority control;
- job/process memory limits where appropriate.

Linux target (future/portable implementation):
- cgroups v2.

Application-level accounting alone is not enough for runaway child processes.

---

## 6. Cancellation

Cancellation sequence:

1. task state → cancelling;
2. stop new model/tool dispatch;
3. graceful interrupt;
4. bounded grace period;
5. kill entire process tree;
6. confirm termination;
7. persist checkpoint/evidence;
8. finalize cancelled state.

No orphan processes.

---

## 7. Worker crash

Worker crash must not imply task loss.

Recovery:

1. supervisor detects exit/lease failure;
2. task enters reconciliation;
3. inspect workspace/Git/process state;
4. verify current `attempt_id/run_epoch`;
5. logically fence the old attempt so stale MAR-mediated events are rejected;
6. identify the old attempt's entire mutation-capable process tree;
7. request graceful termination, then force termination as required;
8. confirm no previous mutation-capable process remains able to write the task workspace;
9. only then atomically advance/admit the replacement `run_epoch`;
10. load last valid semantic checkpoint;
11. resume if safe;
12. otherwise enter `BLOCKED`.

If step 8 cannot be proven, MAR must not start a replacement mutable attempt on that workspace.

---

## 8. MAR daemon crash/restart

Durable source of truth is not in-memory scheduler state.

On startup:

1. load durable tasks;
2. reconcile actual workspaces;
3. reconcile process ownership;
4. mark impossible stale running states;
5. reconcile lingering worker/process identity against attempt epochs;
6. logically fence stale attempts;
7. for any mutable workspace that may be reused, terminate and confirm absence of all stale mutation-capable process trees before replacement dispatch;
8. if this physical condition cannot be proven, mark task `BLOCKED / RECOVERY_REQUIRED`;
9. resume only safe tasks;
10. surface uncertain tasks.

---

## 9. Machine restart

Machine power-off stops local execution.

After restart MAR may resume from durable state.

V1 does not promise continued execution while host is powered off.

---

## 10. Checkpoint model

Checkpoint is semantic, not full transcript.

Minimum:

- Goal Contract hash;
- base revision;
- current revision;
- completed work;
- current hypothesis;
- changed areas;
- verification status;
- blockers;
- remaining work;
- next action;
- critical evidence references.

Full transcript/logs remain external artifacts.

Checkpoint snapshots are immutable/versioned, written atomically, and include an integrity hash/checksum. Recovery uses the latest valid checkpoint; corrupted snapshots never override durable task truth.

---

## 11. Log memory policy

Never retain unbounded stdout/model transcript in RAM.

Use:

- small in-memory ring buffer;
- streamed append-only log files;
- compressed archival where useful;
- selective error extraction for context.

---

## 12. Disk governance

Track:

- worktree size;
- temp/build output;
- log size;
- artifact retention;
- cache size.

Cleanup must be lifecycle-aware, not blind age deletion.

Shared immutable caches are preferred.

---

## 13. Reliability invariants

- transport disconnect does not lose task;
- worker crash does not lose task definition;
- daemon restart does not invent task completion;
- cancellation leaves no process tree running;
- recovery never assumes uncertain side effects are safe;
- resource pressure blocks dispatch before machine instability.


---

## 14. Worker cognition vs durable recovery state

MAR keeps four distinct state layers:

1. immutable Goal Contract;
2. live worker conversation/context;
3. append-only transcript/tool artifacts;
4. periodic semantic checkpoints.

Normal status/resume/steer operations use bounded snapshots and recent context, not full-history replay.

After worker crash, MAR may construct a new model session from Goal + latest valid checkpoint + selected evidence rather than replaying the entire transcript.


---

## 15. Logical fencing vs physical mutation authority

MAR treats these as separate properties.

### Logical authority
Controlled by `attempt_id/run_epoch`.

Protects:
- delayed callbacks;
- stale tool dispatches through MAR;
- stale checkpoints;
- late completion;
- lifecycle CAS races.

### Physical mutation authority
Controlled by OS process termination or enforceable workspace write revocation.

Protects:
- already-running shell commands;
- build/test child processes;
- Python/Node/FFmpeg/browser children;
- direct file writes or open file handles.

For same-workspace mutable recovery, **both conditions must be safe before replacement execution begins**.

If physical write authority cannot be disproven, MAR blocks recovery.


---

## Source: `06_SECURITY_TRUST_MODEL.md`

# MAR V1 — Security & Trust Model

## 1. Trust boundary

MAR V1 assumes one trusted OS user on one host.

All registered clients act on behalf of the same machine owner.

This is not a hostile multi-user isolation boundary.

---

## 2. Security objective

Even under a trusted owner, model-generated actions must remain bounded.

Threats include:

- prompt injection from repository content;
- accidental destructive commands;
- secret exfiltration;
- unrelated filesystem mutation;
- remote Git side effects;
- runaway process creation;
- network abuse.

---

## 3. Separation of identity and authority

Connection identity answers:

> Who/what client is calling?

Task authority answers:

> What may this task do?

Connection success must not imply unrestricted machine authority.

---

## 4. Outer MCP policy

Normal ChatWeb interface exposes high-level operations only:

- submit;
- status;
- steer;
- input;
- cancel;
- result;
- inspect.

Do not expose unrestricted filesystem/shell mutation as the default public surface.

---

## 5. Task capability scope

Each task receives explicit scope, e.g.:

- project root/worktree only;
- local file mutation allowed;
- command execution sandboxed;
- local Git write allowed;
- Git push denied;
- deploy denied;
- network denied/default restricted;
- secret access none unless brokered.

---

## 6. Worker sandbox

A task worktree is not a security sandbox.

The V1 sandbox/authority model must enforce, outside model obedience:

- read scope;
- write scope;
- process-tree ownership;
- Git authority;
- network profile;
- secret access policy.

Child processes inherit the task's process/resource authority.

Task-local Git operations must not imply authority to mutate project integration refs.



Worker-generated commands run in task-controlled execution environment.

The worker should not gain authority merely by writing an executable file.

Execution policy must prevent trivial write→execute privilege expansion outside the task scope.

---

## 7. Secrets

Do not expose long-lived secrets as broadly readable environment/files when avoidable.

Preferred model:

```text
Worker
  ↓ asks for named capability
Secret/Action Broker
  ↓ performs bounded action or injects short-lived credential
External service
```

V1 may initially support simple local credentials, but the architecture should avoid making unrestricted secret browsing part of worker tooling.

---

## 8. Git authority

Separate permissions conceptually:

- read repository;
- mutate task worktree;
- create local task commit;
- integrate locally;
- push remote;
- deploy.

V1 defaults:
- local mutation: allowed by project policy;
- task-local commits: allowed through bounded tooling;
- authoritative integration ref mutation by workers: denied;
- push: denied unless explicitly enabled;
- deploy: denied.

---

## 9. Prompt injection stance

Repository text is untrusted input to the model.

Security must not rely on “the model will ignore malicious instructions”.

Controls belong in:

- capability boundaries;
- sandbox;
- command policy;
- filesystem scope;
- network policy;
- secret separation.

---

## 10. Audit/evidence

Even single-user V1 should preserve task-local provenance:

- Goal Contract;
- model profile;
- commands;
- changed revisions;
- verification;
- integration result;
- approvals/steering events.

This is for debugging and accountability, not enterprise compliance.

---

## 11. Security non-goals

V1 does not claim:

- tenant isolation;
- malicious local-user isolation;
- enterprise RBAC;
- full zero-trust workstation security;
- guaranteed resistance to all supply-chain attacks.


---

## 12. Recovery-time workspace authority

Epoch/state fencing is not a filesystem security primitive.

When a mutable task is recovered into the same workspace, MAR must treat any still-running stale process tree as mutation-capable until termination or OS-enforced write revocation is proven.

V1 default policy is **kill-and-confirm before replacement**.

A stale process being logically non-authoritative is insufficient to permit a new writer on the same mutable workspace.


---

## Source: `07_ACCEPTANCE_BENCHMARK_PLAN.md`

# MAR V1 — Acceptance & Benchmark Plan

## 1. Principle

MAR is accepted by **end-to-end outcome**, not feature count.

Primary metric:

> Goal → Verified Result

---

## 2. Comparison targets

Where possible compare the same tasks against:

- native autonomous coding agent baseline;
- ChatWeb + direct MCP toolbox baseline;
- MAR.

The objective is not to win every micro-benchmark.

The objective is to reduce external orchestration while preserving quality and workstation stability.

---

## 3. Required task suite

### T1 — Tiny fix
Single localized defect.

Measure:
- startup overhead;
- wall-clock;
- external MCP turns.

### T2 — Medium bug
Several files, targeted tests, at least one failure/repair loop.

Measure:
- autonomous loop quality;
- test-driven repair;
- context retrieval.

### T3 — Large multi-file change
Cross-module implementation with regression tests.

Measure:
- context quality;
- sustained autonomy;
- evidence quality.

### T4 — Deep refactor
Multiple call sites and architecture assumptions.

Measure:
- planning;
- base revision discipline;
- rework.

### T5 — Five parallel tasks in one project
Mix independent and overlapping goals.

Measure:
- workspace isolation;
- semantic conflict handling;
- integration behavior;
- disk amplification.

### T6 — Three concurrent projects
Different workload classes.

Measure:
- fairness;
- lazy project loading;
- RAM;
- user responsiveness.

### T7 — Client disconnect
Disconnect ChatWeb after task starts.

Pass:
- task continues or safely blocks without losing state.

### T8 — Worker crash
Kill worker during execution.

Pass:
- task state remains coherent;
- safe recovery or block.

### T9 — MAR daemon crash
Kill daemon during active tasks.

Pass:
- no false completion;
- recover/reconcile after restart.

### T10 — Cancellation during subprocess tree
Cancel while tests/build/browser/child processes are running.

Pass:
- no orphan process.

### T11 — Low-memory pressure
Force memory pressure while several tasks exist.

Pass:
- no system collapse;
- heavy dispatch throttles;
- optional caches evict.

### T12 — Base branch drift
Advance integration branch while task is running.

Pass:
- stale evidence is not accepted blindly.

### T13 — Semantically conflicting goals
Two tasks modify different files but incompatible architecture assumptions.

Pass:
- conflict is surfaced or repair/reconciliation occurs;
- no silent incorrect integration.

---


### T14 — Stale worker physical + logical fencing

Scenario:

1. Worker A owns mutable task/workspace at epoch N.
2. A is considered stale while A or one child remains physically capable of writing.
3. MAR attempts recovery.

Pass criteria:

- replacement Worker B must **not** begin mutable execution while A/process tree remains mutation-capable;
- MAR must terminate and confirm the old mutation-capable tree (or conclusively revoke workspace write authority);
- only after that confirmation may a higher epoch become active for mutable execution;
- after B starts, inject delayed callbacks/messages/checkpoints/tool events from A and verify all are rejected by epoch fencing;
- verify no physical workspace mutation from the old attempt occurs after B admission;
- if old physical mutation authority cannot be disproven, task must enter `BLOCKED / RECOVERY_REQUIRED`.

### T15 — Crash during side effect
Crash after dispatching a local/Git side effect but before durable observation.

Pass:
- recovery observes/reconciles actual state;
- no blind duplicate side effect.

### T16 — Crash during integration
Crash before and after authoritative head advancement.

Pass:
- integration attempt is deterministically recovered;
- no false double integration;
- stale evidence is rejected.

### T17 — Disk reserve pressure
Generate worktree/build/log/artifact growth until MAR approaches configured reserve.

Pass:
- new heavy work is denied;
- optional growth stops;
- threatening task is blocked/terminated before host reserve is exhausted.

---

## 4. Metrics

Required:

- wall-clock time;
- external MCP call count;
- model turn count;
- tool operation count;
- peak MAR daemon RAM;
- peak worker RAM;
- total CPU pressure;
- machine responsiveness;
- disk growth per task;
- worktree cleanup correctness;
- orphan process count;
- resume correctness;
- verification pass rate;
- acceptance pass rate;
- human intervention count;
- rework count;
- integration conflict count;
- stale-attempt mutation rejection count;
- effect-reconciliation count;
- minimum observed host free-disk reserve.

---

## 5. Hard acceptance conditions

V1 fails if any of these remain reproducible:

- lost update between concurrent tasks;
- task lost on ChatWeb disconnect;
- false task completion after daemon crash;
- orphan process after cancellation;
- unlimited worktree/log growth;
- stale verification accepted after base drift;
- one project starves others indefinitely;
- resource governor regularly makes workstation unusable;
- worker can silently widen Goal Contract;
- integration bypasses the single-writer rule;
- two mutation-capable attempts can concurrently affect the same mutable task workspace during recovery.

---

## 6. Performance target philosophy

Do not freeze arbitrary numeric targets before baseline measurement.

First establish comparable baselines.

Then set thresholds based on:

- native agent wall-clock;
- direct MCP wall-clock;
- workstation resource envelope.

A small constant MCP submission overhead is acceptable if long tasks gain autonomy and durability.

---

## 7. Product acceptance

Technical benchmark pass is necessary but not sufficient.

Final owner acceptance must include real usage:

1. choose a real project;
2. submit a real bounded Goal;
3. leave MAR to execute;
4. inspect result/evidence;
5. integrate or reject;
6. confirm that the workflow is materially better than manual ChatWeb↔worker forwarding.


---

## Source: `08_ARCHITECTURE_DECISIONS.md`

# MAR V1 — Architecture Decision Record

This document records decisions that should not be casually reopened.

---

## ADR-001 — Single owner / single host

**Decision:** MAR V1 targets one trusted OS user on one machine.

**Why:** Removes SaaS/RBAC/tenant complexity and keeps focus on autonomous coding quality.

**Consequences:** Multi-user hostile isolation is a non-goal.

---

## ADR-002 — MCP is control plane, not inner agent loop

**Decision:** ChatWeb submits goals and receives task state/results. Worker reasoning/tool loops execute behind MCP.

**Why:** Avoid network/tool round-trip on every file/test operation and allow browser disconnect.

---

## ADR-003 — Durable task state

**Decision:** Task truth is persisted independently of MCP transport and worker processes.

**Why:** Enables recovery, resume, disconnect tolerance and trustworthy state.

---

## ADR-004 — SQLite first

**Decision:** Use SQLite/WAL for V1 orchestration metadata.

**Why:** Single-machine product; simpler than distributed databases while sufficient for authoritative local state.

**Do not add Redis/Postgres unless evidence requires it.**

---

## ADR-005 — One mutable task, one isolated worktree

**Decision:** Concurrent mutable tasks never share a working tree.

**Why:** Prevents physical write races and preserves task-local Git history.

---

## ADR-006 — Parallel execution, serialized integration

**Decision:** Project integration has one authoritative writer.

**Why:** Prevents race conditions and creates one place to handle base drift and evidence invalidation.

---

## ADR-007 — Resource-weighted scheduling

**Decision:** Concurrency is governed by workload + live CPU/RAM/I/O state, not fixed task count alone.

**Why:** Eight waiting model calls are not equivalent to eight builds.

---

## ADR-008 — Process tree belongs to task

**Decision:** Child processes are owned and terminated as part of task lifecycle.

**Why:** Prevents orphan processes and hidden resource leaks.

---

## ADR-009 — Layered context retrieval

**Decision:** Start with Git/metadata + lexical + symbol/dependency retrieval. Semantic embeddings are optional.

**Why:** Keep RAM/CPU/storage low and avoid premature vector infrastructure.

---

## ADR-010 — Revision-bound verification

**Decision:** Evidence is valid only for the revision verified.

**Why:** Base drift can invalidate earlier tests and assumptions.

---

## ADR-011 — Goal Contract is immutable

**Decision:** Worker may not silently redefine acceptance or boundaries.

**Why:** Prevents autonomous execution from redefining success.

---

## ADR-012 — Small MCP public surface

**Decision:** Default ChatWeb-facing API is task-oriented rather than raw-machine-oriented.

**Why:** Better security, compatibility and orchestration semantics.

---

## ADR-013 — Build OS remains separate

**Decision:** MAR executes; Build OS defines/guards Goal, Boundary, Acceptance and reconciliation policy.

**Why:** Prevents MAR becoming a second process-heavy methodology/control framework.

---

## ADR-014 — V1 stops at acceptance

**Decision:** No architecture expansion unless benchmark/review proves a core assumption invalid.

**Why:** Prevent infinite improvement and preserve delivery speed.


---

## ADR-015 — Execution attempt fencing

**Decision:** Every mutable task execution has a durable `attempt_id` and monotonically increasing `run_epoch`. Only the current epoch may perform MAR-authorized mutation or lifecycle transitions.

**Why:** Prevent duplicate/stale workers after crash, hang or recovery.

**Implementation note:** SQLite CAS/transactions are sufficient for single-host V1; no distributed lease service.

---

## ADR-016 — Side-effect reconciliation

**Decision:** Effects with ambiguous crash outcomes use durable operation identity and observation/reconciliation before retry.

**Why:** Exactly-once cannot be generically guaranteed across filesystem/Git/process/external effects.

**Rule:** Do not blindly repeat an effect whose prior outcome is unknown.

---

## ADR-017 — Worker authority is enforced outside the model

**Decision:** Worktree isolation is not treated as a security sandbox. Filesystem, process, Git, network and secret authority are enforced by runtime/sandbox policy.

**Why:** Repository prompt injection must not gain full OS-user authority merely because the model obeys it.

---

## ADR-018 — Crash-safe integration transaction

**Decision:** Project integration uses a durable integration attempt with `expected_head`, candidate revision, current evidence identity and expected-head CAS advancement.

**Why:** Single-writer serialization alone does not make integration crash-atomic.

---

## ADR-019 — Hard resource envelope includes disk

**Decision:** CPU, RAM, disk and process growth are first-class governed resources.

**Why:** Cleanup after completion cannot prevent active tasks from exhausting the host.

---

## ADR-020 — Project Coordinator is reconstructable

**Decision:** The Project Coordinator is an evictable in-memory serialization/cache module, not a second authority.

**Why:** Prevent split-brain truth between orchestrator and project-local runtime state.

---

## ADR-021 — Semantic uncertainty blocks integration

**Decision:** Compatibility outcome may be `COMPATIBLE`, `INCOMPATIBLE` or `UNCERTAIN`. `UNCERTAIN` requires review/block.

**Why:** MAR does not claim perfect semantic-conflict detection.


---

## ADR-022 — Logical fencing does not replace physical writer termination

**Decision:** `run_epoch` fences logical MAR authority only. It does not by itself revoke physical filesystem mutation capability from stale OS processes.

For mutable recovery using the same workspace, MAR must confirm termination of the previous attempt's entire mutation-capable process tree before admitting replacement mutable execution, unless OS-level workspace write authority has been conclusively revoked.

If neither can be proven, recovery blocks.

**Why:** Prevent stale child processes or already-dispatched commands from corrupting the workspace used by a replacement worker.

**V1 simplification:** Prefer kill-and-confirm. Do not build live write-revocation machinery unless implementation evidence later requires it.


---

## Source: `09_RISKS_AND_OPEN_QUESTIONS.md`

# MAR V1 — Risks & Open Questions

**R2 note:** The former P0 architectural gaps around execution fencing, side-effect recovery, worker authority, crash-safe integration, and hard resource bounds are now resolved at the design-contract level. Implementation must still prove them experimentally.

---

## 1. Freeze-gate validation questions

These are no longer requests for a new architecture. Limited re-review should verify that the R2 corrections are coherent.

### V1 — Execution fencing
Can any stale worker/attempt still mutate after a higher `run_epoch` becomes authoritative?

R3 freeze criterion: a higher mutable epoch cannot become active on the same workspace until every prior mutation-capable process tree is confirmed terminated or write authority is conclusively revoked.

### V2 — Side-effect recovery
Can MAR distinguish “not executed”, “executed but not observed”, and “observed” well enough to avoid blind duplicate effects?

### V3 — Worker authority
Is the selected Windows implementation able to enforce the frozen filesystem/process/Git/network/secret authority contract?

### V4 — Integration crash recovery
Can every crash point around `expected_head → candidate → verify → CAS advance → finalize` be deterministically reconciled?

### V5 — Resource boundedness
Can active tasks still exhaust disk/process resources before backpressure takes effect?

### V6 — Resume quality
Does Goal + semantic checkpoint + selected evidence restore enough model state without full transcript replay?

---

## 2. Major implementation risks

### R1 — Worktree/build amplification
Some toolchains create large build artifacts per worktree.

Mitigation:
- shared immutable dependency caches;
- isolated mutable outputs;
- lifecycle cleanup;
- per-project build strategy.

### R2 — Windows process containment edge cases
Some processes may escape naïve parent-child killing.

Mitigation:
- Job Object ownership;
- integration tests with Node/Python/browser/ffmpeg-style process trees.

### R3 — Test suites too expensive for every iteration
Full regression on each inner loop may destroy throughput.

Mitigation:
- targeted verification during iteration;
- required broader verification at completion/integration.

### R4 — Context engine overgrowth
Symbol graph/indexing can become its own product.

Mitigation:
- layered retrieval;
- lazy indexing;
- measurable benefit required for added complexity.

### R5 — Reviewer cost explosion
Independent review on every tiny task may waste tokens/time.

Mitigation:
- risk-tiered verification profiles.

---

## 3. Decisions intentionally deferred

These do not block architecture freeze.

### Language
Rust vs Go.

### First model provider
Provider-neutral abstraction; implementation can start with one.

### Exact MCP protocol mapping
Use official Tasks extension where host support is sufficient; otherwise keep MAR task handles explicit.

### Warm worktree pool
Optimization after correctness.

### Embeddings
Optional after lexical/symbol retrieval benchmark.

### Remote worker
Out of V1.

---

## 4. Explicitly rejected scope creep

Do not add to V1 without benchmark evidence:

- organizations/RBAC;
- cloud orchestration;
- Redis;
- Kafka;
- Kubernetes;
- generalized workflow DSL;
- dozens of AI roles;
- global knowledge graph;
- marketplace/plugin ecosystem;
- deployment automation;
- automatic remote push by default.

---

## 5. External reviewer challenge prompts

Ask reviewers to answer:

1. Which invariant is wrong or insufficient?
2. Which failure mode could still corrupt or lose work?
3. Where can two parallel tasks produce an incorrect integrated result without detection?
4. Which component is unnecessarily complex for one machine?
5. Which component is too weak to achieve native-agent autonomy?
6. What happens when model call, worker process, daemon, or client fails at every lifecycle stage?
7. Which resource can still grow without a hard bound?
8. Which security boundary depends incorrectly on model obedience?
9. Does any state exist only in memory when it should be durable?
10. What would force a redesign after implementation begins?


---

## Source: `10_IMPLEMENTATION_ENTRY_CONTRACT.md`

# MAR V1 — Implementation Entry Contract

**Architecture status:** `FROZEN`

Implementation starts from the frozen MAR V1 architecture.

## Allowed implementation work

- choose Rust or Go;
- select first model provider/backend;
- implement MCP control surface;
- implement SQLite/WAL state;
- implement worker supervisor and process containment;
- implement worktree lifecycle;
- implement context/search/indexing;
- implement agent loop;
- implement verification;
- implement resource governor;
- implement crash-safe integration;
- implement T1–T17 benchmark harness;
- optimize latency, memory, disk, context quality and token usage.

## Not allowed without architecture-reopen evidence

- redesign task lifecycle;
- remove frozen fencing guarantees;
- allow multiple mutation-capable attempts on one mutable workspace;
- bypass serialized authoritative integration;
- move essential task truth into transport/in-memory-only state;
- weaken resource boundedness;
- widen worker authority implicitly;
- add distributed infrastructure, multi-user tenancy or swarm architecture as part of V1.

## Implementation decision rule

When an implementation problem appears:

1. first treat it as an implementation defect;
2. try a local correction consistent with frozen invariants;
3. benchmark;
4. reopen architecture only if evidence shows a frozen invariant itself is insufficient.

## Acceptance gate

V1 implementation is not complete until the T1–T17 acceptance suite passes and owner real-use acceptance confirms the workflow is materially better than manual ChatWeb↔worker forwarding.
