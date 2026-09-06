# Slice 015 — MCP Control Surface

> Historical Slice 015 acceptance records the original seven-tool control surface. Slice 018 extends the V1 surface with exactly two bounded Web-brain relay tools (`brain_turn`, `brain_respond`) while preserving the same rule that MCP exposes no low-level coding/mutation primitives.

**Status:** IMPLEMENTED / VERIFIED

**Frozen architecture source:** MAR V1 Product Spec sections 4, 6, 8 and 11; Architecture V1 MCP Edge; Security & Trust Model Outer MCP Policy; ADR-002 and ADR-012.

## Goal

Expose the small ChatWeb-facing MAR control plane without moving task truth, scheduling, coding primitives, or the inner model↔tool loop into MCP transport state.

Slice 015 keeps SQLite-backed MAR services as the coordination truth and maps them through exactly seven product-level MCP operations:

- `submit`
- `status`
- `steer`
- `input`
- `cancel`
- `result`
- `inspect`

No public `read_file`, `write_file`, `run_shell`, `run_command`, scheduler primitive, or unrestricted machine mutation surface is exposed.

## Official MCP transport

`internal/mcpedge/server.go`
`cmd/mar/main.go`

MAR uses the official `github.com/modelcontextprotocol/go-sdk` package and exposes a stdio server via:

`mar mcp-stdio -db <path>`

The MCP edge owns only protocol validation/projection and translation into existing service calls. It does not own long-running task truth, scheduling, worker execution, or integration authority.

The tool handlers use typed request arguments and bounded high-level response projections. Application failures remain MCP tool errors rather than crashing the protocol session.

## Durable control stream

`internal/domain/control.go`
`internal/store/control.go`
`internal/store/sqlite.go`

Schema version 8 adds `task_controls` with:

- immutable control id;
- task id;
- monotonically increasing task-local version;
- task-local idempotency key;
- control kind;
- bounded JSON payload;
- SHA-256 integrity digest;
- creation timestamp;
- unique `(task_id, version)` and `(task_id, idempotency_key)` constraints.

Control publication runs through the existing single-writer SQLite coordination path. Multiple MCP clients therefore append to one durable task control stream rather than creating transport-local coordination truth.

A replay of the same idempotency key + same content returns the original durable control. Reusing the key for different content is rejected.

## Steering contract

`TaskService.Steer` permits only frozen high-level steering kinds:

- factual context;
- priority clarification;
- blocked alternative selection;
- additional verification request;
- cancellation request.

Steering never mutates the immutable Goal Contract.

`steer(kind=cancel)` delegates to the same authoritative cancellation path as the dedicated `cancel` operation; there is no second cancellation mechanism.

Material goal/acceptance/boundary changes are not represented as steering mutations and therefore cannot silently rewrite task intent.

## Input contract

`TaskService.Input` accepts bounded input only while the durable task is exactly `INPUT_REQUIRED` and the matching current execution attempt remains ACTIVE.

Control insertion and `INPUT_REQUIRED -> RUNNING` happen atomically in one SQLite transaction. A stale attempt cannot be resumed by late user input.

The input payload does not alter Goal Contract authority or project scope.

## Cancellation contract

`TaskService.Cancel` records the cancellation control durably before changing execution authority.

For pre-attempt states, cancellation may finalize immediately because no mutation-capable process exists.

For an active attempt:

1. cancellation intent is durably inserted;
2. the current ACTIVE attempt is logically fenced in the same transaction;
3. the task is **not** falsely marked `CANCELLED` while physical mutation authority may still exist;
4. physical process termination remains owned by the existing supervisor/process-containment path;
5. `FinalizeCancellation` refuses finalization until every task attempt is confirmed `PHYSICALLY_TERMINATED`.

This preserves the frozen distinction between logical fencing and physical write revocation.

## Status / result / inspect projections

`status` returns bounded durable task truth plus the latest durable control and whether cancellation has been requested.

`result` returns the latest Slice 014 `TaskResult`, preserving its revision-bound verification/evidence identity and integrity checks.

`inspect` returns a bounded durable projection containing, when present:

- task;
- workspace;
- latest attempt;
- latest valid semantic checkpoint;
- latest task result and verification evidence;
- at most 32 recent durable control records.

None of these operations reconstruct authority from ChatWeb history.

## Multi-client behavior

Multiple MCP sessions/processes may point at the same MAR database. SQLite remains the only coordination truth.

Concurrent unique steering requests are serialized into one monotonic task-local control sequence. There is no in-memory MCP coordinator and no second writer truth.

## Acceptance evidence

Focused tests PASS for:

- exact seven-tool public MCP surface;
- absence of low-level worker primitives from MCP;
- official MCP in-memory client/server interoperability;
- typed steering argument mapping;
- application errors remain tool-visible protocol results;
- durable/idempotent steering;
- Goal Contract immutability under steering;
- `steer(kind=cancel)` uses the authoritative cancellation path;
- `INPUT_REQUIRED` gating and atomic resume;
- running cancellation logically fences before final cancellation;
- final cancellation blocked until physical termination proof;
- safe pre-attempt cancellation;
- concurrent clients append one monotonic durable control stream;
- control persistence across SQLite close/reopen;
- control integrity tamper rejection.

Repository-wide verification after Slice 015:

- `go test -count=1 -timeout 180s ./...`: PASS with Go 1.27.0 portable and `TEMP/TMP=D:\MAR\.mar\runtime\testtmp`;
- `go vet ./...`: PASS;
- Windows `go build ./cmd/mar`: PASS (artifact emitted under `.mar/runtime`);
- `git diff --check`: PASS.

The known host race-detector limitation remains unchanged; no race PASS is claimed.

## Explicit non-goals

Slice 015 does not claim:

- that MCP is the coding inner loop;
- unrestricted filesystem/shell access for ChatWeb;
- a transport-owned scheduler or task database;
- a second cancellation/process supervisor;
- network/public multi-user authentication or distributed infrastructure;
- final end-to-end self-hosting acceptance;
- `SELF_HOSTING_READY`.

Slice 016 owns the end-to-end self-hosting acceptance path, including proving that the already durable control stream is consumed correctly by the autonomous runtime under real replacement/cancellation/recovery scenarios.
