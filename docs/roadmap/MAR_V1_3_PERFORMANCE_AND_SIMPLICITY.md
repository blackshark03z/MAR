# MAR V1.3 — Performance & Simplicity Program

**Status:** CANONICAL ROADMAP / NOT YET IMPLEMENTATION AUTHORITY
**Date:** 2026-09-19
**Architecture parent:** `docs/architecture/MAR_ARCHITECTURE_CONSTITUTION.md`
**Cognition parent:** `docs/architecture/MAR_EXTERNAL_COGNITION_CONTRACT.md`

## 1. Release intent

MAR V1.3 is not a V2 rewrite.

The purpose is to reduce real end-to-end time and interaction overhead while preserving the accepted durable execution kernel.

Primary direction:

```text
fewer cognition round trips
+
less repeated deterministic work
+
faster repair feedback
+
bounded context
+
same authoritative verification/integration/recovery guarantees
```

## 2. Baseline facts already established

Current MAR research/accepted implementation already establishes that:

- DecisionProjection removed the earlier dominant full-history replay pattern;
- structured `brain_turn` mode reduces duplicated outer Web/MCP representation while preserving authoritative structured payload;
- Web cognition wait can dominate many cognition-heavy tasks;
- context rebuild cost is real but moderate on current MAR fixtures rather than universally dominant;
- final verification can dominate short/docs-only tasks;
- historical failed `go-standard` suites show about 29.5–32.1 seconds of `vet + build` after a failed primary test before repair feedback;
- integration itself is small in the current durable sample relative to cognition/verification;
- V1.2 already corrected Web-wait compute/heavy-capacity starvation while preserving exact attempt/process authority;
- recovery and terminal-workspace retention are now earned operational mechanics and are not V1.3 redesign targets.

V1.3 must build on these facts rather than reopen solved architecture.

## 3. Primary KPI

Primary KPI:

> **Verified Result Latency by representative task class**

Required companion metrics:

- cognition/model turns per successful task;
- cognition wait time;
- useful deterministic actions per cognition turn;
- outward context/tool-result bytes;
- time to first useful failure;
- repair-loop wall time;
- final verification wall time;
- scheduler/resource wait;
- human intervention count;
- final correctness/evidence outcome;
- implementation/runtime/recovery complexity delta.

Never compare unrelated task classes using one raw latency number.

## 4. Hard invariants

V1.3 may not regress:

- immutable Goal/authority boundary;
- Task/Attempt/Run Epoch semantics;
- stale-write fencing;
- physical process/mutation authority;
- isolated mutable workspace;
- evidence/candidate revision binding;
- final authoritative verification correctness;
- crash-safe expected-head integration;
- effect reconciliation;
- runtime/source activation truth;
- fail-closed recovery;
- bounded host resource envelope.

Any invariant regression fails the slice regardless of speed.

## 5. Slice A — already implemented foundation

Current source already contains the first bounded optimization:

### Structured cognition relay

`brain_turn response_mode=structured` preserves the exact authoritative structured WebTurn/TurnRequest payload while replacing the duplicated full text representation with a bounded receipt.

V1.3 must not rebuild this as a new Task Capsule subsystem.

Treat it as the starting baseline for later measurements.

## 6. Slice B — Delta/Event-Oriented Cognition Context

### Problem

Repeated status/inspect/current-state snapshots can cause unnecessary outward bytes and cognition wakeups even when little material state changed.

### Direction

Add derived sequence/delta semantics on top of the same durable task truth.

Preferred cognition wakeups:

- DECISION_REQUIRED;
- AUTHORITY_REQUIRED;
- RECOVERABLE_FAILURE_NEEDS_REASONING;
- CANDIDATE_READY;
- TERMINAL.

Operational noise remains observable locally but does not automatically become cognition context.

### Acceptance

On fixed representative tasks:

- exact durable task outcome remains unchanged;
- stale/missed event recovery is possible from durable current state;
- outward bytes and/or cognition wakeups materially decrease;
- reconnect does not require historical transcript replay;
- no second event database/source of truth is introduced.

## 7. Slice C — Compound Deterministic Operations

### Problem

The external cognition loop currently may spend turns coordinating primitive deterministic operations that MAR can execute safely without an intervening reasoning decision.

### Direction

Mine real tool traces first.

Promote only recurring, high-value sequences such as candidates like:

- inspect symbol + callers + related tests;
- apply bounded patch + format + focused validation + compact diff;
- collect exact failure context;
- summarize candidate diff/affected scope.

The exact operations must come from telemetry, not architecture preference.

### Semantics

Every compound operation must:

- preserve the same authority checks as primitives;
- remain bounded;
- return per-step receipts/evidence where material;
- stop at declared observation barriers;
- expose failure precisely;
- not hide side effects or verification status.

### Acceptance

A compound operation remains only if it produces a measured reduction in cognition turns/tool chatter or wall time on real/representative traces without lowering correctness.

## 8. Slice D — Fast Repair Feedback

### Problem

Current authoritative profiles intentionally collect complete evidence even after an early command fails. Historical evidence shows four failed `go-standard` suites spent roughly 29.5–32.1 seconds on `vet + build` after `go test` had already failed.

That can delay the next repair reasoning turn.

### Direction

Separate:

```text
Repair Feedback Verification
!=
Authoritative Final Verification
```

Repair loop:

```text
change
 -> highest-value/likely-fail checks
 -> first useful failure evidence
 -> return to cognition
```

Final candidate:

```text
complete authoritative verification
 -> exact evidence
 -> integration eligibility
```

### Non-negotiable

Final VERIFIED semantics do not change.

A repair-feedback PASS is not a final verification result.

### Acceptance

On controlled failing fixtures:

- materially lower time to first useful failure / repair turn;
- equal or better repair success on the next turn;
- unchanged full final verification for accepted candidates;
- zero false VERIFIED/COMPLETE outcomes.

## 9. Slice E — Incremental Project Intelligence

This slice is conditional, after B-D.

### Direction

Improve rebuildable deterministic intelligence first:

- revision/file-hash incremental reuse;
- symbols/references;
- package/import relationships;
- changed-file -> affected package -> reverse dependent/test mapping;
- uncertainty/fallback reporting.

Affected analysis supplies evidence to CADS/Tech Lead. It does not silently decide release policy.

### Reopen semantic infrastructure only if measured

Tree-sitter, SCIP/LSP indexing, embeddings/vector retrieval or learned ranking remain research until current deterministic retrieval shows a material Recall@k/MRR/budget deficit on representative repositories.

## 10. Slice F — Minimal proven-safe reuse/cache

Do not build a global Action Cache.

Cache/reuse is admitted one proven-pure case at a time.

Required identity may include:

- exact relevant repository content/revision;
- command;
- toolchain;
- environment/sandbox identity;
- authority boundary;
- input artifacts;
- cache schema/version.

Prefer immutable seed/read reuse over shared writable state when writable sharing would weaken task isolation.

## 11. Connector/protocol adaptation

Provider/protocol work is not the first performance slice.

After the core V1.3 loop is measured:

- evaluate modern MCP stateless/task/event interoperability;
- generalize current Web Brain terminology/adapter code toward External Cognition capabilities where implementation still leaks provider assumptions;
- evaluate provider-managed continuation as an optimization;
- research A2A or similar interoperability only if it creates a real integration need.

No protocol migration may create a second task lifecycle.

## 12. Complexity gate

Every retained V1.3 optimization must record:

- baseline;
- target metric;
- comparable benchmark;
- code/module/dependency growth;
- new durable schema if any;
- new background process if any;
- new failure/recovery path;
- operational setup change;
- measured result.

Reject improvements whose measured user/system benefit is small relative to added architecture/failure-domain complexity.

## 13. Explicit non-goals

V1.3 does not introduce:

- MAR V2 rewrite;
- internal planner/brain;
- multi-agent cognition fabric;
- vector database;
- Redis/PostgreSQL migration;
- actor framework;
- generic DAG workflow engine;
- global cache platform;
- distributed workers;
- broad Owner Console redesign;
- weaker final verification;
- weaker fencing/recovery;
- automatic provider-specific kernel branching.

## 14. Suggested implementation order

```text
Baseline representative task classes
        |
        v
Slice B — delta/event context
        |
        v
Slice C — first telemetry-proven compound ops
        |
        v
Slice D — fast repair feedback
        |
        v
Re-measure end-to-end
        |
        +--> stop if target achieved
        |
        +--> conditional Slice E/F
```

Run one bounded slice at a time. Do not open the next slice merely because it exists in the roadmap.

## 15. Release stop rule

V1.3 should stop when:

1. required task-class benchmarks show a material reduction in Verified Result Latency and/or cognition round trips;
2. no accepted safety/evidence/recovery invariant regresses;
3. complexity-adjusted benefit remains favorable;
4. full release qualification passes;
5. the exact promoted runtime is aligned/trusted/healthy.

Optional research and further micro-optimization must not keep V1.3 open indefinitely.
