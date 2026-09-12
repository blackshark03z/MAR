# R-011 — Remote HTTP Disconnect and Retry Semantics

**Status:** `RESEARCH_ONLY`  
**Baseline:** MAR v1.1.0 source  
**Code changes authorized:** none  
**Date:** 2026-09-12

## Research question

Does MAR actually turn remote Chat/MCP disconnects into a transport inconvenience rather than lost/duplicated work across the real Streamable HTTP/tunnel path, especially when a disconnect occurs at an ambiguous commit boundary?

## Current architecture evidence

MAR's public MCP design already avoids the worst long-call pattern.

The six canonical public tools are bounded control/cognition operations. `submit` durably creates a task and returns; the daemon/worker then owns long execution independently. `task` reads durable status/result/inspect. `brain_turn` reads one pending durable Web turn. `brain_respond` commits one exact bound Web response. Low-level coding commands remain worker-internal rather than being exposed as long synchronous remote calls.

This means normal long coding work does **not** require one HTTP request to remain alive for the task lifetime.

## Current remote transport properties

`internal/mcpedge/http.go` currently constructs Streamable HTTP with:

- `JSONResponse: true`;
- stateful mode by default (`Stateless` is supported but current bridge profiles do not set it);
- MCP session timeout: 30 minutes;
- request body cap: 1 MiB;
- `PropagateRequestCancellation: true`.

The local remote bridge HTTP server configures:

- `ReadHeaderTimeout: 5s`;
- `IdleTimeout: 30s`;
- no explicit Go `WriteTimeout`.

Therefore MAR itself does not impose a short active-response write timeout, but active requests can still be cancelled when the client connection disappears and external tunnel/proxy/client timeouts remain outside MAR's control.

## Durable/idempotent primitives already present

### Submit

`TaskService.Submit` requires an idempotency key. The durable store owns idempotency/conflict semantics. An ambiguous client-side loss after submission can therefore be reconciled/retried using the same logical key instead of blindly creating a second Goal.

### Web brain response

A Web turn is durably bound to `task_id + attempt_id + run_epoch + request_id` with integrity hashes.

`RespondWebTurn` behavior is already suitable for retry semantics:

- the first exact response is committed transactionally;
- an identical retry returns the existing response and `created=false`;
- a different second response conflicts instead of replacing the accepted response;
- a stale/fenced turn is rejected.

Service tests explicitly prove identical-response retry idempotency and rejection of a different second response.

### Long task lifetime

Once `submit` has committed, worker/daemon lifetime is not owned by the remote HTTP request. Existing T7 proves that a CLI stdio MCP client disconnect does not automatically destroy durable task truth and that worker physical termination is recorded safely.

## Coverage gap

The strongest disconnect acceptance today is T7 through CLI stdio. Remote HTTP tests currently prove:

- public tool surface;
- stateful session observation;
- stateless modern MCP discovery support;
- tokenized health path;
- origin/path guards.

They do **not** yet prove the failure windows that matter most for Web Chat/tunnel use:

1. client disconnect before a `submit` transaction commits;
2. client disconnect after commit but before the response reaches the client;
3. same-idempotency-key retry after ambiguous submit;
4. disconnect during `brain_respond` before commit;
5. disconnect after brain response commit but before acknowledgement;
6. reconnect through a new stateful session and recover current durable task/turn;
7. remote session expiry while a durable task continues or waits for cognition;
8. tunnel loss/replacement while task execution continues;
9. stateless versus stateful transport behavior for these same windows.

Until these are exercised end-to-end, it is not enough to infer Web transport reliability from CLI T7 or unit-level idempotency alone.

## Comparison evidence from ChatCode research tooling

During this research, a contained ChatCode `local_exec` operation launched a long-lived `mar.exe` child. The containing durable job remained `RUNNING` and held the raw-command workspace target, causing a later command to queue as `target_busy` even though the user-level intent was merely to start a background app.

This is not a MAR defect. It demonstrates the exact failure mode MAR should avoid: **a long-lived child/process lifetime becoming coupled to one tool invocation/lease lifetime**.

MAR's durable submit -> daemon/worker architecture is conceptually better because long work is detached from the caller after durable admission. R-011 exists to prove that this advantage survives the real remote HTTP/tunnel failure windows rather than relying on architecture reasoning alone.

## Candidate benchmark matrix

A future research harness should use a disposable fixture project and controlled remote HTTP server, never the Owner's authoritative project.

For each stateful and stateless mode, inject connection loss at deterministic barriers:

```text
S1  before durable submit commit
S2  after submit commit / before HTTP acknowledgement
B1  before durable brain-response commit
B2  after brain-response commit / before acknowledgement
R1  while worker executes independently
R2  while task is INPUT_REQUIRED waiting for Web cognition
E1  after MCP session expiry, before reconnect
T1  temporary tunnel process loss/replacement
```

For ambiguous operations, retry with the exact same semantic identity.

## Acceptance invariants

The test is successful only when all applicable invariants hold:

- no duplicate durable task for one idempotency key;
- no duplicate accepted brain response;
- no silent replacement of a different response;
- no false completion;
- no lost durable task/attempt/turn state;
- stale attempts/turns remain rejected;
- reconnect can discover the current durable state without replaying the whole historical transcript;
- transport/session identity never becomes execution authority;
- worker/task lifetime remains independent from the lost remote connection where policy permits;
- exact recovery outcome and timing are observable.

## Metrics

Collect:

- connection-loss phase;
- request latency until loss;
- durable commit observed yes/no;
- retry count;
- duplicate/conflict count;
- recovery time;
- task state before/after reconnect;
- active attempt/run epoch preservation;
- number of historical bytes replayed after reconnect;
- stateful session ID changes;
- transport/tunnel error class;
- final verification/integration outcome.

## Stateless research hypothesis

MAR already has a stateless handler path and a unit test advertising MCP `2026-07-28`. A future experiment should compare it with the current stateful bridge.

Do **not** assume stateless is automatically better. It is promising because durable MAR task/turn handles already carry application state, but switching transport semantics can affect client compatibility, telemetry/session counts, authentication assumptions, cancellation behavior and reconnect UX.

Promotion requires measured improvement on the benchmark matrix, not protocol novelty.

## Phase-0 verdict

`ARCHITECTURE_PROMISING_EVIDENCE_INCOMPLETE`

The current MAR design already contains the key primitives needed for transport-independent long work, and it is structurally less coupled than a long synchronous tool-call chain. The missing evidence is remote HTTP/tunnel failure injection at ambiguous commit/reconnect boundaries.

No transport implementation change is authorized by this document.
