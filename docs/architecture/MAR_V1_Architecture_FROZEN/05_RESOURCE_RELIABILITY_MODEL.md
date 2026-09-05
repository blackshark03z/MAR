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
