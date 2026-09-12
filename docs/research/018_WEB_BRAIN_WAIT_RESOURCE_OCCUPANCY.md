# R-018 — Web Brain wait resource occupancy and polling amplification

**Status:** `RESEARCH_ONLY`  
**Date:** 2026-09-12  
**Verdict:** `P0_RELIABILITY_THROUGHPUT_TRADEOFF`

## Research question

MAR correctly keeps a durable task alive when external Web cognition is temporarily unavailable. Does the current implementation pay too much runtime/resource cost while waiting, such that disconnect resilience can turn into worker-slot starvation or SQLite contention?

This document records evidence and benchmark hypotheses only. It does not authorize changing worker lifetime, fencing, heartbeat, WebTurn semantics, polling cadence, task budgets or scheduler/resource policy.

## Current wait path

When a Web Brain turn is requested, `ProcessRunner.waitForWebTurn()`:

1. durably publishes the bound WebTurn;
2. checks once for an already-available response;
3. starts a wait timer bounded by the current agent `MaxDuration`;
4. polls `WebTurnResponse()` every **200 ms**;
5. heartbeats the active attempt every `LeaseDuration / 3`, with a minimum of one second;
6. returns when the exact turn response becomes durable, context is cancelled, heartbeat fails, or the wait timer expires.

Production runtime defaults `LeaseDuration` to **1 minute**, so a normal Web wait heartbeats roughly every **20 seconds**.

`MaxDuration` is bounded by the task convergence budget. The default task-wide active-execution budget is **30 minutes**. The same active-attempt elapsed time includes time spent waiting for Web cognition.

## Good reliability property

The current design has an important safety advantage:

- the task does not disappear merely because Chat/Web disconnects;
- the exact attempt and run epoch remain authoritative;
- the worker keeps the lease alive while waiting;
- only the exact durable turn response can resume execution;
- stale/fenced responses remain rejectable;
- timeout eventually terminates the worker and MAR confirms physical termination before finalizing the task state.

Therefore the problem is **not** that MAR waits durably. The research question is whether an active OS worker/process/resource claim is the cheapest safe representation of that wait.

## Worker-slot and resource-claim occupancy

Daemon admission places a launched task in the daemon's `active` map. The task is removed only when `RunWorkspaceReady()` returns.

The heavy resource-governor lease is acquired before launch and released by a defer after that runner returns.

Therefore a task waiting for Web cognition remains:

- counted against `MaxConcurrentWorkers`;
- present as an active task;
- holding its heavy execution resource lease;
- retaining active attempt/workspace authority;
- consuming task active-execution budget.

The normal runtime default is **2 concurrent workers**. The normal scheduler estimate also feeds execution admission with **256 MiB RAM reservation + 256 MiB disk reservation per launched task**, and the resource governor treats the execution claim as heavy.

Consequently, two simultaneous Web Brain waits can occupy both normal worker slots, hold roughly **512 MiB of aggregate RAM reservation + 512 MiB of aggregate disk reservation**, and consume both configured heavy-job slots even when neither is using meaningful CPU for model reasoning or coding work. A third READY task cannot launch until capacity is released.

These are reservation/accounting values, not measured resident memory or physical disk consumption. The starvation risk comes from admission bookkeeping and slot occupancy, not from claiming the idle workers literally consume those reserved bytes.

This is a potential starvation mode, not yet a measured production incident.

## SQLite polling amplification

While waiting, `waitForWebTurn()` checks for a response every 200 ms, or **5 checks per second per waiting worker**.

`TaskService.WebTurnResponse()` currently calls `store.GetWebTurn(turnID)`. `GetWebTurn` reads the complete durable row, including:

- full `request_json`;
- `response_json` if present;
- request/response hashes;
- integrity hash;
- all identities and timestamps.

It then validates WebTurn integrity. If no response is present, the worker sleeps until the next 200 ms poll and repeats the same full-row read.

R-006 measured a median DecisionProjection request around **44.7 KiB**. Using that historical median only as an illustrative scale, not a live throughput measurement:

```text
1 waiting worker:
  5 polls/sec * 44.7 KiB
  ~= 223.5 KiB/sec of request JSON revisited

2 waiting workers:
  ~= 447 KiB/sec
  ~= 26 MiB/minute
```

Actual database/page-cache/allocator bytes may differ materially from serialized JSON size. The estimate only demonstrates why this path deserves measurement.

At the p95 request size the potential repeated payload is larger.

## A second 5 Hz polling loop exists in the daemon

Every active daemon task also has `monitorCancellation()` running at the production `ControlPollInterval`, currently **200 ms**.

That loop calls `TaskService.StatusSnapshot()` on every tick. For a task currently waiting on a pending Web turn, the snapshot path performs approximately:

1. task status read;
2. latest control read;
3. latest cancel-control read;
4. pending WebTurn read.

The pending-turn query selects and integrity-validates the full WebTurn row, including `request_json`.

Therefore one waiting Web task structurally has at least two independent 5 Hz DB loops:

```text
worker wait loop:
  5 * GetWebTurn / sec

daemon cancellation loop:
  5 * StatusSnapshot / sec
  ~= 20 component DB reads/sec in the pending-turn path
```

The exact database/sql execution count can vary with branch/error behavior, but the source establishes that both loops exist.

Importantly, both `GetWebTurn` and `PendingWebTurn` reread the full request JSON. Using the historical 44.7 KiB median request only as scale, the two loops together could revisit roughly **26 MiB/minute of serialized request payload per waiting task**, or roughly **52 MiB/minute for two waiters**, before counting row/hash/control/task overhead. This is not a measured disk-throughput claim; SQLite page cache and Go allocation behavior can make physical I/O very different.

## Interaction with R-017 single-connection SQLite policy

MAR deliberately sets `MaxOpenConns(1)` for deterministic authoritative coordination.

Therefore Web-wait polls do not merely consume read I/O. They queue on the same in-process database connection used by:

- attempt heartbeats;
- task transitions;
- other WebTurn commits;
- verification/result persistence;
- integration state;
- Owner Console observability reads.

With one waiting worker, the 5 Hz polling may be negligible. With several waiting workers plus Owner Console polling, it can plausibly become a measurable connection-wait source.

Absence of `SQLITE_BUSY` does not disprove this because the bottleneck can occur in `database/sql` connection waiting before SQLite lock acquisition.

## Timeout outcome

The wait timer is bounded by the current remaining active-execution duration. On timeout, the worker side reports:

```text
web brain turn timed out waiting for ChatGPT response
```

That error propagates through worker process failure handling. MAR then blocks the task while the current attempt is still authoritative and confirms physical process termination with terminal status `worker-process-error`.

This is fail-closed and avoids an automatic duplicate writer/replacement.

The cost is that a temporary Web disconnect can consume a substantial fraction or all of the task's active-execution budget before becoming BLOCKED.

## Distinguish four different resources

Future measurements must separate:

1. **authority occupancy** — attempt/workspace fencing that may need to remain held;
2. **scheduler worker slot** — whether another worker may run;
3. **resource-governor heavy claim** — reserved RAM/disk/heavy-job capacity;
4. **OS worker process** — process/job-object memory/handles and active polling loop.

These do not necessarily have to share one lifetime in a future architecture. However, separating them safely would be a significant design change and must preserve fencing/recovery invariants.

## Historical wait reconstruction — 2026-09-12

The external read-only observer reconstructed all responded durable WebTurn intervals currently present in the V1.1 store:

- 368 responded wait intervals across 27 tasks;
- median wait: **36.772 s**;
- p95 wait: **233.703 s**;
- longest observed wait: **1103.579 s** (~18.4 min);
- aggregate responded wait time: **27,752.893 s** (~7.7 h).

A sweep-line reconstruction found:

```text
max simultaneous pending WebTurns = 1
max distinct waiting tasks        = 1
time with >=2 simultaneous waits  = 0 s
```

Therefore the two-worker starvation scenario is **not historically observed in this dataset**. It remains a valid architectural stress case because the default capacity is two workers, but it must not be presented as an existing production incident.

Using the source-derived two 5 Hz full-WebTurn read loops only as a byte-touch proxy, the historical waits correspond to roughly **25.7 GiB** of request-JSON representation revisited. This is explicitly **not measured physical disk I/O**; SQLite page cache, allocation behavior and row decoding make physical cost different. The value establishes scale for live polling-cost measurement, not throughput truth.

The historical evidence shifts priority slightly: measure/narrow negative polling cost first; keep W2 two-waiter capacity starvation as a controlled stress benchmark rather than a claimed observed defect.

## Benchmark matrix

### W0 — one short Web wait

Hold one Web response for 5–10 seconds, then respond normally.

Measure:

- response poll count;
- SQLite connection wait if observable;
- heartbeat cadence;
- worker RSS/CPU;
- final task latency.

### W1 — one prolonged Web wait

Hold for several minutes while the host remains healthy. Submit a second independent task and measure its admission/start latency.

### W2 — two simultaneous Web waits

Occupy both default worker slots with pending Web turns, then submit a third task.

Acceptance question: is the third task intentionally blocked by safety policy, or is this avoidable idle-capacity starvation?

### W3 — disconnect/reconnect

Disconnect the remote Web/MCP client while the turn is pending, reconnect with a fresh transport session, answer the same durable turn and verify:

- same attempt/run epoch resumes;
- no replacement worker solely because transport changed;
- no transcript replay beyond bounded Decision Projection;
- resource occupancy/recovery time is observable.

### W4 — timeout boundary

Let a fixture reach the configured Web-wait deadline. Verify exact durable terminal state, physical termination proof, heavy-lease release and subsequent scheduler capacity recovery.

### W5 — Console interaction

Repeat W1/W2 with Owner Console closed and open to measure combined R-016/R-017/R-018 DB pressure.

## Metrics

Collect:

- pending Web turns;
- waiting-worker count;
- active-worker slots occupied;
- heavy resource claims held;
- WebTurnResponse polls/sec;
- StatusSnapshot/cancellation-monitor polls/sec;
- full-WebTurn reads/sec from both loops;
- request bytes represented by each polled row;
- sql.DB wait duration/count if instrumentation becomes justified;
- heartbeat jitter;
- task launch queue delay for unrelated work;
- MAR worker/daemon CPU and RSS;
- Windows commit pressure;
- time from Web response commit to worker resume;
- time from timeout/cancel to worker slot + resource lease release.

## W4 scheduler-capacity stress — 2026-09-12

A research-only Go overlay exercised the real `Daemon.launchReady()` + resource-governor path with `MaxConcurrentWorkers=2`, three READY tasks, and a controlled runner that blocks the first two tasks to model the already-proven Web-wait lifetime behavior.

Observed semantics:

```text
initial READY tasks                 3
max concurrent workers             2
first launchReady() starts          2
active daemon slots                 2
active heavy leases                 2
third task while both blocked       NOT_STARTED
release one blocked runner          slot + heavy lease released
next launchReady()                  third task admitted
shutdown                            0 active / 0 leaked leases
```

The first draft fixture failed because test ordering allowed the nominal third task to be selected early and a fake completed task remained READY; after correcting those fixture semantics, the scheduler test passed. No production code was changed.

This test does not emulate the WebTurn polling code itself. Its significance comes from combining two independently established facts:

1. source/runtime evidence shows a Web Brain wait remains inside the active `RunWorkspaceReady()` worker lifetime and retains its heavy lease;
2. the daemon stress test proves two such occupied lifetimes consume both default worker slots and prevent a third READY task from starting until one releases.

Therefore `WEB_WAIT_CAPACITY_STARVATION` is now **semantically confirmed under stress**, even though historical data showed maximum observed pending-Web concurrency of only one task. Treat it as a capacity-risk scenario, not as evidence that production has already suffered this exact two-waiter incident.

## Candidate experiments, not implementation requirements

### C1 — Adaptive response polling

Test slower or backoff polling during long waits while keeping response-to-resume latency acceptable.

This is the smallest candidate and does not by itself solve occupied worker slots.

### C2 — Event-driven WebTurn wakeup

Research whether the in-process runtime can wake a waiting worker when `RespondWebTurn` commits rather than rereading SQLite every 200 ms.

A durable DB check must remain the truth after restart; an in-memory notification can only be an optimization hint.

### C3 — Narrow response-availability query

Research a query that tests response presence/hash/timestamp without loading the full request body on every wait poll. Full integrity validation can happen at the material commit/resume boundary rather than necessarily on every negative poll, if an oracle proves safety equivalence.

### C4 — Park compute capacity while preserving authority

Higher-risk hypothesis: retain durable attempt/workspace fencing but release some combination of worker process, scheduler execution slot or heavy resource reservation while waiting for external cognition.

This must not permit a second mutation-capable writer. Rehydration/resume would need exact task + attempt/run-epoch + Decision Projection binding and physical-process truth.

### C5 — Separate cognition-wait budget from active compute budget

Research whether waiting for a disconnected/slow external brain should consume the same 30-minute active-execution budget as active coding/verification.

Do not change this merely to make timeouts longer. A separate wait budget would need abuse/starvation bounds and clear Owner visibility.

## Safety invariants for any future change

Must preserve:

- no simultaneous mutation-capable attempts for one task/workspace;
- stale process cannot regain authority;
- durable WebTurn remains exact resume identity;
- timeout/cancellation proves physical termination where required;
- no lost Owner cancellation while parked/waiting;
- restart recovery derives truth from durable state, not an in-memory wake channel;
- no hidden transport session becomes authority;
- bounded total wait/resource consumption.

## Phase-0 decision

Treat current Web Brain wait as a **P0 reliability-throughput trade-off**:

- reliability semantics are strong;
- resource efficiency under prolonged/multiple disconnects is not yet demonstrated;
- 200 ms full-WebTurn polling plus two-slot occupancy is a concrete source-backed optimization target.

Measure W0–W5 before changing worker lifecycle or authority semantics.
