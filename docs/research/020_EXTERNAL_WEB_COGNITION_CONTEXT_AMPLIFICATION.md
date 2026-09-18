# R-020 — External Web cognition context amplification

**Status:** `RESEARCH_ONLY`  
**Date:** 2026-09-12  
**Verdict:** `P0_CROSS_BOUNDARY_CONTEXT_HYPOTHESIS`

## Research question

DecisionProjection has substantially bounded the context MAR constructs for each Web Brain decision. Does the **MCP relay into the Web Chat itself** still accumulate large tool results/transcript state and become a separate source of context growth, browser memory pressure or long-conversation instability?

This is a different layer from R-006.

```text
R-006:
MAR durable truth -> bounded DecisionProjection -> TurnRequest

R-020:
TurnRequest -> brain_turn MCP result -> Web Chat/tool transcript -> next Web reasoning
```

No production protocol or payload change is authorized by this document.

## Current `brain_turn` payload

`brain_turn` calls `PendingWebTurn()` and returns:

```text
{
  available: true,
  turn: <domain.WebTurn>
}
```

A pending `domain.WebTurn` contains:

- task/attempt/run-epoch identity;
- request identity/hash/integrity metadata;
- creation timestamp;
- the complete `request` JSON for that Web cognition turn.

The `request` is the bounded TurnRequest containing the exact messages/tools the Web model must reason over.

Unlike `submit`, `status` and `brain_respond`, which have explicit compact receipt tests, `brain_turn` intentionally cannot return only a tiny receipt: external cognition needs the offered reasoning input.

## MCP result carries the structured payload twice for compatibility

MAR's raw task-read helper marshals the result once and places the same JSON in both:

1. a `TextContent` block;
2. `StructuredContent`.

This matches the general MCP structured-tool convention. The MCP specification says that for backwards compatibility a tool returning structured content **SHOULD** also return the serialized JSON in a `TextContent` block. The official Go SDK similarly documents structured output as being represented in structured content and unstructured text content.

Therefore this is not simply a MAR serialization mistake. It is a compatibility trade-off with large tool outputs.

However, for a large `brain_turn`, it means the MCP wire result can carry two semantic copies of the cognition payload, plus JSON-string escaping/envelope overhead.

## Phase-0 scale from existing data

R-006 measured the later DecisionProjection population at:

- median request ~44.7 KiB;
- p95 request ~121.6 KiB;
- accepted V1.1 task: ~248.4 KiB total request volume across 5 Web turns.

If each `brain_turn` result represents the request in both TextContent and StructuredContent, the application payload alone can be represented roughly twice on the MCP response wire before envelope/escaping overhead.

For the accepted five-turn case, that originally implied approximately **497 KiB of duplicated application request representation** as an order-of-magnitude estimate.

A later byte-level reconstruction from the durable `web_turns` population and the current `addRawTaskReadTool` serialization contract tightened this substantially without exposing prompt content. Reconstructing `brain_turn` as `available + WebTurn`, then simulating the current `CallToolResult` with both TextContent and StructuredContent produced:

- all 385 historical turns: inner request 33.87 MB -> simulated CallToolResult 72.76 MB, aggregate ratio ~2.15x;
- 90 DecisionProjection turns: inner request 5.46 MB -> simulated CallToolResult 11.83 MB, aggregate ratio ~2.17x; median 45.8 KiB inner -> ~98.9 KiB result;
- accepted V1.1 task (5 turns): inner request 254,361 bytes -> simulated CallToolResult 541,654 bytes, aggregate ratio ~2.13x.

The >2x factor comes from dual semantic representation plus JSON string escaping/envelope overhead. These are reconstructed application/result bytes, **not measured HTTP wire bytes** and not evidence about hidden model-context retention.

## Byte-level reconstruction from durable history — 2026-09-12

An external research collector reconstructed the exact pending `WebTurn` envelope from the durable store and simulated the current `CallToolResult` representation used by `addRawTaskReadTool`: one JSON copy in `TextContent` plus the same structured value in `StructuredContent`. The collector stores only sizes/ratios, not prompt contents.

This is **not** measured HTTP wire bytes; it is source-contract-faithful application-level reconstruction before HTTP framing/compression.

Measured results:

```text
All 385 historical turns:
  inner TurnRequest total                 33,871,714 bytes
  simulated CallToolResult total          72,757,564 bytes
  aggregate result / inner ratio               2.148x

90 DecisionProjection turns:
  inner TurnRequest total                  5,459,721 bytes
  simulated CallToolResult total          11,831,570 bytes
  median inner request                        45,803 bytes
  median simulated CallToolResult             98,947 bytes
  p95 simulated CallToolResult               271,538 bytes
  aggregate result / inner ratio               2.167x

Accepted V1.1 self-hosting task, 5 turns:
  inner TurnRequest total                    254,361 bytes
  simulated CallToolResult total             541,654 bytes
  aggregate result / inner ratio               2.129x
```

Therefore the compatibility representation is now a **measured application-level amplification signal**, not merely a rough `~2x` estimate. It still does not prove browser/model transcript retention behavior or physical network bytes.

## The more important unknown: Web Chat transcript retention

The wire duplication is observable and bounded. The harder issue is what the Web client/model keeps from prior tool calls.

A Web reasoning sequence looks conceptually like:

```text
brain_turn #1 -> ~N KiB request payload
brain_respond #1
brain_turn #2 -> ~N KiB new request payload
brain_respond #2
...
```

Even though MAR's inner DecisionProjection no longer replays the old full transcript, the external Web Chat may itself retain earlier `brain_turn` tool results as part of its conversation/tool history, subject to platform-specific compaction/context management.

MAR cannot currently observe that internal client/model context directly.

Therefore these two claims must remain separate:

- **PROVEN:** MAR sends a new full bounded reasoning request through `brain_turn` each decision, and MCP compatibility representation includes text + structured forms.
- **UNPROVEN:** ChatGPT necessarily sends every prior `brain_turn` byte back into every future model inference or that this alone caused Chrome OOM.

Do not infer the second from the first.

## Why this matters even after DecisionProjection

DecisionProjection solved a MAR-side append-only history problem: each new cognition request is rebuilt from bounded current truth.

It does **not** guarantee:

- bounded Web Chat conversation storage;
- bounded browser DOM/tool-result memory;
- bounded client-side serialization state;
- bounded model-side conversation history selected by the Web platform;
- low repeated MCP transfer bytes.

Thus a task can be well-bounded inside MAR while the Web control conversation still becomes increasingly heavy across many brain relay turns/tasks.

This distinction is central to diagnosing Chrome OOM and long-chat reliability.

## Measurement plan

### C0 — MCP brain-turn result bytes

For each Web turn, measure externally:

- raw HTTP/MCP response bytes;
- TextContent bytes;
- StructuredContent bytes;
- inner TurnRequest bytes;
- envelope/escaping overhead;
- ratio `wire_bytes / inner_request_bytes`.

Do not log prompt contents; sizes/hashes are sufficient.

### C1 — Same task, increasing turns

Use a disposable fixed fixture that intentionally requires 1, 3, 6, 9 and 12 Web decisions.

Measure:

- total `brain_turn` transfer bytes;
- Chat/browser RSS delta;
- tool-call latency by turn index;
- disconnect/error frequency;
- MAR inner request size by turn index.

If MAR inner request remains flat but browser/tool latency grows with historical turns, the growth likely occurs outside DecisionProjection.

### C2 — Fresh Web conversation versus long-lived conversation

Run equivalent disposable fixtures from:

- a fresh Web Chat context;
- a long-lived Web Chat containing substantial prior tool history.

Compare first-turn tool latency, browser memory, failure rate and completion behavior.

This benchmark must not assume the platform exposes exact hidden context tokens.

### C3 — Multi-episode task

When MAR rolls into a fresh Web cognition episode after an episode budget, measure whether the external Web Chat still carries old episode tool results and whether episode reset materially changes browser/model behavior.

### C4 — Client-dependent behavior

If both ChatGPT and Claude Web are used, compare the same MAR TurnRequest relay. Client transcript/tool-result retention and compaction can differ even when MAR is identical.

## Candidate experiments — lowest risk first

### E1 — Measure text + structured compatibility cost

Determine what current ChatGPT/Claude clients actually consume. The spec's text fallback is `SHOULD`, not a license to remove it blindly. A modern-client-only payload variant could save wire bytes but may break cached/older clients.

Any experiment belongs in an isolated research server first.

### E2 — Compact outer WebTurn envelope, not inner reasoning input

The Web model needs the exact TurnRequest but may not need all durable metadata duplicated around it on every turn.

Research whether outer metadata can be reduced while keeping:

- task/turn/attempt/run-epoch binding;
- request hash/integrity semantics;
- stale-turn protection;
- exact `brain_respond` target identity.

### E3 — Separate large immutable/tool evidence into explicit resources/handles

MCP supports resource links and modern explicit handles, but the Web model still needs necessary content to reason. Handles help only when the model/client can fetch selectively without adding more transcript overhead than they save.

R-006 Tier B/C experiments should measure this rather than assume references are cheaper.

### E4 — Modern multi-round-trip/task protocol adapter

MCP `2026-07-28` introduces stateless request semantics and MRTR/Tasks patterns. Research whether one durable outer operation/request-state flow can reduce repeated top-level tool transcript entries while mapping to the same MAR durable WebTurn semantics.

This is an interoperability/context experiment, not a reason to replace MAR's task model.

### E5 — Provider-controlled cognition for very long runs

Direct provider mode gives MAR tighter control over exactly what messages are sent each inference and avoids relying on Web Chat transcript behavior. It can serve as a comparison baseline for long runs.

The goal is not to abandon Web Brain. The benchmark asks how much reliability/context cost comes specifically from routing cognition through a persistent Web conversation.

## Metrics

Track:

```text
inner_turn_request_bytes
brain_turn_wire_bytes
text_content_bytes
structured_content_bytes
outer_envelope_bytes
brain_turn_latency_ms
brain_turn_index
web_episode_index
browser_chrome_rss_delta
remote_error_count
reconnect_count
completed_goal
```

Where hidden model context size is unavailable, mark it `UNOBSERVABLE` rather than estimate it from transcript bytes.

## Safety/correctness invariants

Any future optimization must preserve:

- exact current TurnRequest semantics presented to cognition;
- task + attempt + run epoch + turn binding;
- stale response rejection;
- durable request/response integrity;
- reconnect from MAR durable truth;
- no dependence on implicit Web conversation memory for Goal/authority truth;
- no hidden client state promoted into execution authority.

## Slice A implementation result — structured response mode

MAR VNext Slice A implements the lowest-risk R-020 experiment as an additive `brain_turn` transport option:

- omitted `response_mode` remains `compat` and preserves the previous full TextContent + StructuredContent representation for cached/older clients;
- `response_mode=structured` keeps the exact authoritative `available + WebTurn` payload in StructuredContent, including the complete bounded TurnRequest and durable identity metadata;
- structured mode replaces only the duplicated full TextContent JSON with a compact receipt containing task/turn/attempt/run-epoch identity;
- `brain_respond`, stale-turn rejection, request/response integrity and durable WebTurn semantics are unchanged.

A large-fixture regression compares the two modes against the same WebTurn. It requires StructuredContent semantic equality and requires the serialized structured-mode CallToolResult to be no more than 60% of compatibility mode. This targets the previously reconstructed ~2.15x application-level amplification without claiming anything about hidden Web-client/model transcript retention.

This implementation also confirms that DecisionProjection is already MAR's bounded inner task/context projection; no second Task Capsule or parallel Project Brain subsystem is required for Slice A.

## Phase-0 decision

Treat external Web cognition context as a **P0 cross-boundary hypothesis**.

DecisionProjection has already fixed most MAR-side transcript replay. The next context benchmark must therefore measure both sides of the boundary:

1. inner MAR TurnRequest size;
2. outer MCP/Web Chat transfer and browser/context behavior.

Do not spend more effort shrinking DecisionProjection until we know which side is now dominating real long-run cost.

## External references

- MCP tool structured content specification: https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2026-07-28/server/tools.mdx
- Official Go SDK tool server documentation: https://github.com/modelcontextprotocol/go-sdk/blob/main/docs/server.md
- MCP 2026-07-28 overview: https://blog.modelcontextprotocol.io/posts/2026-07-28/
