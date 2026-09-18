# MAR ChatCode Adapter

**Date:** 2026-09-19
**Status:** BOUNDED TRANSPORT ADAPTER
**Authority:** MAR remains the only task/execution authority.

## Problem

The current ChatGPT account/session may have ChatCode available as a callable stable gateway while MAR itself is not attached as a callable custom MCP app in that chat.

MAR's local MCP endpoint is healthy and already exposes exactly six canonical tools:

- `project`
- `submit`
- `task`
- `control`
- `brain_turn`
- `brain_respond`

A direct local stateless `tools/call` against the MCP target behind the OpenAI Secure MCP Tunnel has been proven to return canonical MAR project truth, including CADS revision `1a6c4caedda329c9700fcda27e410a0f750bb487`.

## Decision

Provide a thin, stateless transport shim:

`scripts/mar-chatcode-adapter.ps1`

Intended path:

```text
ChatGPT / CADS Tech Lead
        |
        v
ChatCode stable outer gateway
        |
        v
ChatCode local_exec
        |
        v
mar-chatcode-adapter.ps1
        |
        v
MAR loopback MCP endpoint
        |
        v
MAR canonical tools / durable authority
```

ChatCode does not become a MAR task authority.

The adapter has no database, scheduler, task lifecycle, workspace lifecycle, verification state, integration state, retry state or recovery state.

## Fail-closed boundaries

The adapter:

1. allows only the six canonical MAR tool names;
2. discovers the current MCP local target from the live MAR runtime;
3. rejects any target that is not HTTP loopback;
4. accepts tool arguments only as JSON;
5. fails non-zero on JSON-RPC errors;
6. fails non-zero on MAR MCP application/tool errors;
7. verifies the exact canonical six-tool surface when called with `-ListTools`;
8. never prints the public capability URL, tunnel secret, API key or control-plane credential.

## Usage

List and verify the canonical MAR surface:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\mar-chatcode-adapter.ps1 -ListTools
```

Read CADS project truth:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\mar-chatcode-adapter.ps1 -Tool project -ArgumentsJson '{"operation":"context","project_id":"cads"}'
```

Read an existing task:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\mar-chatcode-adapter.ps1 -Tool task -ArgumentsJson '{"operation":"status","task_id":"<task-id>"}'
```

The same adapter may relay `submit`, `control`, `brain_turn`, and `brain_respond`. Their authority and validation remain exactly MAR's existing MCP semantics.

## CADS fallback rule

When the current Web chat exposes MAR directly, use MAR directly.

When MAR is not attached/callable but ChatCode is callable, the Tech Lead may use ChatCode to execute this adapter rather than reimplementing MAR actions through filesystem/Git commands.

This fallback must not:

- resubmit an already completed Goal merely because direct MAR tooling is absent;
- bypass MAR task state;
- mutate a MAR workspace directly;
- fabricate verification/integration status;
- push remote Git unless MAR/CADS separately authorizes publication.

## Non-goals

This slice does not:

- add a new MAR kernel subsystem;
- add a second gateway inside MAR;
- modify the six canonical MCP tools;
- modify ChatCode binary;
- add provider-specific task semantics;
- solve ChatGPT plan/product connector entitlements.

## Acceptance

1. `-ListTools` reports exactly the six canonical MAR tools.
2. `project/context` through the adapter returns the same current CADS HEAD as direct MAR MCP.
3. invalid tool names are rejected locally.
4. non-loopback MCP targets are rejected.
5. MAR tool errors return non-zero and structured failure output.
6. no MAR kernel/runtime behavior changes.
