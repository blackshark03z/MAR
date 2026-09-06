# Slice 017 — Project Brain V1

## Status

`IMPLEMENTED — FROZEN FOR V1 RELEASE ACCEPTANCE`

This slice improves MAR's initial repository context selection for GPT-5.6 Sol Web and provider-backed workers. It does not change task authority, sandboxing, scheduling, verification, or integration semantics.

## Problem

The Slice 016 context engine was safe and bounded, but ranking was dominated by raw substring frequency and Go-only structural analysis. A fixed cross-language localization corpus measured:

- MRR: `0.625`
- Recall@3: `0.833`

Observed failures:

- repetitive documentation could outrank the actual Python/TypeScript symbol definition;
- Python dependency-ripple context could be omitted entirely;
- structural retrieval outside Go was lexical-only.

## Research decision

MAR V1 uses a deterministic search-based Project Brain rather than a local embedding/LLM stack.

Selected evidence and standards:

- Tree-sitter is a strong general syntax/tagging substrate and is explicitly designed for fast, robust parsing and code-navigation queries: https://tree-sitter.github.io/ and https://tree-sitter.github.io/tree-sitter/4-code-navigation.html
- Aider's repository map demonstrates syntax tags + dependency graph + Personalized PageRank under a strict token budget: https://aider.chat/docs/repomap.html
- Sourcegraph distinguishes lightweight search-based code navigation from compiler-accurate SCIP indexing; SCIP is valuable but requires language-specific semantic indexers/toolchains: https://sourcegraph.com/docs/code-navigation and https://sourcegraph.com/docs/code-navigation/writing-an-indexer
- BM25/BM25F remain strong deterministic information-retrieval baselines for structured fields: Robertson & Zaragoza, `The Probabilistic Relevance Framework: BM25 and Beyond`.
- Reciprocal Rank Fusion combines heterogeneous rankings without requiring score calibration; MAR uses the conventional `k=60`: Cormack, Clarke & Buettcher, SIGIR 2009; current Elasticsearch RRF documentation also defaults to 60.
- Agent Retrieval Bench (2026) evaluates coding-agent retrieval with MRR, Recall@k and budgeted context yield and reports that no single retrieval family dominates; RepoMap is especially strong under an 8K context budget: https://agent-retrieval-bench.github.io/

### Rejected for V1

1. **Embedding/neural reranker** — can improve some retrieval metrics, but adds model weights/provider cost, latency, cache lifecycle and calibration. It is not required to pass the V1 localization gate.
2. **SCIP/LSP semantic indexing** — more precise but requires per-language compiler/indexer setup and is disproportionate for the local personal-use V1 runtime.
3. **Official Go Tree-sitter binding** — introduces C/CGo/toolchain coupling on a MAR host intentionally kept portable. Pure-Go/WASM alternatives observed during the review are newer and not mature enough to become a V1 release dependency.
4. **Persistent second Project-Brain database** — the agent builds the initial context once per execution attempt, so an additional durable indexing subsystem is not justified for V1.

These are post-V1 options only if real benchmark/owner evidence demonstrates a retrieval deficiency.

## Frozen V1 retrieval algorithm

### 1. Query intent

Goal Contract text is weighted deterministically:

- goal: weight `5`
- acceptance: weight `3`
- boundaries: weight `1`

Identifiers are expanded into full and component forms (`SessionManager` -> `sessionmanager`, `session`, `manager`) with bounded term count and size.

### 2. Lightweight syntax index

Per-file analysis remains content-hash cached and reconstructable.

- Go: standard-library Go parser/AST for declarations and imports.
- Python: bounded deterministic declaration/import scanner.
- JavaScript/TypeScript family: bounded deterministic declaration/local-import scanner.
- Other languages: lexical/path fallback.

No model call is required to construct the index.

### 3. Relevance ranking

MAR computes a BM25F-style relevance score over three fields:

- path weight: `4`
- symbol weight: `6`
- body weight: `1`

Length normalization is field-specific and query-term IDF is computed over the scanned revision snapshot.

### 4. Structural ranking

Local imports form a directed file dependency graph.

- Go local-module imports are resolved to package files.
- Python local/relative imports are resolved to project files.
- JS/TS relative imports are resolved using common source extensions/index files.

Personalized PageRank:

- damping: `0.85`
- iterations: `20`
- personalization seeds: query-relevant files, falling back to modified files.

### 5. Rank fusion

RRF (`k=60`) combines independently ranked signals:

1. BM25F relevance;
2. exact symbol matches;
3. exact path matches;
4. Personalized PageRank;
5. modified-file fallback.

This avoids calibrating incompatible score scales and preserves deterministic tie-breaking by path.

### 6. Context/evidence bounds

Existing hard limits remain authoritative:

- bounded scanned files/bytes;
- bounded file/snippet size;
- bounded entry count;
- bounded rendered context pack;
- bounded diagnostic reasons;
- revision + Goal Contract hash identity;
- content SHA-256 per entry;
- binary/out-of-root content rejected.

## Frozen benchmark gate

`TestContextRetrievalBenchmarkV1` is the V1 localization corpus and stop condition. It covers:

- Python symbol vs repetitive documentation noise;
- TypeScript symbol + dependency ripple vs documentation noise;
- Python dependency ripple;
- Go dependency ripple.

Accepted V1 thresholds:

- MRR `>= 0.950`
- Recall@3 `= 1.000`
- all existing context safety/budget tests PASS.

Current result:

- MRR: `1.000`
- Recall@3: `1.000`
- `go test ./internal/contextengine`: PASS

## Stop rule

**No further retrieval algorithm, parser framework, embedding model, vector store, semantic indexer, reranker or Project Brain subsystem is added before MAR V1 owner acceptance.**

Reopen this decision only when one of the following produces concrete evidence:

1. the frozen benchmark gate regresses;
2. owner real-use fails because required repository context is not surfaced;
3. a post-V1 measured corpus shows a material Recall@k / MRR / budgeted-context deficit.

Otherwise the Project Brain is considered complete for V1.
