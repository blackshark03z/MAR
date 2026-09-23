# Slice 052 — Context Batch Working-Tree Status

**Date:** 2026-09-23  
**Status:** IMPLEMENTED CANDIDATE

## Goal

Reduce measured ChatWeb/MCP round trips in the common discovery flow without adding a new tool, cache, durable state, or project-analysis subsystem.

## Measured trigger

On the activated MAR runtime, three-sample outer round-trip medians observed from ChatWeb were approximately:

- `project.context`: 1.80 s;
- `project.git_status`: 2.05 s;
- bounded `project.read`: 2.10 s;
- `action.run git rev-parse HEAD`: 1.92 s;
- `project.context_batch`: 2.64 s.

The local deterministic work is much cheaper than these outer timings, so the current productivity bottleneck is round-trip count rather than local Git/filesystem computation. A normal context+working-tree inspection therefore pays roughly one unnecessary extra ~2 s MCP round trip.

## Change

The existing `project.context_batch` operation gains optional `include_git_status=true`.

When requested, the same MCP response contains:

- the existing bounded context batch; and
- the existing bounded `ProjectGitStatusResult`.

No service/indexer/cache semantics change. The implementation simply composes two already-authorized read-only backend operations inside the existing canonical `project` tool.

## Compatibility

- default behavior is unchanged;
- callers that omit `include_git_status` receive the existing response shape;
- the canonical public tool count does not change;
- no additional project authority is introduced.

## Acceptance

1. `context_batch` without the flag remains backward compatible.
2. `include_git_status=true` returns both `context_batch` and `git_status`.
3. Git status retains the same bounded read-only semantics as the standalone operation.
4. focused MCP regressions pass.
5. full repository test/vet/build/diff-check passes before integration/release claims.

## Expected effect

The target benefit is one fewer outer MCP round trip in the common context+working-tree discovery flow. This slice does not claim a local compute speedup and does not justify new caching or transport architecture.
