# Slice 054 — Git Stage + Commit Compound Action

**Date:** 2026-09-23  
**Status:** IMPLEMENTED CANDIDATE

## Goal

Reduce outer MCP round trips in the ordinary Trusted Owner Fast Path by composing two already-authorized deterministic Git primitives: stage selected paths, then commit them.

This slice does not add a new Git capability. It only composes existing `git_stage` and `git_commit` behavior inside the existing canonical `action` tool.

## Measured trigger

On the activated MAR runtime, three direct ChatWeb measurements of standalone `action.git_stage` on an unchanged tracked file were:

- 2.587 s;
- 5.587 s;
- 5.597 s.

The three-sample median was approximately **5.59 s**. The local Git operation itself is cheap relative to that outer latency, so eliminating the standalone stage call from a normal stage+commit sequence has materially higher value than micro-optimizing the local Git implementation.

The sample is intentionally small and host-sensitive; it is evidence for round-trip composition, not a general latency SLA.

## Change

The existing `action` tool gains:

- `operation=git_stage_commit`

Inputs reuse fields already present in the public action schema:

- `paths`: 1..128 project-relative paths, with the same validation as `git_stage`;
- `message`: the commit message, with the same validation as `git_commit`.

Execution is strictly:

1. call the existing bounded `StageProjectPaths`;
2. only if staging succeeds, call the existing bounded `CommitProject`;
3. return one `ProjectGitActionResult` with:
   - `operation=git_stage_commit`;
   - committed revision;
   - staged paths;
   - commit output.

## Safety and compatibility

- no new MCP tool;
- no new request field;
- no new service method;
- no new durable state;
- no shell interpretation;
- no new Git authority;
- project `local_git_write` policy remains authoritative through the existing primitives;
- if staging fails, commit is not attempted;
- if commit fails after staging, the index remains staged exactly as with the equivalent two-call sequence;
- push remains a separate explicitly authorized operation.

This is MCP-edge composition only.

## Acceptance

1. `git_stage_commit` stages before committing.
2. It returns the exact commit revision and staged paths in one response.
3. Existing `git_stage` and `git_commit` semantics remain unchanged.
4. Focused `internal/mcpedge` regression passes.
5. Full repository test/vet/build/diff-check passes before integration/release claims.

## Focused evidence

Current candidate evidence:

- managed `gofmt` on changed MCP files — PASS;
- `go test -count=1 ./internal/mcpedge` — PASS;
- `git diff --check` — PASS;
- regression `TestCallActionGitStageCommit` verifies `stage,commit` call order and result composition.

## Expected workflow effect

Normal commit flow changes from:

```
action.git_stage
  -> action.git_commit
```

to:

```
action.git_stage_commit
```

This removes one outer MCP round trip from every ordinary stage+commit sequence while preserving the same bounded Git primitives and policy checks.
