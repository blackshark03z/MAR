# Goal

Provide the smallest coherent owner-facing MAR surface needed for hands-on V1 acceptance: the owner can enter the product, choose the target project, submit one bounded Goal, understand what MAR is doing, inspect the verified/integrated result and evidence, and make the final accept/reject judgment without the UI bypassing MAR's frozen MCP/runtime authority.

# Critical User Journey

Launch the local MAR owner surface -> select or identify a registered project -> enter/review one bounded Goal Contract -> start the task -> see meaningful task state and required action -> handle any real input/brain dependency without losing orientation -> inspect result/diff/evidence -> accept or reject the owner experience.

# Acceptance

1. A real rendered owner-facing surface exposes one obvious primary path from project selection to Goal submission.
2. The surface uses MAR's bounded MCP/task control plane as authority; it does not create a parallel task database, direct coding loop, direct repository mutation path, or alternate integration authority.
3. The owner can distinguish ready, running/waiting, input-required/blocked, verifying/integrating, complete and failed/cancelled outcomes when those states occur, with a clear next action.
4. The final surface shows the task outcome, changed areas/revision, verification/integration status and evidence detail needed to judge the result.
5. Web-brain/provider dependencies are explicit in the journey; the UI must not pretend autonomous cognition is available when the selected brain transport is not connected/configured.
6. The representative Owner UAT uses a real MAR project/Goal on the identified product build and reaches a result or a concrete product blocker.
7. Only explicit Owner real-use approval may establish PRODUCT_ACCEPTED / SELF_HOSTING_READY.

# Acceptance Fixture / Golden Input

Project: MAR itself on canonical `master`.
Representative Goal: one bounded documentation or low-risk maintenance Goal that changes only explicitly allowed files and can be independently inspected after MAR verification/integration.

# Non-goals

- No MAR V1 architecture redesign.
- No new task lifecycle, coordination database, worker authority, integration path, or resource-governor design.
- No multi-user/SaaS/cloud concerns.
- No decorative dashboard, design-system project, or open-ended polish loop.
- No claim of V1 stable before the owner personally runs the journey.

# Constraints

- Frozen MAR architecture/invariants remain authoritative.
- Owner UI is a client/surface over MAR, not a replacement runtime.
- Prefer the minimum local implementation and existing Go/MCP dependencies before adding frameworks.
- UI findings are limited to current-Goal BLOCKER/HIGH issues; cosmetic alternatives are deferred.
- No push/deploy by default; Git/source and identified runtime evidence remain authoritative.

# Material Decisions

- CADS is adopted as MAR's development operating standard/routing layer; it does not become a MAR runtime subsystem.
- For user-facing work: Product Goal -> Critical User Journey -> User-Facing Workflow -> Frontend Design -> rendered UI Quality Review -> Owner Product Acceptance.
- The frozen architecture remains in `docs/architecture/MAR_V1_Architecture_FROZEN/`; do not create a second root architecture authority merely to satisfy a generic bootstrap template.
- Owner-facing UI must stay on the MCP/task-control side of the frozen topology and must surface, not hide, cognition/brain transport dependencies.

See `docs/decisions/0001-cads-owner-product-workflow.md`.

# Progress

- Technical self-hosting T1-T17 and a MAR-on-MAR engineering run have passed.
- CADS routing, Product Goal/CUJ, Decision Record and the minimum loopback Owner Console are implemented locally.
- Owner Console targeted tests and a full sequential repository test/vet/build gate pass; headless Chrome rendered checks pass at desktop 1440x1000 and narrow 390x844.
- Brain readiness is surfaced before Run. This machine currently has neither a direct ChatGPT->MAR MCP bridge nor provider-mode environment configuration, so autonomous Owner UAT remains concretely blocked on brain transport setup rather than hidden UI behavior.
- Owner hands-on UAT has not yet occurred; MAR V1 is not PRODUCT_ACCEPTED / SELF_HOSTING_READY.

# Discoveries / Blockers

- Current ChatGPT session is not directly connected to MAR's local MCP stdio endpoint; the previous Web-brain owner run used an internal ignored harness. This proves technical flow, not owner entry UX.
- Provider-mode UAT is not currently configured on this machine (`OPENAI_API_KEY`, provider base URL and model are absent); the owner surface must show brain readiness before Run instead of allowing a hidden late stall.
- A generic CADS bootstrap would create a second root `ARCHITECTURE.md`; MAR intentionally keeps its existing frozen architecture authority instead.
- ChatCode raw-command jobs can retain descendant processes after `Start-Process`, and the cancellation supervisor may be unavailable. Render probes must therefore own launch + review + process-tree cleanup inside one bounded command; do not leave detached UI probes running.
- Do not infer a usable browser automation dependency from `npm root`; Playwright was not actually installed. Prefer already-present headless Chrome/Edge for rendered checks unless the project explicitly carries a browser automation dependency.

# Next Safe Action

Run the full sequential repository gate on the bounded CADS/Owner UI slice, inspect the final diff, then commit/push only if clean. After that, launch the owner surface for the owner's first hands-on UAT; V1 acceptance remains pending until the owner personally completes or rejects the journey.
