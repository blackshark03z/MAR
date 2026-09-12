# R-026 — Runtime child supervision and liveness

**Status:** `RESEARCH_ONLY`  
**Baseline:** MAR v1.1.0  
**Code changes authorized:** none  
**Date:** 2026-09-12

## Research question

What happens if the Owner UI parent remains alive but its spawned `mcp-stdio` runtime/daemon child exits unexpectedly?

This is distinct from a remote tunnel disconnect. The Owner UI process owns connection/setup surfaces while the child runtime owns the daemon/scheduler/worker execution loop.

## Current process topology

`runOwnerUI()` starts the runtime using the MCP Go SDK command transport:

```text
Owner UI parent
  -> exec current MAR binary: mcp-stdio ...
  -> MCP client session to child

Owner UI parent also opens:
  -> store.Open(db) for Owner/project/telemetry surfaces
  -> TaskService on that parent handle
  -> remote GPT/Claude bridge handlers using parent TaskService

mcp-stdio child opens separately:
  -> store.Open(db)
  -> Runtime
  -> daemon authority lease
  -> scheduler/resource governor
  -> worker process supervisor
```

The database is shared durable truth but the parent and child are separate process/SQLite-handle roles.

Daemon authority itself is database-scoped rather than permanently bound to this one child. MAR permits multiple `mcp-stdio` processes to point at the same database; only one may hold `<db>.daemon.lock`, while another process can wait and take over after OS handle release on owner death. Therefore the real availability question is **whether any daemon authority is alive/ready for this database**, not merely whether the Owner UI's originally spawned child PID survives.

In the common Owner UI topology there may be no standby daemon process, so child loss can still remove execution capacity. But the benchmark must detect actual daemon availability rather than assume one process topology.

### Live control observation — parent/child topology present

A read-only external process-topology probe on 2026-09-12 observed the live MAR process tree as:

```text
Owner UI parent PID 12060 (`ui`)
  -> child PID 9160 (`mcp-stdio`)
```

The same research snapshot reported both local Owner UI and MCP bridge loopback surfaces reachable. This is useful **control-state evidence** that the normal parent/child topology existed at observation time. It does not prove daemon authority from PID presence alone and is not a substitute for the C1–C4 failure-injection cases.

## Accepted-source isolated liveness benchmark — C1/C2, 2026-09-12

A disposable MAR runtime was built from the current production source tree with no production diff from V1.1 and isolated onto research-only ports/data (`18787/18788`, SQLite backup copy). The binary served the accepted React/Vite Owner Console asset model. No production process, database or route was changed.

### C1 — kill execution child while Owner parent survives

Before killing the `mcp-stdio` child:

```text
Owner root                    200
/api/runtime                  200
/api/tasks                    200
proxied task status           200
remote /health/<capability>   204
```

After killing only the `mcp-stdio` child, both immediately and again after 8 seconds:

```text
Owner parent                  alive
mcp-stdio children            0
Owner root                    200
/api/runtime                  200
/api/tasks                    200
remote /health/<capability>   204
proxied task status           502 (connection closed / EOF)
child respawn                 none observed
```

This is direct executable evidence that parent/route health can remain green while execution capability is absent.

### C2 — remote submit after child death

Using a fresh MCP client against the still-live parent-owned remote bridge after the child was killed, a canonical `submit` was issued with a unique idempotency key into the research DB copy.

Observed:

```text
remote submit                 success
created                       true
task count                    42 -> 43
new task state                SUBMITTED
new task run_epoch            0
state after 8 seconds         SUBMITTED / epoch 0
mcp-stdio children            0
remote health                 204
proxied task status           502
```

The capability token/URL was not persisted in the report.

This proves the liveness mismatch end-to-end: the parent can durably admit new work while no execution engine is available to advance it. The finding does **not** weaken MAR's stale-writer/physical-termination safety model; it is an availability and health-truth gap.

Research classification is now:

`RUNTIME_CHILD_LIVENESS_GAP_CONFIRMED`

A future health contract should distinguish at least public-route reachability, parent durable-service availability, and execution-engine readiness. Automatic respawn is one candidate, not yet a requirement; safe takeover/reconciliation behavior must remain authoritative.

## Source finding — no explicit parent-side child watchdog

After `client.Connect(ctx, CommandTransport, ...)` succeeds, `runOwnerUI()` stores the returned session and later blocks on Owner HTTP server/context lifetime.

No MAR-owned loop in `runOwnerUI()` was found that:

- waits for unexpected `mcp-stdio` child exit;
- marks daemon/runtime unavailable;
- restarts the child;
- closes/rejects remote submit while execution is unavailable.

The session is closed only as part of Owner UI shutdown. Individual proxied `CallTool` operations surface errors when the session/child is unavailable, but that is request-time failure detection rather than runtime supervision.

This is a source finding, not yet a live crash benchmark.

Upstream MCP Go SDK documentation describes `CommandTransport` as starting the supplied `exec.Cmd` subprocess and communicating over stdin/stdout. A 2026 SDK issue discussing a subprocess that exits during connect explicitly notes that retrying on a fresh process would mean the **transport** owns respawn. That upstream signal supports the conservative rule here: do not assume subprocess respawn unless MAR or the SDK version in use explicitly implements and tests it.

External references:

- https://github.com/modelcontextprotocol/go-sdk/blob/main/docs/protocol.md
- https://github.com/modelcontextprotocol/go-sdk/issues/1177
- https://github.com/modelcontextprotocol/go-sdk/issues/1130

As of 2026-09-12, v1.7.0 remains the current stable Go SDK release. The referenced subprocess-discovery issue remains open, so there is no current stable upstream release that can be assumed to provide automatic respawn/supervision for MAR's Owner UI child runtime.

## Source finding — remote bridge bypasses the child MCP session

Both `remoteBridgeManager` and `openAITunnelManager` are constructed with the Owner UI parent's `TaskService` as the remote MCP backend.

Therefore a remote client can reach durable domain operations through the parent even when the child MCP session/daemon is unhealthy.

For example, a remote `submit` can be durably persisted by the parent service without requiring the child daemon to be alive at that instant.

Potential liveness failure:

```text
mcp-stdio child dies
Owner UI parent survives
remote route still reachable
remote submit -> durable SUBMITTED task succeeds
no daemon -> no preflight/scheduling/worker execution
```

The task is durable, but forward progress can stop silently until runtime recovery occurs.

## Owner Console observability gap

`GET /api/runtime` currently reports connection state, sandbox readiness, model/provider configuration, worker limit and Owner UI uptime.

No explicit field was found for:

- child MCP process alive;
- daemon authority lease held;
- daemon heartbeat/tick freshness;
- scheduler/execution engine ready.

The current React `systemHealth(runtime)` implementation confirms the health decision itself does not consult execution-runtime liveness. Its logic is effectively:

```text
runtime unavailable -> Connecting
sandbox not ready -> Action required
primary connection actionable problem -> Degraded
otherwise -> Healthy / "Sandbox and primary routes are stable"
```

There is no child/daemon/execution-ready term in this calculation.

Therefore the **health model is structurally capable of reporting `Hoạt động tốt` while execution runtime is absent**, provided sandbox and connection data remain healthy/stale enough. The remaining live benchmark question is whether killing the child leaves those parent-owned signals in a state that actually triggers this condition and for how long.

## Active worker safety on child crash

Worker process trees are created through the Windows process supervisor with Job Objects using `KILL_ON_JOB_CLOSE`.

If the child runtime process dies abruptly, its OS handles are closed; the containment design is intended to terminate descendant worker process trees rather than leave mutation-capable workers free-running.

However, durable SQLite does not receive an OS termination proof merely because the daemon process disappeared.

On daemon startup, `Daemon.Run()` executes `reconcileUnprovenAttempts()` before normal scheduling. For any nonterminal task whose current attempt is not durably `PHYSICALLY_TERMINATED`, MAR calls `RequirePhysicalRecovery()`:

- active logical authority is fenced;
- task becomes `BLOCKED`;
- attempt intentionally remains not physically terminated;
- replacement admission remains impossible without explicit physical recovery evidence.

This is a strong fail-closed safety property. Runtime child supervision research is therefore primarily about **availability/liveness and truthful health**, not permission to weaken fencing.

## Pending Web brain turn nuance

There is a bounded but confusing crash window.

If the child dies while a task is `INPUT_REQUIRED` waiting for Web cognition:

1. the worker process tree may already be physically gone due to OS containment;
2. durable attempt state can remain `ACTIVE` until daemon restart reconciliation;
3. the Owner UI parent can still serve `brain_turn` / `brain_respond` through its own TaskService;
4. `RespondWebTurn` accepts a response when task state is `INPUT_REQUIRED` and the matching durable attempt is still `ACTIVE`;
5. it transitions the task back to `RUNNING`.

Thus a Web response can be durably accepted after the physical worker has actually disappeared but before durable restart reconciliation has fenced the attempt.

Source inspection confirms the exact gate: `RespondWebTurn` requires the task to be `INPUT_REQUIRED` and the matching durable execution attempt to have `authority_state=ACTIVE`; it then commits the response and atomically transitions the task to `RUNNING`. No physical-process liveness proof is consulted at that boundary. Physical death is reconciled later by daemon recovery/fencing.

This does **not** grant a stale worker new mutation authority—the OS-contained worker is gone, and the next daemon startup fails closed—but it can create a temporary liveness/state mismatch: `RUNNING` with no execution engine/worker.

This failure window is therefore a mandatory P0-0 benchmark, not an optional edge case.

## Reverse failure direction — Owner UI parent disappears first

The lifecycle is intentionally asymmetric.

If the MCP stdio client disappears, `runMCPRuntime()` treats stdio EOF as a client disconnect rather than permission to kill mutation-capable work. It calls `waitForActiveWorkers()` and allows the current daemon/worker execution to reach a bounded terminal point before cancelling the runtime and releasing daemon authority.

This is the same safety principle already proven by T7 for CLI stdio disconnect.

For the Owner UI topology, this creates a potentially useful handoff:

```text
old Owner UI parent dies
  -> old mcp-stdio child sees EOF
  -> active worker is allowed to drain safely
  -> old child continues holding daemon authority while active work exists

new Owner UI starts
  -> new mcp-stdio child can establish MCP session
  -> its daemon goroutine waits for the DB-scoped authority lease
  -> new parent can still access durable parent-side remote/task surfaces
  -> when old child drains and releases authority, waiting child may take over
```

If the old active worker is waiting for a Web brain response, the old worker can remain alive up to its bounded execution/wait budget (normally at most the remaining 30-minute task active-execution budget). A newly started Owner UI/remote bridge can potentially answer the same durable pending WebTurn through the shared database, allowing the old worker to resume and finish before handoff.

Therefore parent crash does **not** automatically imply lost active work. It may instead create a safe draining period.

The availability trade-off is takeover latency:

- without active workers, old child should release daemon authority quickly after EOF;
- with active local work, takeover waits for safe terminal progress;
- with a pending Web turn and no external cognition reconnect, old child can retain daemon authority until the bounded wait expires;
- Secure Tunnel desired-state restart may restore the same connection identity on the new parent, while Quick Tunnel recreation can change public URL and may prevent transparent cognition reconnect.

This reverse direction should be benchmarked rather than "fixed" by killing the old child aggressively, because aggressive termination could weaken the exact safety property T7 protects.

## Existing safety oracle reuse

Do not rebuild daemon-crash safety proof from scratch. V1.1 T9 (`TestAcceptanceT9ActualDaemonCrashReconcilesWithoutFalseCompletion`) already exercises a real daemon crash, proves mutation stops, reopens/reconciles SQLite fail-closed, and rejects fabricated completion. P0-0 should reuse T9 as the stale-writer/false-completion oracle and focus new C1-C4 experiments on the Owner-parent/child topology, execution availability, health truth and takeover latency.

If a P0-0 run violates a T9 safety invariant, one reproduction is immediately material. Otherwise availability/latency conclusions still require repeated comparable samples.

## Benchmark matrix

Use a disposable fixture/runtime. Never kill the Owner's authoritative MAR process merely to collect research evidence.

### Health oracle

For this benchmark, connection reachability and Owner HTTP availability are not sufficient evidence that MAR is operational. Treat the runtime as execution-ready only when all required layers agree:

```text
owner_surface_reachable
+ runtime_child_or_equivalent_daemon_alive
+ daemon authority/execution loop available
+ scheduler able to admit eligible work
+ durable task progress observable
```

A state where GPT/Claude route or Owner UI remains reachable while no daemon can advance an eligible durable task is `EXECUTION_LIVENESS_FAILURE`, not `HEALTHY`. This distinction is required before P0-1 latency measurements are valid, because otherwise a fast/readable parent surface could hide an absent execution engine.

Conversely, child PID loss alone is not automatically a failure when another daemon already owns or safely takes over authority for the same database. The oracle is execution capability plus authority safety, not one fixed process identity.

### C0 — healthy control

Parent + child alive. Submit one tiny task and verify normal state progression.

### C1 — child dies while idle

Kill only the `mcp-stdio` child while Owner UI remains alive.

Measure:

- detection time;
- `/api/runtime` state;
- remote connection state;
- Owner proxied MCP read/action behavior;
- whether parent remains serving HTTP;
- whether child restarts automatically;
- manual recovery required.

### C2 — remote submit after child death

After C1, issue one idempotent submit through the remote/parent MCP surface.

Oracle:

- durable task count exactly one;
- record whether caller receives success/error;
- record task state progression for a bounded window;
- no false `RUNNING/COMPLETE` claim without daemon execution.

### C3 — child dies during active worker

Start an isolated task, kill only the child runtime, and observe:

- worker/process-tree termination;
- durable attempt/task state immediately after crash;
- no continued mutation after containment;
- daemon lock release;
- recovery behavior after a fresh runtime starts;

### C4 — child dies while Web turn is pending, then response arrives

Create one isolated task that reaches a durable pending Web turn. Kill only the runtime child after the turn is durable but before response. While the Owner/remote parent remains alive, submit the exact `brain_respond` once.

Record:

- whether the response is accepted;
- task state immediately after acceptance;
- attempt logical authority state;
- physical worker/process evidence;
- whether any daemon is actually available to consume the response;
- time until restart reconciliation changes/fences the state;
- whether a fresh runtime produces any duplicate worker or stale mutation.

Oracle:

- zero stale mutation and zero duplicate execution authority are mandatory safety invariants;
- `RUNNING` without an execution-capable daemon/worker is an `EXECUTION_LIVENESS_MISMATCH` even when safety remains intact;
- health/UI must not be interpreted as fully operational during that mismatch.
- no blind replacement worker before recovery fencing.

### C4 — child dies during pending Web turn

Create a pending Web brain turn, kill the child, then answer the turn before any new daemon reconciles.

Measure:

- whether brain response is accepted;
- resulting task state;
- actual worker/process state;
- next daemon reconciliation outcome;
- no stale/duplicate side effect.

### C5 — Owner UI parent crash / daemon drain takeover

Kill only the disposable Owner UI parent while one active worker exists.

Observe:

- whether old `mcp-stdio` child remains to drain active work;
- worker mutation/terminal outcome;
- daemon authority lease handoff latency;
- new Owner UI startup/session availability;
- new child waiting/takeover behavior;
- remote connection recovery;
- no second mutation-capable daemon/worker on the same task.

Repeat with the old worker waiting on Web cognition. Measure whether a reconnected new parent can answer the durable pending turn and allow the old worker to finish safely.

### C6 — repeated child crash/restart

If future self-healing is considered, inject repeated crashes to test:

- finite restart budget;
- backoff/jitter;
- daemon lease exclusivity;
- zero dual-daemon window;
- task recovery remains fail-closed.

## Required liveness semantics to research

A future runtime health model should distinguish:

```text
OWNER_UI_ALIVE
REMOTE_ROUTE_READY
MCP_CHILD_ALIVE
DAEMON_AUTHORITY_READY
EXECUTION_READY
```

These are not equivalent.

A remote route that can persist a task is not necessarily proof that a daemon exists to execute it.

## Candidate future self-heal shape, not implementation

If benchmark evidence confirms a meaningful availability problem, a parent-side supervisor could be researched with constraints:

```text
child exit observed
  -> mark execution unavailable immediately
  -> stop claiming full operational health
  -> wait for child physical exit confirmation
  -> bounded restart/backoff
  -> child acquires daemon authority lease
  -> restart reconciliation fences unproven attempts
  -> only then report execution ready
```

Mandatory invariants:

- never run two daemon authorities concurrently;
- never treat child restart as physical termination proof for an old attempt;
- no automatic duplicate submit/replay;
- remote task durability remains idempotent;
- connection and execution readiness remain separately visible;
- Owner can disable/recover manually when automatic restart budget is exhausted.

## Phase-0 verdict

`MATERIAL_AVAILABILITY_HYPOTHESIS`

MAR V1.1 has strong durable/fail-closed daemon restart semantics, but source inspection does not show parent-side supervision of the `mcp-stdio` execution child. Because remote MCP handlers use the parent TaskService directly, the parent may remain remotely reachable and persist work while the daemon child is absent.

Treat runtime-child/daemon liveness as a benchmark validity prerequisite and test C0-C6 before considering any V1.2 runtime supervisor. Preserve the existing safe-drain behavior for client/parent disconnect unless evidence proves a bounded alternative with equal or stronger mutation safety.
