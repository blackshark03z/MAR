# Slice 050 — ChatCode Replacement Discovery Surface

**Date:** 2026-09-23  
**Status:** IMPLEMENTED CANDIDATE

## Goal

Remove the main pre-submit usability gap between direct MAR and ChatCode without reopening MAR's kernel or exposing low-level mutation tools to Web clients.

## Change

The existing canonical MCP `project` tool remains one tool and gains two bounded read-only capabilities:

- `operation=read` accepts optional `start_line` / `end_line`;
- `operation=search` performs bounded UTF-8 text search under one registered project/subtree.

The canonical public tool count remains six.

## Search bounds

Project search:

- requires an explicit registered `project_id`;
- reuses project path confinement / real-path resolution;
- does not follow symlink entries;
- skips `.git` and `.mar`;
- skips non-regular, binary/invalid UTF-8, and files larger than 1 MiB;
- caps one request at 5,000 scanned files, 64 MiB scanned bytes, and 200 matches;
- bounds returned line text;
- returns truncation/scanned counters.

## Non-goals

This slice does not add:

- a seventh public MCP tool;
- public write/shell/Git-mutation primitives;
- durable state;
- a second task/workspace lifecycle;
- planner/model/session/provider logic;
- semantic memory or project-brain dependencies.

Mutation still flows through `submit -> brain_turn/brain_respond -> isolated worker ACI -> verification -> publication`.

## Acceptance

- canonical MCP discovery remains exactly six tools and under the existing schema budget;
- ranged reads preserve normal full-read compatibility;
- bounded search returns project-relative line matches;
- traversal is rejected;
- Git metadata is not searched;
- `internal/service` and `internal/mcpedge` regressions pass;
- full repository qualification is required before integration/release claims.

## Verification evidence

Current working-tree verification after the Slice 050 implementation and the REUSE BEFORE DELETE architecture wording update:

- `go test -count=1 ./internal/service ./internal/mcpedge` — PASS;
- `go vet ./internal/service ./internal/mcpedge` — PASS;
- `go vet ./...` — PASS;
- `go build ./cmd/mar` — PASS;
- `git diff --check` — PASS;
- all 23 Go packages were exercised with `go test -count=1 -timeout 300s` in two exhaustive disjoint package groups — PASS.

The first single-command full-suite attempt was terminated by ChatCode's raw-command `watchdog_no_semantic_progress:300s`; historical MAR evidence already records a sequential full-suite duration around 274 seconds. The exhaustive split is a transport/watchdog workaround, not reduced test coverage. This is also a concrete replacement lesson: MAR should not classify a healthy long-running verification command as stalled solely because it emits no semantic progress event while its process heartbeat remains healthy.

These checks establish candidate-level regression evidence for this slice; they do not by themselves claim runtime activation or release qualification.
