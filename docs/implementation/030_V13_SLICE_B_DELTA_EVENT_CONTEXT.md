# MAR V1.3 Slice B  Delta/Event-Oriented Cognition Context

Date: 2026-09-19
Status: IMPLEMENTED CANDIDATE  FOCUSED GATES PASS
Base revision: ba0b2dc9972984e9c9ce23b4ea164e60c02507dc

## Purpose

Reduce redundant outward cognition context while preserving MAR durable task, WebTurn, DecisionProjection, verification, integration, recovery, and SQLite authority.

## Recovery provenance

The first Slice B task stopped correctly on the task-wide active-execution guard. Durable reconstruction measured 1,983.308 seconds against the 1,800 second limit. That guard was not bypassed.

Recovery input was frozen at D:\MAR\.mar\recovery-input\mar-v13-slice-b-recovery.json with SHA256 533d60c94ac64767664568870170308e0d87965f07af67d6e7e538e31828c112 and reapplied onto the same clean base.

Finalization source bundle SHA256: c9d6f31061b90620dc2ca9bbf0e8e1ac586e961c732c3b23cf0d97197f0c8172. Subsequent recovery work remained subject to the same task-wide budget discipline. Exhausted tasks are provenance only; final authority comes from the finalization task and MAR verification.

## Design

- The complete pending WebTurn request remains persisted and authoritative.
- DecisionProjection remains the rebuildable bounded current-state snapshot.
- brain_turn default full compat/structured behavior remains unchanged.
- Delta is opt-in through context_mode=delta and structured response mode.
- cognition_cursor is an opaque non-authoritative handle to a prior persisted WebTurn identity.
- MAR validates prior task, turn, attempt, run epoch, request hash, integrity, completion, and ordering before using a cursor base.
- Missing, malformed, stale, cross-task, cross-attempt, or cross-epoch cursor falls back to the full current turn.
- Delta is derived from MAR-owned current/prior projection sections plus current protocol tail.
- If delta is not smaller than the full request, MAR returns full instead.

Material cognition event classes are bounded to DECISION_REQUIRED, AUTHORITY_REQUIRED, RECOVERABLE_FAILURE_NEEDS_REASONING, CANDIDATE_READY, and TERMINAL. No event database, event-sourcing authority, background wakeup service, provider-specific state, or second scheduler was introduced.

## Changed areas

- internal/service/cognition_delta.go
- internal/service/cognition_delta_test.go
- internal/mcpedge/server.go
- internal/mcpedge/server_test.go

The six canonical public MCP tool names are unchanged. No database schema, verification contract, integration contract, workspace lifecycle, recovery system, or retention system changed.

## Measurement

Deterministic representative fixture:

- full request: 13,817 bytes
- derived delta: 1,057 bytes
- delta/full ratio: 0.0765
- outward projection reduction: approximately 92.35%

The code keeps delta only when it is smaller than full, providing an in-code complexity/benefit stop rule.

## Focused validation

- go fmt ./internal/service ./internal/mcpedge  PASS
- go test -v ./internal/service ./internal/mcpedge -run TestCognition|TestBrainTurn -count=1  PASS

Observed passing tests: TestCognitionDeltaRecoveryFromCurrentState; TestCognitionDeltaReducesProjectionCost; TestCognitionEventKindsAreBounded; TestBrainTurnStructuredModePreservesStructuredPayloadAndCutsDuplicateText; TestBrainTurnDeltaModeIsOptInAndPreservesDefaultPayload.

## Complexity

Slice B adds one derived service view plus additive optional arguments on the existing brain_turn tool. It adds no second durable source, database, event log, planner, multi-agent fabric, semantic memory, provider-specific kernel branch, or public tool.

## Verdict

Slice B verdict: PASS for bounded implementation and focused acceptance.

This is not a release-qualified claim by itself. The exact candidate must still pass authoritative MAR go-standard verification and serialized integration.