# DR-0002: Owner, Tech Lead Web, CADS, MAR and Console responsibility boundary

Status: Accepted
Date: 2026-09-07
Scope: Product / process / UI boundary

## Context

Owner hands-on review and the Astra audit showed requirement drift: the initial Owner Console made a non-IT owner choose project base revisions, write engineering acceptance criteria, choose verification profiles and authority, even though MAR's frozen Product Brief says the normal flow starts with intent discussed in ChatWeb and a Goal Contract produced by the client/Tech Lead.

The owner explicitly wants to discuss ideas with a Tech Lead Web agent, compare product options, answer only missing owner-level decisions, let the Tech Lead apply CADS, then let MAR execute the resulting bounded work to a finished product.

## Decision

Responsibility is:

`Owner -> Tech Lead Web -> CADS -> MAR -> Worker(s) -> verified/integrated product -> Tech Lead Web -> Owner feedback/acceptance`

### Owner

Provides product intent, preferences, trade-offs, consequential authority and real-use feedback. The normal flow must not require the owner to design Git, verification, sandbox, API or worker mechanics.

### Tech Lead Web

Is the primary conversational/product interface. It must proactively discover material engineering/product concerns the owner may not know to mention, explain trade-offs in owner language, apply CADS, form the bounded Goal Contract, define criterion-specific scenario/oracle checks, coordinate work and explain results.

### CADS

Owns development method: product goal framing, Critical User Journey, information/workflow/interaction design, architecture reasoning, engineering concerns, acceptance design, convergence and owner UAT discipline. CADS does not become a second runtime ledger/dispatcher when MAR owns the execution.

### MAR

Owns execution mechanism and durable truth: project/task/workspace/attempt state, capability admission, sandbox/process authority, scheduling/resource bounds, safe parallel execution, criterion-bound evidence persistence, recovery and serialized integration.

### Owner Console

Is a secondary local operations/setup surface. Normal functions are connection readiness/setup, Projects, project permissions/capabilities, current/recent Work, attention/blockers, result/evidence inspection, usage that MAR can truthfully measure, system health and Owner feedback. Engineering contract fields belong under Advanced diagnostics/fallback, not the primary owner journey.

## Connection rule

Client capability must be represented truthfully. A local stdio MCP server may be directly usable by local MCP clients such as desktop integrations, while cloud ChatWeb clients may require a remote MCP endpoint/tunnel and plan/workspace support. MAR must not label a client `Connected` or autonomous merely because a configuration hint exists.

## Acceptance consequences

- Zero manual Tech Lead <-> worker forwarding is a hard product criterion.
- Normal owner flow requires no developer vocabulary.
- Human questions are reserved for human decisions.
- Owner feedback is bound to the exact task/result candidate.
- Technical PASS never substitutes for Owner Product Acceptance.
- Console visual polish cannot precede correction of workflow/continuity/setup blockers.

## Revisit when

Revisit only if owner real-use evidence or a client-platform constraint proves this responsibility split unusable. Client-specific connection mechanisms may evolve without changing this boundary.
