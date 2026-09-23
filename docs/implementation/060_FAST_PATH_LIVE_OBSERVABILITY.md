# Slice 060 — Fast-Path Live Observability

**Date:** 2026-09-24  
**Status:** IMPLEMENTED CANDIDATE  
**Scope:** additive observability projection only; no authority, task lifecycle, database, or execution-path redesign.

## Problem proved before implementation

MAR v1.4.0 was stable, but the Owner Console could be visually idle while a directly connected client was actively using the Trusted Owner Fast Path.

A controlled proof used the already-registered AutoVideoPipeline project:

- project id: `local-autovideopipeline-d9b02213edc39140`;
- root: `D:\Video\AutoVideoPipeline`;
- a read-only `project.context` call completed through live MAR in about 1.855 s;
- the OpenAI Secure Tunnel `last_activity_at` advanced;
- the active durable AutoVideoPipeline task count remained 0.

This proved the transport saw real work while Live Operations could not project it as activity. The cause was structural, not an AutoVideoPipeline attachment/configuration defect:

- Live Operations derived active flows only from durable MAR tasks;
- fast-path `project` / `action` calls intentionally do not create durable tasks;
- transport telemetry counted requests/sessions but did not expose tool name, operation, project id, start/end, or duration;
- live token visualization was sourced from governed Web Brain turn usage only.

AutoVideoPipeline was used only as a read-only workload. No AutoVideoPipeline source file, Git ref, task lifecycle, or configuration was changed by this research.

## Prior-art check

The design reuses the established observability model rather than inventing a MAR-specific tracing system:

- OpenTelemetry RPC spans model operations with a start, end, duration and error status;
- OpenTelemetry's MCP demo instruments inbound MCP tool calls as spans;
- OpenTelemetry guidance recommends capturing only attributes that provide clear operational value and avoiding verbose/sensitive payload capture.

MAR does not add an OpenTelemetry dependency in this slice. It copies the proven span-shaped semantics into the smallest local in-memory projection required by Owner Console.

## Shadow proof before production patch

A prototype under `.mar/runtime/research-observability/` was kept outside tracked production source.

### Minimal metadata parser

One valid `tools/call` request containing a synthetic secret in arguments was parsed into only:

- JSON-RPC/tool identity;
- `tool`;
- `operation`;
- `project_id`.

200,000 parses completed in about 629 ms (~3.15 us/parse, ~318k parses/s).

The synthetic secret was present in the request but absent from telemetry output.

### Real end-to-end shadow call

A loopback shadow proxy forwarded a real `project.context` request to the live MAR MCP endpoint:

- HTTP 200;
- real MAR result returned;
- observer emitted `start`;
- observer emitted `complete`;
- measured server-side duration ~33 ms;
- no payload content retained.

A three-second `action.run` proof showed `start` was visible after 700 ms while the tool was still running, then `complete` appeared at ~3291 ms. This proves the Console can display active fast-path work before completion.

### Privacy proof

A sentinel value `SHADOW_PRIVATE_SENTINEL_9F2C7A` was placed in a valid request query. The request succeeded, while the bounded event stream did not contain the sentinel.

### Bounded-loss proof

A shadow ring with capacity 4 received six start/complete events:

- retained events: 4;
- `dropped=2`;
- no unbounded growth.

### Failure-isolation proof

The shadow observer was configured to panic on both start and completion observation:

- observer errors: 2;
- tool call HTTP status: 200;
- MAR result remained valid;
- execution was unaffected.

### End-to-end overhead proof

Twelve alternating direct-vs-shadow read-only calls against the same live MAR endpoint measured:

- direct median ~67.9 ms;
- shadow median ~60.7 ms.

The shadow median being lower is not treated as a speed improvement; it demonstrates observer overhead was below run-to-run noise at this sample size. The isolated parser cost remained ~3.15 us/call.

## Production design

### Typed MCP observer

The MCP server exposes an optional `ToolCallObserver`.

For `tools/call`, the observer emits only:

- monotonic per-server call id;
- timestamp;
- phase: `start` or `complete`;
- canonical tool name;
- operation when present;
- project id when present;
- duration on completion;
- outcome on completion.

The observer reads the already-decoded `CallToolRequest`; it does not re-read or wrap the HTTP response stream.

Observer panics are isolated and cannot fail the tool call.

### Bounded per-connection ring

OpenAI Secure Tunnel, GPT Web fallback, and Claude Web each keep their own in-memory ring:

- capacity: 256 events;
- newest events retained;
- overwritten event count exposed as `dropped_operations`;
- no SQLite writes;
- no durable task or new lifecycle;
- no background telemetry daemon.

### Owner runtime projection

Each operational connection may expose:

- `recent_operations`;
- `dropped_operations`.

The fields are additive.

### Live Operations

The React Owner Console continues its existing two-second runtime poll and adds:

1. active fast-path tool calls without converting them into durable tasks;
2. recent MCP activity rows with project, tool/operation, connector, duration and outcome;
3. an execution-activity heartbeat;
4. a token heartbeat based only on observed Web Brain token counters.

The token chart changes from cumulative totals to observed token throughput. Tool-call frequency is never converted into estimated token usage.

## Privacy boundary

The production observer does not retain:

- tool arguments;
- query/prompt/content values;
- command lines;
- file contents;
- API keys or capability tokens;
- tool output.

## Non-goals

Slice 060 does not:

- create a generic tracing backend;
- add OpenTelemetry as a dependency;
- persist telemetry to SQLite;
- create tasks for fast-path calls;
- change MCP authority or tool schemas;
- change AutoVideoPipeline;
- change token accounting semantics;
- solve long-running request detachment or tool-call batching.

## Candidate evidence

Before full release qualification:

- `go test -count=1 ./internal/mcpedge` — PASS;
- `go test -count=1 ./cmd/mar` — PASS after rebuilt embedded UI;
- React `npm run check` — PASS;
- React production `npm run build` — PASS;
- `git diff --check` — PASS;
- production regressions cover bounded tool metadata, privacy, observer-panic isolation and bounded ring overflow.

## Release acceptance still required

1. full exact-HEAD `go test -p 1 -count=1 -timeout 300s ./...`;
2. `go vet -p 1 ./...`;
3. `go build -p 1 ./...`;
4. `git diff --check`;
5. exact HEAD unchanged and clean;
6. activate exact candidate;
7. from the already-connected ChatGPT path, make a read-only AutoVideoPipeline fast-path call;
8. verify live `/api/runtime` exposes matching `start/complete` metadata for AutoVideoPipeline without creating an active durable task;
9. verify runtime remains `HEALTHY / ALIGNED / trusted_for_release=true`;
10. only then mark Slice 060 VERIFIED / ACTIVATED.
