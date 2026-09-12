# R-003 — Read-Only Research Observer Design

**Status:** `RESEARCH_ONLY`  
**Baseline:** MAR v1.1.0  
**Code changes authorized:** none  
**Date:** 2026-09-12

## Research question

How much of MAR performance, reliability, context use and recovery quality can be reconstructed automatically from the accepted V1.1 system without modifying production code, and what is the smallest safe observer needed to turn that evidence into continuous research data?

## Initial empirical inventory

A read-only inspection of the current MAR durable store found:

- 42 durable tasks;
- 7 `COMPLETE` tasks;
- 23 `BLOCKED` tasks;
- 12 `CANCELLED` tasks;
- 76 execution attempts;
- 385 durable Web turns, of which 368 have responses;
- 13 semantic checkpoints;
- 13 verification-evidence records;
- 21 task-result records;
- 37 task-control records.

The recent task sample already contains realistic long-running behavior: completed tasks with multiple run epochs/attempts, tasks with dozens of Web turns, blocked tasks, cancelled tasks, controls, checkpoints and verified results. This is enough historical variation to test an observer before adding synthetic workload.

The local Owner API at `127.0.0.1:8787` was not listening during one research probe even though the durable store remained readable. Therefore a research observer must not depend exclusively on the Owner Console process being alive.

## Source hierarchy

The Observer should prefer the least coupled, most supported source that can answer a metric.

### Source A — MAR read-only public/local observability surfaces

When MAR is live, prefer existing read-only endpoints and stable read APIs for runtime/connection readiness, recent tasks and task state, task status/result/inspect, usage summaries, and project/revision identity.

### Source B — exported/snapshotted durable evidence

For historical reconstruction or when the live UI/API process is unavailable, prefer an explicit read-only snapshot/export if MAR later exposes one. Such an export should be analytical evidence only and must never become execution authority.

### Source C — SQLite read-only fallback during research

Until an export exists, an external research process may open the current MAR SQLite file in read-only mode for historical analysis. It must use read-only mode, never write/migrate/checkpoint/vacuum the MAR database, never infer runtime authority from analytical reads, tolerate schema evolution by versioning the parser, and stop rather than guess when expected data is absent.

This is a research fallback, not a proposed product API.

### Source D — operating-system observation

Use Windows counters/process inspection only for facts not available from MAR, such as host commit/pagefile pressure, process RSS/private bytes, process count/tree, CPU utilization, free disk, and optionally browser-process pressure where process identity can be established without guessing.

OS samples are observational and must not restart, throttle, kill or reconfigure MAR.

## What V1.1 can already reconstruct

### High-confidence without new instrumentation

1. Task wall-clock envelope — `tasks.created_at -> tasks.updated_at` for terminal historical tasks.
2. Attempt/replacement count — execution-attempt lineage and run epochs.
3. Attempt start/termination envelope — where start/termination timestamps exist.
4. External Web Brain wait — each responded WebTurn has `created_at -> responded_at`.
5. Web-turn volume — turns per task/epoch and pending versus responded turns.
6. Context payload size proxy — serialized WebTurn request/response bytes can be measured from durable payloads without replaying them into a model.
7. Checkpoint density — checkpoint count/timestamps per task/attempt.
8. Verification timepoint and identity — verification evidence is revision/profile/environment bound and timestamped.
9. Result timepoint, resource summary, verdict and integration state — durable TaskResult.
10. Control/Owner intervention proxy — durable task controls by type and timestamp.
11. Retry/replacement density — attempts and run epochs per terminal outcome.
12. Success/blocked/cancelled distributions — durable task states and verified results.

### Partially reconstructable

1. Verification duration — end/timepoint is durable, but precise verification start is not universally represented as one canonical timestamp.
2. Integration duration — integration result/state is durable, but a complete start/end span is not necessarily available for every historical path.
3. MAR orchestration overhead — total envelope minus known waits is only a residual and can contain worker/context/queue time; it must not be labeled precise MAR overhead.
4. Worker active execution time — attempt envelope is available, but it may include waiting and orchestration around active process work.
5. Owner intervention count — controls are strong evidence for explicit controls but do not capture every human interaction outside MAR.
6. Context repetition ratio — request bytes can be compared, but semantic duplication needs a bounded analysis method before it is trustworthy.

### Not reliably reconstructable from V1.1 alone

1. exact context-build latency;
2. exact MCP/tunnel request latency per call;
3. exact reconnect/disconnect timeline across browser/client/tunnel layers;
4. precise worker CPU-active time;
5. continuous Windows commit/RAM/CPU pressure over historical tasks;
6. precise browser renderer memory attributable to MAR interaction;
7. time-to-first-useful-action unless the qualifying action is defined and timestamped by existing evidence;
8. end-to-end distributed trace across client -> transport -> MAR -> brain -> worker -> verification -> integration.

These gaps are candidates for future measurement only if they materially limit a research question. They are not automatic V1.2 instrumentation requirements.

## Observer architecture candidate

```text
MAR public read surfaces ─┐
                          ├─> Collector -> Normalizer -> Task Measurement Record
SQLite read-only fallback ┤                         |
                          │                         v
Windows observations ─────┘                   local research data
                                                    |
                                                    v
                                      baseline / regression analysis
                                                    |
                                                    v
                                         compact research report
```

The Observer is deliberately not a daemon required by MAR. MAR must remain fully functional if the Observer is absent or broken.

## Collector modes

### 1. Passive live sampling

When MAR is running, sample lightweight public runtime/task information at a low cadence, initially no faster than every 5 seconds during an active measurement window and much slower when idle. Avoid overlapping polls. Capture only changed samples where practical.

### 2. Terminal-task reconstruction

When a task becomes terminal, reconstruct a normalized record from durable evidence. This should be the primary data product because terminal truth is less noisy than high-frequency UI snapshots.

### 3. Host-pressure sampling

During selected research windows, capture bounded OS samples, for example every 5–10 seconds, then aggregate to peak/median values. Do not continuously retain raw process telemetry indefinitely.

### 4. Daily lightweight probe

Measure read-only MAR health/read/context latency and connection readiness. Do not submit mutation-capable tasks merely to create metrics.

### 5. Weekly controlled evaluation

Use fixed benchmark fixtures only after historical reconstruction is working. Compare distributions, not single runs.

## Task Measurement Record candidate

Every field carries a source and availability state.

```text
identity:
  task_id
  project_id
  run_epochs
  base/final revision where available
  terminal state
  measurement schema version

outcome:
  verification verdict
  integration status
  result present
  unresolved-risk count

work volume:
  attempts
  web_turns
  responded_web_turns
  checkpoints
  controls
  agent_turns
  agent_tool_calls
  model_input/output/total_tokens

known timing:
  task_wall_time
  web_brain_wait_total
  web_brain_wait_median/p95
  first_attempt_start_offset
  attempt_envelope_total
  verification/result timepoints

context:
  web_request_bytes_total
  web_response_bytes_total
  bytes_per_turn
  tokens_per_result where available

reliability:
  replacement_count
  pending/stale turn observations
  controls by kind
  terminal outcome

host samples when enabled:
  peak_commit_pressure
  peak_process_memory
  peak_cpu
  min_free_disk
```

A value is never silently zero when unavailable. Use `UNAVAILABLE`, `NOT_APPLICABLE`, or a measured value with source.

## Derived metrics with safe semantics

Allowed initial derived metrics:

- `attempts_per_terminal_task`;
- `web_turns_per_terminal_task`;
- `web_wait_share_of_task_envelope` (only when the task envelope is valid; explicitly a share of envelope, not a full stage decomposition);
- `request_bytes_per_web_turn`;
- `input_tokens_per_verified_result`;
- `controls_per_task`;
- `verified_success_rate`;
- `replacement_rate`;
- `terminal_state_distribution`.

Do not yet publish `MAR overhead = total - model wait` as a precise metric. The residual includes unseparated worker, queue, context, verification and integration time.

## Storage and retention candidate

Raw research data should live outside canonical MAR source, for example:

```text
D:\MAR-Research\observer\
  raw\YYYY-MM-DD\
  normalized\task-records\
  baselines\
  reports\
```

Repository `docs/research/` stores only benchmark definitions, schemas/contracts, summarized findings and decisions about whether evidence is sufficient.

Initial retention candidate:

- high-frequency raw OS samples: 14 days;
- normalized task records: retain indefinitely during research unless privacy/size requires otherwise;
- weekly aggregates/reports: retain indefinitely;
- payload bodies: do not copy by default; retain byte counts/hashes/derived metadata unless a bounded investigation specifically needs content.

## Observer overhead budget

The Observer must be cheaper than the effects it measures.

Initial research budget:

- idle CPU: effectively negligible;
- live sampling CPU: target <1% average on the host;
- memory: target <150 MiB working set;
- no additional model calls during passive observation;
- no MAR database writes;
- no overlapping API polls;
- bounded disk growth with retention cleanup;
- any probe that materially changes task scheduling/resource admission invalidates its own performance sample.

These are research targets, not product requirements.

## Phase-0 experiment

Before any observer implementation is proposed for V1.2:

1. reconstruct all existing terminal task records from the 42-task historical store using a one-off read-only research script outside MAR production source;
2. quantify field availability per metric;
3. group comparable tasks only where task class/environment can be identified without guessing;
4. identify which missing metrics actually prevent answering current P0 questions: connection reliability, context amplification and end-to-end latency;
5. produce one `OBSERVABILITY_GAP_REPORT` with evidence-ranked gaps;
6. decide whether a standalone research sidecar is sufficient or whether a narrow MAR instrumentation proposal is justified.

## Exit criteria for R-003

R-003 is ready to promote into an implementation proposal only if historical reconstruction demonstrates useful measurement with no production mutation, at least one material P0 research question cannot be answered from current evidence, the missing signal is precisely defined, the expected instrumentation cost/overhead is bounded, and a benchmark exists that proves the added signal improves a decision rather than merely dashboard detail.

Until then, the correct action is research and external observation, not MAR code changes.
