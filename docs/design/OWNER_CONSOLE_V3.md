# MAR Owner Operations Console v3 — Interaction Contract

Status: ACCEPTED DESIGN BASELINE for implementation

## Design objective

The console is an owner operations surface, not a telemetry gallery. Every visible component must answer a user question, enable a decision, or lead directly to an action. Technical evidence remains available through progressive disclosure.

## Professional design principles

1. Answer-first: show the operational answer before raw state or evidence.
2. Action-first: every actionable condition names one next action and routes to the place where it can be completed.
3. Directed browsing: summary metrics are navigation affordances, not decorative cards.
4. Progressive disclosure: Answer -> Why / next action -> Technical evidence.
5. Semantic status: distinguish operational readiness, transport reachability, live activity/session state, stale telemetry, and unknown/unmeasured values.
6. Evidence-bound telemetry: never infer a measured value from configuration, desired state, missing counters, or stale data.
7. Calm hierarchy: urgent/actionable items visually outrank analytics and historical information; normal/empty states recede.
8. Accessible by default: WCAG 2.2 AA keyboard focus, target size, status messages, contrast, and responsive navigation.

## Global questions

The console must let the owner answer within seconds:

- Can MAR be used now?
- Does anything require my action now?
- What is MAR doing now?
- Are GPT and Claude usable, and are they actually active?
- Which workspace am I viewing?
- What did MAR use today / this week, and how much of that is actually measured?
- If something fails, what do I do next and where is the evidence?

## Navigation contract

### Overview — "What is happening and what needs me?"

Must show:
- aggregate operational state: Operational / Degraded / Action required / Unavailable;
- one-sentence explanation composed from real subsystem state;
- immediate owner actions only;
- active task summary;
- GPT/Claude operational summary;
- measured usage summary.

Clicks:
- system state / provider KPI -> Connections when recovery is required;
- active tasks KPI -> Tasks filtered by current workspace;
- action KPI -> Action Center on Overview;
- usage KPI -> Usage preserving workspace scope;
- provider row -> Connections and the matching provider card.

Must not show raw diagnostics or turn historical engineering failures into owner alarms.

### Tasks — "What is MAR doing and why is this task not done?"

A task must expose product-language stage first:
Preparing / Working / Checking result / Needs your input / Completed / Could not complete.
Technical state remains secondary evidence.

Task detail must answer:
- goal;
- current stage;
- whether owner action is required;
- what happens next;
- verification/integration result if available;
- evidence only on expansion.

### Workspaces — "Where may MAR work?"

Must show readiness, root, current HEAD, permissions, and actions. Workspace selector is global context for task/usage/attention data; runtime-wide connections are not hidden by workspace filtering.

### Connections — "Can GPT/Claude reach MAR, and are they active?"

Never collapse these concepts into one status:
- configuration / desired state;
- transport readiness/reachability;
- live activity/session truth;
- last activity;
- next recovery action.

Stateless transports must say session count unavailable. Link ready is not the same as an active session.

### Usage — "What did MAR actually measure?"

Must show today, calendar week, 30-day daily input/output/total and telemetry coverage. Missing counters are unknown/unmeasured, never zero. Provenance must state durable-result/local-bucket semantics and absent provider attribution when applicable.

### Diagnostics — "What is the technical evidence?"

Contains raw runtime/task inspection and deep evidence. It must not compete visually with normal owner flow.

## Attention contract

Overview Action Center includes only actionable-now owner/system items:
- sandbox preparation;
- provider recovery/configuration;
- unsupported workspace readiness;
- task INPUT_REQUIRED.

Broader engineering states (BLOCKED, FAILED, unverified result, unresolved risk, non-integrated result) remain visible in Tasks and Diagnostics but do not inflate the Overview owner-action counter by default.

## Visual system

- explicit dark operations theme with high-contrast surfaces and one restrained accent;
- 8px spacing rhythm, 12px card radius, subtle elevation/borders;
- sidebar is stable and readable at desktop widths, horizontal navigation only at compact widths;
- status uses text + shape/color, never color alone;
- one primary action per problem context; secondary/technical actions are visually quieter;
- large KPIs are clickable only when they have a navigation payoff;
- empty states are compact and calm, not large dead zones.

## Acceptance

1. Desktop 1366–1920 px: sidebar labels are fully readable with no clipping or horizontal overflow.
2. At 900 px and below: navigation becomes horizontal and content uses full width.
3. Overview system state cannot say Operational/Healthy when sandbox is unavailable or a primary provider has an actionable error.
4. Overview owner-action count excludes historical BLOCKED/FAILED engineering tasks unless the task is INPUT_REQUIRED.
5. Every overview KPI routes to a meaningful destination.
6. GPT/Claude rows distinguish ready/connected/session unavailable/live activity.
7. Task list uses product-language stage; exact technical state remains accessible.
8. Runtime poll failure displays stale truth and never preserves old Connected as current.
9. Keyboard focus remains visible/not obscured; interactive targets are at least 44px in this UI.
10. No fake token/session/provider metrics are introduced.
