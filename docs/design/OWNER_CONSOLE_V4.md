# MAR Owner Operations Console v4 — Live Observability Contract

Date: 2026-09-09
Status: implementation baseline

## Product question

The Console must not feel like a static collection of telemetry cards. At any moment the Owner should be able to answer:

1. Is MAR usable right now?
2. What changed in the last few seconds?
3. Is GPT/Claude transport actually active, merely ready, or failing?
4. What task is executing or waiting for an AI turn?
5. How many model tokens have been observed during the current active execution?
6. Is that token number live runtime evidence, durable completed-result usage, or unavailable?
7. What requires Owner action now?

Every prominent UI element must answer one of these questions or lead to the action that resolves it.

## Visual direction

v4 replaces the card-grid feeling with an operations surface:

- one semantic `Right now` answer,
- one compact summary rail,
- one dominant Live Operations surface,
- an event stream for observed state transitions,
- a small live token trend,
- Current Work as a list rather than nested cards,
- Action Center and Connections as secondary operational lists,
- durable 7-day usage as context, not the focal surface.

Dividers, whitespace and typography establish hierarchy before borders. Motion is limited to a subtle live beacon and respects `prefers-reduced-motion`.

## Realtime truth model

### Connection observation

The Owner UI polls authoritative runtime snapshots every 2 seconds without overlapping polls. It derives bounded client-side events only from changes between snapshots:

- connection status transitions,
- MCP request-count deltas where the transport exposes them,
- active-session count changes where session lifecycle is authoritative,
- new last-activity timestamps,
- task product-stage transitions.

The initial Event Stream is seeded from the first real snapshot so the surface is informative immediately. Derived events are presentation only and never become execution authority.

### Live active-turn token observation

For Web Brain tasks, each Web Brain request/response is already persisted in SQLite. v4 reads the current task/run-epoch turns and aggregates completed `TurnResponse.Usage` values while the task is active.

Source identifier: `WEB_TURN_DURABLE_ESTIMATE`.

This source is:

- durable across the Owner UI / MCP process boundary,
- restart-safe,
- scoped to an exact task and run epoch,
- updated when each model turn response is durably recorded,
- explicitly estimated because Web Brain token accounting is derived from bounded request/response size for runtime budgeting.

It is **not provider billing data**. The UI prefixes active-turn totals/rates with `~` and labels the source as estimated.

If an active task is using a provider mode without an authoritative cross-process per-turn source, live token telemetry remains unavailable. MAR must never convert unavailable telemetry into zero or infer it from unrelated counters.

### Durable usage remains separate

Today/week/30-day/all-time usage continues to use latest durable `TaskResult.ResourceSummary` per task. That is completed-result usage and remains separate from live current-epoch usage to avoid double counting.

## Waiting semantics

`INPUT_REQUIRED` has two distinct product meanings:

- pending durable Web Brain turn -> **Waiting for AI**, not Owner attention;
- agent `request_input` without a pending Web Brain turn -> **Needs your input**, actionable by Owner.

The Action Center must not treat an AI-turn wait as an Owner blocker.

## Interaction rules

- Live Operations updates from the 2-second observation loop.
- Event Stream retains a bounded recent history in the current browser session.
- Token sparkline and observed rate are derived only from successive live token samples.
- Workspace changes reset live samples/events and establish a new scoped baseline.
- Connection rows remain clickable to the full Connections view.
- Durable token summary remains clickable to Usage.
- Stale runtime polling immediately invalidates live connection claims.

## Accessibility and reliability

- Status is conveyed by text in addition to color.
- Live region text is concise; the entire dashboard is not a continuously-announced region.
- Reduced-motion preference disables unnecessary pulse animation.
- Non-interactive trends do not create keyboard stops.
- Keyboard focus and target-size contracts from v3 remain in force.
- All telemetry surfaces fail closed when their source is missing, stale or invalid.

## Acceptance

v4 is acceptable only when:

- no horizontal overflow exists at desktop or compact widths,
- sidebar remains structurally bounded to the viewport,
- Live Operations is the primary content surface,
- initial Event Stream shows real current connection/task snapshot state,
- a subsequent connection/task/token delta generates an event,
- active Web Brain token total changes after a durable turn response without waiting for TaskResult,
- Web Brain wait is shown as `Waiting for AI` and not Owner action,
- durable usage and live estimated usage are visibly distinct,
- missing live telemetry displays unavailable rather than fabricated zero,
- existing sandbox/security/connection/session/release gates remain green.
