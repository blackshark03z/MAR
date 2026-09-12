# Slice 019 — V1.2 runtime truth and supervision

**Status:** `PLANNED / NOT_STARTED`  
**Scope source:** `docs/research/028_V1_2_BOUNDED_SCOPE_DECISION.md`  
**Implements:** R-027 `C-02` + `C-01`  
**Production implementation authority:** not opened by this document

## Goal

Make MAR able to answer two runtime questions truthfully and durably:

1. **What exact release binary/source is running?**
2. **Is the execution runtime actually available, not merely the Owner/remote surface?**

The slice must preserve the V1.1 task/attempt/fencing model. It must not redesign MCP, SQLite coordination, worker authority, verification or integration.

## User-facing workflow / CUJ

Normal healthy case:

```text
Owner starts MAR
  -> launcher/runtime proves release identity
  -> Owner Console shows runtime identity + execution ready
  -> remote submit is accepted
  -> daemon schedules work normally
```

Execution-child failure case:

```text
Owner/remote surface remains reachable
  -> mcp-stdio execution child exits
  -> MAR detects execution_runtime_ready=false
  -> Console/diagnostics show degraded execution truth
  -> durable tasks are preserved
  -> new submits are explicitly queued/not-executing or intentionally rejected
  -> optional bounded restart may restore execution only after authority safety checks
```

Stale artifact case:

```text
launcher/runtime sees binary/source/manifest mismatch
  -> mismatch is explicit
  -> benchmark/release gate refuses to treat runtime as accepted V1.2
  -> no silent green/healthy state
```

## Acceptance

### A1 — Release artifact/source provenance

A promoted runtime must expose one immutable identity record containing at least:

- release/version;
- source commit;
- production-tree identity or equivalent source fingerprint;
- binary SHA-256;
- Go/toolchain identity;
- SQLite schema compatibility/version;
- build timestamp;
- Owner UI asset/build identity.

Acceptance tests must prove:

1. a clean build from the intended release commit binds to that commit;
2. the binary SHA-256 matches the release manifest;
3. schema compatibility is checked before normal runtime use;
4. UI asset identity matches the intended source release;
5. substituting a known stale binary produces an explicit mismatch/degraded signal;
6. benchmark/release gates reject mismatched or unbound runtime identity;
7. the accepted release record can identify exactly which executable was promoted.

### A2 — Execution-runtime health truth

Kill only the `mcp-stdio` execution child while leaving the Owner parent/remote bridge alive.

Require:

1. `execution_runtime_ready` becomes false within a bounded detection interval;
2. Owner surface reachability and remote route reachability remain distinct signals;
3. no overall healthy state may be derived from route/UI reachability alone;
4. existing durable tasks remain intact;
5. new submit behavior is explicit: durable queued/not-executing or intentionally rejected, never falsely active;
6. proxied execution/task operations surface a typed degraded condition rather than a misleading generic healthy state;
7. no duplicate daemon authority is created;
8. stale workers never gain or retain mutation authority because of supervision logic.

### A3 — Optional bounded child restart

Automatic child restart is allowed in this slice only if it can be proven safe without weakening physical/logical fencing.

If implemented, acceptance additionally requires:

- one replacement execution child at a time;
- daemon authority lease/takeover converges to one authority holder;
- worker/process-tree reconciliation occurs before mutation-capable replacement;
- bounded retry/backoff with a terminal degraded state;
- repeated child crashes do not spin indefinitely;
- restart preserves durable tasks and pending WebTurns.

If these conditions cannot be proven in the slice, ship **truthful degraded state without auto-restart** rather than expanding scope.

## Material decisions

### D1 — No new durable task lifecycle state solely for UI wording

Do not add a new task state merely to say "runtime unavailable" unless implementation evidence proves the existing lifecycle cannot represent the truth.

Prefer runtime/operations health + presentation semantics such as:

```text
SUBMITTED + execution_runtime_ready=false
=> queued / execution unavailable
```

This avoids reopening the durable task state machine without necessity.

### D2 — Runtime identity is release evidence, not decorative metadata

Identity fields used by acceptance/benchmark gates must come from build/runtime evidence, not editable UI labels or filenames such as `mar-v1-stable.exe`.

### D3 — Detection before self-healing

Truthful detection/degraded behavior is mandatory. Automatic restart is secondary and must not block delivery of the health-truth fix if safe restart grows beyond the bounded slice.

### D4 — Parent reachability is not daemon health

The Owner UI parent and remote bridge may remain reachable while execution is absent. Health aggregation must represent this topology explicitly.

## Non-goals

- MCP transport rewrite;
- new task orchestration service;
- second coordination database;
- worker capacity redesign (Slice D / C-04);
- connection/tunnel process self-healing (later V1.2 Slice C);
- UI visual redesign;
- model/provider changes;
- sandbox readiness caching (next slice);
- verification optimization;
- broad telemetry platform.

## Constraints

- preserve V1.1 logical + physical fencing;
- SQLite remains durable coordination truth;
- remote submit/idempotency semantics remain compatible;
- no stale binary may be promoted by filename convention alone;
- no hidden authority escalation;
- failure must remain fail-closed for mutation authority;
- V1.1 production remains usable until the V1.2 candidate is independently accepted.

## Required evidence before integration

At minimum capture:

- source commit / production-tree identity;
- built executable hash;
- runtime-reported identity;
- release manifest hash/binding;
- schema identity;
- UI asset identity;
- child PID/role before fault injection;
- health snapshot before/after child death;
- durable task count/state before/after child death;
- daemon authority state during optional restart;
- proof no duplicate/stale mutation authority exists.

## Regression set

Must retain PASS for relevant existing coverage including:

- T7 client disconnect semantics;
- T8 worker crash safety;
- T9 daemon crash/recovery safety oracle where applicable;
- T14 stale-worker physical/logical fencing;
- submit/brain-response idempotency;
- 9/9 transport semantics sentinel;
- integration freshness/CAS protections;
- Owner Console/remote public-surface contract.

## Slice stop rule

Slice 019 is complete when A1 + A2 pass and regression gates are clean. A3 is included only if safe bounded restart also passes without widening architecture.

Do not absorb sandbox caching, task-summary optimization, connection self-healing or Web-wait capacity work into this slice.
