# MAR Connection Usability Truth

**Date:** 2026-09-19
**Status:** BOUNDED PRE-V1.3 CORRECTION
**Architecture:** no kernel/task/authority redesign

## Problem

Owner real-use showed a false usability impression: the GPT/Claude public route could be healthy and `LINK_READY` while no Web client had initialized the MAR MCP server in the current runtime. In the reproduced state, route readiness was true while `initialized=false`, `tools_listed=false`, and requests were zero.

This is not a scheduler, CADS task, verification, or integration defect. It is a connection-truth/product-surface gap.

## Existing stable endpoint truth

MAR already has two distinct GPT paths:

1. **OpenAI Secure MCP Tunnel** — primary stable GPT path. A saved tunnel identity is reused across restart when its prerequisites are present.
2. **GPT Server URL fallback** — Streamable HTTP capability URL. Temporary Quick Tunnel mode may change hostname after restart; configured Stable URL mode is persistent.

This slice does not invent a new tunnel or second connection authority.

## Correct connection model

Route reachability and client usability are separate facts.

For Streamable HTTP connectors:

```text
ROUTE_UNAVAILABLE
  -> ROUTE_READY
  -> CLIENT_ATTACHED       (observed MCP initialize)
  -> USABLE                (initialize + tools/list observed)
```

The runtime exposes these derived facts:

- `connection_stage`;
- `client_attached`;
- `tools_discovered`;
- `usable_from_client`;
- `endpoint_stable`.

They are derived from existing route/telemetry truth and create no new durable authority.

## UI rule

The Owner Console must not display a connector as usable merely because the route is online.

Connection cards separate:

- Route;
- Client attachment;
- Tool discovery;
- Usable.

OpenAI Secure MCP Tunnel remains the preferred stable GPT path. Temporary capability URLs remain explicit fallback/debug paths.

## Compatibility

Existing backend status values such as `LINK_READY`, `CONNECTED`, and `IDLE` remain unchanged for compatibility. The new `connection_stage` is additive.

The six canonical MCP tools remain unchanged:

`project`, `submit`, `task`, `control`, `brain_turn`, `brain_respond`.

No gateway-tool expansion is included in this slice. A stable gateway is reconsidered only if real attachment succeeds and schema/tool-cache evidence still shows a material failure.

## Acceptance

1. A healthy route with no MCP initialize reports `ROUTE_READY` and `usable_from_client=false`.
2. Observed initialize reports `CLIENT_ATTACHED` but not usable until tool discovery.
3. Observed initialize + tools/list reports `USABLE`.
4. Stable-route identity is represented separately from client attachment.
5. Existing MCP tool semantics and task authority are unchanged.
6. React production assets build successfully.
7. Relevant Go regression plus full repository test/vet/build pass before promotion.
