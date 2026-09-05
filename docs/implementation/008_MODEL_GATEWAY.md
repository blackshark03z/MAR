# Slice 008 — Model Gateway

**Architecture:** FROZEN
**Bootstrap milestone:** `MAR_BOOTSTRAP_STABLE -> SELF_HOSTING_READY`
**Status:** COMPLETE / VERIFIED

## Goal

Provide one narrow inference boundary for the future autonomous agent loop without binding MAR core logic to one provider.

## Decisions

- `internal/model.Provider` is the provider-neutral interface.
- `Gateway` validates normalized turns before provider dispatch.
- First concrete backend: OpenAI-compatible Chat Completions HTTP API.
- The backend is intentionally stateless: complete normalized message/tool history is supplied by MAR rather than making remote provider conversation state authoritative.
- API keys are read from a configured environment-variable name at call time and are never persisted in MAR task state.
- `X-Client-Request-Id` carries MAR turn identity when supported.
- Model calls are **not automatically retried** by the gateway. Retry policy belongs to durable agent/task orchestration where cost and uncertain outcomes can be reasoned about explicitly.
- HTTP responses are size-bounded before JSON decoding.
- Context cancellation and request timeout are enforced by `net/http`.

## Normalized contract

A turn contains:
- durable caller-generated `request_id`;
- model profile/model id;
- complete ordered messages;
- assistant function calls and corresponding tool-result messages;
- bounded function definitions using JSON Schema;
- optional reasoning-effort hint.

A response contains:
- provider response/request ids;
- assistant text/tool calls;
- finish reason;
- input/output/total token accounting.

## Scope boundary

Slice 008 does not implement:
- provider routing;
- automatic failover;
- automatic retry;
- model selection policy;
- coding tools;
- context retrieval;
- autonomous loop;
- persistent transcript/checkpoint.

Those belong to later bootstrap slices.

## Acceptance

1. Invalid normalized turn is rejected before provider call.
2. Tool definitions, assistant tool calls, and tool results map correctly to the concrete backend.
3. Provider response maps back to normalized assistant/tool-call representation.
4. Token usage and request identifiers are preserved.
5. Missing secret fails closed without exposing a key.
6. Context cancellation terminates the HTTP request.
7. Provider errors are bounded and typed; no implicit retry occurs.
8. Oversized responses are rejected before unbounded memory growth.
9. Existing Slice 001–007 tests remain green.
10. `go test -count=1 ./...`, `go vet ./...`, Windows build, and `git diff --check` pass.

## Evidence

- Targeted `./internal/model/...` suite: PASS, including tool-call mapping, timeout/cancellation, typed HTTP failures, missing-key failure, response-size bound, and no implicit retry.
- Full `go test -count=1 -timeout 90s ./...`: PASS.
- `go vet ./...`: PASS.
- Windows build `./cmd/mar`: PASS.
- `git diff --check`: PASS.
- Earlier hanging cancellation fixture was corrected; current suite completes within bounded test time.
- Final implementation revision is the Git commit containing this document.
