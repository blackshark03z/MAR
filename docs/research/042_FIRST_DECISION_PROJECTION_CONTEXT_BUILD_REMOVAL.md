# First DecisionProjection Context Build Removal

Date: 2026-09-22
Status: VERIFIED CANDIDATE
Baseline: 8fd01dbc9f90f8b08d49eba6f0488ac66ef8c749
Evidence basis: R-021 and C-07

## Goal

Remove the structurally duplicated repository-context build that occurred before DecisionProjection turn 1, without caching worktree state or weakening freshness checks.

## Previous path

```text
pre-loop Engine.Build()
turn 1 -> DecisionProjectionState -> fresh Engine.Build()
turn 2 -> DecisionProjectionState -> fresh Engine.Build()
...
```

The pre-loop pack was discarded before the first production model request in DecisionProjection mode.

R-021 previously measured a repeated warm `Engine.Build()` at roughly 0.62-0.69s on its accepted-source fixture, with five Git subprocesses per snapshot. That historical measurement is scale evidence, not a new latency claim for this commit.

## New path

```text
DecisionProjection mode:
turn 1 -> DecisionProjectionState -> fresh Engine.Build()
turn 2 -> DecisionProjectionState -> fresh Engine.Build()
...

legacy/non-projection mode:
pre-loop Engine.Build() -> initial task message
```

Important safety properties:

- no revision-only or worktree cache was introduced;
- the first DecisionProjection context is still built after current durable DecisionProjection state is read;
- the pack revision is still checked against `state.CurrentRevision`;
- the Goal hash is still checked against the immutable Goal Contract;
- stale revision mismatch blocks before any model request;
- every later DecisionProjection turn still performs a fresh repository context build;
- legacy/non-projection behavior retains its original initial context build.

## Verification

Focused regression:

- `TestDecisionProjectionBuildsRepositoryContextOnceBeforeFirstTurn`: PASS;
- `TestDecisionProjectionStaleRevisionStillFailsClosedBeforeModelTurn`: PASS;
- `TestLegacyLoopRetainsInitialContextBuild`: PASS;
- `TestLoopDecisionProjectionRetainsOnlyImmediateProtocolTail`: PASS.

Real Web E2E:

- `TestRuntimeE2EWebBrainMCPWorkerVerifyIntegrate`: PASS in ~36.92s;
- worker -> WebTurn -> bounded mutation -> completed candidate -> verification -> integration remains intact.

Package regression:

- `go test ./internal/agent ./internal/worker`: PASS.

Full repository:

- `go test ./...`: PASS;
- wall time ~95.2s;
- `cmd/mar` ~11.0s;
- `internal/orchestrator` ~89.0s.

## Architectural consequence

DecisionProjection remains the bounded, revision/state-bound cognition snapshot. This change removes repeat computation around it rather than removing or weakening it.

The expected benefit is one fewer full context build on every DecisionProjection execution episode before turn 1. Based only on the older R-021 fixture, that is plausibly a moderate sub-second saving per episode; this commit does not claim a new production latency measurement.
