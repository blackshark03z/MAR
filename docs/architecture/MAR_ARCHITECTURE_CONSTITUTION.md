# MAR Architecture Constitution

**Status:** CANONICAL — LONG-TERM ARCHITECTURE SOURCE OF TRUTH
**Date:** 2026-09-19
**Scope:** Future MAR evolution after the accepted V1/V1.2 safety architecture
**Change policy:** May be changed only by explicit architecture review backed by evidence.

**Accepted amendment — 2026-09-21:** `docs/architecture/MAR_HYBRID_SIMPLIFICATION_DECISION.md` is the canonical evidence-backed amendment for workspace materialization, lifecycle simplification, cleanup-first resource governance, authority/capacity decoupling, and immutable runtime promotion. It preserves the earned safety outcomes while explicitly allowing simpler representations.

## 1. Purpose and precedence

This constitution defines the long-term architectural boundary of MAR.

The frozen V1 architecture under `docs/architecture/MAR_V1_Architecture_FROZEN/` remains authoritative for the safety, authority, recovery, verification and integration invariants already earned and accepted. Historical release records remain historical truth for the release they describe.

For **future architecture evolution**, this constitution is the canonical boundary. A future release may narrow or specialize it, but must not silently contradict it.

If an older document uses terms such as **Web Brain**, **GPT Web Brain**, **Agent Worker planner**, or other provider-specific cognition language, interpret those terms through the boundary defined here unless the document is describing historical implementation evidence.

## 2. Product position

MAR is a:

> **Durable Local Execution Kernel for External Cognition.**

MAR is not an AI framework, not a development methodology, not a provider-specific agent runtime, and not an internal multi-agent cognition system.

Canonical responsibility chain:

```text
Owner
  |
  v
CADS
  |  intent / acceptance / engineering policy
  v
External Cognition Runtime
  |  reasoning / planning / decisions
  v
MAR Edge
  |  capability translation / durable handles
  v
MAR Kernel
  |  execute / verify / persist facts / recover
  v
Local Project + Host Reality
```

The stable responsibility split is:

```text
CADS = defines how work should be framed and judged

External Cognition = decides what should be done

MAR = makes the decision act on real project state safely,
      durably, observably and recoverably
```

## 3. External Cognition definition

**External Cognition Runtime** means any replaceable reasoning environment that can consume bounded MAR facts/capabilities and return bounded decisions or action intents.

Examples may include:

- ChatGPT Web;
- Claude Web;
- Grok Web;
- Gemini or other Web clients;
- provider-managed agent sessions;
- API-backed reasoning sessions;
- future coding/agent runtimes;
- future interoperable agent protocols.

No specific provider, model, Web client, transport session or provider-native memory is task authority.

MAR must improve naturally when cognition becomes stronger:

```text
stronger cognition
  -> fewer reasoning turns
  -> larger useful bounded action groups
  -> less repeated context transfer
  -> lower verified-result latency
```

without requiring MAR kernel redesign.

## 4. Stable kernel boundary

The following are long-lived MAR kernel primitives. They should change only when strong evidence proves an invariant or deployment assumption insufficient:

- single durable coordination authority;
- durable Task / Attempt / Run Epoch identity;
- logical fencing plus physical mutation-authority control;
- one simultaneously mutating attempt -> one isolated mutable workspace while active; workspace is reconstructible execution materialization, not durable task truth;
- process-tree ownership;
- worker authority weaker than daemon authority;
- resource governor and bounded host envelope;
- scheduler/fairness mechanics;
- bounded context/decision projection;
- durable semantic checkpoint/facts;
- revision-bound verification;
- criterion/evidence binding;
- expected-head crash-safe integration;
- side-effect intent/reconciliation;
- fail-closed recovery;
- bounded retention/reclamation;
- runtime/source/artifact identity and activation truth.

This part of MAR should become deliberately boring.

## 5. Adaptive policy boundary

The following may evolve faster when benchmark/model capability changes justify it:

- context ranking and context-size budgets;
- delta projection shape;
- retrieval/index policy;
- tool grouping and compound deterministic operations;
- observation-barrier policy;
- repair-feedback ordering;
- verification-plan presets supplied by CADS/Tech Lead;
- connector capability profiles;
- provider/session continuation optimizations;
- scheduler weights and resource estimates;
- specific bounded caches;
- Project Intelligence ranking and affected-set analysis;
- transport/protocol adapters.

Rule:

> **Stable mechanism, adaptive policy.**

Prefer a policy/configuration change over a new runtime subsystem when both can deliver equivalent benefit.

## 6. Durable truth

### 6.1 One coordination authority

MAR must have one durable coordination authority.

For the current single-owner/single-host product profile:

> **SQLite/WAL is the canonical durable coordination truth.**

Do not add Redis, PostgreSQL, a second coordination database, an actor-state store or event-sourcing authority unless a measured deployment requirement invalidates the single-host assumption.

The architectural invariant is **one durable coordination authority**. SQLite is the canonical implementation for the current product profile, not a promise that every hypothetical future distributed MAR must use SQLite forever.

### 6.2 Facts versus transcript

MAR stores authoritative facts such as:

- Goal/contract identity;
- task state;
- attempt/run epoch;
- current authority;
- revision/workspace identity;
- decisions/checkpoints;
- unresolved effects;
- verification/evidence identity;
- integration result;
- recovery/retention state.

Raw model messages, transcripts, tool outputs, diffs and logs may be retained as bounded evidence/artifacts when useful.

Rule:

> **Facts are authority. Transcripts are evidence/artifacts, not durable working memory or task authority.**

MAR must not require replaying an ever-growing conversation history to recover or continue a task.

## 7. Context and memory complexity

MAR context must be **minimum sufficient context**, derived from current durable facts plus selected immutable evidence.

Target memory behavior:

```text
RAM ~= O(active work + bounded active context + bounded caches)
```

not:

```text
RAM ~= O(project age + prior chats + historical turns)
```

Larger future model context windows do not by themselves justify sending more data.

Provider-native prompt caches, long sessions, compaction, native memory and continuation handles may be used as optimizations. Loss of that provider state must remain recoverable from MAR durable truth.

## 8. Protocol and provider neutrality

MAR kernel must not branch on provider identity.

Avoid:

```text
if GPT ...
if Claude ...
if Grok ...
```

inside task lifecycle, scheduler, workspace, verification, integration or recovery.

The edge may use a capability profile describing behavior such as:

- interactive turn support;
- persistent session continuation;
- parallel/programmatic tool calls;
- tool discovery;
- streaming/events;
- async/background execution;
- usage metadata;
- transport and authentication mode.

Kernel decisions should depend on capabilities and durable MAR state, not vendor names.

MCP is an adapter/control protocol, not task authority. Future MCP Tasks, A2A or other protocols should map to existing MAR durable task semantics rather than create a second task lifecycle.

## 9. Transport/session rule

Transport lifetime is disposable.

```text
transport/chat/provider session != task ownership
```

A disconnect, browser restart, tunnel replacement, request retry or provider-session loss must not silently:

- duplicate task identity;
- replace current attempt authority;
- redefine Goal/acceptance;
- invalidate durable evidence without cause;
- grant mutation authority;
- fabricate completion.

Stateful transports/provider sessions may be exploited for performance, but durable correctness must not depend on them.

## 10. Cognition/action boundary

MAR should reduce unnecessary cognition round trips, but must not become a generic workflow engine.

Preferred unit:

### Bounded Action Batch

A bounded action batch may contain deterministic operations that do not require a new reasoning decision between them.

It must preserve:

- explicit authority;
- action ordering;
- stop/failure semantics;
- per-action receipts/evidence;
- effect identity where needed;
- bounded output;
- current attempt/run-epoch binding.

Use an **Observation Barrier** whenever newly observed reality may change the next decision.

Principle:

> Execute deterministically until new evidence is required for the next decision.

Do not claim generic transaction/atomic semantics across filesystem, Git, process or external side effects. Existing Effect Reconciliation remains authoritative.

## 11. Verification boundary

MAR executes verification mechanics and records exact evidence.

CADS + Tech Lead/External Cognition decide what must be proven and may produce a bounded Verification Plan.

MAR must preserve final authoritative verification semantics:

- exact candidate revision;
- exact verification profile/plan;
- environment/tool identity where required;
- criterion-specific observation;
- stale candidate/environment rejection;
- crash-safe integration binding.

Repair/convergence feedback may be faster and narrower than final verification, but:

> **Fast repair feedback must never redefine VERIFIED.**

## 12. Project Intelligence

Project Intelligence is derived, rebuildable assistance for context/retrieval/impact analysis.

It is not a second source of truth.

Preferred progression:

1. Git/path/revision identity;
2. exact/lexical search;
3. symbols/import/dependency graph;
4. test/affected mapping;
5. incremental hash/revision reuse;
6. semantic retrieval only when benchmark proves deterministic retrieval insufficient.

No vector database, embedding platform, Tree-sitter/SCIP infrastructure or learned retrieval is admitted merely because it is technologically advanced.

## 13. Performance objective

Primary performance metric:

> **Verified Result Latency by representative task class.**

Supporting metrics include:

- cognition/model turn count;
- cognition wait time;
- tool/action count;
- useful deterministic actions per cognition turn;
- context/tool-result bytes sent outward;
- time to first relevant context;
- time to first useful failure;
- repair-loop wall time;
- final verification wall time;
- scheduler/resource wait;
- integration time;
- attempt/rework count;
- human intervention count;
- runtime RAM/CPU/disk/process cost.

A lower latency result is not an optimization if correctness, evidence, authority, recovery or final acceptance quality regresses.

## 14. Complexity-adjusted admission rule

A new runtime subsystem or material optimization is admitted only when all are true:

1. the problem is measured or reliably reproduced;
2. MAR materially contributes to the problem;
3. expected benefit is meaningful on real/representative workloads;
4. the change is bounded;
5. an acceptance oracle exists;
6. safety/authority/recovery invariants remain intact;
7. benefit justifies implementation, runtime, recovery and maintenance complexity.

Useful decision aid:

```text
OptimizationScore
=
(ExpectedWallTimeReduction * Frequency * Confidence)
/
(ImplementationComplexity
 + RuntimeComplexity
 + RecoveryRisk
 + MaintenanceCost)
```

This is a Tech Lead decision aid, not a production scheduler algorithm.

Prefer deletion/simplification when benefit is equivalent.

## 15. Runtime subsystems not admitted by default

Do not add by default:

- MAR-internal AI planner;
- internal multi-agent cognition fabric;
- AI memory platform;
- semantic reasoning service;
- second BrainAdapter framework;
- Vector DB / embedding memory;
- Redis/PostgreSQL for current single-host coordination;
- actor framework;
- global event-sourcing platform;
- generic DAG workflow engine;
- generic CAS/cache platform;
- mandatory Tree-sitter/SCIP/LSP infrastructure;
- OpenTelemetry platform as a prerequisite;
- provider-specific kernel branches;
- remote/distributed worker architecture.

Any of these may be reconsidered only through the admission rule.

## 16. MAR Lab boundary

MAR Lab / Research / Benchmark remains outside the production kernel.

It may:

- observe metrics;
- reconstruct history;
- run controlled benchmarks;
- detect regressions;
- research bottlenecks;
- prepare proposals.

It must not become:

- execution authority;
- a second scheduler;
- a second durable truth;
- an automatic production mutator/deployer.

A proven improvement enters production through a normal bounded MAR/CADS change with acceptance evidence.

## 17. Long-term compatibility test

A future MAR architecture change should pass this question:

> If the external cognition becomes dramatically stronger next year, does this MAR change become more useful, remain neutral, or become legacy overhead?

Prefer changes that let stronger cognition produce fewer turns and more verified work while the kernel remains stable.

## 18. Current forward direction

MAR V1.3 B-D is the completed bounded performance/simplicity optimization set; later E/F work remains closed under its stop rule.

The next architecture program is **MAR V2 — Hybrid Simplification**, governed by `docs/architecture/MAR_HYBRID_SIMPLIFICATION_DECISION.md`, `docs/architecture/MAR_CONDITIONAL_EXECUTION_KERNEL.md`, and `docs/roadmap/MAR_V2_HYBRID_SIMPLIFICATION.md`.

MAR is no longer the mandatory coding path. Its lifecycle begins only for work routed into governed execution because the Goal requires runtime isolation, durable recovery, fencing, durable authority, resource governance or crash-safe integration. Provider/model/session/cognition mechanics are replaceable harness concerns; current implementations remain compatibility surfaces but are closed to forward expansion unless a kernel invariant demonstrably requires it.

This is an incremental simplification migration, **not a from-scratch V2 rewrite**. Priority order:

1. baseline current storage/lifecycle cost;
2. cleanup-first resource lifecycle;
3. checkpoint/rehydrate inactive BLOCKED/NEEDS_INPUT work;
4. lazy isolated workspace materialization;
5. lifecycle/status simplification;
6. decouple attempt authority heartbeat from resource-slot waits;
7. immutable versioned runtime promotion;
8. compact historical evidence/derived state where measured.

No phase may weaken final verification, stale-worker fencing, owner-work protection, expected-head integration, dangerous-action controls, or runtime identity truth.

## 19. Governance

Changes to this constitution require:

- explicit architecture review;
- the evidence or changed assumption that motivates the change;
- impact on frozen invariants;
- expected benefit;
- complexity/failure-domain cost;
- acceptance oracle;
- migration/backward-compatibility implications.

Release-specific implementation may proceed only after this architecture boundary and the relevant CADS/Goal acceptance are consistent.
