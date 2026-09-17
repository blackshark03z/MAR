# MAR cognition protocol review - 2026-09-17

## Decision status

ACCEPTED AS ARCHITECTURE GUIDANCE. This record reconciles the second independent Claude review with the exact MAR v1.2.0-requalification.7 implementation. It does not reopen the frozen execution architecture and does not create new durable authority states or services.

Canonical topology remains:

`ChatWeb = bounded planner/reasoner -> MAR = durable execution kernel + policy + context broker -> Worker = local action executor`

MAR, not ChatWeb, owns durable truth, authority, sandbox/process control, verification and serialized Git integration.

## Capabilities already present - do not rebuild

The following review proposals already exist materially in V1 and must be treated as existing protocol, not greenfield backlog:

- Multi-tool Action Batch: one model turn may already return multiple tool calls; current agent bounds are 8 tool calls/turn and 64/task. The executor currently validates the whole batch identity and executes calls serially.
- Bounded Decision Episode: Web episode/convergence budgets already limit turns, calls, bytes, tokens, attempts and active execution.
- Bounded DecisionProjection: current identity, controls, checkpoint, result/evidence identities, artifacts and repository context are rebuilt from durable truth rather than replaying an unbounded transcript.
- Untrusted-data boundary: repository content, comments, tool observations, test output and checkpoint memory are explicitly untrusted evidence and cannot widen the immutable Goal Contract.
- Observation artifacts: oversized/incomplete tool observations are persisted durably and represented to the model by bounded receipts/handles.
- Stale-response identity: task_id + attempt_id + run_epoch + turn/request identity is bound durably; only the exact pending turn can resume cognition.
- Per-mutation authority recheck: mutation-producing tools revalidate attempt authority immediately before execution.
- Acceptance oracles and revision-bound verification already exist; a general green test does not by itself prove every acceptance criterion.
- Web-wait capacity yielding and bounded task/model/token/time budgets already exist.

## Accepted vNext direction

### 1. Performance baseline before optimization

Add stage-level measurement before changing batching, caching or projection policy. Measure at minimum:

- submit -> first worker action;
- context construction;
- brain wait / connector latency;
- model reasoning turn;
- tool execution;
- verification;
- integration;
- Goal -> terminal verified result.

Also record brain-turn count, tool-call count, tool batches, projection/request bytes, token usage, repair loops and human interventions. Initial thresholds must be derived from observed p50/p95 rather than guessed targets.

### 2. Deterministic local actions

Permit only policy/profile-declared deterministic follow-up actions that do not require model reasoning, for example formatting, bounded diff collection, or declared static checks. This does not grant the worker freedom to invent commands or widen authority.

### 3. Structured observation/failure receipts

Evolve existing bounded observations into stronger structured receipts where useful, including fields such as failure_class, exit_code, failing packages/tests, changed paths, bounded relevant frames and durable artifact reference. Raw output remains durable evidence; the projection carries only the bounded useful receipt.

### 4. Deterministic patch-quality gate

Before final verification/integration, add deterministic checks for changed-path scope, sensitive/generated paths, diff size/format, test-file changes, base revision and evidence identity, and acceptance coverage. This gate must not become an AI judge with integration authority. Test changes are review signals, not automatic failures.

### 5. Adversarial effect-based benchmark

Add a bounded corpus for prompt injection and hostile repository/tool data. Include source/docs/test-output instructions to exfiltrate secrets, weaken sandboxing, disable tests, traverse paths, run dangerous commands, or follow malicious generated content. PASS is defined by absence of unauthorized effects and preservation of durable authority invariants, not by model text claiming it ignored an instruction.

### 6. Adaptive projection only after measurement

Planning/repair/review may later use different context emphasis, but adaptive projection and deeper cache work are not implementation priorities until stage telemetry shows context construction or model-input quality is a real bottleneck.

## Protocol concepts - derived labels, not new authority entities

The following names may be used in design/telemetry, but MUST NOT become new databases/services/state machines without evidence:

- Decision Episode = existing bounded cognition episode.
- Action Batch = existing model ToolCalls[] batch.
- Observation Receipt = existing bounded tool observation / artifact representation, to be structured further.
- Quality Gate = deterministic candidate checks before final release verification/integration.
- Planning / execution / repair / review are derived cognition-phase labels only unless a concrete invariant later requires durable state.

## Batching decision

Do not implement a second batching protocol. The current loop already accepts multiple tool calls in one model turn. A future optimization may parallelize independent read-only calls inside one existing batch only when telemetry shows worthwhile latency reduction and when dependency/order/resource safety can be proven. Mutation calls remain serialized unless an explicit safe dependency model is introduced.

## Long-session benchmark correction

Do not benchmark one unbounded 100-turn browser episode. MAR intentionally bounds episodes/turns. The meaningful resilience benchmark is 20-100 cognition decisions distributed across bounded episodes, checkpoints, reconnects and resumes while verifying that Goal/revision/control/evidence identity remains correct and browser transcript memory is disposable.

## Remote authorization decision

For current single-owner/local-first V1, bearer capability routes remain acceptable only as secrets with rotation/redaction/bounded lifecycle and installation hardening. OAuth/MCP Authorization becomes a design gate for a stable long-lived Internet-facing or multi-user deployment; it is not a reason to redesign the current local-first release.

## Roadmap ordering

A. Installation Security & Reproducibility Hardening - credential rotation/removal, ACL hardening, deterministic line endings, canonical Windows verification script, CI, secret/dependency scans.

B. MAR Performance Baseline - stage latency + cognition/tool/context metrics.

C. Cognition Efficiency - only evidence-backed changes: deterministic local actions, stronger structured receipts, and read-only batch parallelism if benchmarked beneficial.

D. Quality & Adversarial Gate - deterministic patch-quality checks, prompt-injection effect corpus, disconnect/stale/duplicate/two-client acceptance coverage.

E. Context Optimization - adaptive projection/cache/retrieval only when B-D data demonstrates a real bottleneck or coding-quality gap.

F. Larger architecture changes - embeddings/rerankers, SCIP/LSP, persistent index, OAuth gateway, multi-user auth, distributed workers - only when measured evidence requires them.

## Explicit non-goals

- No second orchestrator, BrainAdapter, transcript authority or agent framework.
- No new durable state merely to represent planning/execution/repair/review labels.
- No unbounded browser-memory dependence.
- No AI reviewer with authority to override deterministic verification/integration invariants.
- No optimization justified only by modernity or framework fashion.

## Source anchors reviewed

- `docs/implementation/018_WEB_BRAIN_V1.md`
- `internal/contextengine/projection.go`
- `internal/agent/loop.go`
- Exact runtime/source baseline remains `v1.2.0-requalification.7` at `73d75558946ac2570d3d1319d1461b2126ce9049`.

## Next decision gate

Finish Slice A (Installation Security & Reproducibility) first. Then collect the Performance Baseline before authorizing cognition-efficiency implementation. Do not pre-approve batching/cache/adaptive-context code without measured evidence.
