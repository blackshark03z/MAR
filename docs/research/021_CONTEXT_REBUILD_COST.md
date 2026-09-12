# R-021 — Decision context rebuild cost

**Status:** `RESEARCH_ONLY`  
**Date:** 2026-09-12  
**Verdict:** `P0_REPEAT_COMPUTE_HYPOTHESIS`

## Research question

MAR rebuilds repository context to keep each Web decision fresh and self-contained. How much latency, process churn and disk/CPU work comes from rebuilding context when the relevant workspace state has not materially changed?

This document does not authorize caching stale context, weakening repository freshness, removing Git status checks or skipping post-mutation rebuilds.

## Current build path

`agent.Loop.Run()` performs an initial context build before entering the model-turn loop.

When DecisionProjection mode is enabled, every model turn then performs another fresh context build after loading authoritative DecisionProjection state:

```text
initial Engine.Build()

turn 1 -> DecisionProjectionState -> Engine.Build()
turn 2 -> DecisionProjectionState -> Engine.Build()
...
```

The fresh pack is identity-checked against the authoritative current revision and Goal hash before it can enter the DecisionProjection.

## Immediate duplicate at the first DecisionProjection turn

In projection mode, the initial pre-loop context pack is constructed and validated, but the first actual model request replaces the old initial message set with a newly built DecisionProjection and calls `Engine.Build()` again.

No coding tool has run between those two context builds.

Therefore the current source contains a strong structural hypothesis of duplicated context computation before the first Web request.

Whether the first pack can be safely reused depends on all identity/freshness conditions remaining identical. This must be proven by test, not assumed.

## Git snapshot cost per context build

`GitRepository.Snapshot()` currently launches bounded contained Git commands for:

1. `git rev-parse --verify HEAD`;
2. tracked files via `git ls-files --cached`;
3. untracked files via `git ls-files --others --exclude-standard`;
4. modified files via `git diff --name-only`;
5. staged files via `git diff --cached --name-only`.

Each operation is a separate contained process invocation.

Therefore one context build has a baseline of roughly **five Git subprocess invocations** before source-file scanning/ranking.

For a five-turn DecisionProjection task, the current structural path can perform six context builds (initial + one per turn), or roughly **30 Git snapshot subprocesses**, subject to early termination/error paths.

This is a source-derived count, not a measured latency total.

## File scan/hash cost per build

Default context-engine bounds allow one build to scan up to:

- 2,000 files;
- 8 MiB source bytes;
- 512 KiB per file;
- 12 selected context entries;
- 64 KiB final pack;
- 32 search terms.

For candidate files the engine:

- reads file content;
- SHA-256 hashes the bytes;
- performs lexical scoring;
- may perform Go syntax/symbol analysis;
- ranks candidates and extracts snippets.

The analysis cache is keyed by file-content hash and can avoid recomputing parsed Go analysis for an unchanged hash. However the engine still needs to read/hash the source in the current design to discover that hash.

Thus cache hits do not eliminate file-read/hash/snapshot work.

## Existing phase-0 evidence

R-006 found that repository context was byte-identical to the preceding projection in roughly **64.6%** of comparable DecisionProjection turn pairs.

That does not prove the rebuild was unnecessary: repository/worktree state could change while the selected pack happens to remain identical.

It does show that repeated computation often produced identical output, making build-time measurement worthwhile.

## Why revision-only caching is unsafe

MAR context includes working-tree state, not only committed HEAD.

The same Git revision can have different:

- modified files;
- staged files;
- untracked files;
- source bytes after coding-tool mutation or a permitted `run_command`.

Therefore `currentRevision` alone is not a sufficient cache key.

A future reuse design would need a trustworthy workspace-generation/freshness identity that detects every mutation-capable path, including command-driven changes.

## Benchmark matrix

### C0 — first-turn duplicate

Measure initial Build and turn-1 Build separately on the same fixture.

Record:

- Git subprocess count;
- snapshot duration;
- scan/read/hash duration;
- total Build duration;
- output pack hash/bytes.

Expected research question: do the two packs have identical source/freshness identity, and what is the cost of producing the second one?

### C1 — read-only turn

Run a turn whose only worker action is `read_file` or `search_text`, then measure the next context build.

Compare current rebuild with a research-only reuse simulation. No production reuse unless workspace immutability between observations is proven.

### C2 — mutation turn

Run `replace_exact`, `write_file`, or a mutation-capable command. Confirm the next context build observes changed workspace truth. This is the negative control: any cache/reuse policy must invalidate here.

### C3 — large repository

Use a disposable repository near the scan-file/byte limits. Measure context-build share of total turn latency.

### C4 — repeated unchanged pack

For runs where adjacent output pack hashes are equal, quantify how much time/process work was spent reconstructing identical output.

## Metrics

Track per context build:

```text
context_build_ms
snapshot_ms
snapshot_git_processes
files_considered
files_read
source_bytes_read
source_bytes_hashed
analysis_cache_hits
analysis_cache_misses
pack_bytes
pack_hash
workspace_generation_or_fingerprint
triggering_previous_tool_class
```

At task level:

```text
context_build_count
context_build_total_ms
context_build_share_of_goal_pct
identical_adjacent_pack_count
first_turn_duplicate_cost_ms
```

## Candidate experiments, not requirements

### E1 — Reuse initial pack for DecisionProjection turn 1

This is the narrowest hypothesis. If no authority/workspace state can mutate between initial pack validation and first projection construction, reuse could remove one full snapshot/scan.

The oracle must prove the resulting projection is byte/semantically equivalent and stale-state rejection is unchanged.

### E2 — Reuse after provably read-only worker turns

MAR's typed tools distinguish read-only operations from explicit mutation tools. Research whether a context pack can remain valid after a strictly read-only turn.

Caution: a `run_command` may mutate the workspace even when the model's intent appears read-like. Classification must follow authority/effect semantics, not natural-language intent.

### E3 — Explicit workspace mutation generation

Higher-risk hypothesis: increment a durable/authoritative workspace generation whenever any mutation-capable effect can change context-relevant state, then bind context packs to that generation.

This could permit safe reuse without rescanning after truly read-only turns, but generation correctness must cover commands, Git effects, recovery and external-authority boundaries.

### E4 — Snapshot batching

Research whether the five Git observations can be obtained with fewer contained process launches while preserving:

- HEAD identity;
- tracked/untracked distinction;
- modified/staged truth;
- helper/hook/credential hardening;
- bounded output.

Do not trade fewer processes for weaker Git safety.

### E5 — Incremental file analysis cache

If file-read/hash cost is material after snapshot optimization, research a cache keyed by a trustworthy file/workspace identity. OS metadata alone may be insufficient under adversarial/stale conditions; correctness must win over cache hit rate.

## Safety invariants

Any future optimization must preserve:

- current revision/worktree truth at every material model decision;
- changed/staged/untracked file visibility;
- Goal hash/revision binding;
- fail-closed revision mismatch;
- context update after all mutation-capable operations;
- restart/recovery without depending on stale in-memory cache;
- no external/hidden modification silently bypassing freshness checks.

## Accepted-source source-level benchmark — 2026-09-12

A research-only Go overlay benchmark executed `Engine.Build()` eight times against the current MAR production source tree while production diff from V1.1 was empty and host pressure was within the research gate (`commit 75.3%`, CPU median 16.3%, RAM load 54%). The same `Engine` instance was reused so builds 2–8 reflect a warm analysis cache, while repository snapshot/file read/hash work still executes normally.

Measured results:

```text
builds                              8
files scanned/build               273
pack bytes/build                13,367
pack hashes identical              8/8
Git subprocesses/snapshot            5
Git subprocesses total              40

first Build()                    783.7 ms
warm Build() median             620.2 ms
first Git snapshot              399.0 ms
warm Git snapshot median        302.7 ms
```

Warm snapshot therefore represents roughly **49%** of median warm Build latency in this fixture; the remaining ~317 ms covers file reads/hashes, ranking/scoring, pack construction and related context work. One run showed a host-noise outlier (`Build 1092 ms`, snapshot 731 ms), so this is not a p95 production baseline.

A second research-only overlay run explicitly froze the `RepositorySnapshot` after the first production `GitRepository.Snapshot()` and repeated `Engine.Build()` against that fixed snapshot. Its medians were approximately:

```text
production Snapshot() median          294 ms
full repeated Build() median          691 ms
Build() with frozen snapshot median   369 ms
files scanned/build                    273
pack bytes/build                    16,283
entries/build                           12
```

This independently corroborates the earlier warm-build result and splits the cost more directly: roughly 40–45% is attributable to repository snapshot/Git observation in this fixture, while another ~50% remains even when snapshot acquisition is removed because source files are still reread, hashed and rescored. Reusing the same `Engine`/analysis cache did not collapse repeated build time toward zero.

The important result is not merely latency: all eight builds reconstructed the **exact same pack hash** while still paying 40 Git subprocess launches and repeated scan/hash work.

### Task-level scale estimate

For the current structural pattern of `initial Build + one Build per DecisionProjection turn`, a five-turn task can pay six builds. Using this measured fixture only as an order-of-magnitude estimate:

```text
first build + 5 warm-median builds
≈ 0.784 s + 5 × 0.620 s
≈ 3.9 s context-build work
```

The immediate initial/turn-1 duplicate is therefore worth roughly **0.6–0.8 s** on this repository/host state if safe reuse can be proven. That is real avoidable work, but it is not currently large enough to explain multi-minute Web Brain waits or long task completion times by itself.

## Phase-0 decision

`REPEAT_COMPUTE_CONFIRMED; CURRENT_COST_MODERATE_NOT_DOMINANT`

Context rebuilding is measurably repetitive and snapshot/process work is a meaningful share of each build. The narrow first-turn reuse hypothesis remains worth testing because it is structurally safe-looking and low scope, but R-021 should **not** outrank Web wait/polling, outer cognition amplification, runtime identity, or transport/tunnel reliability solely on speed impact. Re-measure on larger repositories before promoting broader caching/workspace-generation machinery.
