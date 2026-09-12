# R-024 — External collector extension plan for P0 benchmarks

**Status:** `RESEARCH_ONLY`  
**Baseline:** MAR v1.1.0  
**Production-code changes authorized:** none  
**Date:** 2026-09-12

## Purpose

Extend the existing external Research Observer just enough to answer P0-1 through P0-3 without adding telemetry to MAR V1.1.

The collector remains outside MAR authority under `D:\MAR-Research\observer`. This document specifies measurements only; implementation of the external scripts is not part of MAR product source.

## Design principle

Prefer short, high-resolution benchmark windows over continuous expensive monitoring.

Normal passive observer:

```text
hourly snapshot + 5/10/30/60s active watchers
```

Controlled benchmark burst:

```text
250 ms to 1 s sampling
maximum bounded window
metadata only
no payload bodies
```

High-resolution collection must stop automatically after the benchmark window so the observer does not become the performance problem.

## E1 — Process lifecycle sampler

### Goal

Measure process churn caused by Owner Console runtime polling and context construction.

### Observe

For a bounded benchmark window, record only:

- process name;
- PID / parent PID;
- create/exit timestamp;
- RSS/working-set;
- CPU time delta;
- process I/O counters where supported.

Relevant process classes:

- MAR executable;
- sandbox-host-check child MAR process;
- `cmd.exe` created by sandbox readiness probe;
- `git.exe` created by context snapshot/verification/integration;
- worker process;
- tunnel-client/cloudflared only when transport experiment is active.

Do not persist full command lines, environment variables or user document paths merely for process counting.

### Derived metrics

- process launches/minute by class;
- median/p95 lifetime by class;
- aggregate CPU time by class;
- aggregate read/write bytes by class where available;
- concurrent child-process peak.

### Research use

P0-1 can verify whether the structural estimate of ~30 sandbox-check launches/minute occurs in practice.

P0-3 can count actual Git subprocesses per context decision rather than relying only on source-path estimates.

## E2 — SQLite file/WAL observer

### Goal

Measure whether Console/Web-wait experiments correlate with durable-store growth/churn without reading or modifying the SQLite database through a second writer.

### Observe

Read filesystem metadata only:

- `mar.db` size;
- `mar.db-wal` size when present;
- `mar.db-shm` size when present;
- timestamp changes.

Do not issue VACUUM, CHECKPOINT, PRAGMA mutations or any SQLite write.

### Limit

File-size changes are not query latency and not disk I/O attribution. They are only coarse activity signals.

## E3 — Owner endpoint latency sampler

### Goal

Measure the observer effect from outside the Owner UI.

During controlled phases probe only safe read endpoints already used by the active transport watcher, with the same local authentication/boundary method already established by the external observer.

Candidate endpoints:

- runtime;
- task list;
- one selected task status/result/inspect only during the selected-task phase.

Measure:

- wall duration;
- HTTP status;
- response bytes;
- timeout/error class.

Do not send mutation requests.

### Important

The sampler itself must run at a much lower request rate than the UI under test, or run as isolated single probes at phase boundaries. Otherwise it contaminates the A/B result.

## E4 — Benchmark phase marker

A controlled run needs explicit phase identity:

```text
O0_CONSOLE_CLOSED
O1_CONSOLE_LIVE
O2_CONSOLE_TASK_SELECTED
W0_WEB_IMMEDIATE
W1_WEB_DELAY_10S
W2_WEB_DELAY_60S
...
```

Store only phase ID, fixture ID/version, start/end UTC and environment-profile hash.

The collector must never infer "Console closed" merely from Chrome process count. Browser process topology is not a reliable tab-state oracle.

## E5 — Context-build external correlation

### Goal

Measure context-build wall/process cost without instrumenting `Engine.Build()` initially.

For controlled context fixture runs correlate:

- worker turn/WebTurn creation timestamps;
- Git child-process bursts;
- process I/O/CPU;
- durable request creation time;
- Pack/request byte metrics already produced by the context collector.

This will not perfectly isolate pure `Engine.Build()` CPU time, but it can answer the first decision question: whether repeated context construction is operationally material.

If the external envelope is too ambiguous to distinguish context cost from surrounding worker work, only then consider narrow V1.2 internal timing instrumentation.

## E6 — Web-wait capacity sampler

For P0-2, correlate:

- number of active/pending Web-turn tasks;
- worker/MAR process state;
- task ready/queue delay;
- host RAM/commit pressure;
- process I/O/CPU;
- durable WebTurn timestamps;
- local Owner/MCP latency.

The collector should not poll WebTurn payload bodies at high frequency itself. It should derive pending state from already-bounded metadata where possible so research does not duplicate the suspected amplification.

## E7 — Runtime child / daemon liveness correlation

For P0-0, extend the bounded process sampler to distinguish the Owner UI process tree without storing full command lines:

- identify the MAR process listening on Owner UI loopback as parent candidate;
- observe MAR child PIDs/PPIDs and lifetime transitions;
- correlate child disappearance with local MCP/Owner API health and durable task progress;
- record whether another MAR process appears/takes over after the child exits;
- never attempt to acquire `<db>.daemon.lock` merely to test liveness, because acquiring the authority lease would perturb execution authority.

The existence of the `.daemon.lock` path alone is not proof of a live lock holder. Prefer process/task-progress correlation externally; if that remains ambiguous, a future narrow read-only daemon heartbeat may be considered only after benchmark evidence requires it.

## E8 — Tunnel/admin metrics collector

When a MAR-owned cloudflared/tunnel client exposes a loopback metrics/admin surface, collect only documented bounded operational metrics such as:

- HA connections;
- active streams;
- concurrent requests;
- request error counters;
- protocol/QUIC RTT where available.

Ownership must be established before scraping. A ChatCode-owned cloudflared process must never be attributed to MAR.

## Privacy and safety boundary

External P0 collectors must not persist:

- prompts/tool payload bodies;
- API keys/tokens;
- connector capability URLs containing secrets;
- complete process command lines;
- arbitrary browser history/tab titles;
- file content.

They must not:

- kill/restart a process;
- mutate MAR SQLite;
- start a MAR task;
- alter sandbox readiness;
- change pagefile/resource policy;
- write production worktrees.

## Sampling overhead oracle

Before using a collector in a performance benchmark, measure the collector alone.

Reject or lower its cadence if it causes a material change in:

- CPU;
- Windows commit/RSS;
- process count;
- local endpoint latency;
- disk I/O.

The default target is to keep the observer substantially below the effect size the benchmark is trying to detect.

## Rollout order

1. process lifecycle sampler;
2. phase marker;
3. DB/WAL metadata;
4. sparse Owner endpoint latency;
5. Console O0/O1/O2 A/B;
6. Web-wait W0-W4;
7. context-build C0-C5;
8. tunnel/admin metrics only when MAR-owned transport is active.

## Current decision

`EXTEND_EXTERNAL_OBSERVER_BEFORE_MAR_INSTRUMENTATION`

R-023 shows the remaining P0 blind spots are narrow enough that external metadata collection should be attempted first. Do not add general-purpose telemetry to MAR V1.1.
