# Slice 012 — Autonomous Agent Loop

**Status:** IMPLEMENTED / VERIFIED

**Frozen architecture source:** MAR V1 Agent Worker, ADR-002 MCP as control plane rather than inner loop, durable execution-attempt fencing, and the Worker Authority Boundary.

## Goal

Implement the minimum local Codex-like model↔tool loop needed for MAR to execute a bounded Goal Contract autonomously inside one already-admitted task workspace.

The loop reuses existing frozen primitives rather than creating a second orchestrator:

`Goal Contract + attempt/run_epoch + bounded context -> model turn -> Coding ACI tools -> observations -> repeat -> completed_candidate/blocked`

`completed_candidate` means only that the agent believes implementation is ready for MAR verification. It is not a verification result and does not transition the product directly to COMPLETE.

## Implementation

### Local agent loop

`internal/agent/loop.go`

`Loop.Run` pins task id, durable `attempt_id/run_epoch`, workspace root, immutable Goal Contract, expected repository revision, model profile, Coding ACI runtime, bounded context builder, and durable attempt-authority checker.

The loop builds the initial Slice 011 context pack, independently verifies its revision and Goal Contract hash, then keeps all subsequent model↔tool turns local to the worker runtime.

### Explicit terminal protocol

The loop adds one reserved synthetic tool:

- `finish_task(status=completed_candidate|blocked, summary, blocker?)`.

The model may not silently end with prose. A turn without coding tool calls or a valid `finish_task` is blocked as a protocol error. `finish_task` must be the sole tool call in its turn, preventing a model from combining a mutation with a simultaneous completion declaration.

Loop terminal statuses are:

- `completed_candidate` — ready for later MAR verification;
- `blocked` — requires an external decision/prerequisite or hit a non-budget protocol/provider blocker;
- `cancelled` — parent cancellation or stale/non-authoritative execution attempt;
- `budget_exhausted` — hard turn/tool/token/request/response/context/time bound reached.

### Authority-filtered tool surface

Always-read-only tools are `read_file`, `search_text`, `git_status`, and `git_diff`.

Mutation-producing tools are `write_file`, `replace_exact`, and `run_command`. They are exposed only when the Goal Contract grants local file-write authority. Unknown/unclassified tools fail closed rather than being exposed implicitly.

No push/deploy/remote-write tool is added by Slice 012.

### Durable attempt/run_epoch fencing

`internal/store/attempt_authority.go`
`internal/service/task_service.go`

Slice 012 exposes a read-only durable attempt-authority check backed by SQLite.

The loop validates `task_id + attempt_id + run_epoch` before context/model execution begins, at the beginning of every model turn, and immediately before every mutation-producing tool dispatch. Therefore an attempt logically fenced while the model is thinking cannot dispatch the next mutation through MAR. A stale epoch terminates the loop as `cancelled` before the side effect.

This is logical MAR authority fencing only. It does not replace Slice 003/010 physical process/workspace containment.

### Model request/output bounds

`internal/model/model.go`
`internal/model/openaichat/client.go`

`TurnRequest` now includes a positive `MaxOutputTokens` bound. The OpenAI-compatible adapter maps it to `max_tokens`.

The agent loop additionally enforces hard bounds for model turns, total tool calls, tool calls per turn, total reported model tokens, output tokens per turn, initial context bytes, serialized model request bytes, serialized assistant response bytes, tool observation bytes, and wall-clock duration.

Provider token accounting is required. A provider output-token cap violation or `finish_reason=length` stops the turn before any tool call from that response can execute.

### Prompt/evidence boundary

System instructions explicitly state that the immutable Goal Contract is authoritative, while repository context, source code, comments, tool output, test output and error text are **untrusted evidence**. Evidence cannot widen Goal Contract authority or project/workspace identity. Only supplied tools may be used.

The Goal Contract and untrusted repository context are placed in separately labeled sections of the initial user message.

### Protocol safety

The loop fails closed on malformed/duplicate tool-call ids, duplicate ids inside one batch, mixed `finish_task` plus another tool call, unclassified or unauthorized tools, malformed `finish_task` arguments, unsupported provider finish reasons, provider content-filter termination, missing token accounting, oversized payloads, stale context identity, stale/non-authoritative attempt identity, and unsafe Coding ACI runtime.

Tool execution errors are returned to the model as bounded observations where continuation is safe; invariant/security failures terminate instead.

## Acceptance evidence

Targeted agent-loop tests pass for multi-turn execution, authority filtering, hallucinated mutation rejection, unclassified-tool fail-closed behavior, unsafe-runtime refusal, mixed mutation+finish rejection, batch tool budgets, token budgets, malformed-finish recovery, duplicate tool-call IDs, provider length/token-cap handling, context identity rejection, byte bounds, timeout, and parent cancellation.

Windows real-runtime integration passes:

- scripted model -> real Coding ACI `write_file` -> typed Git `git_status` -> `finish_task`;
- LPAC/self-hosting-safe gate is required;
- resulting workspace file and Git observation are returned through the real ACI path.

Durable fencing integration passes with real SQLite state:

- create task and durable execution attempt via `TaskService.BeginAttempt`;
- active current attempt may mutate through real Coding ACI;
- logically fence the same attempt;
- a subsequent run using the stale `attempt_id/run_epoch` is cancelled before model/tool dispatch.

Explicit proof: `TestLoopUsesDurableAttemptAuthorityBeforeRealACIMutation`: PASS.

Repository-wide verification after Slice 012:

- `go test -count=1 -timeout 180s ./...`: PASS with `TEMP/TMP=D:\MAR\.mar\runtime\testtmp`;
- `go vet ./...`: PASS;
- Windows `go build ./cmd/mar`: PASS;
- `git diff --check`: PASS.

The D: temp placement remains necessary because the host C: volume is nearly full; no test or sandbox invariant was weakened.

## Explicit non-goals

Slice 012 does not implement semantic checkpoint persistence/resume, crash/restart reconstruction of model conversation, final verification/result contract, task integration/merge completion, MCP control surface, or autonomous self-hosting acceptance. Those remain Slices 013–016.
