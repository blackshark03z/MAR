# Slice 011 — Minimal Context Engine

**Status:** IMPLEMENTED / VERIFIED

**Frozen architecture source:** ADR-009 Layered Context Retrieval and MAR V1 Context Architecture.

## Goal

Implement the minimum bounded context system needed by the autonomous worker without introducing a global vector database or replaying full conversation history.

The implemented retrieval order is:

1. Git/repository metadata;
2. bounded lexical relevance;
3. Go symbol/import analysis;
4. local dependency/same-package expansion;
5. deterministic bounded context pack.

Semantic embeddings remain optional and are not part of Slice 011.

## Implementation

### Repository snapshot

`internal/contextengine/git_windows.go`

`GitRepository` obtains a read-only working-tree snapshot using fixed Git operations executed through the existing Windows Job Object containment primitive:

- exact `HEAD` revision;
- tracked files;
- untracked, non-ignored files;
- modified files;
- staged files.

Git execution is host-side but typed/fixed rather than model-controlled. The adapter:

- supplies a sanitized environment rather than ambient user environment;
- disables system config, interactive credential prompts and optional locking;
- disables fsmonitor, external diff, textconv and submodule traversal for snapshot operations;
- uses NUL-delimited path output;
- enforces a hard command-output bound;
- propagates context cancellation;
- rejects unsafe/control-character paths.

### Context engine

`internal/contextengine/engine.go`

`Engine.Build`:

1. validates the immutable Goal Contract and context root;
2. snapshots Git state;
3. fails closed if the observed repository revision differs from `ExpectedRevision`;
4. binds the context pack to the Goal Contract hash;
5. extracts bounded weighted intent terms from Goal, Acceptance and Boundaries;
6. prioritizes repository metadata/path/status candidates;
7. scans only within configured file-count/file-size/total-byte limits;
8. rejects paths escaping the root, non-regular files, binary/non-UTF-8 data and generated/runtime roots;
9. hashes every included file by SHA-256;
10. applies lexical, symbol and dependency scoring;
11. emits bounded line snippets with explicit reasons and Git status;
12. hard-fits the rendered pack to `MaxPackBytes`.

Intent extraction itself is bounded: oversized input text, excessive unique terms and oversized individual terms cannot make the context pack or ranking state grow without limit.

### Go symbol/dependency analysis

`internal/contextengine/go_analysis.go`

Go files are parsed with the standard Go parser. The engine extracts:

- functions and methods;
- types;
- vars/consts;
- import paths.

Analysis is cached by file-content SHA-256, not by mutable path. The cache has a fixed entry bound and deterministic FIFO eviction.

For the highest-ranked relevant Go files, local module imports and same-package files receive bounded dependency boosts. No whole-repository dependency graph is kept resident.

### Context pack contract

A `Pack` contains:

- observed revision;
- Goal Contract hash;
- bounded intent terms;
- scanned/skipped counts;
- truncation state;
- ranked entries with path, Git status, SHA-256, score, line range, reasons and snippet text;
- exact rendered byte count.

Given the same revision, files, Goal Contract and configuration, ranking/output is deterministic.

Repository source text is **untrusted evidence**, not instruction. Slice 012 must delimit context packs from system/agent instructions and must never interpret code comments or repository text as authority to widen the Goal Contract or worker permissions.

## Security / reliability properties verified

- revision mismatch fails closed;
- path traversal metadata is rejected/ignored before file access;
- symlink/reparse resolution is checked against the registered root before reading;
- control characters in repository paths cannot inject fake context headers;
- invalid UTF-8 / binary content is not placed in model context;
- Git output overflow fails rather than returning a partial inventory;
- context cancellation reaches Git snapshot execution;
- scan file count, scan bytes, individual file bytes, cache entries, terms, snippets, entries and final pack bytes are all bounded;
- source entries are hash-bound;
- no semantic/vector infrastructure is required.

## Acceptance evidence

Targeted context-engine suite passes, including:

- deterministic symbol ranking;
- local Go dependency expansion;
- revision mismatch fail-closed;
- scan and pack budgets;
- modified-file priority when lexical evidence is absent;
- content-hash cache bound;
- oversized-intent hard pack bound;
- unsafe path and binary-text rejection;
- real Git revision/state snapshot;
- ignored-file exclusion;
- leading-space filename identity;
- Git output-bound failure;
- Git cancellation;
- real Git snapshot → context engine → bounded context pack integration.

Repository-wide verification after Slice 011:

- `go test -count=1 -timeout 180s ./...`: PASS when `TEMP/TMP` are placed on drive D;
- `go vet ./...`: PASS;
- Windows `go build ./cmd/mar`: PASS;
- `git diff --check`: PASS.

The first full-suite attempt failed only because the host C: volume had approximately 0.09 GB free and Go-in-LPAC could not write compiler artifacts. Re-running the unchanged tree with `TEMP/TMP=D:\\MAR\\.mar\\runtime\\testtmp` passed the complete repository suite. This is a host storage condition, not a code bypass or a passing result obtained by weakening tests.

## Explicit non-goals

Slice 011 does not implement:

- autonomous model/tool loop;
- semantic checkpoints/resume;
- verification/result contract;
- MCP control surface;
- semantic embeddings or vector database;
- global always-loaded code index;
- transcript replay.

Those remain later frozen bootstrap slices.
