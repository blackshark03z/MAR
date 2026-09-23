# Slice 053 — Project Read SHA-256 Revision

**Date:** 2026-09-23  
**Status:** IMPLEMENTED CANDIDATE

## Goal

Close a practical Trusted Owner Fast Path parity gap by returning the exact full-file SHA-256 revision with bounded project reads.

Before this slice, `project.read` / `project.read_many` returned bounded UTF-8 content but not the file revision required by hash-guarded `action.patch`, `action.write`, `action.remove`, and `action.rename`. A normal edit therefore needed an extra host command such as PowerShell `Get-FileHash`. Because trusted-owner host execution is intentionally not network-sandboxed, that workaround also required `network_allowed=true` for a deterministic local hash.

MAR's internal ACI `read_file` already returns a SHA-256 revision. This slice reuses the same proven idea at the public project discovery surface.

## Change

`ProjectReadResult` now includes:

- `sha256`: lowercase hex SHA-256 of the complete file payload read under the existing bounded project-read policy.

The hash is computed from the same payload already read for content, so there is no second filesystem read.

For ranged `project.read` and ranged entries in `read_many`:

- `content` remains the requested bounded line range;
- `sha256` remains the revision of the complete original file.

That makes the returned revision directly usable as `expected_sha256` for a subsequent mutation.

## Compatibility

- no new MCP tool;
- no new request argument;
- default read semantics and bounds are unchanged;
- existing consumers that ignore the additive response field remain compatible;
- `read_many` inherits the field automatically from the same `ProjectReadResult`;
- no durable state, cache, indexer, shell, child process, or network authority is added.

## Acceptance

1. full-file `project.read` returns the correct SHA-256 revision;
2. ranged `project.read` preserves the full-file SHA-256 while returning only the selected content range;
3. `project.read_many` returns one full-file SHA-256 per file;
4. traversal, UTF-8, regular-file, and size bounds remain unchanged;
5. focused `internal/service` and `internal/mcpedge` regressions pass;
6. full repository test/vet/build/diff-check passes before integration/release claims.

## Focused evidence

Current candidate evidence:

- managed `gofmt` on changed Go files — PASS;
- `go test -count=1 ./internal/service ./internal/mcpedge` — PASS;
- `git diff --check` — PASS;
- service regression verifies the SHA-256 of the exact payload;
- MCP contraction regression verifies ranged reads preserve the full-file revision and `read_many` carries revisions for every returned file.

## Measured workflow effect

The common safe edit path changes from:

```
project.read
  -> action.run(Get-FileHash)
  -> action.patch
```

to:

```
project.read
  -> action.patch
```

That reduces the edit precondition flow from 3 MCP calls to 2, a 33% reduction in round-trip count for that sequence. Slice 052 measured the outer `action.run` round-trip at roughly 1.92 seconds median on the MAR/ChatWeb setup; this slice removes that entire deterministic hash round-trip and the need to enable host network authority solely for hashing.

The slice does not claim broader end-to-end development speedup beyond this measured workflow reduction.
