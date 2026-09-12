# R-003 — Read-Only Research Observer for MAR

**Status:** `RESEARCH_ONLY`  
**Baseline:** MAR v1.1.0  
**Code changes authorized:** none  
**Date:** 2026-09-12

## Research question

How much of MAR's performance, reliability, context use, recovery and resource behavior can be reconstructed automatically from MAR v1.1's existing durable truth and operating-system observations, without adding production instrumentation or changing execution behavior?

The preferred experiment is a separate read-only observer. Its job is analytical only:

```text
MAR v1.1 durable/runtime truth + OS counters
                 ↓
        Read-Only Research Observer
                 ↓
      normalized local measurements
                 ↓
     task timelines / benchmark records
                 ↓
       baseline + anomaly analysis
```

The Observer must never become execution authority, a retry engine, a second scheduler, a task-state store used by MAR, or a source of product truth.

## Initial read-only probe against the real MAR database

A read-only SQLite probe was executed against the accepted v1.1 runtime database using SQLite URI `mode=ro`. It did not mutate MAR state.

Observed durable population at the time of the probe:

- 42 tasks total;
- task states: 23 `BLOCKED`, 12 `CANCELLED`, 7 `COMPLETE`;
- 76 execution-attempt rows across 38 tasks;
- 39 tasks with workspaces;
- 385 Web-turn rows across 35 tasks;
- 27 tasks with at least one responded Web turn;
- 13 tasks with verification evidence;
- 21 TaskResult rows across 13 distinct tasks;
- 7 tasks with integration attempts;
- 13 semantic-checkpoint rows across 10 tasks;
- 13 effect-intent rows;
- no durable Owner-feedback rows yet.

Coverage over all 42 historical tasks from existing durable truth:

| Signal | Tasks covered | Coverage |
| --- | ---: | ---: |
| execution attempt exists | 38 | 90.5% |
| workspace exists | 39 | 92.9% |
| Web turn exists | 35 | 83.3% |
| responded Web turn exists | 27 | 64.3% |
| semantic checkpoint exists | 10 | 23.8% |
| verification evidence exists | 13 | 31.0% |
| TaskResult exists | 13 | 31.0% |
| integration attempt exists | 7 | 16.7% |

For the 13 tasks with a durable latest TaskResult, the existing timestamps were sufficient to derive the following historical observations:

- task-record lifetime (`tasks.created_at -> tasks.updated_at`): 13/13 covered, median ~495.5 s;
- submit-to-first-attempt (`tasks.created_at -> first execution_attempt.started_at`): 13/13 covered, median ~0.94 s, observed p95-like sample ~2.12 s;
- first-to-last attempt span: 13/13 covered, median ~493.8 s;
- integration-attempt duration where integration rows exist: 7/13 covered, median ~1.25 s.

These durations are evidence about recorded timestamps, not yet semantic stage timings. In particular, attempt span can include waiting for external cognition and task `updated_at` is not guaranteed to mean an immutable completion timestamp.

### Initial Web-turn traffic signal

The same read-only probe measured only persisted JSON lengths and timestamps; it did not export prompt/response content.

Across 385 durable Web turns:

- stored request JSON: 33,871,714 bytes;
- stored response JSON: 854,988 bytes;
- request/response stored-byte ratio: ~39.6:1;
- cumulative responded-turn wait time: ~27,752.9 s (~7.7 h);
- pending Web-turn rows at probe time: 17.

A sampled response shape confirmed durable usage metadata with `input_tokens`, `output_tokens`, `total_tokens`, and `estimated` fields.

The ~39.6:1 ratio is a research signal, not proof of a context defect. Stored JSON bytes are not identical to on-wire bytes, and task complexity differs. It does justify deeper context-amplification measurement before proposing redesign.

## Existing V1.1 sources the Observer can reuse

### 1. `tasks`

Useful fields:

- task identity and project identity;
- state;
- `run_epoch`;
- `created_at` / `updated_at`;
- contract hash.

Do not export raw `contract_json` by default. Goal/product text is unnecessary for most performance analysis and can contain sensitive project context.

### 2. `execution_attempts`

Useful fields:

- task/run-epoch lineage;
- authority state;
- `started_at`, `heartbeat_at`, `terminated_at`;
- terminal status;
- attempt/replacement counts.

This is the primary durable source for worker-attempt lifetime and replacement/recovery lineage.

### 3. `workspaces`

Useful fields:

- base/head revision identity;
- workspace state;
- created/updated/removed timestamps.

Do not export full paths unless needed for environment-profile identity; prefer a normalized project/workspace identifier.

### 4. `web_turns`

Useful fields:

- exact task/attempt/run-epoch lineage;
- request ID and turn ID;
- request/response hashes;
- `created_at` / `responded_at`;
- `length(request_json)` / `length(response_json)`;
- response usage metadata when available.

The Observer should query byte lengths and usage fields without copying message/code contents into research storage.

### 5. `semantic_checkpoints`

Useful fields:

- checkpoint count and version;
- candidate/current revision identity;
- checkpoint creation time.

Do not export checkpoint payload contents by default.

### 6. `verification_evidence`

Useful fields:

- task/attempt/run epoch;
- candidate revision;
- verification profile identity/hash;
- verdict;
- evidence creation time.

The current table provides evidence-completion time but not an explicit verification-start timestamp, so exact verification duration cannot be reconstructed from this table alone.

### 7. `task_results`

Useful fields:

- latest result/version;
- candidate/final revision;
- verdict;
- integration status;
- ResourceSummary: agent turns, tool calls, model input/output/total tokens;
- result creation time.

Do not export detailed evidence text or unresolved-risk bodies unless a specific research question requires them.

### 8. `integration_attempts`

Useful fields:

- created/updated timestamps;
- expected/candidate/observed revision identity;
- status/failure class.

This currently gives one of the cleanest stage durations because both start-like and update timestamps exist.

### 9. `effect_intents`

Useful fields:

- effect count;
- state;
- reconciliation count;
- created/updated/dispatched/observed timestamps.

This can support duplicate/ambiguous-effect and reconciliation research without exporting effect payloads.

### 10. Runtime/local observation

Useful existing surfaces include Owner Console runtime snapshots, connection readiness/status, bounded request counters where available, active task/run epoch, live Web usage, health endpoints, Git identity and MAR-owned process identity.

These are runtime observations rather than durable historical truth. The Observer may sample them, but must label gaps and stale samples explicitly.

## Exact, derived and currently unavailable metrics

The Observer must attach a provenance/quality class to every metric.

### `DURABLE_EXACT`

Can be derived directly from existing durable timestamps/counters without interpreting stage semantics:

- task creation/update timestamps;
- attempt start/heartbeat/termination timestamps;
- attempt/replacement count;
- Web-turn creation/response timestamps;
- persisted Web-turn JSON byte lengths;
- persisted Web-turn usage fields;
- checkpoint/evidence/result timestamps;
- integration created/updated timestamps;
- integration/verdict/state identities;
- effect reconciliation counts/timestamps;
- ResourceSummary totals.

### `DERIVED_APPROXIMATE`

Useful but must not be named as a stronger semantic fact:

- task-record lifetime from `created_at -> updated_at`;
- submit-to-first-attempt as an admission/start proxy;
- execution-attempt span as a broad active-attempt window;
- cumulative responded Web-turn wait as external-cognition wait evidence;
- stored JSON length as a context-payload proxy;
- time between checkpoint/evidence/result events as coarse stage boundaries.

Do not report attempt span as pure worker CPU/execution time, or stored JSON bytes as exact network bytes.

### `UNAVAILABLE_IN_V1_1_HISTORY`

The initial probe found no authoritative historical source for:

- per-MCP/HTTP request latency and error class across all calls;
- disconnect/reconnect events and exact tunnel-downtime windows;
- context-engine build start/end duration;
- Decision Projection byte size as a separate field;
- worker-process launch latency separated from attempt admission;
- verification start time separated from evidence completion;
- continuous CPU/RAM/Windows commit/pagefile/disk-pressure history;
- Chrome renderer/browser memory history;
- exact Owner manual-intervention count for past tasks;
- exact time-to-first-useful-progress as a domain-semantic milestone.

These are candidate live-sampling signals or future narrow V1.2 instrumentation gaps. Their absence does not authorize adding telemetry yet.

## Observer safety and privacy contract

The Observer should default to metadata-only collection.

### Must not copy by default

- `contract_json` content;
- Web-turn request/response message content;
- checkpoint payloads;
- effect payloads/results;
- verification command output/evidence bodies;
- environment variable values;
- connector path tokens, tunnel credentials or API keys;
- process command lines that may contain secrets.

### Safe default fields

Prefer:

- IDs/hashes;
- state/status/verdict enums;
- timestamps/durations;
- row/turn/tool/attempt counts;
- byte lengths;
- token counts;
- process IDs/names and resource counters;
- normalized revision identities;
- error classes after redaction.

The Observer's own research store must live outside canonical Git source and must never be read by MAR for authority or recovery decisions.

## Candidate Observer collection loop

This is a research design, not an implementation authorization.

### Incremental durable scan

While MAR has active/recent tasks:

1. open MAR SQLite read-only;
2. query only metadata newer than the last local analytical cursor;
3. use `length(...)`, hashes and timestamps instead of copying large JSON payloads;
4. parse only bounded usage/status fields needed for measurement;
5. close/renew read transactions quickly so the Observer does not hold long snapshots.

No writes, migrations, PRAGMAs that change database behavior, locks held across sleeps, or repair actions are allowed.

### Adaptive live sampling

Candidate cadence to validate experimentally:

- active MAR task: runtime/connection snapshot every 2–5 s;
- relevant OS/process counters: every ~5 s;
- idle system: back off to 30–60 s or stop sampling;
- lightweight daily health probes: once per day;
- fixed benchmark suite: weekly;
- destructive/failure-injection probes: manual or low-frequency controlled runs only.

The correct interval should be chosen after measuring observer overhead. Do not assume 2 s is free merely because the Owner UI already polls at that cadence.

### Local analytical output

Candidate local-only records:

```text
observer_run
environment_profile
raw_metadata_event
os_sample
connection_sample
task_measurement_record
benchmark_run
anomaly_signal
```

A small observer-owned SQLite/JSONL store is acceptable for research if it is explicitly analytical and never becomes MAR task truth. Large traces stay out of Git. Repository documents preserve only benchmark definitions, durable conclusions and material findings.

## Task Measurement Record v0 research shape

A future experimental record should distinguish values from provenance:

```text
task_id
project_id
run_epoch_count
attempt_count
terminal_state
final_verdict
integration_status

observed_task_record_lifetime_ms
submit_to_first_attempt_ms
attempt_span_ms
web_turn_count
responded_web_turn_count
web_wait_ms
web_request_stored_bytes
web_response_stored_bytes
model_input_tokens
model_output_tokens
model_total_tokens
checkpoint_count
verification_evidence_count
integration_attempt_count
effect_count
reconciliation_count

live_transport_error_count        = UNAVAILABLE or sampled
live_reconnect_count              = UNAVAILABLE or sampled
peak_mar_rss                      = UNAVAILABLE or sampled
peak_worker_rss                   = UNAVAILABLE or sampled
peak_system_commit_ratio          = UNAVAILABLE or sampled
browser_renderer_memory           = UNAVAILABLE or sampled

metric_provenance[]
observer_schema_version
environment_profile_hash
```

Do not synthesize unavailable values as zero.

## Automatic baseline strategy

### Real work

Use real tasks for broad trend discovery, anomaly examples and workload realism. Do not compare two arbitrary real tasks as if they have equal difficulty.

### Fixed benchmarks

Use controlled task classes from R-002 for regression detection. A benchmark result should be grouped only with the same fixture, source/environment class and material runtime configuration.

### Baseline maturity

Do not set strict automatic regression thresholds from the current small/noisy history. First collect repeated comparable runs. Until then, reports should use:

- `OBSERVED_CHANGE`;
- `POSSIBLE_REGRESSION`;
- `POSSIBLE_IMPROVEMENT`;
- `ENVIRONMENT_NOISE`;
- `INSUFFICIENT_COMPARABLE_EVIDENCE`.

Promotion to an implementation candidate requires persistent evidence, not one slow run.

## Initial conclusions from the probe

1. **A read-only Observer is feasible without changing MAR v1.1.** Core task/attempt/Web-turn/result lineage is already durable.
2. **Historical task timing coverage is stronger than expected for attempts, but weak for semantic stages.** We can measure broad windows now, not yet cleanly separate context build, worker compute, verification and transport.
3. **Web-turn payload amplification deserves dedicated study.** The persisted request bytes were ~39.6x persisted response bytes across the observed history.
4. **Connection/reconnect and host-pressure history are genuine blind spots.** They require live sampling if we want evidence without modifying MAR.
5. **Verification duration is not directly reconstructable.** Evidence completion exists; explicit start timing does not.
6. **Owner-intervention history is absent in the current data.** Future research needs either an external observer event or explicit Owner-feedback use; do not infer it from task states.
7. **The first implementation experiment, if later authorized, should be an external metadata-only observer, not OpenTelemetry or new production tables.** Only proven blind spots should justify future MAR instrumentation.

## Next research experiments

Without changing production code:

1. reconstruct normalized Task Measurement Records for the existing 42-task history using metadata-only queries;
2. identify stale/pending Web-turn patterns by terminal task state without exporting turn content;
3. sample live MAR/worker/system resource counters during normal real work and quantify observer overhead;
4. sample connection/readiness transitions during normal use and one controlled reconnect experiment;
5. define 3–4 fixed, cheap benchmark fixtures before attempting weekly regression detection;
6. after at least 10 comparable runs per cheap fixture, evaluate whether simple rolling median/p95 thresholds are stable enough to automate.

No production-code change is authorized by this document.
