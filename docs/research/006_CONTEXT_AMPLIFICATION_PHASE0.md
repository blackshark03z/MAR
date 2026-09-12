# R-006 — Context Amplification Phase-0

**Status:** `RESEARCH_ONLY`  
**Baseline:** MAR v1.1.0  
**Code changes authorized:** none  
**Date:** 2026-09-12

## Research question

Where do MAR Web Brain request bytes actually come from, how much historical replay remains in the accepted DecisionProjection design, and which context costs are evidence-backed candidates for future optimization without weakening durable decision truth?

This report analyzes durable WebTurn request metadata/structure offline. No prompt/tool payload body was sent to another model, no MAR database row was mutated, and no production source/runtime behavior was changed.

## Dataset

Phase-0 used the 385 durable WebTurns present in the MAR v1.1 research store:

- 295 historical legacy turns created before DecisionProjection became the normal bounded context path;
- 90 DecisionProjection turns from the later architecture transition;
- total serialized request bytes: approximately 33.87 MB.

The dataset spans MAR development checkpoints, so historical behavior must not be attributed automatically to the final accepted V1.1 source.

## Finding 1 — Tool schemas were not the dominant historical byte source

Across all 385 requests:

- `messages`: approximately 32.22 MB / 95.1% of request bytes;
- `tools`: approximately 1.53 MB / 4.5%;
- tool definitions were byte-identical across consecutive turns in the measured episodes.

Therefore the earlier context-amplification problem was primarily message/history payload, not tool-schema size.

## Finding 2 — Legacy full-prefix replay was real, but is not the current DecisionProjection behavior

The historical legacy and DecisionProjection populations differ materially:

| Metric | Legacy | DecisionProjection |
| --- | ---: | ---: |
| Turns | 295 | 90 |
| Request bytes | 28.41 MB | 5.46 MB |
| Median request | 78.7 KiB | 44.7 KiB |
| p95 request | 212.2 KiB | 121.6 KiB |
| Median messages/request | 15 | 6 |
| Max messages/request | 69 | 11 |
| Estimated exact-prefix replay factor | 4.47x | 1.04x |

For legacy requests, approximately 21.06 MB of message bytes were exact prefixes already present in the prior turn. In contrast, DecisionProjection requests showed only about 0.20 MB of exact-prefix carry-over under the same structural comparison.

The source contract agrees with the measurement: projection mode rebuilds each turn from a fresh bounded DecisionProjection and retains only the immediately preceding assistant/tool protocol tail required for tool-call pairing. Regression coverage explicitly rejects older protocol history leaking into the next turn.

**Conclusion:** full append-only transcript replay is a historical defect already substantially closed before V1.1 acceptance. Do not reopen it as if it were the current architecture.

## Finding 3 — Current projection bytes are concentrated in three areas

For 90 measured DecisionProjection turns, median projection payload was approximately 30.9 KiB and p95 approximately 55.5 KiB.

Top-level projection byte contribution:

- `untrusted_repository_context_json`: ~54.8%;
- `recent_protocol_evidence`: ~23.1%;
- `goal_contract`: ~17.8%;
- `latest_checkpoint`: ~3.2%;
- all identity/control/result/artifact metadata combined: small remainder.

Within repository context, `entries` account for ~97.6% of repository bytes.

Repository context was byte-identical to the preceding projection in roughly 64.6% of comparable turn pairs. Goal Contract was identical in 100% of comparable turn pairs; checkpoint was identical in roughly 94.4% where present. These repetitions are measurements, not proof that they can be safely removed: a stateless cognition request may still require stable truth unless an alternative cache/handle/retrieval contract preserves semantics.

## Finding 4 — `truncated=true` does not mean projection byte exhaustion

All 90 measured DecisionProjections had `truncated=true`, but none exceeded 90 KiB and the largest reported projection was about 59.2 KiB.

The observed repository pack was at its item-selection ceiling on every turn:

- repository entries: 12/12 on all measured projection turns;
- repository terms: median/max 32;
- projection recent items: median 2, max 6;
- observation artifacts: usually zero, max 1.

Therefore current `truncated` primarily reflects bounded repository selection/item limits, not a projection hitting the configured 96 KiB context byte ceiling. Future telemetry should distinguish **selection truncation** from **byte-budget exhaustion** instead of exposing one ambiguous flag.

## Finding 5 — Immediate tool evidence is duplicated inside current requests

DecisionProjection includes `recent_protocol_evidence`, while the agent loop also appends the immediately preceding assistant/tool messages as `protocolTail` so model tool-call pairing remains valid.

Offline comparison found:

- 194 recent tool-evidence items;
- 178 (91.8%) exactly matched a tool message in the same request by `tool_call_id + content`;
- exact duplicated tool content: ~468.7 KiB;
- duplicate content represents ~8.6% of all measured DecisionProjection request bytes.

For the V1.1 accepted self-hosting task (`task-b06d9ab9...`), all measured recent tool evidence matched the protocol tail exactly:

- 5 Web turns;
- total request bytes: ~248.4 KiB;
- median request: ~41.8 KiB;
- duplicated recent/tail tool content: ~28.4 KiB, or ~11.4% of the task's request bytes.

This is a concrete optimization candidate because it is same-request duplicate content. It is **not yet a change requirement**: research must prove whether removing/transforming one copy preserves both tool-call protocol validity and DecisionProjection semantics.

## Finding 6 — `read_file` dominates current protocol-tail tool bytes

Within measured DecisionProjection protocol tails:

- `read_file`: ~81.7% of tool-result content bytes;
- `search_text`: ~10.7%;
- `git_diff`: ~3.4%;
- `run_command`: ~3.3%;
- mutation/status/checkpoint tools: small remainder.

This suggests future context research should first test whether large read observations are represented efficiently (bounded excerpts, artifact handles, delta/reference semantics) before optimizing tiny control messages.

## Finding 7 — Episode payload headroom deserves a stable-source benchmark

Historical DecisionProjection epochs had a median observed cumulative request volume of ~433 KiB. Seven of eleven measured epochs exceeded 400 KiB. Three historical development epochs exceeded 512 KiB and later ended `worker-budget-exhausted`.

Those epochs crossed a development transition and may predate/encompass activation of the final task-wide/episode budgets. They do **not** prove that final V1.1 violates its current 512 KiB episode guard.

However, current request sizes imply limited headroom: a roughly 40–60 KiB request repeated across many decisions can consume a 512 KiB episode budget before the nominal decision-count limit becomes the binding constraint.

Required next evidence is a controlled benchmark on the exact stable V1.1 source that records:

- request bytes per decision;
- cumulative episode bytes;
- which budget becomes binding first;
- useful progress/checkpoint achieved per KiB;
- whether any candidate context reduction changes outcome quality.

## Accepted V1.1 case study

The accepted self-hosting release-candidate task is the best bounded sample currently tied to the final product direction:

- terminal state: `COMPLETE`;
- one execution attempt;
- five Web turns;
- task wall envelope: ~7 minutes;
- total observed Web Brain wait: ~2 minutes;
- median Web Brain response wait: ~19.4 seconds; max ~51.1 seconds;
- total request volume: ~248.4 KiB;
- median projection payload: ~29.4 KiB;
- exact recent/tail duplication: ~11.4% of request bytes.

This sample shows MAR overhead cannot be inferred as `task wall time - model wait`; worker execution, verification/integration and other waits remain mixed in the residual.

## Automatic observer addition

An external research collector now produces `D:\MAR-Research\observer\reports\context_latest.json` without modifying MAR. It records at least:

- legacy vs DecisionProjection turn counts/bytes;
- projection median/p95 request size;
- projection median payload size;
- repository/recent/contract byte totals;
- exact recent/tail duplicate bytes/share;
- tool-schema serialized bytes.

The collector reads MAR durable data read-only, performs no model calls and participates in no task authority.

## Research hypotheses to test next

### H1 — Remove same-request evidence duplication

Test a research-only simulated request representation where tool evidence appears once while maintaining model protocol pairing and durable projection semantics. Measure byte savings and whether required decision information remains available. Do not change production code until an end-to-end oracle proves equivalence.

### H2 — Repository context delta/cache/reference

Because repository context is often unchanged across adjacent decisions, test whether a stable reference/hash plus bounded changed context could preserve reasoning quality with fewer bytes. Compare against the full current projection using the same task fixture and acceptance oracle.

### H3 — Read observation representation

Evaluate read-file output sizing, overlap and artifact-handle behavior. Prefer improving the largest measured byte class before optimizing small metadata.

### H4 — Budget efficiency rather than only byte minimization

Use metrics such as:

```text
verified_progress_per_100KiB
useful_tool_result_bytes / request_bytes
cumulative_request_bytes_before_checkpoint
cumulative_request_bytes_before_terminal_result
```

The goal is not the smallest prompt. The goal is the smallest context that preserves or improves successful convergence.

## Phase-1 optimization ladder

The measured byte classes should not be treated as equally removable. Future experiments should proceed from lowest semantic/recovery risk to highest.

### Tier A — Exact same-request duplicate elimination

Measured target: ~468.7 KiB across the 90 DecisionProjection turns, ~8.6% of total request bytes.

This is the strongest first experiment because both copies coexist inside the same request. The oracle must still prove that removing or referencing one copy does not break tool-call pairing or durable evidence interpretation.

### Tier B — Large read-observation representation

`read_file` accounts for ~81.7% of measured protocol-tail tool-result content. Test bounded excerpts, overlap reduction and artifact/handle retrieval before touching stable Goal/authority data.

This can reduce payload while preserving the fact that the model may still request exact source when needed.

### Tier C — Repository-context reuse/reference

Repository context is ~32.7% of total measured DecisionProjection request bytes and is byte-identical across many adjacent turns. That is a large optimization pool, but it is **not** equivalent to proven waste.

A reference/delta design must survive:

- reconnect from a fresh remote session;
- Web chat/context loss;
- worker/attempt replacement;
- source revision change;
- missing/stale cache entry.

Fallback to a full self-contained projection must remain possible.

### Tier D — Goal Contract reuse/reference

Goal Contract bytes are ~10.6% of measured DecisionProjection request volume and were identical across the measured turns. They are also core authority/intent context.

Do not optimize this by relying silently on conversational memory. Any compact representation would need explicit identity/hash semantics and a proven way to recover the full immutable contract when external cognition has lost prior context.

### Tier E — Broader context-protocol redesign

Only after Tiers A–D are benchmarked should research consider larger changes such as differential DecisionProjection, persistent external cognition handles, or different episode semantics. A smaller request is not an improvement if reconnect reliability, correctness, or convergence worsens.

## Context optimization acceptance

For any candidate, compare the same fixture against current V1.1 and require all of:

- same final acceptance outcome;
- no increase in Owner intervention;
- no loss of reconnect/fresh-session recoverability;
- no stale revision/authority confusion;
- lower median and cumulative request bytes;
- no material increase in model turns or wall time;
- no new dependency on hidden client/session state.

A candidate that saves bytes but requires more turns may be a net regression. Track both `bytes/Goal` and `turns/Goal`.

## Current verdict

`MATERIAL_RESEARCH_SIGNAL`

DecisionProjection has already eliminated the dominant legacy full-history replay pattern. Remaining measured context cost is now concentrated enough to study precisely: repository entries, immediate protocol evidence, Goal Contract, and large read observations. Same-request recent/tail duplication is the clearest low-risk optimization hypothesis, while repository/reference changes require stronger quality evidence.

No production-code change is authorized by this report.
