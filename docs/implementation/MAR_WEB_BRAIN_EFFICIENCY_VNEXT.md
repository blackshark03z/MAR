# MAR Web-Brain Efficiency VNext

**Date:** 2026-09-18  
**Status:** ARCHITECTURE / IMPLEMENTATION SOURCE OF TRUTH

## Product definition

MAR is a **local agentic development runtime/harness for Web Chat cognition**.

MAR has **NO internal MAR brain** in the current architecture.

Canonical responsibility chain:

```
Owner
  -> Web Chat / Tech Lead
  -> CADS engineering method
  -> MAR execution runtime
  -> local repository / tools / verification / integration
```

### Web Chat / Tech Lead owns

- product/architecture reasoning;
- intent clarification;
- trade-offs/design choices;
- CADS application;
- task decomposition;
- verification/risk policy;
- review and owner-facing explanation.

### CADS owns

- engineering method;
- acceptance semantics;
- Product Accepted / Release Qualified / Runtime Activated definitions;
- design/UX gates;
- risk/escalation standards;
- requirements for what must be proven.

### MAR owns

- durable task/session state;
- project-scoped context mechanics;
- workspace execution;
- sandbox/authority enforcement;
- deterministic repository operations;
- context/result compaction;
- verification-plan execution;
- evidence capture;
- candidate/integration identity;
- crash/restart recovery;
- runtime/source identity.

## Core principle

```
CADS decides what must be proven.
Web Chat reasons about what should be done.
MAR makes that reasoning act on the repository safely and efficiently.
```

MAR must not become a second development methodology.

## Stable architecture goal

```
WEB CHAT
   |
   | high-level reasoning / decisions
   v
MAR SESSION BOUNDARY
   |
   +-- DecisionProjection / delta context
   +-- Project Intelligence
   +-- deterministic compound operations
   +-- workspace / sandbox / authority
   +-- verification executor / evidence
   +-- integration / recovery
   |
   v
LOCAL REPOSITORY
```

## Stable pre-release priorities

### P0-A — Compact outer Web-brain relay — `SLICE_A_IMPLEMENTED`

Source review after the initial VNext plan confirmed that MAR already has the bounded context mechanisms this item was trying to create:

- Project Brain V1 already performs deterministic focused repository retrieval;
- DecisionProjection already rebuilds bounded current task/candidate/checkpoint/evidence context from durable truth instead of replaying an append-only transcript.

Therefore MAR does **not** add a second Task Capsule subsystem.

Slice A instead removes avoidable duplication at the outer MCP/Web boundary. `brain_turn` now supports an additive `response_mode=structured` mode. The authoritative `available + WebTurn` payload, including the exact TurnRequest, remains unchanged in `StructuredContent`; only the compatibility `TextContent` copy is reduced to a bounded identity receipt. Omitting `response_mode` keeps the previous `compat` behavior exactly for cached/older clients.

A large-fixture regression requires structured mode to preserve semantic StructuredContent equality while reducing the serialized application-level CallToolResult to at most 60% of compatibility mode. No authority, task identity, DecisionProjection, Project Brain, verification, integration or recovery semantics change.

### P0-B — Delta/event-oriented brain context

Reduce repeated status/inspect payloads. Expose material changes since the previous meaningful sequence/checkpoint and coalesce heartbeat noise.

Normal Web reasoning should resume only for:
- DECISION_REQUIRED
- AUTHORITY_REQUIRED
- RECOVERABLE_FAILURE_NEEDS_REASONING
- CANDIDATE_READY
- TERMINAL

### P0-C — Compound deterministic operations

Use telemetry to identify recurring primitive sequences and promote only high-value ones. Candidate targets:
- inspect_symbol_context;
- apply_format_validate;
- collect_failure_context;
- summarize_candidate_diff.

No compound operation may hide authority changes or verification evidence.

## P1 — Incremental Project Intelligence

Project Intelligence is a **derived, rebuildable index**, not a second source of truth.

V1 uses:
- Git revision/path identity;
- exact search;
- package/import graph;
- symbols/references from native tooling where available;
- test mapping;
- recent changed scope.

Invalidation is revision/file-hash based and incremental.

### Affected analysis

Affected analysis belongs to **Project Intelligence**, not verifier policy.

It may return:
- direct changed packages;
- reverse dependents;
- test dependencies;
- confidence/uncertainty;
- fallback-required reasons.

CADS + Tech Lead decide how impact data becomes a Verification Plan.

## Verification boundary

Long-term shape:

```
Verification Plan
  -> MAR validates authority/path/tool constraints
  -> MAR executes commands against exact candidate
  -> MAR records concrete command/evidence identity
  -> MAR reports PASS / FAIL / UNVERIFIED observations
```

Language-specific presets may remain, but verifier core must not accumulate development-policy logic for every language.

## Context discipline

1. send summaries + references, not full historical outputs;
2. retrieve source just-in-time;
3. do not resend unchanged known information;
4. keep raw data local and expandable by reference;
5. prefer exact/symbol/graph retrieval before semantic retrieval;
6. deduplicate context before Web delivery.

## KPI

Primary product KPI:

> **verified useful change per Web reasoning round-trip**

Supporting metrics:

```
web_roundtrips_per_task
brain_turns_per_task
primitive_tool_calls_per_task
compound_tool_calls_per_task
tool_result_bytes_to_web
estimated_tool_result_tokens_to_web
raw_context_bytes
compacted_context_bytes
context_reduction_ratio
project_query_latency_ms
project_index_cache_hit_rate
time_to_first_relevant_context_ms
time_to_first_failed_test_ms
time_to_repair_feedback_ms
time_to_candidate_ms
time_to_verified_candidate_ms
verification_wall_ms
integration_wall_ms
final_outcome
```

A change that reduces calls but lowers correctness is not an optimization.

## Stable exit targets

Before this optimization line is Stable:

1. no regression in authority, stale-write fencing, candidate identity, evidence integrity, safe integration or runtime activation truth;
2. median Web boundary calls for representative bounded code tasks is materially below baseline;
3. Task Capsule/delta payload is smaller than repeated raw status/inspect/tool output on the same workload;
4. no normal Owner flow requires run_epoch/attempt/raw-evidence vocabulary;
5. project/context retrieval remains rebuildable from repository/runtime truth;
6. a representative Web-driven code task reaches verified candidate without manual prompt forwarding.

## Slice plan

### Slice A — implemented

`SLICE_A_IMPLEMENTED`

Implement the **modern structured `brain_turn` response mode** on top of the existing DecisionProjection/Project Brain architecture.

Acceptance achieved by design/regression:
- default `compat` remains backward compatible and preserves the old dual full TextContent + StructuredContent response;
- opt-in `structured` preserves the exact authoritative StructuredContent payload required for Web cognition;
- structured mode replaces only the duplicated full TextContent copy with a bounded identity receipt;
- a large-fixture regression enforces semantic payload equality and a serialized application-result size of no more than 60% of compat;
- no new database authority, internal model, Project Brain, task authority, verification or integration policy is introduced.

This slice addresses the proven outer-relay amplification from R-020 without reopening the already-bounded inner DecisionProjection.

### Slice B

Add delta/event sequence semantics using the same durable task state.

### Slice C

Mine repeated tool traces and add the first 1–3 compound deterministic operations.

### Slice D

Project Intelligence V1 and affected-impact capability.

## Deferred after Stable

- Tree-sitter generic incremental parsing;
- semantic embeddings/vector DB;
- learned retrieval/test selection;
- advanced resource-aware scheduling;
- local-model routing;
- multi-agent execution;
- remote cache.

These remain research, not Stable blockers.

## Decision on abandoned Speed V2 candidate

The previous affected-planner implementation is not integrated. Its useful algorithms remain research input for Project Intelligence, but verification policy will not be embedded into verifier core under that design.

## Non-goals

- no MAR-internal reasoning model;
- no rewrite of earned safety/recovery core;
- no vector DB in Slice A;
- no new verification policy;
- no broad UI redesign;
- no remote deployment change.
