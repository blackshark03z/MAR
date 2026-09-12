# Slice 021 — V1.2 compact live-usage reads

**Status:** `IMPLEMENTED / TARGETED_VERIFIED / BENCHMARKED`

## Goal

Reduce Owner Console `/api/tasks` read amplification for active Web Brain tasks without changing execution authority, durable WebTurn semantics, token accounting, or integrity behavior.

## Change

- Owner live usage now reads a compact WebTurn observation containing turn id, response JSON, timestamps and stored hash metadata.
- The compact query deliberately does **not** select `request_json`.
- Completed response JSON is still canonical-hash checked.
- WebTurn envelope integrity is recomputed from the same durable identity/hash/timestamp metadata.
- Authoritative execution, Web Brain, convergence-budget and verification paths continue using full WebTurn records and their existing integrity checks.
- No schema migration, second cache, materialized summary table, or SQLite architecture change was introduced.

## Acceptance

1. Pending/completed live-usage semantics remain correct.
2. Corrupted response/hash metadata fails closed.
3. Owner task-list live token behavior remains unchanged.
4. The heavy active-task fixture materially reduces read amplification relative to the historical V1.1 path.
5. Targeted tests, vet, and build pass.

## Evidence

- Domain/store WebTurn/WebEpisode/WebTurnUsage targeted tests: PASS.
- `TestOwnerUILiveUsageUsesDurableWebTurnsAndBrainWaitIsNotOwnerAttention`: PASS.
- `go vet ./internal/domain ./internal/store ./cmd/mar`: PASS.
- Build to `.mar/runtime/verify/mar-slice-c.exe`: PASS.
- Existing research fixture: 19 WebTurns, approximately 0.92 MB durable request+response payload.
- Historical pre-change extra read I/O for 10 `/api/tasks` calls: approximately 8.55 MiB.
- Slice 021 extra read I/O for the same fixture/call count: 1.0013 MiB.
- Read-amplification reduction: approximately 88% while response size remained effectively unchanged (~974 bytes extra for the active row).
- Benchmark emitted zero sandbox-check subprocesses, preserving Slice 020 observer-effect correction.

## Stop rule

This slice ends with the compact read path. Do not add task-summary tables, background aggregators, SQLite pool changes, or new telemetry infrastructure unless later evidence proves this bounded query insufficient.
