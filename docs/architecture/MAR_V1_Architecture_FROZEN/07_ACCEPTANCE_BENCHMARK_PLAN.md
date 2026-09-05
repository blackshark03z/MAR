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
