# R-002 — Automated Measurement and Evaluation of MAR

**Status:** `RESEARCH_ONLY`  
**Baseline:** MAR v1.1.0  
**Code changes authorized:** none  
**Date:** 2026-09-12

## Research question

How can MAR be continuously measured and evaluated with minimal Owner effort so that future changes are driven by evidence rather than anecdotes, while keeping MAR v1.1 production behavior unchanged during the research phase?

The desired end state is an automated loop:

```text
real work + controlled probes
          ↓
passive measurements
          ↓
normalized task traces / metrics
          ↓
baseline comparison
          ↓
regression / anomaly detection
          ↓
research finding
          ↓
only then: candidate V1.2 requirement
```

The measurement system must separate model/provider delay from MAR overhead, must not treat missing telemetry as zero, and must not become a second execution authority.

## Current MAR measurement assets

MAR v1.1 already exposes useful foundations that should be reused before adding instrumentation:

- durable `TaskResult.ResourceSummary` with agent turns, agent tool calls and model input/output/total tokens;
- durable `WebTurn` records bound to task, attempt and run epoch, with request/response payloads and timestamps;
- Owner Console runtime snapshots, task/run-epoch state, connection status, request counters where available, active-session truth where available, live Web Brain usage and durable usage summaries;
- durable task/attempt/checkpoint/verification/integration truth;
- connection health/readiness and recent activity;
- Windows process/resource governance and existing recovery/acceptance tests.

These sources are useful but not yet sufficient to answer where wall-clock time, context amplification, disconnect risk or host pressure is spent across an entire task.

## Missing evidence to research

Before implementing any new production telemetry, determine the minimum signals needed for the following gaps:

1. **Stage duration:** time spent in submit/admission, context build, external brain wait, worker startup, worker execution, verification, integration and blocked/waiting states.
2. **Transport latency and failure:** request duration, status/error class, reconnect count, tunnel loss/recovery and ambiguous retry count.
3. **Context amplification:** request/response bytes, Decision Projection bytes, artifact-handle bytes, repeated-context estimate and external-cognition token usage per turn/task.
4. **Host pressure:** process count, MAR/worker/browser memory where observable, Windows committed bytes/pagefile pressure, CPU and free disk over task time.
5. **Recovery quality:** interruption start, recovery start, recovery completion, duplicate mutation count, stale-authority rejection and Owner intervention.
6. **Outcome quality:** verification verdict, integration state, acceptance result and whether the useful Goal/CUJ completed.

## Proposed automation architecture

### Layer A — Passive real-work measurement

Every real MAR task should eventually produce one bounded **Task Measurement Record** derived from existing durable truth plus monotonic timings. The record is analytical evidence only and must not own task state.

Conceptual identity:

```text
task_id
run_epoch / attempt lineage
product/source revision
model/provider identity when authoritative
transport mode
measurement schema version
```

Conceptual measurements:

```text
time_to_first_action
time_to_first_verified_progress
total_wall_time
active_execution_time
external_model_wait_time
mar_overhead_time
verification_time
integration_time
remote_call_count
reconnect_count
transport_error_count
request_bytes / response_bytes
model_input_tokens / output_tokens
worker_start_count / replacement_count
owner_intervention_count
peak_host_commit_pressure
peak_relevant_process_memory
final_verdict
product_acceptance_outcome
```

Do not force every value to exist. Unknown must remain `UNAVAILABLE` with source/reason.

### Layer B — Read-only Research Observer

The preferred first experiment is a separate, read-only **Research Observer**, not production instrumentation inside MAR.

It would be allowed to read only already-exposed/durable evidence such as:

- Owner Console/local telemetry endpoints;
- read-only MAR SQLite data or explicitly exported snapshots;
- process/OS performance counters;
- Git identity;
- bounded local logs/health endpoints.

It must not submit, steer, cancel, retry, mutate Git, write MAR SQLite, rotate tunnels or otherwise participate in execution authority.

The Observer would write research data outside canonical product source, for example under a dedicated local research-data root. Repository docs retain conclusions and benchmark definitions, not large raw traces.

This sidecar approach allows measurement to begin without changing the accepted MAR v1.1 runtime. If it proves insufficient, missing signals become evidence for narrowly scoped future V1.2 instrumentation.

### Layer C — Controlled benchmark suite

Real-work telemetry alone is noisy. Maintain a small fixed suite of representative task classes and run them periodically under controlled conditions.

Candidate classes:

1. bounded project read/context task;
2. small worker edit + focused verification;
3. external Web Brain turn + worker task;
4. verification-heavy task;
5. reconnect/disconnect recovery task;
6. worker-crash recovery task;
7. concurrent two-task workload;
8. context-heavy task with large observations/artifacts.

Each benchmark must start from an identified source/environment and use a fixed acceptance oracle. Repeated trials should record environmental pressure so host noise is not mistaken for agent/runtime quality.

### Layer D — Failure-injection benchmark

For transport/recovery research, intentionally interrupt only disposable transport/process boundaries while preserving safety constraints.

Example schedule for a long task:

```text
T+3m   disconnect Web/browser client
T+7m   stop disposable tunnel transport
T+10m  restore tunnel
T+15m  short network interruption
T+20m  reconnect client and inspect task
```

Primary acceptance:

- zero duplicate authoritative mutations;
- zero lost durable task state;
- zero false completion;
- stale actors remain fenced;
- reconnect does not require full historical transcript replay;
- task either continues safely or reaches an explicit durable wait/block state;
- recovery path and final candidate identity are observable.

### Layer E — Baseline and regression detector

Do not compare single runs. Maintain rolling distributions per benchmark/task class and environment profile.

Candidate automatic signals:

- median and p95 task wall time;
- median/p95 MAR-only overhead where separable;
- transport failure/reconnect rate;
- context bytes and tokens per successful Goal;
- verification share of total wall time;
- Owner interventions per Goal;
- worker restart/replacement rate;
- product success/verification rate;
- recovery time after induced failure;
- host pressure peaks.

A future regression detector should alert only when a change is both statistically/materially worse and persistent enough to matter. Small noisy fluctuations should not create backlog churn.

## Primary performance decomposition

The first useful invariant is to decompose total task time instead of reporting only one duration:

```text
TOTAL
 = transport/admission
 + MAR context/orchestration
 + external model wait
 + worker startup/execution
 + verification
 + integration
 + explicit blocked/waiting time
```

The categories must not double-count. The purpose is to answer, for example, whether a 20-minute task was slow because the model spent 12 minutes reasoning or because MAR spent 7 minutes on context, reconnect and worker startup.

## Context-efficiency scorecard

For each external cognition episode/task where measurable, collect:

- Decision Projection bytes;
- full request bytes;
- observation/artifact bytes referenced versus embedded;
- response bytes;
- model input/output tokens;
- count of repeated durable facts across turns;
- number of turns before continuation/reset;
- browser/client disconnects during the episode.

Useful derived metrics may include:

```text
context_bytes_per_useful_checkpoint
input_tokens_per_verified_change
remote_calls_per_completed_goal
repeated_context_ratio
```

These are research metrics, not product acceptance criteria until validated.

## Connection-reliability scorecard

Track by transport type and client:

- availability/readiness time;
- successful call rate;
- 5xx/timeout/reset classes;
- disconnects per hour/task;
- mean/median recovery time;
- retries that reused the same operation identity;
- ambiguous operations requiring reconciliation;
- client reconnects that resumed durable work without duplicate mutation.

The target is not an impossible zero-disconnect transport. The target is that transport loss rarely becomes task loss.

## Automated cadence candidate

A future automation should use three cadences rather than one expensive continuous suite:

### Continuous / every real task

Record passive per-task measurements and anomalies.

### Daily lightweight health

Run a small read/context probe plus connection/readiness checks; summarize only failures and material latency drift.

### Weekly evaluation

Run the fixed benchmark suite, aggregate rolling baselines, compare the current stable baseline/candidate, and create one compact research report containing only material changes.

Heavy failure-injection and concurrency benchmarks should be manual or lower-frequency during research so they do not consume resources or destabilize normal work unnecessarily.

## Reporting contract

An automated report should answer five questions first:

1. Did MAR become faster or slower for successful Goals?
2. Did connection/recovery reliability materially change?
3. Did context/tokens/remote-call amplification materially change?
4. Did resource pressure or Owner intervention increase?
5. Is the evidence strong enough to open a research finding or V1.2 candidate requirement?

Suggested verdicts:

- `NO_MATERIAL_CHANGE`
- `IMPROVEMENT_SIGNAL`
- `REGRESSION_SIGNAL`
- `INSUFFICIENT_EVIDENCE`
- `ENVIRONMENT_NOISE`

Never automatically promote a regression signal into production-code work.

## OpenTelemetry / protocol alignment research

OpenTelemetry now has active GenAI semantic conventions for model latency/token usage and demonstrated MCP instrumentation with trace propagation across agent -> MCP service -> downstream calls. MCP 2026-07-28 also moves toward stateless requests and explicit task handles, making correlation by durable task/operation identity more natural.

A future MAR implementation should evaluate whether adopting standard trace/span conventions reduces custom observability code. This is research only; no OpenTelemetry dependency is authorized by this document.

The likely trace shape to evaluate is:

```text
MAR task
 ├─ transport / MCP request
 ├─ context build
 ├─ external cognition turn
 │   ├─ model request
 │   └─ response
 ├─ worker attempt
 │   └─ tool operations
 ├─ verification
 └─ integration
```

Cross-process trace propagation must never replace MAR's existing durable task/attempt/run-epoch authority. Traces are observations, not execution truth.

## Proposed first experiment

Before any production instrumentation change:

1. Select 10-20 recent/representative successful and failed MAR tasks.
2. Determine which proposed metrics can already be reconstructed from v1.1 SQLite, Owner telemetry, Git and OS data.
3. Mark every missing metric explicitly.
4. Run 3-5 controlled baseline tasks and manually validate the reconstructed timeline against observed behavior.
5. Decide whether a read-only Observer can automate enough of the measurement without modifying MAR.
6. Only then propose the minimum missing production signals, if any.

## Promotion gate

This research may justify implementation only if the automated measurement demonstrates a repeated/material issue and can define a benchmark that will prove the candidate improvement. Observability that is merely interesting, expensive to retain, privacy-sensitive, or not actionable should not be added.
