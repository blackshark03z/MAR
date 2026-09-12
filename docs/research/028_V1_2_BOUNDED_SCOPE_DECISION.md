# R-028 — MAR V1.2 bounded scope decision

**Status:** `SCOPE_DECISION_CANDIDATE`  
**Date:** 2026-09-13  
**Baseline:** MAR v1.1.0 remains frozen  
**Implementation authority:** none until implementation is explicitly opened

## Product goal

MAR V1.2 should make the accepted V1.1 architecture **truthful, recoverable and cheaper to observe under real long-running Web work** without replacing the transport model, durability model, authority model, SQLite source of truth, or worker sandbox.

The release is successful when runtime/release identity is trustworthy, execution and connection failures become explicit/self-healing, proven observability waste is removed, and external-cognition waiting no longer blocks unrelated useful work.

## Scope rule

V1.2 is deliberately bounded. Only requirements with reproduced/measured evidence and a concrete acceptance oracle are implementation scope. New research topics, UI redesign, general telemetry platforms, generic agent frameworks, transport rewrites and broad caching are out of scope unless a core acceptance test proves the current boundary insufficient.

## Core release scope

### Slice A — Runtime and release truth

**A1 — Artifact/source provenance (`C-02`)**

Bind promoted runtime identity to release version, source commit, production-tree identity, binary SHA-256, Go/toolchain identity, SQLite schema compatibility and UI asset identity. Runtime/launcher surfaces must expose mismatch and benchmark gates must reject stale/unbound binaries.

**A2 — Execution child liveness (`C-01`)**

Owner/remote reachability must not imply execution readiness. If `mcp-stdio` dies, MAR must transition to an explicit degraded/queued state within a bounded detection interval and either perform safe bounded restart/takeover or clearly report that execution is unavailable. No duplicate daemon authority or stale mutation authority is allowed.

### Slice B — Remove proven observer waste

**B1 — Sandbox readiness cache with fail-closed invalidation (`C-03`)**

Normal `/api/runtime` polling must not spawn a sandbox probe process every refresh. Cache/revalidation must remain boot/preparation/failure aware and fail closed for worker admission.

**B2 — Narrow live-usage/task-summary reads (`C-05`)**

Task-list/live usage surfaces must read only the durable summary needed for pending state, activity and usage. Historical cognition request bodies must not be reread solely to render summary telemetry.

### Slice C — Connection desired-state recovery

**C1 — Bounded owned-process self-healing (`C-06`)**

Owned tunnel/client processes whose desired state is `running` must converge back to running with bounded retry/backoff or transition to explicit degraded state with diagnostics. Repeated failure must not spin indefinitely or silently rotate identity.

### Slice D — External-cognition wait capacity

**D1 — Waiting must not monopolize scarce heavy execution capacity (`C-04`)**

Two durable Web waits must not consume all default heavy execution capacity and indefinitely block an unrelated READY task. The implementation may park/yield, release only the heavy lease, or checkpoint/restart, but must preserve exact task/attempt/run-epoch/turn binding, stale-response rejection, physical fencing and bounded budgets.

This slice is **implementation-gated by sandbox readiness and an end-to-end safety harness**. If the host prerequisite cannot be established, V1.2 may ship the other core slices while D1 remains explicitly deferred; it must not be approximated with a mock and called proven.

## Bounded follow-up scope after core stability

These items may be implemented in V1.2 only after Slices A–D pass their acceptance gates and the change remains small.

### E1 — Remove immediate duplicate first-turn context build (`C-07`)

Only reuse/remove the structurally duplicate pre-turn/turn-1 build when repository/worktree/Goal identity cannot change between the two points. Do not create a broad revision-only cache.

### E2 — Fast repair feedback vs authoritative final verification (`C-08`)

A failed repair iteration may return useful failure evidence sooner, but VERIFIED/integration must still require the complete configured verification profile with revision/environment-bound evidence.

If either E1/E2 expands beyond a bounded change, defer it rather than enlarge the release.

## Explicitly out of scope

Do not include in V1.2 without new blocker evidence:

- MCP/Streamable HTTP rewrite;
- SQLite replacement, second coordination store, or generic connection-pool redesign;
- broad repository/context cache machinery;
- mandatory OpenTelemetry/Prometheus/Grafana stack;
- generic multi-agent/worker fabric redesign;
- cloud/distributed workers;
- general Owner Console visual redesign;
- provider/model proliferation;
- relaxing sandbox, fencing, verification freshness or integration CAS safety;
- hidden retries or infinite self-healing loops.

## Cross-slice acceptance contract

V1.2 cannot be declared stable until all implemented core slices prove their own oracle and the following release-level regressions pass:

1. promoted binary is cryptographically/source bound and stale substitution is detected;
2. kill execution child while parent remains reachable: health degrades truthfully, no duplicate authority, durable work remains intact;
3. 30 normal runtime polls after valid readiness produce zero extra sandbox-check children;
4. active 19-turn task summary preserves visible semantics while materially reducing full-payload read cost (target >=75% reduction versus R-016 fixture);
5. owned tunnel/client exit yields bounded recovery or explicit degraded state with backoff truth;
6. if D1 is implemented, two Web waits + third READY task proves useful capacity without duplicate mutation authority;
7. ambiguous-ACK/precommit/session-expiry transport sentinel remains 9/9 PASS or stronger;
8. real tunnel replacement with a pending WebTurn still resumes the exact durable turn;
9. stale-worker physical/logical fencing, crash recovery and integration freshness remain PASS;
10. final full verification profile remains authoritative before VERIFIED/integration;
11. accepted-source representative T1 baseline is run only after runtime identity and sandbox prerequisites are valid.

## Release stop rule

Once the above bounded scope passes Product Acceptance, stop V1.2. Do not add additional optimization merely because research can continue.

New findings discovered during implementation are handled as follows:

- safety/correctness blocker affecting an in-scope invariant → fix before release;
- material regression caused by the V1.2 change → fix before release;
- unrelated improvement or low-impact optimization → backlog/research for a later release.

## Recommended implementation order

```text
A1 provenance
  -> A2 child liveness / degraded truth
  -> B1 sandbox-readiness observer waste
  -> B2 narrow live-usage reads
  -> C1 connection self-healing
  -> D1 Web-wait capacity (only with valid sandbox/end-to-end harness)
  -> E1/E2 only if still bounded and justified
  -> full cross-slice regression
  -> Owner real-use acceptance
```

## Decision

`V1_2_SCOPE_BOUNDED_FOR_IMPLEMENTATION_ENTRY`

The release should improve **truth, recovery and measured throughput cost** while leaving the successful V1.1 durable execution architecture intact.
