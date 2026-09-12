# R-022 — P0 benchmark sequence for MAR V1.2 research

**Status:** `RESEARCH_ONLY`  
**Baseline:** MAR v1.1.0 / `PRODUCT_ACCEPTED`  
**Code changes authorized:** none  
**Date:** 2026-09-12

## Purpose

R-004 defines the overall passive/daily/weekly benchmark cadence. R-022 narrows the immediate P0 queue using evidence discovered in R-010 through R-021 so research proceeds in a causal order instead of opening more topics indefinitely.

The goal is to answer one question at a time:

> Where does MAR materially spend time/resources or lose reliability on the frozen V1.1 architecture, and which measured problem is large enough to justify a future V1.2 change?

No benchmark result in this document authorizes a production-code change.

## Global validity gate

Every benchmark run must pass these gates before its latency/reliability numbers may enter a baseline.

### G0 — Runtime identity

Record and verify:

- source/tag identity;
- executable path;
- executable SHA-256;
- executable build/source identity when available;
- SQLite schema compatibility;
- benchmark fixture version.

If the executable cannot be bound to the intended source/release baseline, outcome is `NOT_RUN_RUNTIME_IDENTITY`.

### G1 — Host prerequisite

Record:

- sandbox-host readiness for mutation fixtures;
- relevant Go/toolchain identity;
- available RAM / Windows commit pressure;
- free disk / MAR disk budget;
- no unrelated severe host pressure.

If a required safety prerequisite is absent, outcome is `NOT_RUN_ENVIRONMENT` rather than regression.

### G2 — Observer integrity

Confirm the external Research Observer is read-only and its own cost remains bounded. A benchmark whose observer materially changes the workload is invalid.

### G3 — Comparable workload

Only identical fixture/version/transport/model classes are aggregated. Real tasks remain observational evidence, not direct latency peers.

## P0-0 — Runtime child liveness and supervision

Before performance A/B, prove the Owner UI's spawned `mcp-stdio` runtime child is alive and execution-ready. Run the controlled C0-C4 cases from R-026 on a disposable runtime: child death while idle, remote submit after child death, active-worker crash containment, and pending-Web-turn crash window. Runtime-child absence invalidates later task-latency benchmarks because a reachable Owner/remote surface is not equivalent to an active daemon.

Qualitative failure signals include false execution-ready health, lost durable work, dual-daemon authority, surviving stale worker mutation, or false completion. Availability gaps without safety violation remain research signals.

## P0-1 — Owner Console observer-effect A/B

### Question

Does opening the Owner Console materially increase process churn, SQLite pressure, CPU/commit load, or task latency?

### Source evidence

R-016 / R-017 found:

- React polls runtime/tasks every 2 seconds;
- `/api/runtime` calls a real `sandbox-host-check` subprocess;
- `/api/tasks` performs per-task durable reads, including WebTurn history;
- each `store.Open()` deliberately uses one Go SQLite connection, but Owner UI parent and mcp-stdio runtime are separate processes/handles; shared pressure must therefore be measured at the SQLite/WAL/filesystem/host level rather than assumed to be one global pool.

### Fixture

Run a fixed cheap read-only or isolated task sequence under:

- A: Owner Console closed;
- B: Owner Console open on Live/Overview;
- C: Owner Console open on Tasks with one selected task.

Minimum 10 comparable samples per condition before numeric baseline conclusions.

### Measure

- task wall time;
- SQLite operation latency where externally observable;
- MAR process CPU/RSS;
- Windows commit pressure;
- subprocess creation count/rate;
- `sandbox-host-check` process count;
- DB/WAL bytes if available read-only;
- Owner API request latency;
- observer overhead.

### Oracle

A material, repeatable B/C degradation over A is a `MATERIAL_OBSERVER_EFFECT`. One noisy run is `INSUFFICIENT_EVIDENCE`.

## P0-2 — Web Brain wait occupancy and polling amplification

### Question

Does a disconnected or slow external Web Brain consume disproportionate worker/resource/SQLite capacity while no useful cognition is occurring?

### Source evidence

R-018 found that a pending Web turn currently:

- keeps the active worker/daemon slot;
- retains the heavy resource lease;
- reserves the configured execution envelope (~256 MiB RAM and ~256 MiB disk by current defaults);
- counts toward active execution budget;
- polls WebTurn response every 200 ms;
- is also observed by daemon cancellation/status polling every 200 ms;
- can reread full WebTurn request JSON through those paths.

### Fixture

Controlled disposable task requiring one Web turn:

- W0: immediate response;
- W1: 10 s delayed response;
- W2: 60 s delayed response;
- W3: client disappears then reconnects;
- W4: two simultaneous waiters with a third ready task.

### Measure

- active worker count;
- queued task wait time;
- heavy leases held;
- host pressure;
- SQLite read latency / query count proxy;
- WebTurn bytes reread proxy;
- recovery time;
- task/attempt/run-epoch identity before and after reconnect;
- final correctness.

### Oracle

Reliability requires zero lost/duplicate authority. Performance research asks whether waiting work causes measurable starvation or DB pressure relative to W0.

## P0-3 — Context-build cost

### Question

How much time/process/disk work is spent rebuilding repository context per model decision, and when is that rebuild actually necessary?

### Source evidence

R-021 found each context build can perform:

- five contained Git commands (`rev-parse`, tracked, untracked, modified, staged);
- up to 2,000-file / 8 MiB source scan;
- reread and SHA-256 of source;
- lexical/symbol scoring.

DecisionProjection mode also performs an initial context build before the turn loop and then another build in turn 1. Historical evidence shows repository projection bytes are unchanged across many adjacent turns.

### Fixture

Use one fixed repository/Goal with deterministic turns:

- C0: first build;
- C1: immediate rebuild with no workspace change;
- C2: read-only tool turn then rebuild;
- C3: one bounded file mutation then rebuild;
- C4: working-tree mutation with unchanged HEAD;
- C5: commit/revision change.

### Measure

- build duration;
- number/duration of Git subprocesses;
- files/bytes scanned;
- file reads/hash work proxy;
- resulting Pack hash/serialized bytes;
- correctness of revision/dirty-state detection.

### Oracle

Any future reuse/cache hypothesis is valid only if C3-C5 freshness semantics remain fail-closed. A speedup that can miss dirty working-tree changes is rejected.

## P0-4 — Internal DecisionProjection bytes vs external Web-chat amplification

### Question

How much context reduction inside MAR survives the MCP/Web Chat boundary?

### Source evidence

R-006 shows current DecisionProjection replay factor is near 1.04x, with same-request recent evidence duplication around 8.6% in measured projection turns. R-020 shows `brain_turn` must carry the exact pending request and MCP compatibility can serialize structured result in both text and structured forms.

### Fixture

Run fixed Web-turn episodes with 1, 3, 6, and 10 decisions using the same fixture class.

### Measure

- durable `request_json` bytes;
- MCP response wire bytes if externally observable;
- number of Web tool calls;
- Chat/Chrome renderer RSS delta;
- browser commit-pressure delta;
- reconnect/resume context required;
- model input/output tokens when reported;
- same-request duplicate bytes.

### Oracle

Do not claim a browser/context defect from MAR request bytes alone. A finding requires correlation between outer Web interaction growth and browser/client pressure or externally visible context limits.

## P0-5 — Remote HTTP ambiguous-ACK fault injection

### Question

Does MAR preserve exactly-once semantic effects when the caller loses the response around a durable commit boundary?

### Cases

Run each through stateful MCP Link and stateless Secure Tunnel handler where supported:

- submit: disconnect before commit;
- submit: disconnect after commit / before ACK;
- exact idempotency-key retry;
- brain response: disconnect before commit;
- brain response: disconnect after commit / before ACK;
- reconnect through a new remote session;
- durable task continues while transport is unavailable.

### Measure

- durable task/turn count;
- duplicate/conflict count;
- recovery time;
- calls required after reconnect;
- transport/session identities;
- client error class;
- final candidate/result identity.

### Oracle

Qualitative failure if any of these occur:

- duplicate durable task for one semantic submission;
- duplicate accepted brain response;
- lost committed state;
- stale authority accepted;
- false completion.

Latency is secondary to correctness here.

## P0-6 — Tunnel failure-domain classification

### Question

Can external metrics distinguish client/tunnel/origin/MAR failures without adding production instrumentation?

Use cloudflared/admin/metrics where available and the external observer to correlate:

- HA connection state;
- active streams;
- request errors;
- concurrent requests;
- QUIC RTT;
- local MAR readiness;
- remote request outcome.

A 502/timeout must not be assigned to MAR unless the evidence locates the failure at or behind the MAR origin.

## P0-7 — Verification and integration decomposition

### Question

What fraction of verified task wall time is consumed by verification/integration safety work, and which repeated checks are materially expensive?

### Measure

- `go test`, `go vet`, `go build` duration separately;
- environment fingerprint duration;
- candidate-head/cleanliness checks;
- `LatestFreshResult` revalidation cost around integration;
- Git CAS/integration duration;
- total verify-to-complete duration.

### Oracle

No safety check becomes an optimization candidate merely because it repeats. A candidate must preserve the same stale-evidence/head-drift/physical-authority protections under failure injection.

## P0-8 — Stable T1 baseline

Only after P0-1 through P0-7 measurement surfaces are trustworthy:

1. verify runtime identity;
2. prepare sandbox host prerequisite;
3. use the current public-contract-compatible T1 harness;
4. collect at least 10 successful comparable runs;
5. retain failures separately;
6. establish median/range before setting regression thresholds.

T1 is a baseline consumer, not the first diagnostic tool. Running it before the preceding measurement work risks producing one wall-clock number with no causal decomposition.

## Stop conditions

Stop a benchmark branch and do not collect more samples when:

- runtime identity is ambiguous;
- safety prerequisite is missing;
- fixture/harness is known to be stale;
- observer overhead is material;
- environment pressure is outside the benchmark class;
- an authoritative safety invariant fails once (duplicate mutation, lost task, false completion, stale writer acceptance).

Safety failures do not require ten samples.

## Research promotion rule

A future V1.2 implementation candidate requires all of:

1. repeatable measured problem;
2. evidence MAR materially contributes to it;
3. bounded change hypothesis;
4. benchmark oracle proving benefit;
5. explicit safety/non-regression gates;
6. expected complexity justified by the measured benefit.

If the measured effect is small, environmental, or already dominated by model/provider time, keep V1.1 behavior.

## P0 evidence ledger — 2026-09-12

The original linear queue has now produced enough evidence that remaining work should be driven by gaps, not by blindly repeating the sequence.

| Branch | Current evidence state | Remaining material gap |
| --- | --- | --- |
| G0/G1 | `LIVE_RUNTIME_INVALID / ISOLATED_SOURCE_RUNTIME_AVAILABLE` | Authoritative root runtime is behaviorally stale; isolated accepted-source benchmarks are valid. Mutation baseline still needs sandbox readiness. |
| P0-0 runtime child | `LIVENESS_GAP_CONFIRMED` | C1/C2 live-proven: parent/health remain reachable after child death; remote submit persists and stalls `SUBMITTED`. Pending-Web-turn crash window / safe recovery can reuse T9 safety evidence plus one isolated liveness case if needed. |
| P0-1 Console observer effect | `MEASURED` | `/api/runtime` subprocess amplification confirmed. `/api/tasks` terminal and active-WebTurn costs measured. Browser-rendering delta remains optional unless Chrome pressure needs attribution. |
| P0-2 Web wait | `HISTORICAL_COST_MEASURED / STARVATION_STRESS_CONFIRMED` | 368 waits reconstructed; historical concurrency never exceeded one. A real daemon/resource-governor overlay stress proves two blocked worker lifetimes consume both default slots/heavy leases and prevent a third READY task from starting until one releases. Capacity risk is confirmed; production incidence remains unobserved. |
| P0-3 context build | `MEASURED` | Repeated build ~0.6–0.7 s; snapshot and reread/hash costs decomposed. Broader cache machinery is not justified yet; only narrow reuse hypotheses remain. |
| P0-4 outer Web context | `REPRESENTATION_AMPLIFICATION_CONFIRMED` | Text+Structured application representation ~2.17x on DecisionProjection sample. Actual Web-client/browser retention and RSS correlation remain unobservable/unmeasured. |
| P0-5 ambiguous ACK | `CORE_CORRECTNESS_PROVEN / SESSION_EXPIRY_PROVEN / TUNNEL_REPLACEMENT_PROVEN` | Submit/brain-response lost-ACK retry and precommit cancellation are proven in stateful/stateless handlers; stateful session expiry recovers via new session + same semantic identity (sentinel 9/9 PASS). A real Quick Tunnel process replacement preserves one pending WebTurn and resumes it through the new route. Actual OS worker-process continuity is `BLOCKED_BY_SANDBOX` on the current boot, not failed; mid-transaction crash injection is optional. |
| P0-6 tunnel classification | `FAULT_DOMAINS_PROVEN` | MAR-owned cloudflared metrics are collected and ownership-disambiguated. Disposable fault injection proves distinct signatures for origin-down/tunnel-alive (`HA=1`, request-errors increment, public 502) versus tunnel-down/origin-alive (local 200, metrics unavailable, public 502). Production incident frequency remains unmeasured. |
| P0-7 verification/integration | `MEASURED` | Verification can dominate some tasks; integration is small. Optimization must remain profile/task-sensitive. |
| P0-8 stable T1 | `BLOCKED` | Authoritative running binary is not accepted V1.1; sandbox host is not prepared. Do not contaminate baseline. |

### Updated execution priority

Research should now prioritize only decisions that could materially change V1.2 architecture:

```text
1. runtime/execution health truth + recovery semantics (P0-0)
2. Web-wait capacity/polling under concurrency (P0-2 W4)
3. real tunnel/origin failure classification and recovery (P0-6)
4. outer Web/client context correlation only if browser/OOM symptoms recur (P0-4)
5. T1 baseline only after accepted runtime identity + sandbox prerequisite are restored
```

P0-1, P0-3 and P0-7 no longer need broad measurement expansion. Their current data is sufficient to rank bounded future optimization hypotheses. P0-5 correctness should not be reopened unless a new failure window is identified.

This ledger replaces the earlier assumption that every P0 branch needs equal additional sampling. The purpose is convergence, not indefinite observability work.
