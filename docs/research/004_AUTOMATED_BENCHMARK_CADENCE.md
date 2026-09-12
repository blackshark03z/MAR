# R-004 — Automated Benchmark Cadence for MAR

**Status:** `RESEARCH_ONLY`  
**Baseline:** MAR v1.1.0  
**Code changes authorized:** none  
**Date:** 2026-09-12

## Research question

How should MAR be evaluated automatically often enough to detect real regressions in speed, context, connection reliability and recovery, without turning the benchmark system itself into a significant source of load, instability or false alarms?

The key design choice is to reuse existing MAR acceptance fixtures wherever possible rather than create a separate benchmark implementation that can drift from product behavior.

## Existing benchmark assets to reuse

The frozen V1 benchmark plan already defines T1–T17 and the current repository includes executable acceptance fixtures for important classes.

High-value existing fixtures include:

- **T1 — Tiny fix:** localized edit + verification; good for startup/admission/worker/verification overhead.
- **T2 — Medium bug/repair loop:** failing test -> repair -> passing verification; good for context/tool-loop efficiency and rework.
- **T3 — Large multi-file change:** useful for sustained context and multi-file coordination.
- **T4 — Deep refactor:** useful for broad context/reasoning and rework.
- **T7 — Client disconnect:** already proves disconnect does not automatically cancel active durable work.
- **T8 — Worker crash:** already proves a worker crash cannot fabricate completion and physical termination remains durable.
- **T9–T17:** daemon crash, cancellation/process tree, resource pressure, base drift, semantic conflicts, stale-worker fencing, ambiguous effects, integration crash, disk reserve.

The benchmark program should call these authoritative fixtures or equivalent product-level paths rather than reimplement their semantics elsewhere.

## Three-cadence model

### 1. Passive — every real MAR task

No synthetic workload.

The Research Observer derives metadata-only Task Measurement Records from normal work and samples live connection/resource state only while useful.

Purpose:

- discover real-world bottlenecks;
- measure workload diversity;
- identify examples for later controlled reproduction;
- observe context/token/call amplification;
- observe normal connection churn and host pressure.

Real tasks are not automatically compared against one another as equal-difficulty benchmarks.

### 2. Daily — cheap health suite

Daily automation should be intentionally small and non-destructive.

Candidate probes:

#### D1 — Local MAR health/readiness

Measure:

- local UI/health readiness;
- daemon availability;
- connection-route readiness/status;
- read-only SQLite open/query latency;
- Git/source identity availability.

No coding worker required.

#### D2 — Control-plane/project read

Perform one bounded project/context read against a fixed small fixture or MAR metadata surface.

Measure:

- request latency;
- response byte size;
- success/error class;
- remote/tunnel availability when the remote path is under test.

This is the cheapest repeated transport/context health signal.

#### D3 — Optional tiny isolated task

Run the existing T1 tiny-fix fixture only when host resource pressure is normal and no important interactive work is active.

Measure:

- submit-to-attempt;
- task wall time;
- attempt count;
- model/tool calls;
- token/context bytes where applicable;
- verification time proxy;
- final VERIFIED/INTEGRATED outcome.

Because T1 uses isolated fixture data, it must never mutate the MAR product worktree.

Daily T1 should remain optional until its measured cost is low enough to justify the cadence.

### 3. Weekly — representative evaluation suite

Candidate weekly core:

- T1 tiny fix;
- T2 medium repair loop;
- one bounded Web-Brain/context-heavy fixture;
- T7 client-disconnect recovery;
- T8 worker-crash recovery;
- one two-task concurrency fixture if host resources allow.

Weekly outputs should compare only like-for-like fixture/environment groups and report distributions, not just one PASS/FAIL.

### 4. Low-frequency / manual — destructive or expensive suite

Do not run the full failure matrix every day or week by default.

Candidates:

- T3/T4 large/refactor workloads;
- T5/T6 multi-task/multi-project pressure;
- T9 daemon crash;
- T10 cancellation during subprocess tree;
- T11 low-memory pressure;
- T12 base drift;
- T13 semantic conflict;
- T14 stale-worker physical/logical fencing;
- T15 crash during side effect;
- T16 crash during integration;
- T17 disk-reserve pressure;
- real tunnel/network interruption experiments.

Run these before a material runtime/transport/recovery release, after relevant architecture changes, or at a low scheduled frequency on a controlled machine window.

## Automatic admission rules for benchmarks

The benchmark runner must not degrade normal Owner work merely to collect metrics.

A future runner should skip/defer heavy probes when any of these are true:

- MAR is executing an Owner-priority task;
- CPU/RAM/commit/disk pressure is above a safe research threshold;
- the machine is in a known unstable/recovery state;
- the relevant external provider/transport is unavailable for reasons unrelated to MAR;
- the fixture's required host prerequisite is not satisfied.

A skipped benchmark is `NOT_RUN`, not `PASS` or `FAIL`.

## Environment profile

Every controlled run needs a bounded environment identity so unrelated host/model changes do not look like MAR regressions.

Candidate profile fields:

```text
MAR source HEAD / release tag
observer schema version
OS/build
machine identity class (not personal identifier)
logical CPU / RAM capacity
free disk bucket
Windows commit-pressure bucket
Go/runtime/toolchain identity
transport mode
model identity / reasoning mode when authoritative
benchmark fixture ID/version
```

Secrets, usernames, full machine identifiers, connector tokens and raw prompts must not be included.

## Metrics by benchmark class

### Universal

- outcome/verdict;
- wall time;
- submit-to-first-attempt;
- attempt/replacement count;
- model turns/tool calls/tokens;
- request/response stored bytes;
- verification/integration evidence;
- observer/host pressure;
- Owner intervention count if applicable.

### Connection/recovery

- interruption start;
- task state at interruption;
- disconnect/reconnect count;
- recovery-observed time;
- duplicate operation count;
- stale-authority rejection;
- lost task/result count;
- historical transcript replay required or not;
- final task/candidate identity preservation.

### Context-heavy

- Decision Projection/context bytes where observable;
- full Web-turn stored request bytes;
- tokens per turn;
- repeated durable fact estimate if later measurable;
- turns/calls per verified result;
- browser/host pressure during episode.

## Baseline construction

Do not establish numeric release thresholds from one run.

Recommended research sequence per cheap fixture:

1. collect at least 10 comparable successful runs;
2. inspect environmental variance and outliers;
3. establish median and p95-like range;
4. collect failure/error rate separately from latency;
5. only then test regression thresholds against historical replay or intentional slowdowns.

Initial report language remains non-authoritative:

- `STABLE_SIGNAL`;
- `POSSIBLE_IMPROVEMENT`;
- `POSSIBLE_REGRESSION`;
- `ENVIRONMENT_NOISE`;
- `INSUFFICIENT_COMPARABLE_EVIDENCE`.

## Candidate regression rules to validate, not yet freeze

A future detector may combine relative and absolute change so tiny baselines do not create noise.

Example research hypothesis:

```text
latency regression signal
  = comparable median worsens materially
    AND absolute delta is operationally meaningful
    AND signal persists across multiple runs
```

Reliability regressions should usually be stricter than small latency changes. One lost task, duplicate authoritative mutation, false completion, stale-worker mutation or invalid verification is a qualitative failure even when latency statistics look normal.

No numeric percentage is frozen by this document.

## Daily report contract

Daily automation should normally be silent when healthy.

If material, report only:

```text
MAR daily health: OK | DEGRADED | RESEARCH_SIGNAL

- availability/readiness change
- cheap latency drift
- transport/error anomaly
- host-pressure anomaly
- pending/stale durable-work anomaly
- next recommended research action, if any
```

Do not create a backlog item from every anomaly.

## Weekly report contract

One compact weekly report should answer:

1. Is successful Goal completion faster/slower for comparable fixtures?
2. Did MAR-only overhead materially move?
3. Did context bytes/tokens/remote calls per successful fixture change?
4. Did connection/recovery reliability change?
5. Did worker/verification/integration behavior regress?
6. Did host pressure or observer overhead become material?
7. Is any signal strong enough to open a dedicated research finding?

Detailed raw measurements stay outside Git. Only durable conclusions and benchmark-definition changes belong in `docs/research/`.

## Automation boundary

The future benchmark scheduler should be outside MAR's execution authority during the research phase. Windows Task Scheduler or another simple host scheduler may start a read-only Observer/report process and isolated fixture runs. MAR itself must not make product decisions based on the research database.

This separation prevents the monitoring experiment from becoming a hidden second orchestrator.

## Recommended rollout order

1. **Observer-only:** reconstruct existing history and sample live OS/connection metadata.
2. **Daily D1/D2:** cheap read-only health probes.
3. **Collect 10+ comparable samples.**
4. **Optional daily T1:** only if measured overhead is acceptably low.
5. **Weekly T1/T2/T7/T8 core.**
6. **Add context-heavy/concurrency fixtures.**
7. **Use destructive T9–T17 only at release/recovery research checkpoints.**
8. **Only after the measurement loop is trusted, consider narrow V1.2 production instrumentation for proven blind spots.**

No production-code change is authorized by this document.
