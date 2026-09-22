# MAR Conditional Kernel Responsibility Inventory

Date: 2026-09-22
Status: ACTIVE MEASUREMENT BASELINE
Architecture authority: docs/architecture/MAR_CONDITIONAL_EXECUTION_KERNEL.md

## Baseline

Historical local database at HEAD 9c2aae4 contains 318 tasks:
- BLOCKED: 168 (52.8%)
- CANCELLED: 64 (20.1%)
- COMPLETE: 86 (27.0%)

There are 464 execution attempts. Dominant terminal classes:
- worker-budget-exhausted: 182
- worker-exited-before-verification-finalization: 140
- worker-process-error: 58
- worker-blocked:other: 19
- worker-blocked:model: 9
- worker-blocked:decision-projection: 3

Web cognition is pervasive:
- 2,224 web_turn rows
- 296/318 tasks have web turns
- 79 tasks have at least one pending web turn

This history mixes development experiments and multiple MAR generations, so it is not a causal benchmark. It is strong evidence that lifecycle/cognition/runtime coupling is material enough to justify reduction and prospective measurement.

## Code footprint

Approximate Go LOC by package:
- internal/agent: 2,475
- internal/contextengine: 2,601
- internal/worker: 2,025
- internal/mcpedge: 1,499
- internal/orchestrator: 5,739
- internal/verification: 2,205
- internal/integration: 1,507
- internal/processctl: 3,265
- internal/resourcegov: 1,162
- internal/workspace: 1,752
- internal/service: 4,038
- internal/store: 6,267

The obvious cognition/harness cluster (agent + contextengine + worker + mcpedge) is about 8.6k LOC, comparable to the core safety packages verification + integration + processctl + resourcegov + workspace (~9.9k LOC). It is therefore not a thin adapter.

## Responsibility classification

### KERNEL

Retain:
- durable task/attempt authority and run-epoch fencing
- OS/process/filesystem isolation and physical termination proof
- resource governance when explicitly required
- candidate/revision identity
- criterion-bound verification/evidence
- expected-head/CAS integration
- ambiguous external-effect reconciliation when MAR owns the effect
- governed crash/recovery/reconciliation
- runtime/artifact promotion identity

### COMPAT_HARNESS

Preserve for compatibility, freeze expansion:
- provider-mode cognition
- Web-brain relay and web_turn persistence
- brain_turn / brain_respond MCP surface
- DecisionProjection and cognition-delta shaping
- Project Brain/context assembly used to feed MAR-owned cognition
- agent prompt/session/tool loop
- provider/model/reasoning configuration in worker startup

Representative paths:
- internal/agent/**
- internal/contextengine/**
- internal/domain/web_turn.go
- internal/service/web_turn_service.go
- internal/store/web_turn.go
- provider/Web branches in internal/worker/child_windows.go
- cognition fields in internal/worker/protocol.go
- brain_turn / brain_respond in internal/mcpedge/**
- provider/model CLI wiring in cmd/mar/main.go

## First deletion seam

Current worker.StartRequest mixes governed execution truth with cognition implementation:
- Task / Attempt / WorkspacePath / sandbox and resource fields are kernel-facing.
- ProviderConfig / AgentProfile / AgentConfig are harness-facing.
- child_windows.go constructs OpenAI/Web providers, model Gateway, contextengine and agent loop inside the worker process.
- parent/child RPC also carries DecisionProjectionState and WebTurn.

First bounded refactor: introduce a harness-neutral execution/harness configuration boundary while preserving current JSON/wire compatibility and all runtime behavior. Do not delete Web/provider support in this slice.

Acceptance:
1. durable authority/evidence schema unchanged;
2. current worker start frames remain backward-compatible;
3. kernel execution config can be reasoned about without provider/model/session fields;
4. provider/Web behavior remains compatibility-only;
5. focused worker/orchestrator tests and full applicable Go tests pass;
6. net code/lifecycle growth must be minimal and create a future deletion seam, not another orchestration layer.

## Measurement rule

Future MAR metrics use governed tasks only. Track:
- block/cancel causes
- waiting/resource/input duration
- worker restarts
- authority/fencing failures
- time-to-candidate
- candidate-to-verified/integrated duration
- owner interventions
- cognition/Web-wait contribution

A future harness adapter is accepted only when it enables deletion/retirement of equivalent MAR responsibility. Adding another session lifecycle without deletion is rejected.
