# R-001 — Transport-Independent Long-Running Execution

**Status:** `RESEARCH`  
**Priority candidate:** `P0`  
**Implementation:** prohibited during this research item unless separately approved after evidence review  
**Baseline:** MAR v1.1.0

## Problem statement

Long Web/Chat tool loops can involve many sequential remote calls over minutes or longer. In systems such as ChatCode, a long chain increases exposure to browser disconnects, client/tool timeouts, gateway/tunnel resets, network interruption, large responses, context growth, and uncertain retry semantics.

MAR should be studied for a stronger property than simply keeping a connection alive longer:

> **Long work must not require a long-lived remote connection.**

The candidate architectural invariant to test is:

> **Transport lifetime is disposable; durable execution lifetime is independent.**

This is a research hypothesis, not yet a V1.2 requirement.

## Why this matters

A normal long path may span:

`Chat/Web client -> MCP/HTTP -> tunnel -> MAR -> external cognition -> worker -> verification -> integration`

If progress depends on every layer remaining continuously connected, reliability decreases with task duration. Long request lifetimes also increase the chance that context, response payloads, browser memory, and retry ambiguity grow together.

MAR v1.1 already contains relevant primitives — durable tasks/jobs, SQLite authority, run epochs, fencing, checkpoints/evidence, stale-turn rejection, bounded Web cognition, process ownership, and idempotent operation concepts. Research should determine whether these are sufficient to make client/transport loss a recoverable presentation/control-plane event rather than an execution failure.

## Questions to answer

### Q1 — Current dependency on connection lifetime

For each major operation class, determine whether loss of the initiating Web/MCP connection:

- cancels the underlying operation;
- leaves it safely running;
- leaves the outcome durable but difficult to rediscover;
- creates an ambiguous retry window;
- can duplicate mutation/work;
- can strand a pending external-cognition turn.

### Q2 — Long request versus detached job

Determine which operations currently keep a remote request open while substantial work occurs and which return a durable identifier quickly. Compare failure exposure, latency, context volume, and observability.

Candidate direction to evaluate:

`submit intent -> durable task/job handle -> short status/event/steer interactions`

rather than:

`submit intent -> hold one HTTP/MCP call open until long work completes`.

### Q3 — External cognition disconnect

When MAR is waiting for ChatGPT/Claude reasoning and that Web/client session disappears, determine whether MAR can durably preserve:

- task identity;
- attempt/run epoch;
- exact pending brain turn;
- bounded Decision Projection/context identity;
- already-completed worker/evidence state;
- the one valid next action.

The desired research property is that reconnect resumes/reissues the same bounded reasoning need without replaying the complete historical conversation or launching duplicate work.

### Q4 — Retry ambiguity and idempotency

Study the case:

1. client submits an operation;
2. MAR receives it and may begin work;
3. connection dies before the client receives acknowledgement;
4. client retries.

The benchmark must detect duplicate tasks, duplicate mutations, duplicate external effects, incorrect epoch replacement, or loss of the original result. Existing intent/operation identity mechanisms should be evaluated before proposing another deduplication subsystem.

### Q5 — Context and memory effect

Measure whether detached durable execution reduces:

- number of Web tool calls;
- tool transcript volume returned to the model;
- repeated repository/runtime context;
- browser renderer memory;
- model tokens;
- reconnect payload size;
- need to replay old tool results.

A reliability design that increases context amplification is not automatically an improvement.

### Q6 — Observability and steering

Detached work must not become a black box. Determine the minimum bounded state needed for a Tech Lead/Owner to understand and steer work without micromanaging individual commands.

Candidate information surface:

- current durable phase/state;
- useful progress since last observation;
- current blocker or pending external cognition;
- material decision requiring input;
- current candidate/evidence identity;
- next safe action;
- bounded recent events rather than the entire transcript.

## Failure-injection benchmark candidate

Run a representative task long enough to exercise multiple execution phases. Intentionally inject failures at controlled points, for example:

- close or reload the Web client;
- interrupt the remote MCP/tunnel path;
- restart the tunnel/bridge;
- temporarily disconnect the network;
- reconnect from a new Web session;
- repeat a previously uncertain submit/request;
- interrupt while MAR is waiting for external cognition;
- interrupt while a worker is executing locally.

Exact timing is secondary; the important requirement is deterministic evidence about what state existed before and after each fault.

### Candidate acceptance observations

A successful design would aim for:

- zero duplicate authoritative mutations;
- zero lost durable task state;
- zero incorrect completion claims;
- zero stale worker mutation authority;
- no unnecessary full-conversation replay;
- preservation of candidate/evidence identity;
- safe rediscovery of the current task after reconnect;
- bounded recovery time and bounded owner intervention;
- explicit `WAITING_EXTERNAL_COGNITION`-equivalent behavior when remote reasoning is genuinely required rather than false failure;
- correct cancellation/steering semantics after reconnect.

These are research targets and must be refined against actual V1.1 behavior before becoming release acceptance criteria.

## Measurements

For each injected fault record:

- operation/task/run identity;
- phase at disconnect;
- transport involved;
- whether the remote call was acknowledged;
- whether local execution continued;
- time to detect disconnect;
- time to recover/reconnect;
- number of retries;
- duplicate work/mutation count;
- context bytes/tokens before and after reconnect;
- browser/host memory where observable;
- owner actions required;
- final task/result/evidence identity;
- correctness of final state.

## Possible solution families to compare

Do not assume one implementation. Research at least these families:

1. **Long-lived request with stronger keepalive/timeouts.** Simple, but may only move timeout boundaries and preserve tight coupling.
2. **Durable async task/job handle.** Remote calls become short; client polls or subscribes to bounded events/status.
3. **Event cursor / resumable stream.** Durable job plus bounded incremental observation after reconnect.
4. **Hybrid.** Short synchronous calls for small work, automatically detached durable jobs beyond a threshold or when the underlying operation is inherently long-running.

Compare complexity, compatibility with current ChatGPT/Claude/MCP behavior, context pressure, latency, security/authority semantics, and migration risk.

## Risks of over-engineering

- adding a second orchestration authority beside the existing MAR task/job model;
- creating another event database when existing durable state is sufficient;
- polling so aggressively that status traffic becomes a new bottleneck;
- obscuring material decisions from the Tech Lead/Owner;
- treating eventual consistency as strong synchronous truth;
- weakening fencing/cancellation semantics for convenience;
- building around temporary client limitations that newer models/protocols may remove.

Prefer extending/reusing existing durable primitives if evidence later supports implementation.

## Relationship to other MAR research

This topic directly intersects:

- connection/transport reliability;
- context efficiency and Chrome/browser pressure;
- long-running cognition;
- runtime recovery;
- observability;
- self-hosting quality;
- stronger future model generations capable of longer autonomous work.

The likely long-term value is therefore broader than fixing timeout symptoms: if validated, transport-independent execution can reduce the coupling between increasingly long AI work and increasingly fragile interactive Web sessions.

## Research exit conditions

This item is ready for a V1.2 design decision only after:

1. current V1.1 behavior is mapped for the relevant disconnect cases;
2. at least one controlled failure-injection benchmark is executed;
3. observed failures are separated into client/platform, transport, MAR, worker, and external-cognition causes;
4. baseline metrics are recorded;
5. the smallest solution family that materially improves the measured failures is identified;
6. trade-offs and regressions are explicit;
7. acceptance can be stated independently of the proposed implementation.

Until then, keep this item `RESEARCH` and do not modify production code for it.
