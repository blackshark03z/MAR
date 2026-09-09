# MAR Owner Operations Console v5 — Light + Multi-Flow Operations Contract

## Product intent

Owner Console v5 is a light, modern operations surface designed for one owner monitoring MAR across multiple workspaces, execution flows and AI connection routes. The Console must answer operational questions before exposing implementation detail.

## Questions the UI must answer

1. Is MAR usable right now, and what is degraded?
2. How many execution flows are live now?
3. How many authoritative MCP sessions are observable, and on which routes?
4. How many configured/reachable routes exist for GPT and Claude?
5. Which flow is Working, Waiting for AI, Checking result, Needs owner input, or failing?
6. Which workspace owns each live flow?
7. What live token usage is actually observable for the current execution epoch?
8. What changed in the last few seconds: route state, request count, session count, task stage, token delta?
9. What must the Owner do now?

## Semantic model — do not collapse these numbers

- **Route**: a configured transport/path such as OpenAI Secure Tunnel, GPT Server URL fallback or Claude Web.
- **Authoritative session**: a session identity reported by a transport that actually has session lifecycle semantics. Stateless transports remain unknown; the UI may show `5 + ?`, never invent a zero for the unknown route.
- **Live execution flow**: one active MAR task execution epoch. Flow identity is `task_id + run_epoch`, so retries/replacements never masquerade as the same live flow.
- **Provider live activity**: recent/active route or session activity. It is not the same as route readiness.
- **Live tokens**: completed Web Brain turn usage for the exact active task/run epoch, sourced from durable `web_turns`. It is labelled estimated (`~`) because it is runtime budgeting data, not provider billing.
- **Durable usage**: completed TaskResult resource summaries. It remains separate from live active-turn telemetry.

Provider attribution for Web Brain execution flows is currently unavailable because durable WebTurn records do not store the responding connector identity. v5 must say `Web Brain durable estimate` / unattributed instead of assigning the flow to GPT or Claude without evidence.

## Information architecture

Sidebar:

- Overview
- Live Operations
- Tasks
- Workspaces
- Connections
- Usage
- Diagnostics

### Overview

Executive operational answer only: aggregate health, live-flow count, authoritative session count, route readiness, owner-action count and durable tokens today. Clicking a realtime metric opens Live Operations or Connections rather than expanding another dashboard card.

### Live Operations

Primary realtime surface. It contains:

- live execution-flow count;
- Waiting-for-AI count;
- authoritative session summary;
- reachable-route summary;
- active-turn token total/rate;
- separate GPT and Claude live zones, each showing route readiness, authoritative sessions, requests, live paths and recent provider events;
- one shared live-token monitor on the same screen (total, input, output, rate and trend);
- execution-flow table keyed by task + run epoch;
- provider-specific route/session detail inside each zone instead of one mixed route table.

### Connections

Configuration and recovery actions. It retains the three distinct states: configuration, transport readiness, live activity/session truth.

### Usage

Historical/durable accounting only. It must not double-count live active-turn estimates.

## Visual system

v5 intentionally uses a light theme independent of OS preference:

- page background `#F6F8FB`;
- white primary surfaces;
- slate text hierarchy;
- blue `#2563EB` as the primary accent;
- emerald success, amber warning, red error;
- soft borders/shadows instead of dark card chrome;
- tables for scalable multi-flow information instead of one card per flow;
- compact summary rails for high-level answers;
- Overview stays intentionally terse; explanatory copy is moved to titles/details rather than repeated in every panel;
- GPT and Claude remain visually separate zones so multiple routes/sessions can be scanned without provider mixing.

The visual system should remain calm when the number of flows grows. Tables scroll inside bounded containers; page-level horizontal overflow is not allowed.

## Realtime contract

The operational polling loop remains non-overlapping at 2 seconds. Each fresh cycle may update route/session/task state and append bounded event deltas. Initial snapshot events are seeded immediately so the Event Stream is not blank on first load.

Missing observability remains missing. Examples:

- stateless Secure Tunnel session count => unavailable;
- provider-mode per-turn live token source absent => unavailable;
- Web Brain live usage => estimated and explicitly labelled;
- provider attribution absent => unavailable.

## Accessibility

Retain WCAG 2.2 AA-oriented keyboard/focus/target/status behavior from v3/v4. Live regions are bounded and must not announce every decorative animation. Color is never the only status cue.

## Acceptance

- light visual tokens render as the default theme;
- Overview exposes distinct live flows, sessions and routes;
- Live Operations is a separate navigable view;
- `run_epoch` is present in Owner task telemetry and flow identity;
- multiple routes do not collapse into one provider connection count;
- unknown sessions remain unknown (`+ ?`/unavailable), not zero;
- Waiting for AI remains distinct from Owner input;
- live Web Brain token estimates remain separate from durable Usage;
- targeted Go/JS/diff gates pass;
- full repository regression/vet/build and revision-bound release gate pass before ENGINEERING_STABLE is claimed;
- `Tunnel_api.txt` remains untouched/untracked.
