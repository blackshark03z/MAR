# R-007 — Automated Research Observer Runtime

**Status:** `RESEARCH_ONLY / ACTIVE_EXTERNAL_OBSERVER`  
**Baseline:** MAR v1.1.0  
**MAR production-code changes:** none  
**Date:** 2026-09-12

## Purpose

Establish a real automated measurement loop for MAR without modifying MAR v1.1 execution behavior, source, SQLite state, tunnels or task authority.

The observer is deliberately external. MAR must work identically whether this research tooling exists, is stopped, or fails.

## Runtime location

Research tooling and raw outputs live outside canonical MAR source:

```text
D:\MAR-Research\observer\
  observer_run.py
  context_metrics.py
  transport_probe.py
  aggregate_daily.py
  run_snapshot.py
  reports\
    context_latest.json
    transport_latest.json
    daily_latest.json
    daily_latest.md
  snapshots\
    YYYYMMDD-HHMMSS.json
```

The scripts may evolve freely as research tooling. They are not MAR product components and do not authorize future runtime changes.

## Automated schedule

Windows Task Scheduler now contains:

```text
MAR Research Observer Hourly
```

It runs `D:\MAR-Research\observer\run_snapshot.py` once per hour using the installed Python 3.11 interpreter.

The first integrated snapshot completed all three collectors successfully in about five seconds. This cost is low enough for hourly research sampling on the current machine, but observer overhead remains itself a measured quantity and cadence should be reduced if it becomes material.

## Snapshot pipeline

Each scheduled run performs only read/observe work:

```text
Durable reconstruction
       +
Context metrics
       +
Transport/process observation
       ↓
versioned snapshot JSON
       ↓
rolling 24h aggregation
       ↓
daily_latest.json / daily_latest.md
```

### Durable collector

Reconstructs historical/terminal task evidence from MAR durable truth in read-only mode: task envelopes, attempts, WebTurns, checkpoints, controls, verification/results and resource summaries where available.

### Context collector

Tracks legacy versus DecisionProjection request populations, request sizes, projection composition and exact same-request recent-evidence/protocol-tail duplication. It stores counts/bytes and derived metrics, not copied prompt/tool payload bodies.

### Transport collector

Observes desired configuration and actual local process/port/API state without starting/stopping/restarting anything.

The probe is **ownership-aware**. A process named `cloudflared.exe` is not attributed to MAR solely by name. The initial prototype briefly observed one cloudflared process while MAR was stopped; ownership inspection proved it belonged to ChatCode (`ChatCode` parent and ChatCode runtime path). Transport observer v1 therefore attributes MAR-owned process state using MAR-root/path and parent evidence, avoiding this false positive.

## Desired versus observed state

Connection health cannot be derived from process existence alone.

The observer separates:

```text
desired state
- OpenAI tunnel desired_running
- connector preferred modes

observed state
- MAR-owned process counts
- Owner UI loopback listener/API
- local MCP bridge listener
- owned cloudflared/tunnel-client presence
```

Current example while MAR is intentionally not running:

```text
state = NOT_RUNNING
signals = []
```

Even though durable configuration says the OpenAI tunnel is desired-running and GPT/Claude connector modes are temporary, MAR being absent is not automatically classified as a connection regression. A later research policy may distinguish intentional shutdown from expected always-on availability, but that intent must not be guessed.

## Rolling report semantics

The 24-hour aggregator currently emits only:

- `OK` — enough samples exist and no material research signal is present;
- `RESEARCH_SIGNAL` — a measured anomaly deserves investigation;
- `INSUFFICIENT_EVIDENCE` — not enough comparable samples exist to make a stability claim.

The first report correctly returned `INSUFFICIENT_EVIDENCE` with one sample.

The report includes:

- sample count;
- transport-state distribution;
- unique research signals;
- durable snapshot success rate;
- Owner API success rate only for `LOCAL_READY` samples;
- new terminal-task count where comparable;
- observer runtime;
- latest context metrics.

It does not create coding tasks, change backlog priority or mutate MAR.

## Safety / authority boundary

The external observer must never:

- write/migrate/vacuum/checkpoint MAR SQLite;
- submit/steer/cancel/retry MAR tasks;
- start/stop/restart MAR or connector processes;
- rotate connector links or credentials;
- mutate Git/product worktrees;
- call AI models merely for passive telemetry;
- mark a product/release accepted or failed;
- automatically turn a research signal into V1.2 implementation scope.

Its outputs are analytical evidence only.

## What hourly sampling can and cannot prove

Hourly snapshots are useful for:

- durable task/context trend changes;
- long-lived availability/configuration mismatches;
- observer/data-source failures;
- disk/resource trend snapshots;
- building a low-cost baseline over days/weeks.

Hourly sampling cannot reliably reconstruct short disconnects or exact reconnect duration. A 20-second tunnel outage can occur completely between samples. Exact transport/reconnect research therefore requires a separate bounded **active measurement window** with 5–10 second sampling or authoritative transport events. Such a watcher should run only during selected MAR-use/reliability experiments until its overhead and value are proven.

## Next evidence gates

1. Accumulate at least 10 hourly samples before interpreting ordinary availability/latency drift.
2. Accumulate multiple `LOCAL_READY` samples during real MAR use before establishing connection latency/reliability baselines.
3. Add a bounded active transport watcher during one real MAR work session to measure short disconnect/reconnect events.
4. After enough comparable stable-source tasks exist, run the fixed T1/T2/T7/T8 benchmark cadence from R-004.
5. Only then determine whether missing transport timestamps/context metrics justify narrow V1.2 production instrumentation.

## Current verdict

`AUTOMATED_RESEARCH_LOOP_ACTIVE`

Passive automated measurement is now operating outside MAR with low measured overhead and without changing V1.1 runtime behavior. The next priority is evidence accumulation plus an active-session transport/reconnect experiment; not additional dashboard implementation.
