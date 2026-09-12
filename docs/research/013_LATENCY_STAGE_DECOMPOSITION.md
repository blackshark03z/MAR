# R-013 — Latency Stage Decomposition

**Status:** `RESEARCH_ONLY`  
**Baseline:** MAR v1.1.0  
**Code changes authorized:** none  
**Date:** 2026-09-12

## Research question

How much of a MAR task's wall time can be assigned to distinct stages with defensible evidence, so future performance work optimizes MAR rather than blaming it for model, worker, verification, queue or host time?

## Core rule

Never publish:

```text
MAR overhead = total task time - model wait
```

unless every other material interval has been separated.

The residual currently includes multiple unrelated phases and would be a misleading performance metric.

## Durable timing inventory in V1.1

### Strong timing evidence

**Task envelope**

- `tasks.created_at`;
- terminal/final `tasks.updated_at`.

This supports overall historical wall-clock envelope for terminal tasks, but the row keeps only the latest state timestamp rather than every transition.

**Execution attempts**

- `started_at`;
- `heartbeat_at`;
- `lease_deadline`;
- `terminated_at`;
- run epoch / terminal status.

This supports attempt/replacement envelopes and physical-lifetime evidence. It does not prove every second inside an attempt was active worker compute.

**Web cognition waits**

Each durable Web turn has:

- `created_at`;
- `responded_at`.

This provides the strongest exact interval for external Web Brain waiting and can be summed per task/epoch without guessing.

**Integration**

`integration_attempts` persist:

- `created_at`;
- `updated_at`;
- integration status/failure/identity.

This supports a bounded integration-attempt span.

### Useful timepoints without complete spans

**Workspace**

Workspaces persist `created_at`, `updated_at`, state and removal time where applicable. Creation/readiness can sometimes bound preparation, but one mutable `updated_at` is not a complete history of every workspace state.

**Verification**

Verification evidence and TaskResult have durable `created_at` timestamps. They strongly identify the completed verification/result point, candidate revision, profile/environment and verdict.

However the exact `RUNNING -> VERIFYING` start transition is written by updating the task row; later transitions overwrite `tasks.updated_at`. Historical data therefore does not universally retain one canonical verification-start timestamp.

**Controls/checkpoints/artifacts**

These provide material-event timestamps useful for explaining pauses/retries but are not complete stage boundaries.

## Missing durable spans

Historical V1.1 data cannot precisely assign all of the following without live observation or additional instrumentation:

- submit/admission duration;
- PREFLIGHT duration;
- WAITING_RESOURCE duration;
- workspace preparation start-to-ready for every path with transition-level precision;
- context-build start/end;
- queue/scheduler waiting inside broader envelopes;
- exact worker CPU/tool-active duration versus worker waiting;
- exact verification start-to-end on every historical task;
- REVIEWING / READY_TO_INTEGRATE state duration where later task updates overwrite the timestamp;
- individual remote HTTP/tunnel call latency;
- reconnect recovery intervals before the active transport watcher existed.

There is no separate append-only `task_state_history` table in V1.1; ordinary state transitions update `tasks.state` and `tasks.updated_at` in place.

## What the external Observer can do now

### Historical reconstruction

Use exact durable spans where available:

```text
task_envelope
web_brain_wait_total
attempt_envelope(s)
integration_attempt_span
verification_result_timepoint
workspace/control/checkpoint timepoints
```

Everything else remains `UNAVAILABLE` or explicitly `RESIDUAL_UNATTRIBUTED`.

### Live reconstruction

When MAR is running, the external observer can sample public task state and record transition timestamps outside MAR.

At a 5-second active cadence, this can approximate:

```text
Preparing
Working
Waiting for AI
Verifying/checking
Integrating
Blocked/attention
```

with bounded error of roughly the sampling interval plus request latency.

This live timeline is analytical evidence only. It is not execution authority and can contain gaps if the observer or Owner API is unavailable.

## Stage accounting contract

A future normalized Task Measurement Record should use non-overlapping buckets and preserve confidence/source:

```text
TOTAL_TASK_ENVELOPE

known:
  web_brain_wait
  integration_attempt
  explicit_blocked_wait

bounded/approximate:
  workspace_prepare
  worker_attempt_envelope
  verification_window

unknown:
  residual_unattributed
```

Do not force the buckets to sum to 100% by inventing values. Overlap must be detected instead of silently double-counted.

Each duration should carry metadata similar to:

```text
value_ms
source
precision
confidence
availability
```

Example source values:

- `DURABLE_EXACT`;
- `LIVE_SAMPLE_5S`;
- `DERIVED_BOUND`;
- `UNAVAILABLE`.

## Current accepted-task example

The accepted V1.1 research case previously measured approximately:

- task envelope: about 7 minutes;
- 5 Web turns;
- total durable Web Brain wait: about 2 minutes.

The remaining roughly 5 minutes **must not** be called MAR overhead. It includes some combination of worker execution, context construction, orchestration, verification, integration, scheduling and host effects that the historical store does not fully separate.

This example is why the decomposition contract matters before optimization decisions are made.

## Candidate phase-1 experiment

Once a schema-compatible runnable V1.1 artifact is identity-bound and MAR is live:

1. let the external observer sample state every 5 seconds;
2. run only read-only D1/D2 probes first;
3. compare sampled transitions against durable attempt/WebTurn/integration timestamps;
4. quantify observer timing error and missing intervals;
5. decide whether live external sampling is sufficient for P0 speed research;
6. only if a material stage remains unknowable, consider a narrow future append-only timing/trace signal inside MAR.

## Possible future instrumentation — not authorized

If external observation proves insufficient, the smallest useful production signal may be an append-only material transition/event record rather than a full metrics platform.

Candidate event identity:

```text
task_id
run_epoch
attempt_id where applicable
from_state
to_state
occurred_at
reason/source
```

This would enable durable state-duration reconstruction, but adds schema/write volume and another integrity surface. It must earn its cost through a benchmark decision that cannot otherwise be made.

OpenTelemetry or a broader distributed tracing stack is a later option, not the default first implementation.

## Phase-0 verdict

`PARTIAL_DURABLE_COVERAGE`

V1.1 already contains enough exact timestamps for task envelope, Web Brain waiting, attempts and integration to make useful progress. It does **not** contain enough historical timing truth to attribute all remaining wall time to MAR internals.

The research program should use exact + sampled evidence and retain an explicit unattributed residual instead of fabricating stage precision.
