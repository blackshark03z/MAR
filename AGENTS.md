# MAR implementation instructions

Before planning or implementation, read:

1. `docs/architecture/MAR_V1_Architecture_FROZEN/00_ARCHITECTURE_FREEZE_RECORD.md`
2. `docs/architecture/MAR_V1_Architecture_FROZEN/10_IMPLEMENTATION_ENTRY_CONTRACT.md`
3. the architecture document relevant to the current subsystem.

The MAR V1 architecture is FROZEN.

Do not redesign task lifecycle, execution fencing, workspace isolation, integration serialization, durable state, worker authority, or resource boundedness unless concrete implementation/benchmark evidence proves a frozen invariant insufficient.

Prefer small vertical slices with tests and revision-bound evidence.

When the CADS skill library is available, route work by current event without creating lifecycle state:

- first contact / stale context -> Project Cold-Start;
- new or materially changed Goal / missing acceptance -> Product Goal Framing;
- normal implementation under an established Goal -> Goal Execution;
- bug / regression / unexpected runtime behavior -> Systematic Debugging;
- new or materially changed user journey / navigation / discoverability -> User-Facing Workflow;
- new or materially changed screen / component / interaction / responsive layout -> Frontend Design;
- before user-facing Product Acceptance -> UI Quality Review, then Product Acceptance;
- workspace bloat / competing worklines / Goal closure residue -> Workspace Hygiene.

For user-facing work, frame the Product Goal and Critical User Journey first, resolve workflow/information architecture before visual implementation, verify the real rendered surface, and reserve `PRODUCT_ACCEPTED` for explicit Owner real-use acceptance. Engineering PASS, clean Git, build hashes, or agent reports are not Owner acceptance.

CADS is a development standard/guardrail, not MAR runtime architecture. Do not copy or create a second canonical architecture authority: MAR V1 architecture remains canonical only under `docs/architecture/MAR_V1_Architecture_FROZEN/`. `TASK.md` carries current Goal context and `docs/decisions/` carries accepted material rationale.

No push or deploy by default.
