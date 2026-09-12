# R-005 — Phase-0 Observability Gap Report

**Status:** `RESEARCH_ONLY`  
**Baseline:** MAR v1.1.0  
**Code changes authorized:** none  
**Date:** 2026-09-12

## Purpose

Summarize what the first read-only historical reconstruction proves, which P0 questions can already be measured without modifying MAR, and which blind spots are material enough to research next.

The reconstruction tooling ran outside the MAR repository under `D:\MAR-Research\observer` and opened the MAR durable store read-only. No MAR source/runtime behavior or authority was changed.

## Historical dataset

The current store contained 42 terminal tasks: 7 `COMPLETE`, 23 `BLOCKED`, and 12 `CANCELLED`, with 76 execution attempts, 385 durable Web turns, 368 responded Web turns, 13 semantic checkpoints, 13 verification records, 21 task-result rows, and 37 durable controls.

## Reconstruction coverage

Phase-0 reconstructed:

- task wall-time envelope: **42/42 (100%)**;
- attempt timing: **38/42 (90.5%)**;
- Web-turn history: **35/42 (83.3%)**;
- verification evidence: **13/42 (31.0%)**;
- latest durable result: **13/42 (31.0%)**;
- durable ResourceSummary: **13/42 (31.0%)**.

Low verification/result coverage is expected because many historical tasks ended blocked or cancelled before producing a result. It is not evidence that result telemetry is missing for completed successful paths.

## First performance/context findings

Across all 385 historical Web turns:

- durable request payload bytes: **33,871,714** (~32.3 MiB);
- durable response payload bytes: **854,988** (~0.82 MiB);
- average request size: **~87,979 bytes/turn**;
- average response size: **~2,221 bytes/turn**;
- aggregate request/response byte ratio: **~39.6x**.

This does not by itself prove waste: requests legitimately contain context required for reasoning. It does prove that request-side context dominates external-cognition wire volume and is therefore a high-value target for measurement of duplication, Decision Projection size and artifact/reference efficiency.

For the historical sample, `COMPLETE` tasks (n=7) had median wall time ~31.0 min, median attempts 2 and median Web turns 17. `BLOCKED` tasks (n=23) had median wall time ~10.1 min, median attempts 1 and median Web turns 2. `CANCELLED` tasks (n=12) had median wall time ~11.9 min, median attempts 1 and median Web turns 1. Task classes are not normalized, so these values are descriptive only and must not be treated as comparative performance benchmarks.

The largest historical request-volume examples included approximately 6.37 MiB / 56 Web turns / 7 attempts / 88.1 min / COMPLETE; 5.42 MiB / 47 turns / 6 attempts / 117.5 min / COMPLETE; 3.94 MiB / 62 turns / 5 attempts / 74.8 min / BLOCKED; and 3.02 MiB / 35 turns / 5 attempts / 77.6 min / COMPLETE.

These examples show that long tasks can combine many external-cognition turns, multiple attempts and multi-megabyte request traffic. The dataset is sufficient to study whether request growth is driven by legitimate changing evidence or avoidable repeated context.

## What can be automated now without MAR code changes

Strongly supported now:

1. per-terminal-task wall-time envelope;
2. attempt/replacement count and attempt timing envelope;
3. Web-turn count and responded/pending count;
4. external Web-Brain wait per responded turn;
5. durable request/response byte volume;
6. checkpoint/control density;
7. verification/result identity, verdict and ResourceSummary where reached;
8. terminal-state distribution;
9. simple anomaly signals such as unusual turns/attempts/bytes for a comparable fixture.

With live external sampling, the Observer can additionally collect Windows commit/pagefile pressure, MAR/worker process memory and CPU, free disk/process tree, and route/readiness availability when a local observability surface is live.

## Material blind spots

### G1 — Per-call transport latency and disconnect/reconnect timeline

**Priority:** P0 research gap.

Current durable task state proves whether work survived, but does not reconstruct every MCP/tunnel/browser disconnect, retry and recovery interval. This prevents precise attribution of perceived slowness or long-call fragility to the transport layer.

Next research action: external client-side probe/connection sampling around a fixed read-only operation and controlled T7-style disconnect fixture. Do not instrument MAR until external observation proves insufficient.

### G2 — Context construction/decomposition

**Priority:** P0 research gap.

Durable request bytes prove total request volume but do not directly separate Decision Projection, current observation/tool output, durable facts repeated from prior turns, artifact references versus embedded content, or protocol/schema overhead.

Next research action: analyze a bounded sample of stored WebTurn request structures by field/hash/semantic identity without sending them to a model. Produce duplication/component distributions before proposing instrumentation.

### G3 — Precise stage timing

**Priority:** P1.

Task and attempt envelopes exist, but exact context-build, worker-active, verification-start and integration-start timing are incomplete. This limits precise calculation of `MAR-only overhead`.

Next research action: determine whether existing logs/events already expose these boundaries. Only missing boundaries that materially affect a decision should become V1.2 telemetry candidates.

### G4 — Historical host/browser pressure

**Priority:** P1.

Historical CPU/RAM/Windows commit/browser-renderer pressure was not durably sampled. It cannot be reconstructed after the fact.

Next research action: external 5–10 second host sampling during selected tasks. Browser attribution remains research-only unless process identity can be established reliably.

### G5 — Owner interaction outside MAR

**Priority:** P2.

Durable task controls capture explicit MAR interventions but not every manual action in browser/terminal or external setup step.

Next research action: use controlled benchmark fixtures and explicit manual-event markers only when needed; do not add general user surveillance.

## Research ranking after Phase 0

The evidence supports this order:

1. **Context amplification decomposition** — request traffic is already large enough to justify analysis.
2. **Transport/reconnect measurement** — directly addresses long tool-loop disconnect/timeouts and cannot currently be reconstructed precisely.
3. **External host-pressure sampling** — needed for Chrome/Windows commit/resource investigations.
4. **Stage-timing refinement** — only after the first three show where attribution remains ambiguous.
5. **Broader telemetry frameworks** — defer OpenTelemetry/Prometheus/Grafana until a proven blind spot justifies their cost.

## Decision

Phase 0 supports the hypothesis that a standalone read-only Research Observer can answer a substantial portion of MAR performance/reliability questions without production changes.

Therefore do not add production telemetry yet. Continue external measurement, automate passive reconstruction and cheap health probes, run the controlled benchmark cadence from R-004, and open a V1.2 instrumentation proposal only for blind spots that remain decision-blocking after external observation.
