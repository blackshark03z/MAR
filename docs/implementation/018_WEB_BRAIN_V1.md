# Slice 018 — GPT Web Brain + Multi-Client V1

## Status

`IMPLEMENTED CANDIDATE — HOST SANDBOX PREPARATION + FINAL REGRESSION PENDING`

This slice makes GPT-5.6 Sol Web a first-class MAR coding/reasoning brain without exposing low-level mutation authority through MCP. Provider-backed autonomous execution remains available as an optional unattended mode.

## Product decision

MAR V1 separates cognition from mutation authority:

`GPT-5.6 Sol Web -> typed MCP brain relay -> durable MAR task/attempt -> isolated worker ACI -> verification -> serialized integration`

The Web model decides the next assistant/tool-call turn. MAR persists and binds that turn to the exact task/attempt/run epoch. The child worker executes any permitted coding tool calls under the immutable Goal Contract and Windows sandbox. The MCP process never gains direct filesystem, Git-mutation, or command authority.

## Durable Web-turn contract

SQLite schema v10 adds durable Web brain turns bound to:

- `task_id`;
- `attempt_id`;
- `run_epoch`;
- model `request_id`;
- canonical request hash;
- canonical response hash;
- integrity hash;
- creation/responded timestamps.

Request/response identity is semantic/canonical JSON rather than raw serialization order, so MCP structured-content reserialization cannot create a false integrity failure.

When a Web turn is pending the task enters `INPUT_REQUIRED`. The worker keeps its attempt authority alive with bounded heartbeats while waiting. Only a response to the current exact `turn_id` resumes the task.

## MCP surface

The V1 public surface is now exactly nine task-oriented operations:

- `submit`
- `status`
- `steer`
- `input`
- `cancel`
- `result`
- `inspect`
- `brain_turn`
- `brain_respond`

`brain_turn` is read-only cognition transport. `brain_respond` can only answer the bound pending turn; it cannot directly execute a coding primitive.

Low-level `read_file`, `search_text`, `write_file`, `replace_exact`, Git inspection, and `run_command` remain worker-internal.

## Multi-client authority

Multiple MCP stdio processes/chats may point at the same MAR database, but only one process may own daemon scheduling/integration authority at a time.

A database-scoped exclusive Windows file lease (`<db>.daemon.lock`) provides process-level singleton authority:

- one MCP process owns the daemon;
- other MCP processes remain control/read clients while waiting for authority;
- kernel handle release on process death permits takeover;
- the lease is scoped by database path, so unrelated MAR installations/databases do not block each other.

`TestDaemonAuthoritySerializesMultipleMCPProcessesAndAllowsTakeover`: PASS.

## UX state contract

`status` now includes bounded user-facing guidance including:

- state detail;
- next valid action;
- `brain_turn_available` when GPT Web cognition is required.

A `BLOCKED` task surfaces the bounded worker terminal diagnostic where available. `INPUT_REQUIRED` distinguishes owner input from a pending Web brain turn. `COMPLETE` points the client toward `result`/`inspect` rather than requiring internal-state knowledge.

## Evidence

Completed before the current host sandbox prerequisite reset:

- `TestRuntimeE2EWebBrainMCPWorkerVerifyIntegrate`: PASS (~36.76s).
- exact flow proven without a model-provider API key: MCP submit -> durable Web turn -> Web tool call -> isolated worker mutation -> `finish_task` -> `go test/vet/build` -> physical termination -> serialized integration -> `COMPLETE`.
- final result: technical `VERIFIED`, `integration_status=INTEGRATED`.

Current candidate checks:

- `internal/contextengine`: PASS;
- `internal/domain`: PASS;
- `internal/service`: PASS;
- `internal/mcpedge`: PASS;
- daemon authority takeover unit test: PASS;
- `go vet ./...`: PASS;
- `go build ./...`: PASS;
- `git diff --check`: PASS (Windows LF/CRLF warnings only).

Current worker/T7 runtime rerun is blocked by the Windows host prerequisite, not by a relaxed fallback: `sandbox-host-check` currently reports `AppContainer NUL probe failed: Access is denied`. `sandbox-host-prepare` correctly refuses without Administrator elevation. MAR intentionally keeps `SelfHostingSafe=false` until this prerequisite passes.

## Remaining V1 gate

1. Run `mar sandbox-host-prepare -workspace <project-root>` from an elevated Administrator terminal after the current Windows boot.
2. Re-run `sandbox-host-check` and require PASS.
3. Re-run T7/real-worker/Web-brain E2E and full sequential repository tests.
4. Perform owner real-use UX1-UX7.
5. Only then commit/push/release and claim `SELF_HOSTING_READY`.

## Stop rule

No additional Web-brain protocol, MCP tools, model routing subsystem, UI/dashboard, retrieval algorithm, or autonomous orchestration layer is added before V1 owner acceptance unless a mandatory regression proves the current contract insufficient.
