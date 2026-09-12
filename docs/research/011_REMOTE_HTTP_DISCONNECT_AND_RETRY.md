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
- the normal MCP Link remote bridge for `chatgpt-web` / `claude-web` currently uses the handler's stateful default;
- the OpenAI Secure Tunnel manager already creates the same remote handler with `Stateless: true`;
- stateful MCP session timeout: 30 minutes;
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

A second, more direct ambiguous-ACK case was then observed while attempting a structured test task. The caller received an HTTP `502 Upstream or external service error`, so from the chat side admission looked unsuccessful. A later durable Job query showed that the backend had in fact created `job-18d48837e4b6fc88-e6` for the request and advanced it through its own lifecycle. The job ultimately failed for an unrelated runner-classification reason, but the important transport fact is independent of that failure: **caller-visible 502 did not prove the operation had not been admitted**.

This is not a MAR defect. It demonstrates two concrete classes MAR should handle better than an ordinary synchronous tool loop:

1. long-lived child lifetime coupled to a tool/lease lifetime;
2. ambiguous acknowledgement where the caller sees failure after the backend has already materialized durable work.

MAR's durable submit -> daemon/worker architecture is conceptually better because long work is detached from the caller after durable admission. R-011 exists to prove that this advantage survives the real remote HTTP/tunnel failure windows rather than relying on architecture reasoning alone.

MAR's service-level submit evidence is already strong: identical idempotency-key + identical Goal Contract returns the original task; the same key with a different Goal Contract is rejected; and a concurrency test with 12 simultaneous duplicate submits produces one durable task with exactly one creator.

### Source-level remote HTTP ambiguous-ACK fault injection — PASS

A research-only Go overlay test was executed against the accepted production source without modifying MAR files. The harness used the real `NewRemoteHTTPHandler`, real `TaskService`, a temporary SQLite store, and a response writer that deliberately discarded the first `submit` acknowledgement **after** the backend had durably committed the task. A new MCP client/session then retried the exact same submit idempotency key and Goal Contract.

Both existing transport semantics passed:

```text
stateful:
  first submit durable commit = YES
  first caller usable ACK     = NO (injected loss)
  reconnect/new session       = YES
  retry created               = false
  retry task_id               = original task_id
  submit calls                = 2
  verdict                     = PASS

stateless:
  first submit durable commit = YES
  first caller usable ACK     = NO (injected loss)
  reconnect/new session       = YES
  retry created               = false
  retry task_id               = original task_id
  submit calls                = 2
  verdict                     = PASS
```

The test itself completed in ~0.11 s once compiled; the enclosing research job took ~122 s because the source test was cold-compiled under a heavily loaded host. The wall-clock job duration is therefore **not** a transport latency measurement.

This closes benchmark matrix case **S2 — after submit commit / before HTTP acknowledgement** for both stateful and stateless source paths.

### Source-level `brain_respond` ambiguous-ACK fault injection — PASS

A second research-only overlay harness created a real temporary MAR task, advanced it into an active attempt, created a durable pending WebTurn, then used the real remote HTTP handler for `brain_respond`. The response writer discarded the first acknowledgement only after `TaskService.RespondWebTurn` committed the response and resumed the task from `INPUT_REQUIRED` to `RUNNING`. A fresh MCP client/session retried the exact same turn/response.

Both transport semantics passed:

```text
stateful:
  response durable commit      = YES
  task resumed RUNNING         = YES before caller retry
  first caller usable ACK      = NO (injected loss)
  reconnect/new session        = YES
  retry created                = false
  retry turn_id                = original turn_id
  respond calls                = 2
  verdict                      = PASS

stateless:
  response durable commit      = YES
  task resumed RUNNING         = YES before caller retry
  first caller usable ACK      = NO (injected loss)
  reconnect/new session        = YES
  retry created                = false
  retry turn_id                = original turn_id
  respond calls                = 2
  verdict                      = PASS
```

The test body completed in ~0.12 s and the warm enclosing research job in ~3.3 s. This closes **B2 — after brain-response commit / before acknowledgement** for both stateful and stateless source paths.

### Deterministic pre-commit cancellation barrier — PASS

A third source-level overlay harness placed an explicit barrier in the HTTP backend **immediately before** calling the real durable `TaskService` operation. The client started the tool call, the server backend confirmed it had entered the barrier, then the client request context was cancelled. With `PropagateRequestCancellation: true`, the backend observed `ctx.Done()` and returned without invoking the durable operation. A fresh MCP session then retried normally.

All four combinations passed:

```text
submit / stateful:
  backend observed cancellation = YES
  hidden durable submit          = NO
  reconnect retry created task   = YES
  verdict                        = PASS

submit / stateless:
  backend observed cancellation = YES
  hidden durable submit          = NO
  reconnect retry created task   = YES
  verdict                        = PASS

brain_respond / stateful:
  backend observed cancellation = YES
  task remained INPUT_REQUIRED   = YES
  pending turn remained current  = YES
  reconnect retry created reply  = YES
  task resumed RUNNING           = YES
  verdict                        = PASS

brain_respond / stateless:
  backend observed cancellation = YES
  task remained INPUT_REQUIRED   = YES
  pending turn remained current  = YES
  reconnect retry created reply  = YES
  task resumed RUNNING           = YES
  verdict                        = PASS
```

The two test bodies completed in ~0.20 s total once compiled; the enclosing warm research job took ~2.4 s.

This is strong evidence for S1/B1 at a deterministic **pre-TaskService/pre-transaction boundary**. It does **not** claim deterministic fault injection in the middle of SQLite commit itself. Transactional atomicity and idempotency still protect that narrower micro-window, but a dedicated storage-layer crash/cancel test would be required to label an exact mid-transaction barrier as independently proven.

Stateful session-expiry recovery is now executable evidence as well. A research-only transport harness uses the same MAR `NewServer` + durable `TaskService` while shortening only the SDK session timeout to 1 second. After the original stateful session expires, its next call is rejected; a newly connected client retries the same idempotency key and receives the original task with `created=false`. The transport semantics sentinel now passes 9/9 cases, including this session-expiry path.

Real Quick Tunnel process replacement is now executable evidence for durable pending cognition. A research-only handler created a real pending WebTurn, exposed it through cloudflared #1, killed that cloudflared process completely, verified the durable task remained `INPUT_REQUIRED` with the same unanswered turn, then started cloudflared #2 and reconnected through its new public hostname. `brain_respond` on the replacement route accepted the original turn and resumed the task to `RUNNING` without recreating task/turn identity.

The remaining transport uncertainty is now narrow: actual OS worker-process continuity while the external route disappears/reappears, plus optional exact mid-transaction crash/cancel micro-windows. The real-worker acceptance path cannot be rerun validly on the current boot because `sandbox-host-check` is not prepared; this gap is therefore `BLOCKED_BY_SANDBOX`, not failed. Existing source semantics show the Web-wait worker polls durable turn state independently of the remote route, but that is not promoted to OS-process proof. Transport/durable-state continuity itself is proven across session expiry, ACK loss, and real Quick Tunnel process replacement.

## Current transport telemetry gap

Current remote telemetry is useful for live UI status but is not sufficient for fault attribution.

`RemoteHTTPEvent` currently records request arrival time, HTTP method, JSON-RPC method, session ID, Host and Origin. The remote bridge then keeps request count, last-seen time, session cardinality and a few initialization/tool-list flags in memory. The Secure Tunnel observer similarly marks last activity/success when an MCP event is observed.

It does **not** durably record, per request:

- negotiated MCP protocol version;
- HTTP/JSON-RPC final outcome;
- response status/error class;
- request/response byte counts;
- handler duration;
- client cancellation/disconnect timestamp;
- retry correlation/operation identity;
- tunnel/proxy hop responsible for a 502/504/reset.

The in-memory connector telemetry is also lost across MAR process restart. Therefore existing Owner telemetry cannot reconstruct a historical disconnect timeline or prove where a failed acknowledgement originated.

This is a measurement gap, not automatic authorization to add production instrumentation. The first fault-injection harness should capture these fields externally where possible. Only fields that cannot be measured reliably outside MAR should become candidates for future internal instrumentation.

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

MAR already has both transport semantics in production source: MCP Link is stateful while the OpenAI Secure Tunnel local MCP handler is stateless, and a unit test proves the stateless handler advertises MCP `2026-07-28`. This creates a natural A/B research boundary without first changing transport implementation.

Do **not** assume stateless is automatically better. It is promising because durable MAR task/turn handles already carry application state, but transport choice can affect client compatibility, telemetry/session counts, authentication assumptions, cancellation behavior and reconnect UX. The experiment should compare existing paths before proposing convergence on one mode.

Promotion requires measured improvement on the benchmark matrix, not protocol novelty.

## Phase-0 verdict

`REMOTE_HTTP_PRECOMMIT_CANCEL_AND_POSTCOMMIT_ACK_LOSS_RECOVERY_PROVEN_AT_SERVICE_BOUNDARIES; TUNNEL_AND_SESSION_EVIDENCE_INCOMPLETE`

Accepted-source fault injection now proves both sides of the normal TaskService boundary for stateful and stateless HTTP paths: cancellation before durable service invocation propagates without hidden state change, while acknowledgement loss after durable commit recovers idempotently for both submit and brain response. The remaining evidence gap is no longer basic HTTP idempotency; it is session expiry, long worker continuity, real tunnel/process loss/replacement, and optional deeper storage-layer crash points inside transactions.

No transport implementation change is authorized by this document.
