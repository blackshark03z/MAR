# Slice 055 — Frozen-Schema-Safe Context + Status

**Date:** 2026-09-23  
**Status:** IMPLEMENTED CANDIDATE

## Goal

Preserve the measured one-round-trip context+working-tree optimization when ChatGPT has frozen an older MAR tool input schema.

Slice 052 added optional `include_git_status=true` to `project.context_batch`. The current live conversation proved that a stale ChatGPT tool snapshot rejects that new property before the request reaches MAR.

The correction must not require a new MCP tool, new request field, backend method, cache, or lifecycle.

## Change

The existing canonical `project` tool gains one operation string:

- `operation=context_status`

It reuses request fields already present in the older public schema:

- `project_id`;
- `query`;
- `max_results`;
- `max_entries`;
- `max_bytes`.

Execution is the same bounded composition already implemented by Slice 052:

1. build `ProjectContextBatchResult`;
2. read bounded `ProjectGitStatusResult`;
3. return both in one response.

The existing `context_batch + include_git_status=true` path remains supported for refreshed clients.

## Why an operation alias

The public `operation` field is already a string in the frozen schema. Therefore a stale client can send a new operation value without needing a new input property.

This makes the optimization usable in the current conversation after runtime activation even if ChatGPT has not refreshed the tool schema.

## Safety and compatibility

- no new MCP tool;
- no new request field;
- no service/backend change;
- no new authority;
- `context_batch` default output remains unchanged;
- non-Git/research-only callers can continue using `context_batch`;
- `context_status` is explicitly Git-oriented and may fail under the same conditions as standalone `git_status`.

## Acceptance

1. `context_status` returns both `context_batch` and `git_status`.
2. Existing `context_batch` behavior remains unchanged.
3. Existing `include_git_status=true` compatibility remains intact.
4. focused `internal/mcpedge` regression passes.
5. full repository test/vet/build/diff-check passes before release claim.
6. after activation, the current frozen-schema ChatGPT conversation can call `operation=context_status` without reconnecting or adding a new request field.

## Focused evidence

- managed `gofmt` — PASS;
- `go test -count=1 ./internal/mcpedge` — PASS;
- `git diff --check` — PASS;
- `TestCallProjectContextBatch` covers both the Slice-052 flag path and the frozen-schema-safe alias.
