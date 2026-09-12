# R-017 — SQLite handle topology and observability contention

**Status:** `RESEARCH_ONLY / CORRECTED`  
**Date:** 2026-09-12  
**Verdict:** `MEASURE_FILE_LEVEL_CONTENTION_BEFORE_ANY_DB_CHANGE`

## Correction notice

An earlier phase-0 reading overstated the contention model by saying Owner Console reads and runtime authority writes all queue behind **the same Go `database/sql` connection**.

That is not the full V1.1 topology.

`runOwnerUI()` launches a separate `mcp-stdio` child process for the MCP/runtime/daemon path and also opens its own SQLite handle in the Owner UI parent. The child `runMCPRuntime()` opens the same database path independently.

Therefore:

```text
Owner UI process
  store.Open(db) -> sql.DB A -> MaxOpenConns(1)

mcp-stdio child / daemon runtime
  store.Open(db) -> sql.DB B -> MaxOpenConns(1)
```

The two paths do **not** share one in-process `database/sql` pool/connection.

This correction weakens the original hypothesis substantially and must be applied before benchmark interpretation.

## Current SQLite contract

Every `store.Open()` configures its own handle with:

```text
SetMaxOpenConns(1)
SetMaxIdleConns(1)
PRAGMA journal_mode=WAL
PRAGMA foreign_keys=ON
PRAGMA busy_timeout=5000
PRAGMA synchronous=FULL
```

The source comment explains the per-handle design intent: keep one SQLite connection so connection-scoped PRAGMAs and transaction ordering are deterministic for that service instance.

Separately, runtime daemon authority is protected by an exclusive daemon lease so only one daemon owns orchestration authority at a time.

## Actual concurrency topology

SQLite WAL can permit readers on one connection/process while another connection/process writes. The Owner UI direct reads therefore have more concurrency with runtime authority writes than the earlier single-pool model implied.

Potential pressure now has to be reasoned about at different layers:

1. **Per-handle `database/sql` queue** — operations within one `store.SQLite` instance serialize behind its one connection.
2. **SQLite/WAL coordination** — separate parent/child handles still coordinate through the same database/WAL and durability policy.
3. **Filesystem/page-cache/I/O pressure** — repeatedly scanning large WebTurn rows can create allocation/cache/I/O work even without lock contention.
4. **CPU/memory pressure** — JSON scanning, integrity validation and response construction can perturb the same host while workers run.

Absence of `database is locked` errors remains insufficient to prove zero performance effect, but the likely mechanism is no longer "all work waits on one Go connection."

## Owner UI direct database work

R-016 remains valid on the read-amplification shape:

- task list polling every 2 seconds;
- structurally up to `1 + 3*N` reads for up to 30 recent tasks;
- live usage can load full WebTurn request/response JSON;
- selected task detail also creates additional MCP/read traffic.

These operations occur primarily on the Owner UI parent's database handle for list/usage/feedback/project surfaces.

Owner actions that proxy through `callTool()` execute through the child MCP runtime and its independent store handle. Some Owner UI operations such as project/feedback/connector configuration also write through the parent handle, so the database still has multiple legitimate MAR clients.

## Why measurement is still useful

Even with separate handles, high-frequency/full-payload reads may still affect:

- Owner API latency;
- parent-process CPU/RSS;
- SQLite page-cache churn;
- WAL/filesystem activity;
- host disk/cache pressure;
- child/runtime write latency under unfavorable WAL/checkpoint/IO conditions.

But this must be demonstrated, not inferred from `SetMaxOpenConns(1)` alone.

## Revised benchmark

### S0 — Console closed

Measure fixed-window host/runtime baseline.

### S1 — Console Live/Overview open

Measure:

- Owner process CPU/RSS;
- child runtime CPU/RSS;
- `/api/runtime` and `/api/tasks` latency;
- DB/WAL file metadata trend;
- process churn;
- task/heartbeat/verification timing on a comparable fixture.

### S2 — Tasks view with selected task

Add selected-task detail polling and compare against S0/S1.

### S3 — Historical WebTurn size pressure

Use disposable fixture data with increasing WebTurn history and measure Owner API latency/CPU/RSS. Do not inflate the authoritative production database merely for research.

### S4 — Concurrent authority writes

During a controlled isolated task, compare durable transition/heartbeat timing with Console closed/open. A material effect here is stronger evidence than UI latency alone.

## Metrics

Prefer external evidence first:

- parent/child process CPU and RSS separately;
- process I/O counters where available;
- `/api/tasks` p50/p95;
- `/api/runtime` p50/p95;
- DB/WAL file-size/timestamp changes;
- task heartbeat jitter;
- WebTurn response commit/recovery envelope;
- verification/integration timing;
- host commit/disk pressure.

If external evidence indicates DB-level contention but cannot locate it, narrow future instrumentation could collect `database/sql` stats **per handle/process** rather than assuming one global pool.

Useful internal metrics, only if later justified:

```text
DB A WaitCount / WaitDuration
DB B WaitCount / WaitDuration
query duration by bounded operation class
transaction duration
WAL/checkpoint-related latency where observable
```

## Accepted-source isolated read-cost calibration — 2026-09-12

Using the same accepted-source isolated Owner backend described in R-016, `/api/tasks` was polled alone every 2 seconds against a consistent database backup containing 30 recent terminal tasks.

Across three repeats, endpoint median latency was approximately **8 ms, 11 ms and 29 ms**. In the two steady-state repeats, process read-I/O increased by about **0.51 MiB per five calls** above the adjacent no-request baseline, or roughly **0.10 MiB/call** (~3 MiB/min at a 2-second cadence). No sandbox subprocesses were created by this endpoint.

The terminal-only result does not establish dominant contention, but the active WebTurn path has now been measured separately. A research DB-copy fixture exposed one `RUNNING` task with 19 durable turns totaling ~919 KiB. Across three repeats, 10 `/api/tasks` calls consumed ~9.80–9.84 MiB process read I/O versus ~1.25–1.29 MiB in the terminal-only phase — a stable **~8.55 MiB extra per 10 calls**. Median endpoint latency rose from 10.7–27.1 ms to 38.0–45.5 ms, while returned JSON grew by only ~975 bytes.

This confirms meaningful full-row reread amplification in `liveUsageForTask -> ListWebTurnsByTaskEpoch`, but still does **not** prove harmful SQLite lock contention with daemon writes. The evidence supports a narrower optimization direction: expose/query only the response usage + pending/last-turn metadata needed for the task list instead of hydrating full request JSON on every poll.

Any future DB architecture change still requires demonstrated authority-write interference. Query-count aesthetics alone remain insufficient justification.

## What should not be proposed from current evidence

Do not propose:

- `SetMaxOpenConns(4)` as a generic fix;
- a new read-only database connection merely to separate Owner reads from runtime writes — those paths are already process/handle-separated;
- weaker `synchronous` or durability settings;
- schema/authority changes.

The lowest-risk optimization hypotheses remain above the DB layer:

1. stop rereading full request payloads for summary telemetry;
2. batch N+1 Owner reads;
3. reduce/adapt polling cadence;
4. cache boot-scoped sandbox readiness safely;
5. measure again.

These can reduce load regardless of whether SQLite lock contention is ultimately material.

## Safety invariants

Any future DB-related change must preserve:

- one durable coordination truth;
- daemon authority lease semantics;
- atomic task/attempt/fencing transitions;
- idempotency;
- migration/schema correctness;
- FULL durability unless separately re-justified;
- no observer promotion into execution authority.

## Corrected phase-0 decision

`P0_SINGLE_CONNECTION_CONTENTION` is withdrawn as stated.

The corrected research hypothesis is:

> Owner Console's frequent/full-payload reads may create measurable parent-process and shared SQLite/WAL/filesystem pressure, but they are not all serialized through the same Go SQL connection as daemon authority writes.

Run Console closed/open A/B before any database architecture change. If no meaningful task/authority latency delta appears, close this research branch and optimize only obvious UI/read waste where worthwhile.
