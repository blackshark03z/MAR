# R-008 — Active Transport Watch

**Status:** `RESEARCH_ONLY / ACTIVE`  
**Baseline:** MAR v1.1.0  
**MAR production-code changes:** none  
**Date:** 2026-09-12

## Purpose

Hourly snapshots cannot observe short connection loss/recovery events. R-008 adds a bounded external transport watcher that samples only MAR local surfaces and records state transitions without changing MAR, its connectors or its durable authority.

## Runtime

External research script:

```text
D:\MAR-Research\observer\transport_watch.py
```

State/event output:

```text
D:\MAR-Research\observer\transport_watch_state.json
D:\MAR-Research\observer\events\YYYY-MM-DD.ndjson
```

The watcher is started for the current user through the Startup shortcut:

```text
MAR Research Transport Watch.lnk
```

A Task Scheduler `ONLOGON` trigger was attempted first but the current non-elevated context correctly received `Access is denied`; research did not request elevation. The user Startup mechanism requires no UAC and keeps the observer outside MAR authority.

## Sampling behavior

When no MAR local surfaces are observed:

```text
sample interval = 30 seconds
state = NOT_RUNNING
```

When either local surface appears:

```text
sample interval = 5 seconds
```

Each sample checks only:

- loopback Owner UI TCP on 8787;
- Owner runtime API response when 8787 is listening;
- loopback MCP bridge TCP on 8788;
- local connection latency for those probes.

Classification:

- `NOT_RUNNING` — neither local surface is listening;
- `LOCAL_READY` — Owner API is healthy and MCP bridge is listening;
- `PARTIAL_OR_UNHEALTHY` — some local surface exists but the full local contract is not healthy.

Only state changes are appended to the event log. The latest state file is overwritten in the external research directory.

## Single-instance and retention behavior

The watcher uses an external PID lock so repeated startup/manual invocation does not intentionally create duplicate watchers. Stale lock recovery is bounded to the external research directory.

Event files older than 14 days are removed by the watcher. No MAR logs, artifacts or database rows are deleted.

## Initial observation

The first live watcher state was:

```text
NOT_RUNNING
```

with neither 8787 nor 8788 listening. This is not classified as a regression because the observer cannot yet prove the Owner expected MAR to remain continuously running.

A prior process-name-only transport probe saw one `cloudflared.exe` while MAR was absent. Ownership inspection proved it belonged to ChatCode, not MAR. The regular hourly transport probe was therefore upgraded to use ownership evidence; R-008 local-surface monitoring avoids process-name attribution entirely for fast state sampling.

## What this watcher can establish

Over real MAR sessions it can measure:

- local startup-to-ready transition;
- occurrence count of `LOCAL_READY -> PARTIAL_OR_UNHEALTHY`;
- duration of local degraded periods to roughly the sampling resolution;
- local ready/restored transition time;
- Owner API and loopback MCP latency around transitions;
- whether MAR local surfaces disappear together or independently.

## What it cannot establish

R-008 alone cannot prove:

- public tunnel availability from ChatGPT/Claude;
- whether a remote request received a 502/504 outside the local machine;
- browser/Web client disconnect timing;
- exact packet/network cause;
- remote connector route health when local surfaces remain healthy;
- whether a task lost work after a transport interruption.

Those require correlation with remote request telemetry/durable task progress and controlled T7-style interruption benchmarks.

## Safety boundary

The watcher does not:

- start/stop/restart MAR;
- start/stop/restart cloudflared or tunnel-client;
- call any mutation endpoint;
- submit/control tasks;
- read prompt/tool payload bodies;
- write MAR SQLite;
- modify Git;
- call AI models.

## Next research use

Accumulate transition evidence during normal MAR operation. After at least several real `LOCAL_READY` sessions exist, correlate watcher events with:

- hourly observer snapshots;
- WebTurn/task progression;
- connection request/error telemetry where available;
- controlled T7 client-disconnect experiments.

Only then decide whether V1.2 needs authoritative transport-event instrumentation inside MAR.

## Current verdict

`ACTIVE_LOCAL_TRANSPORT_MEASUREMENT`

Exact short local-surface interruptions can now be observed automatically without changing V1.1 production code. Public/remote disconnect causality remains a known blind spot.
