# MAR External Cognition Contract

**Status:** CANONICAL — ARCHITECTURE SOURCE OF TRUTH
**Date:** 2026-09-19
**Parent:** `docs/architecture/MAR_ARCHITECTURE_CONSTITUTION.md`

## 1. Purpose

This document defines the provider/model-neutral boundary between MAR and external reasoning environments.

It replaces **Web Brain** as the long-term architecture term.

**Web Brain / GPT Web Brain** remains valid only when describing a specific current/historical adapter or implementation path.

## 2. Definition

External Cognition Runtime is any replaceable reasoning environment that:

1. receives a bounded, current MAR projection;
2. understands the offered capabilities;
3. makes a reasoning decision;
4. returns bounded action/tool intent or a terminal reasoning result.

Examples include Web chats, API-backed model sessions and provider-managed agent sessions.

External cognition is non-authoritative.

It does not own:

- task lifecycle;
- Goal/acceptance authority;
- attempt/run epoch;
- workspace authority;
- process lifecycle;
- verification truth;
- integration truth;
- recovery truth;
- durable project coordination.

## 3. Required MAR-side properties

Every actionable cognition request must be bound to current durable facts sufficient to reject stale or mismatched responses.

At minimum, current implementation semantics must preserve the equivalent of:

- request/turn identity;
- task identity;
- current attempt/run epoch when mutation-capable execution is involved;
- current workspace/candidate/revision identity where relevant;
- bounded Goal/acceptance/authority facts;
- unresolved effect/safety facts;
- current verification/integration status;
- references to selected immutable evidence.

A stale cognition response is historical evidence only.

## 4. Capability profile

Provider/vendor identity must not leak into MAR kernel decisions.

A connector may expose a profile conceptually equivalent to:

```text
ConnectorCapabilityProfile

identity:
  connector_id
  display_name

transport:
  transport_mode
  auth_mode

cognition:
  interactive_turns
  persistent_session_continuation
  parallel_tool_calls
  programmatic_tool_calls
  tool_discovery
  streaming_events
  async_background_reasoning

observability:
  usage_metadata
  provider_event_ids
  health_semantics
```

The exact schema may evolve. Kernel behavior should depend on capabilities, not provider names.

## 5. Provider/session state

Provider-native state may include:

- conversation/session ID;
- previous-response handle;
- WebSocket continuation;
- prompt/cache handle;
- provider compaction;
- native memory;
- provider task/run ID.

This state is an optimization/cache.

It must never become MAR durable task authority.

If provider state disappears, MAR must be able to build a fresh bounded cognition request from durable facts and immutable evidence references.

## 6. Context discipline

Default outward context is **minimum sufficient context**.

Prefer:

- compact current state;
- material deltas;
- identifiers/hashes;
- focused source snippets;
- explicit evidence references;
- expand-on-demand handles.

Avoid:

- full transcript replay;
- repeated unchanged snapshots;
- duplicated tool results;
- repeated repository scans when a safe derived result is still fresh;
- provider-specific hidden memory as a correctness prerequisite.

## 7. Delta/event wake-up semantics

External cognition should be invoked because a material reasoning boundary was reached.

Preferred event classes:

- `DECISION_REQUIRED`;
- `AUTHORITY_REQUIRED`;
- `RECOVERABLE_FAILURE_NEEDS_REASONING`;
- `CANDIDATE_READY`;
- `TERMINAL`.

Heartbeat, polling and other operational noise should be coalesced unless it materially changes a decision.

The durable task state remains the source of truth; event/delta projections are derived.

## 8. Bounded Action Batch

External cognition may request multiple deterministic actions in one reasoning turn when no new evidence is required between them.

A batch must be:

- bounded;
- ordered;
- authority-checked;
- current-attempt/current-epoch bound where relevant;
- observable;
- stoppable on declared failure conditions;
- receipt-producing.

Example:

```text
search symbol
read definition
read callers
read related test
---------------- observation barrier
reason about patch
apply bounded edits
format
run focused test
---------------- observation barrier
reason about failure/success
```

MAR should promote recurring primitive sequences into compound deterministic operations only when telemetry shows material benefit.

Do not turn this into a generic workflow language.

## 9. Observation Barrier

An Observation Barrier exists whenever the next action depends on newly observed reality.

Examples:

- source discovery changes patch strategy;
- a focused test fails;
- Git state changes;
- an external side effect has uncertain outcome;
- authority/input is required;
- verification produces a new failure class.

Principle:

> **Execute deterministically until new evidence is required for the next decision.**

## 10. Protocol adapters

MCP is currently a primary MAR control/cognition adapter.

Future protocol features such as asynchronous task handles, event streams, A2A or other agent interoperability protocols may be added as edge adapters when useful.

They must map to existing MAR semantics:

```text
external protocol task/session/agent handle
              |
              v
       MAR durable identity
```

They must not create:

- a second task lifecycle;
- second scheduler;
- second integration authority;
- second durable memory truth.

## 11. Cancellation and disconnect

Remote request cancellation or transport loss is not automatically Owner task cancellation.

Once a durable MAR operation has committed, subsequent execution follows MAR task/control semantics.

Reconnect/retry behavior must preserve idempotent or reconciled outcomes at material effect boundaries.

## 12. Security/authority

External cognition never receives authority merely because it can request a tool/action.

Every mutation-capable action remains subject to:

- Goal/authority boundaries;
- MAR task/attempt/run-epoch validity;
- sandbox/process authority;
- workspace scope;
- Git/integration policy;
- network/secret/deploy policy;
- effect reconciliation where outcome may be ambiguous.

Model obedience is not an enforcement boundary.

## 13. Compatibility objective

MAR must support both ends of the cognition spectrum:

```text
today:
many short reasoning turns

future:
longer provider-managed reasoning episodes,
larger useful action groups,
possibly internal provider subagents
```

MAR should gain speed as reasoning runtimes improve without moving durable project authority out of MAR.
