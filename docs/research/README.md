# MAR Research Program

**Status:** `RESEARCH_ONLY`  
**Stable product baseline:** `MAR v1.1.0` / `PRODUCT_ACCEPTED`  
**Implementation authority:** none — this directory does not authorize production-code changes  
**Started:** 2026-09-12

## Purpose

This directory is the durable source of truth for research that may inform a future MAR release. MAR v1.1 remains the frozen stable baseline while research continues.

The primary subject is **MAR itself**: speed, reliability, transport, context efficiency, recovery, concurrency, observability, resource use, self-hosting quality, and adaptation to stronger AI models. CADS is used only as a workflow reference for what MAR must support; CADS is not the primary research target here.

Research must not silently become implementation scope. A finding becomes a future requirement only after evidence is strong enough to justify the cost, trade-offs, and regression risk.

## Research rules

1. **No production-code changes during the research phase.** Research may inspect source, runtime behavior, logs, metrics, protocols, external systems, and current standards.
2. **Measure before redesign.** Prefer task-level evidence and reproducible probes over intuition.
3. **Separate MAR cost from model cost.** A slow model response is not automatically a MAR performance defect.
4. **Prefer deletion/simplification over new machinery.** Stronger models may make previous scaffolding unnecessary.
5. **Transport is disposable; execution truth must be durable.** Losing a browser, MCP connection, tunnel, or Web session should not by itself corrupt or duplicate a durable MAR task.
6. **Do not weaken V1.1 safety/authority invariants from research alone.** Any future change to fencing, sandboxing, side-effect safety, verification authority, or source-of-truth rules requires explicit evidence and review.
7. **CADS is a workflow reference.** Use it to evaluate whether MAR supports Goal/CUJ/acceptance, Tech-Lead/Worker handoff, evidence, and low-code Owner interaction without unnecessary friction.
8. **Frontier-model changes are inputs, not automatic requirements.** New model capability should trigger reevaluation of both opportunities and obsolete scaffolding.

## Primary research axes

### R1 — End-to-end speed

Measure time-to-first-action, time-to-useful-progress, wall-clock completion time, worker startup, context construction, model wait, verification, Git/integration, and transport overhead.

### R2 — Connection and transport reliability

Study 502/504, tunnel lifecycle, reconnect, stale routes, browser/client disconnects, MCP semantics, stable versus temporary transport, and whether long work can survive short-lived connections.

### R3 — Context efficiency and browser pressure

Measure request/response bytes, repeated context, Decision Projection size, tool-result amplification, artifact/handle use, model tokens, browser/renderer memory, and whether reconnect requires historical replay.

### R4 — Long-running cognition and execution

Study continuous sessions versus bounded episodes, checkpoints, external cognition loss/reconnect, task handles, resumability, safe interruption, and steering without forcing the Web client to remain continuously connected.

### R5 — Runtime recovery

Exercise daemon crash, worker crash, browser loss, tunnel loss, network interruption, machine restart, disk pressure, pagefile/commit pressure, stale process, stale brain turn, and partial side effects.

### R6 — Concurrency and throughput

Study multiple projects, multiple GPT/Claude clients, workers/subagents, SQLite/Git/workspace contention, CPU/RAM pressure, fairness, scheduler behavior, and safe parallelism.

### R7 — Brain/worker and model adaptation

Compare one brain + one worker with planner/reviewer/subagent/team patterns only when evidence justifies them. Track stronger model generations as replaceable intelligence engines rather than hard-coding workflow around specific names.

### R8 — Verification efficiency

Measure focused versus broad verification cost, incremental/impact-based verification opportunities, cache validity, parallel-safe checks, and the proportion of total task time spent proving rather than producing the candidate.

### R9 — Observability

Research task-level tracing across client -> transport -> MAR -> brain -> worker -> verifier -> integration, with latency, retries, disconnects, resource pressure, context bytes, tokens, and material state transitions.

### R10 — Resource economy

Measure RAM, Windows commit/pagefile, CPU, process count, disk caches, context bytes, tokens, and wall-clock cost per completed Goal.

### R11 — Self-hosting quality

Compare MAR-through-MAR against direct coding/coordination paths on real tasks. Determine where MAR adds reliability/continuity versus avoidable latency or orchestration overhead.

### R12 — Owner interaction and CADS fit

Measure Owner interventions, terminal/manual actions, questions requiring technical knowledge, visibility of current work, steering clarity, and whether MAR supports the actual CADS-style workflow without forcing process ceremony.

## Common task metrics

For representative real tasks, prefer collecting:

- time to first useful action;
- time to first verified progress;
- total completion time;
- model-wait time versus MAR overhead;
- number of remote/MCP calls;
- number and duration of disconnects/reconnects;
- bytes/tokens sent to external cognition;
- repeated-context ratio where measurable;
- browser/host memory and Windows commit pressure;
- worker startup/replacement count;
- verification time;
- Owner intervention count;
- duplicate/ambiguous side effects;
- final correctness and acceptance outcome.

## Decision gate for future implementation

A research item should not become MAR V1.2 implementation scope merely because it is technically interesting. Promotion requires a demonstrated user/system problem, evidence that MAR materially contributes to it, a bounded proposed improvement, expected benefit, regressions/trade-offs, and an acceptance benchmark that can distinguish improvement from placebo.

## Active topics

- [R-001 — Transport-independent long-running execution](001_TRANSPORT_INDEPENDENT_LONG_RUNNING_EXECUTION.md)
- [R-002 — Automated measurement and evaluation](002_AUTOMATED_MEASUREMENT_AND_EVALUATION.md)
- [R-003 — Read-only Research Observer design](003_RESEARCH_OBSERVER_DESIGN.md)
- [R-004 — Automated benchmark cadence](004_AUTOMATED_BENCHMARK_CADENCE.md)
- [R-005 — Phase-0 observability gap report](005_OBSERVABILITY_GAP_REPORT_PHASE0.md)
- [R-006 — Context amplification phase-0](006_CONTEXT_AMPLIFICATION_PHASE0.md)
- [R-007 — Automated Research Observer runtime](007_AUTOMATED_RESEARCH_OBSERVER_RUNTIME.md)
- [R-008 — Active transport watch](008_ACTIVE_TRANSPORT_WATCH.md)
- [R-009 — Host resource pressure watch](009_HOST_RESOURCE_PRESSURE_WATCH.md)
- [R-010 — Stable-source benchmark phase 0](010_STABLE_SOURCE_BENCHMARK_PHASE0.md)
- [R-011 — Remote HTTP disconnect and retry semantics](011_REMOTE_HTTP_DISCONNECT_AND_RETRY.md)
- [R-012 — Release artifact provenance and runtime identity](012_RELEASE_ARTIFACT_PROVENANCE.md)
- [R-013 — Latency stage decomposition](013_LATENCY_STAGE_DECOMPOSITION.md)
- [R-014 — MCP 2026 transport evolution and MAR implications](014_MCP_2026_TRANSPORT_EVOLUTION.md)
- [R-015 — Tunnel failure domains and external metrics](015_TUNNEL_FAILURE_DOMAINS_AND_EXTERNAL_METRICS.md)
- [R-016 — Owner Console observer effect](016_OWNER_CONSOLE_OBSERVER_EFFECT.md)
- [R-017 — SQLite single-connection contention](017_SQLITE_SINGLE_CONNECTION_CONTENTION.md)
- [R-018 — Web Brain wait resource occupancy](018_WEB_BRAIN_WAIT_RESOURCE_OCCUPANCY.md)
- [R-019 — Verification and integration cost decomposition](019_VERIFICATION_AND_INTEGRATION_COST.md)
- [R-020 — External Web cognition context amplification](020_EXTERNAL_WEB_COGNITION_CONTEXT_AMPLIFICATION.md)
- [R-021 — Decision context rebuild cost](021_CONTEXT_REBUILD_COST.md)
- [R-022 — P0 benchmark sequence](022_P0_BENCHMARK_SEQUENCE.md)
- [R-023 — Measurement coverage matrix](023_MEASUREMENT_COVERAGE_MATRIX.md)
- [R-024 — External collector extension plan](024_EXTERNAL_COLLECTOR_EXTENSION_PLAN.md)
- [R-025 — Connection self-healing and health semantics](025_CONNECTION_SELF_HEALING_AND_HEALTH_SEMANTICS.md)
- [R-026 — Runtime child supervision and liveness](026_RUNTIME_CHILD_SUPERVISION_AND_LIVENESS.md)
- [R-027 — MAR V1.2 candidate requirements](027_V1_2_CANDIDATE_REQUIREMENTS.md)
- [R-028 — MAR V1.2 bounded scope decision](028_V1_2_BOUNDED_SCOPE_DECISION.md)

Research is now converged into R-028. More topics should be added only when new evidence invalidates the bounded scope or reveals a distinct release blocker; do not extend the program by default.
