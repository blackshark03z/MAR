# R-014 — MCP 2026 transport evolution and MAR implications

**Status:** `RESEARCH_ONLY`  
**Date:** 2026-09-12  
**Verdict:** `ADAPTATION_OPPORTUNITY_WITH_BENCHMARK_GATE`

## Research question

Should MAR change its remote transport model now that MCP `2026-07-28` has a stateless core and a Tasks extension for long-running work, and newer Go SDK work is concentrating on transport teardown, resource bounds and session-leak hardening?

This document records research only. It does not authorize an MCP SDK upgrade, transport migration, public-tool change, or production-code modification.

## Current MAR position

MAR currently pins:

- `github.com/modelcontextprotocol/go-sdk v1.7.0`;
- MCP Link / remote bridge using stateful Streamable HTTP;
- OpenAI Secure Tunnel local MCP handler using `Stateless: true`;
- durable MAR application tasks exposed through `submit`, `task`, `control`, `brain_turn`, and `brain_respond` rather than by keeping one remote request open for the lifetime of a coding task.

This means MAR already separates durable application state from most transport lifetime, but it still has two different transport semantics in active code paths.

## Relevant MCP `2026-07-28` changes

The current MCP specification removes protocol-level handshake/session state from the modern protocol. Each request carries protocol/client metadata, and application state is expected to be represented by explicit handles rather than hidden transport session state.

For long-running work, MCP now places durable asynchronous execution in the `io.modelcontextprotocol/tasks` extension. A server may return a task handle from `tools/call`; clients later use `tasks/get`, `tasks/update`, and `tasks/cancel` instead of requiring the original HTTP connection to remain alive.

The Go SDK documentation also states that response-stream resumability from the old protocol is not the recovery mechanism for `2026-07-28`: if an in-flight response stream breaks, the client re-issues the request. Therefore idempotency and explicit durable identity at the application layer become more important, not less.

## Current MAR primitives already aligned with that direction

MAR already has useful primitives for this model:

- durable task IDs independent of transport session IDs;
- idempotent task submission through an idempotency key;
- durable Web brain turns bound to task + attempt + run epoch;
- idempotent replay of an identical `brain_respond` response;
- conflict rejection for a different second brain response;
- daemon/worker execution that continues outside the original `submit` request;
- read-only task/result/inspect surfaces that can reconstruct progress after reconnect.

Therefore a future adoption of the MCP Tasks extension should be evaluated first as a protocol adapter over MAR's existing durable task state, not as a reason to replace MAR's internal task model.

## Important current distinction: stateful link vs stateless tunnel

MAR currently provides a useful natural A/B research boundary:

1. **MCP Link / remote bridge** — stateful Streamable HTTP, with a 30-minute idle session timeout.
2. **OpenAI Secure Tunnel MCP handler** — `Stateless: true` and capable of negotiating the modern `2026-07-28` protocol.

With Go SDK v1.7.0, this distinction also changes protocol-era behavior: modern `2026-07-28` Streamable HTTP is accepted only in stateless mode. A stateful handler negotiates/falls back to the legacy handshake/session era instead. Therefore an A/B comparison must record the actual negotiated protocol version; otherwise session mode and protocol revision become confounded variables.

No transport should be promoted merely because stateless is newer. The decision must be based on measured disconnect recovery, latency, context/tool-call behavior, compatibility and operational complexity.

## Go SDK evolution signal

MAR's pinned v1.7.0 is the first stable Go SDK release supporting MCP `2026-07-28`.

As of 2026-09-12, upstream still identifies v1.7.0 as the current stable Go SDK release for the `2026-07-28` protocol. No later stable release can currently be treated as an automatic transport/lifecycle fix. Upstream also still has open issues around CommandTransport subprocess death during discovery/fallback and `Ping` behavior that can poison/reconnect modern sessions. Therefore MAR research must benchmark its own child/runtime supervision and reconnect semantics instead of assuming a dependency upgrade already resolves them.

Upstream v1.8 prerelease work is concentrated on exactly the operational classes MAR is researching:

- session/resource leak hardening;
- teardown/deadlock/hang fixes;
- bounded input/event buffering;
- protocol-version negotiation recovery;
- faster non-blocking cancellation behavior;
- explicit supported-protocol-version control.

This is evidence that transport robustness remains a moving target upstream. It is not evidence that MAR should immediately update to a prerelease SDK.

## Benchmark questions before any migration or SDK upgrade

### B1 — Disconnect after durable commit but before client ACK

For `submit` and `brain_respond`:

1. deliver request;
2. allow MAR to durably commit the operation;
3. sever the HTTP connection before the client observes the response;
4. reconnect and retry using the same durable/idempotent identity;
5. verify exactly one semantic effect exists.

Required evidence:

- no duplicate task;
- no duplicate Web response;
- no lost task state;
- no stale authority;
- retry result resolves to the already-committed operation.

### B2 — Disconnect before durable commit

Sever the request before MAR commits the effect. Retry must either create one operation or return a clear non-ambiguous failure; it must never create an unknown duplicate effect.

### B3 — Stateful versus stateless reconnect

Run identical bounded sequences through both existing transport paths and measure:

- initialization/discovery overhead;
- calls required after reconnect;
- reconnect wall time;
- error rate;
- transport bytes;
- session-related failure count;
- client compatibility;
- whether task execution itself is interrupted.

### B4 — Long work without long connection

Start a durable coding task, disconnect the remote client while worker execution is active, reconnect later and recover state/result without transcript replay or worker replacement caused solely by the remote transport loss.

### B5 — SDK-version comparison

Only after the preceding benchmark exists, compare pinned v1.7.0 with a later stable Go SDK release. Do not compare against a prerelease as the production recommendation baseline.

Measure:

- teardown latency;
- reconnect success;
- memory/process stability over repeated connect/disconnect cycles;
- request/error behavior on protocol-version negotiation;
- regression in existing legacy-client compatibility.

## MCP Tasks extension research hypothesis

Potential future shape:

```text
remote tools/call
    -> MAR durable task handle
    -> client disconnects freely
    -> MAR execution continues
    -> tasks/get or existing task read maps to the same durable truth
```

The desired property is not "MCP Tasks everywhere". The desired property is:

> remote request lifetime is disposable; MAR application execution identity is durable and explicit.

If MAR's existing `submit` + `task` semantics already satisfy this with lower compatibility risk, the Tasks extension may provide only interoperability value rather than a core architecture change.

## Risks of premature migration

- breaking clients still negotiating legacy stateful MCP;
- adding a second task abstraction rather than adapting the existing one;
- falsely assuming request cancellation should cancel durable MAR execution;
- confusing transport request cancellation with Owner task cancellation;
- adopting prerelease SDK hardening without stable regression evidence;
- losing currently useful compatibility aliases or Web brain behavior.

## Important cancellation distinction

`PropagateRequestCancellation=true` in the HTTP transport must not be interpreted as "client disconnect cancels MAR task".

For durable application operations, request-context cancellation should stop only work that is safe to abandon because the response can no longer be delivered. A durable task that has already been admitted/committed must follow MAR's own authority and control semantics.

This distinction must be explicitly tested before any broader adoption of stateless transport behavior.

## Phase-0 decision

Do **not** change transport or dependency versions yet.

Next evidence sequence:

1. implement research-only remote HTTP fault injection outside production code;
2. test ambiguous ACK windows for `submit` and `brain_respond`;
3. A/B existing stateful MCP Link against existing stateless Secure Tunnel handler;
4. establish repeated reconnect/leak/resource measurements;
5. only then decide whether a future MAR release should standardize on modern stateless MCP, expose the Tasks extension, update the SDK, or keep the current application-level task protocol.

## External references

- MCP 2026-07-28 specification release: https://blog.modelcontextprotocol.io/posts/2026-07-28/
- MCP Tasks extension: https://tasks.extensions.modelcontextprotocol.io/specification/draft/tasks
- Official Go SDK protocol documentation: https://github.com/modelcontextprotocol/go-sdk/blob/main/docs/protocol.md
- Official Go SDK repository: https://github.com/modelcontextprotocol/go-sdk
