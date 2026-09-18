# MAR VNext — ChatCode Learnings and Optimization Backlog

**Date:** 2026-09-18  
**Status:** RESEARCH / INPUT TO VNEXT

## Research question

Which ideas from ChatCode materially improve MAR speed, context efficiency and Web-driven coding experience without weakening MAR's durable execution, safety, evidence and integration guarantees?

## Current reality

MAR currently has **NO internal MAR brain/model**. The connected Web Chat is the reasoning brain. MAR is the local execution/runtime boundary that gives the Web model controlled access to project state, workspace tools, commands, verification, durable task state and integration.

The optimization target is:

> Make each Web reasoning round-trip produce more useful repository work with less repeated context and less deterministic work delegated back to the Web model.

## What ChatCode gets right

The strongest lessons to adopt are:

1. **deterministic tools before AI** — search, Git, parsers, tests, formatters, LSP/static analysis should answer deterministic questions first.
2. **Focused Project Brain** — keep projects local, index files/symbols/relations, and retrieve only relevant context instead of bulk-sending the repository.
3. **Do not resend known information** — cache project knowledge and avoid repeated context transfer.
4. **Project-scoped permissions** — each workspace/project owns its path, permissions and operational bindings.
5. **Maximize useful outcome per frontier reasoning call** — optimize verified value created, not raw AI-call count.
6. **Escalate only when necessary** — strong AI should receive filtered context, dependency relationships and test evidence rather than rediscovering deterministic facts.

Public references:
- https://chatcode.ozintech.com/philosophy
- https://chatcode.ozintech.com/philosophy/responsible-ai-routing?lang=en
- https://chatcode.ozintech.com/security
- https://chatcode.ozintech.com/guide?lang=vi
- https://chatcode.ozintech.com/

## MAR telemetry lessons

Recent MAR work exposed two independent latency classes:

- **Verification-dominant tiny tasks:** even one-turn/one-tool no-op work can spend minutes in broad Go verification.
- **Web-cognition/orchestration-dominant tasks:** historical MAR research found several longer tasks where Web wait dominated task wall time.

Speed V1.x also showed that cache-layer complexity alone did not materially improve the Web-driven experience:
- removing `-count=1` did not produce a large wall-time reduction;
- shared `GOCACHE` gave little observed benefit;
- shared read-only `GOMODCACHE` did not establish a meaningful speedup;
- raising package parallelism conflicted with Windows commit/pagefile evidence;
- the repository does not expose a `testing.Short()` contract.

Therefore optimize the whole **Web → MAR → repository** loop, not only command micro-latency.

## Adopt now

### Compact Task Capsule

Keep raw data local and send the Web brain a compact canonical projection containing:
- goal/acceptance summary;
- base/current candidate identity;
- material decisions/checkpoint;
- changed files/areas;
- focused validation state;
- unresolved failures/questions;
- recent relevant source references;
- references to raw command/tool outputs.

### Delta/event-oriented context

Send material state changes instead of repeated snapshots. Coalesce heartbeat/status noise. Normal Web wake-up reasons should converge on:
- DECISION_REQUIRED
- AUTHORITY_REQUIRED
- RECOVERABLE_FAILURE_NEEDS_REASONING
- CANDIDATE_READY
- TERMINAL

### Compound deterministic operations

Promote repeated primitive tool sequences only when telemetry shows real savings. Candidate examples:
- inspect symbol + callers + tests + related files;
- apply patch + format + focused validation + compact diff;
- collect failure context around exact source/test locations.

### Incremental Project Intelligence V1

Use deterministic/native mechanisms first:
- Git paths/revisions;
- package/import relationships;
- symbols/references available from native tooling;
- test mappings;
- recent changed scope.

No vector database is required before Stable.

### Affected-set as Project Intelligence capability

Changed-file → package → reverse dependent/test impact analysis is useful, but it belongs to Project Intelligence. It returns deterministic impact + uncertainty. CADS + Tech Lead decide whether verification uses that affected set or escalates to full verification.

## Keep in MAR core

Earned complexity remains core:
- durable task/attempt/run-epoch identity;
- SQLite execution truth;
- isolated workspace/worktree;
- OS sandbox and explicit authority;
- heartbeat/lease/stale-write fencing;
- candidate revision identity;
- machine-observable evidence;
- safe integration preserving owner work;
- runtime/source activation identity;
- crash/restart recovery.

## Move out of MAR core policy

CADS + Web Tech Lead own:
- Product Accepted vs Release Qualified policy;
- affected vs full verification strategy;
- high-risk escalation;
- UX/design gates;
- release/promotion policy;
- language-specific development methodology.

MAR exposes mechanics and executes supplied plans.

## Measure before deciding

Controlled A/B evidence is required before keeping or reverting:
- shared writable `GOCACHE` and external writable cache grant;
- shared read-only `GOMODCACHE` as a speed optimization;
- repeated environment/freshness hashing cost;
- cache-seeding strategies.

## Research later / post-Stable

Useful ideas deferred:
- Tree-sitter generic incremental parsing;
- semantic embeddings/vector retrieval;
- hybrid BM25+dense reranking;
- MMR/context-budget ranking;
- resource-aware DAG scheduling;
- local-model routing;
- learned test selection;
- remote/distributed cache;
- multi-agent orchestration.

## Research conclusion

The ChatCode lesson to retain is a resource-allocation principle:

> deterministic computation should prepare precise local context so the Web brain spends reasoning only where reasoning adds value.

MAR should combine that efficiency with its durable execution, isolation, evidence, recovery and integration guarantees.
