# MAR V1.3 Slice C — Compound Deterministic Operations

Date: 2026-09-19
Status: IMPLEMENTED CANDIDATE — FOCUSED GATES PASS
Base revision: 2ce7ccec63996c9872406352428fb075b371143c

## Purpose

Reduce repeated deterministic same-file mutation chatter without weakening MAR's authority, fencing, observation, verification, integration, or recovery semantics.

This is the first telemetry-proven Slice C operation. It is intentionally narrow and is not a generic action-batch DSL or workflow engine.

## Telemetry baseline

The target was selected from durable MAR WebTurn telemetry rather than architecture preference.

Across the sampled 250 recent tool-bearing WebTurns:

- replace_exact appeared 154 times.
- adjacent single-replace_exact turns on the same path appeared 91 times.
- adjacent single-replace_exact turns on different paths appeared 32 times.
- observed same-path single-replace runs were:
  - length 2: 27 runs
  - length 3: 12 runs
  - length 4: 4 runs
  - length 5: 3 runs

This made repeated same-file exact mutation the strongest first candidate for a compound deterministic operation.

## Chosen operation

The new worker-visible coding operation is:

`replace_many_exact`

Execution remains in the existing ACI runtime. The worker package contains only compatibility naming/receipt declarations; it does not add a second execution path.

Semantics:

- exactly one workspace-relative file path;
- one starting `expected_sha256`;
- one ordered replacement list;
- minimum one replacement;
- maximum 16 replacements;
- every replacement requires non-empty search text and positive `expected_count`;
- all replacements are evaluated in memory in order;
- later replacements see the in-memory result of earlier replacements;
- any SHA or exact-count mismatch stops before file mutation;
- updated size is checked against the existing ACI mutation byte limit;
- `atomicWrite` is invoked once only after all replacement preconditions succeed;
- result includes `before_sha256`, `final_sha256`, and bounded per-replacement receipts.

The existing `replace_exact` operation remains available and unchanged.

## Authority and observation boundary

`replace_many_exact` is classified exactly like `replace_exact` under existing `LocalFileWrite` authority.

The agent loop still performs current-attempt/current-run-epoch fencing around mutation-producing tools. Existing observation capture receives the compound tool result through the same ExecuteTool boundary.

No database schema, durable source of truth, scheduler, recovery subsystem, worker process model, provider branch, or public MAR MCP domain-tool surface changed.

## Observation barrier rationale

These exact same-file edits can be combined only because no new external observation is required between them. Each later exact-match precondition is deterministically evaluated against the in-memory result of the prior step.

The observation barrier remains after the final file mutation.

Formatting, test execution, Git inspection, and any external side effect are deliberately not folded into this operation. Their observations may change the next reasoning decision and therefore remain separate actions.

## Failure semantics

The operation is fail-closed:

1. resolve the existing write target through the same path guard as `replace_exact`;
2. read the file once;
3. validate the starting SHA-256;
4. simulate every ordered replacement in memory;
5. fail on the first invalid or mismatched replacement;
6. write once only after all simulation succeeds.

Focused tests verify that starting-hash failure, ambiguous/excess matches, and a missing later match leave the original file bytes unchanged. Oversized batches are rejected without mutation.

## Deterministic measured result

A representative regression performs three independent exact edits against the same original file.

Primitive path:

- 3 `replace_exact` worker tool calls
- each call requires the current file hash produced by the prior mutation

Compound path:

- 1 `replace_many_exact` worker tool call
- one starting SHA-256
- three ordered in-memory replacement preconditions
- one final atomic file write

Measured tool calls: 3 -> 1, a 66.7% reduction for this representative same-file sequence.

The regression verifies that primitive and compound paths produce byte-identical final content.

This result does not claim that every three edits save two cognition turns: external cognition can already issue multiple independent tools in one turn. The measured benefit is removal of repeated mutation tool calls and intermediate hash hand-offs for deterministic same-file sequences, which directly addresses the 91 observed same-path adjacent replace pattern.

## Focused validation

Focused validation on the Slice C candidate:

- `go test -p 1 ./internal/aci -run ReplaceMany|ToolDefinitions -count=1` — PASS
- `go test -p 1 ./internal/agent -run Tool|Authority -count=1` — PASS
- `go test -p 1 ./internal/worker -run ReplaceManyExactCompatibilityContract -count=1` — PASS

Covered behavior includes:

- successful ordered dependent replacements;
- starting SHA mismatch;
- ambiguous/excess exact-match failure;
- missing later replacement failure;
- no partial write on failure;
- explicit 16-operation batch bound;
- compound tool schema exposure and dispatch;
- mutation authority classification;
- backward-compatible `replace_exact` naming;
- byte-identical final output against three primitive calls.

## Complexity delta

Added:

- one small ACI implementation file;
- one focused ACI regression test file;
- additive ACI schema/dispatch entry;
- additive mutation classification in the existing agent authority switch;
- small worker-visible compatibility declarations/tests required by the frozen acceptance contract;
- this evidence document.

Not added:

- no durable schema;
- no second tool runtime;
- no generic transaction layer;
- no background process;
- no planner or multi-agent fabric;
- no workflow/DAG engine;
- no new recovery path;
- no new public MAR MCP domain tool;
- no Slice D behavior.

## Verdict

Slice C verdict: PASS for the bounded implementation candidate and focused acceptance.

This is not yet a release-qualified claim. The exact candidate must still pass authoritative `go-standard` verification and MAR serialized integration before Slice C is COMPLETE.
