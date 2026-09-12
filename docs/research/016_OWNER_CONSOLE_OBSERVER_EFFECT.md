# R-016 — Owner Console observer effect

**Status:** `RESEARCH_ONLY`  
**Date:** 2026-09-12  
**Verdict:** `MATERIAL_PERFORMANCE_HYPOTHESIS`

## Research question

Does the Owner Console itself create enough subprocess, SQLite, MCP and browser work to measurably reduce MAR throughput or workstation stability while it is merely observing the system?

This is a distinct problem from UI visual quality. The Console is part of MAR's observability path; an observability surface that materially perturbs the system being observed can corrupt performance measurements and degrade normal work.

No production-code change is authorized by this document.

## Source evidence — fixed 2-second operational polling

The React Owner Console currently refreshes both runtime and task-list data every 2 seconds while the app is open:

```text
reloadRuntime()
reloadTasks()
```

Usage refresh runs every 30 seconds. A selected task detail also refreshes every 5 seconds.

The 2-second cadence is unconditional; it does not currently become slower when the system is idle.

## Source evidence — runtime polling launches a real sandbox probe subprocess

`GET /api/runtime` calls `sandboxReadiness()` on every request.

`sandboxReadiness()` invokes the MAR executable as:

```text
mar sandbox-host-check -workspace <probe>
```

`CheckSandboxHostReady()` is not a cached flag read. On Windows it constructs a small readiness environment and executes a real sandboxed `cmd.exe` probe:

```text
type NUL > NUL
```

with a probe timeout of up to 5 seconds.

At a 2-second UI polling interval this can request roughly 30 sandbox-host-check child launches per minute while the Console is open, subject to actual request duration/browser scheduling.

The readiness check is important because Windows can reset the NUL device security descriptor on reboot. The research question is **how often it needs to be re-probed**, not whether the safety prerequisite should be removed.

## Source evidence — task-list polling performs repeated N+1 durable reads

`GET /api/tasks` first reads up to 30 recent tasks. For every returned task it then performs separate reads for:

1. live Web-turn usage;
2. latest task result;
3. latest Owner feedback.

At the current limit that is structurally up to:

```text
1 + (3 * 30) = 91 SQLite query operations per /api/tasks poll
```

before counting work internal to individual queries.

At one poll every 2 seconds, the source-level upper shape is therefore approximately 45.5 query operations/second solely from this task-list path when all 30 slots are populated. This is a structural count, not a measured SQLite CPU cost.

## Source evidence — live token display rereads full WebTurn payloads

For each task with a positive run epoch, live usage calls:

```text
ListWebTurnsByTaskEpoch(taskID, runEpoch, 128)
```

The underlying query selects full fields including:

- `request_json`;
- `response_json`;
- request/response hashes;
- integrity hash;
- timestamps and identities.

Each row is scanned into a full `WebTurn` and integrity-validated. The Owner Console then primarily uses the response usage/timestamps to derive live token counters.

Phase-0 context research already showed Web request payloads are the large side of the durable turn record. Therefore repeatedly reading complete request JSON merely to render token/activity summaries is a credible I/O/allocation amplification hypothesis.

This does **not** prove SQLite is currently a bottleneck. It proves the UI poll cost scales with historical prompt payload rather than only with the small summary the UI displays.

## Source evidence — selected task reads are serialized through one MCP mutex

The selected Task detail refreshes every 5 seconds and issues status, result, inspect and feedback reads concurrently at the HTTP layer.

The first three task reads proxy through `ownerUIBackend.callTool()`. That function holds one global `mcpMu` for the entire `session.CallTool(...)` operation.

Therefore concurrent Owner Console MCP calls become serialized on a single mutex. The same call path is also used for Owner actions such as submit/cancel/input.

Potential failure mode:

```text
slow/background task read
    -> holds mcpMu
    -> another detail read waits
    -> owner action waits behind read traffic
```

This is a head-of-line-blocking hypothesis. It must be benchmarked before proposing lock/session changes because MCP client-session concurrency semantics and authority consistency matter more than micro-optimizing a mutex.

## Why this may matter to earlier symptoms

The Owner has previously observed Chrome memory pressure/OOM and concern about long tool/context activity. Current source evidence suggests at least three independent observation costs that should be separated from execution cost:

- browser polling/rendering;
- repeated MAR sandbox probe process creation;
- repeated SQLite loading/scanning of WebTurn payloads.

Therefore "MAR is using X CPU/RAM while Console is open" must be compared against a Console-closed baseline before attributing resource use to worker execution.

## Phase-0 runtime microbenchmark — `/api/runtime` process effect

On 2026-09-12 the external `D:\MAR-Research` observer ran a read-only two-phase microbenchmark against the live Owner API while the host was under unrelated high CPU/commit pressure. The benchmark is valid for **event-count evidence only**, not latency attribution.

- Baseline: 20 s with no synthetic `/api/runtime` requests.
- Stimulated: 20 s with 10 synthetic `/api/runtime` requests at a 2 s cadence.
- All 10 synthetic requests returned HTTP 200.
- Short-lived `sandbox-host-check` MAR processes observed during the stimulated phase increased by about 11 relative to the baseline window.
- Stimulated request latency had a median around 171 ms and max around 330 ms, but these timing values are explicitly **not baseline-quality** because Windows commit pressure was oscillating between roughly 81% and 99% and unrelated processes were saturating CPU.

This runtime evidence supports the source finding that observing `/api/runtime` causes real sandbox readiness process work. It does **not** yet prove the full Console materially slows task execution; O0–O4 A/B remains required under a valid host environment.

## Benchmark design — Console closed versus open

Use the same machine state and no mutation workload for each phase.

### O0 — Console closed baseline

Measure for a fixed window:

- MAR RSS/CPU;
- Chrome RSS/process count;
- Windows commit pressure;
- child process launches;
- SQLite read activity where observable;
- 8787/8788 latency from an external observer.

### O1 — Live/Overview open and idle

Open Owner Console with no selected Task-detail work. Measure the same signals.

### O2 — Tasks view with one selected task

Keep a representative task selected so the 5-second detail reads are active. Measure the same signals.

### O3 — Active MAR execution with Console closed

Run a fixed benchmark fixture and measure worker/task performance.

### O4 — Same execution with Console open

Repeat the identical fixture with Live/Tasks observation enabled.

The important quantity is not absolute CPU alone but the **observer delta** between comparable phases.

## Additional metrics

Collect if possible:

- `/api/runtime` p50/p95 duration;
- `/api/tasks` p50/p95 duration;
- bytes read from WebTurn rows per task-list poll;
- number of WebTurn rows scanned per poll;
- sandbox-check child launches/minute;
- failed/overlapping runtime polls;
- `mcpMu` wait time versus CallTool execution time;
- Owner action latency with and without background detail polling;
- MAR/Chrome RSS delta with Console open;
- task completion-time delta with Console open.

## Accepted-source isolated backend A/B — 2026-09-12

A research-only binary was built from the current source tree with `production_diff_from_v1.1 = empty`; its Go VCS metadata binds to `d1e04e48898db87669fb1d4852448cbbdc08cea0`. A Go overlay changed only the disposable remote-bridge loopback port `8788 -> 18788`, and the UI ran on `18787` against a `sqlite3.backup()` copy of the authoritative database. The isolated root served the accepted React/Vite asset model (`react-owner-console-v1` + hashed assets). No production runtime, DB or ports were changed.

Three repeated 10-second baseline/runtime-only/tasks-only phases measured backend polling without browser rendering:

```text
/api/runtime, 5 polls / 10 s:
  sandbox-host-check launches     5 / 5 polls in every repeat
  median endpoint latency         ~102–133 ms
  transient process count         2 -> 5
  transient RSS max increase      ~29–31 MiB versus adjacent baseline

/api/tasks, 5 polls / 10 s, 30 terminal tasks:
  sandbox launches                0
  median endpoint latency         ~8–29 ms
  response size                   ~39,985 bytes
  extra process read I/O          ~0.51 MiB / 5 calls in two stable repeats
                                  (~0.10 MiB/call; ~3 MiB/min at 2 s cadence)
```

CPU deltas are noisy and the simple end-of-phase process counters undercount short-lived sandbox-child CPU/I/O, so they are not promoted as authoritative cost numbers. The process-launch and transient-RSS observations are direct.

This changes the research ranking:

1. `/api/runtime` sandbox-readiness polling is a **confirmed observer effect** and the narrowest optimization candidate.
2. `/api/tasks` terminal-task repeated reads are measurable but are not a dominant latency bottleneck on the 30-terminal-task snapshot.
3. Browser/React rendering remains outside this backend-only A/B and must not be inferred from these numbers.

### Active WebTurn task calibration

A follow-up isolated fixture killed the execution child to freeze daemon behavior, then changed only the **research DB copy** so one historical task appeared `RUNNING` at an epoch containing 19 durable WebTurns (~919,469 request+response bytes). The parent `/api/tasks` path was then measured against the same 30-task list before and after activation.

Across three independent repeats of 10 calls each:

```text
terminal-only median latency          10.7 / 18.3 / 27.1 ms
one active 19-turn task median        38.0 / 45.5 / 38.2 ms
median latency delta                 +27.3 / +27.2 / +11.1 ms

terminal read I/O / 10 calls          1.25–1.29 MiB
active read I/O / 10 calls            9.80–9.84 MiB
extra read I/O / 10 calls             ~8.55 MiB (stable all repeats)
extra read I/O / call                 ~0.855 MiB
RSS mean delta                        +3.7–4.3 MiB
response-size delta                   only ~975 bytes
```

The large read-I/O increase with a tiny response-size increase confirms that the cost is primarily internal loading/validation/decoding of historical WebTurn rows for live usage, not bytes sent to the UI. At the current 2-second polling cadence, one such active fixture structurally implies roughly ~25.7 MiB/min of additional process read I/O if the same history is reread on every poll. This is a fixture-specific estimate, not a production-wide rate.

This promotes **summary-only/narrow live-usage reads** above generic terminal-list batching as the more evidence-backed `/api/tasks` optimization target.

## Research Observer self-effect correction — 2026-09-12

The active transport watcher originally called `/api/runtime` every 5 seconds while MAR was running. R-016 proved that each such call executes a real `sandbox-host-check` subprocess, so the observer itself was injecting roughly 12 sandbox-check launches per minute into the system it was supposed to observe.

The external watcher was corrected to classify local readiness from bounded TCP probes plus MAR process-role observation (`ui` parent and `mcp-stdio` child), without calling `/api/runtime` on its 5-second hot path. After restarting the exact research watcher process, a 20-second verification window sampled MAR processes 76 times and observed **zero** `sandbox-host-check` launches. Hourly snapshots may still call `/api/runtime` once because that cadence is low and explicitly accounted for.

This is a methodological result: observer overhead must be part of the benchmark validity gate, not merely measured after the fact.

## Candidate optimizations to benchmark, not implement yet

### C1 — Boot/readiness cache with explicit invalidation

Because the NUL prerequisite is boot-scoped, research whether readiness can be checked at runtime start and after explicit sandbox preparation, then refreshed at a much lower cadence or on relevant failure.

Safety must remain fail-closed. A cached positive result must have a clear invalidation/recheck policy.

### C2 — Summary-only WebTurn usage query

Research a read-only summary path that does not load full historical request bodies when the UI only needs pending state and token usage.

Prefer deriving the smallest truthful query over adding a new durable aggregate schema prematurely.

### C3 — Batch task-list query

Measure whether result/feedback/live-usage data can be fetched in bounded batch queries instead of N+1 per task.

### C4 — Adaptive polling

Compare fixed 2-second polling against activity-aware polling, for example fast only while tasks/connections are changing and slower while idle.

### C5 — Remove read/action head-of-line blocking

Only after mutex-wait measurement, evaluate whether read-only MCP calls can use safe concurrency, separate sessions, or direct bounded service reads without creating semantic divergence.

## Acceptance for any future optimization

A future candidate must preserve:

- truthful sandbox readiness;
- current durable task/result semantics;
- no invented live telemetry;
- Owner actions remaining authoritative and idempotent;
- same visible freshness within a justified tolerance;
- no reduction in security/fencing guarantees.

And it must measurably improve at least one of:

- host CPU/process churn;
- SQLite read/allocation load;
- UI/API latency;
- Chrome/MAR memory pressure;
- task throughput/latency while Console is open.

## Phase-0 decision

Before changing runtime, run the Console closed/open A/B benchmark and add the relevant counters to the external Research Observer where possible.

The current source is sufficient to classify this as a **material performance hypothesis**, but not yet as a V1.2 implementation requirement.
