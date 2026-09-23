# MAR Architecture Stability Policy

**Status:** ACCEPTED — FORWARD STABILITY POLICY
**Date:** 2026-09-22
**Applies to:** all post-`830adf5` MAR architecture work

## Frozen target

The forward architecture is:

```text
Owner / ChatWeb model
        |
        v
CADS intent / design / acceptance
        |
        v
MAR productivity / compatibility surface
        |
        +------------------------------+
        | ordinary bounded work        | governed runtime properties needed
        v                              v
direct bounded execution           MAR trust kernel
        |                              |
        +---------------+--------------+
                        v
               tools / executors
                        |
                        v
                     candidate
                        |
                        v
              independent acceptance
```

Default cognition remains outside MAR. ChatWeb is the default planning/reasoning brain for the owner workflow.

MAR is not required to become a second internal AI agent framework. The MAR product may include a productivity/convenience layer inspired by proven ChatCode capabilities; the governed trust kernel remains conditional and authoritative only for the runtime properties it enforces.

## Stable responsibility split

### ChatWeb / external cognition

Owns:
- understanding owner intent;
- planning and reasoning;
- implementation decisions;
- selecting/directing ordinary tools;
- explaining results.

### CADS

Owns:
- intent/design obligations;
- product invariants;
- acceptance model;
- property-based route selection;
- independent product acceptance.

### MAR

At the product boundary MAR may own useful local-development productivity surfaces such as project discovery, context retrieval, research helpers, adapters, task convenience, and ChatCode-inspired workflow features when they materially improve the owner journey.

Its trust kernel owns governed runtime properties when explicitly selected:
- authority and revocation;
- OS/process/filesystem isolation;
- stale-writer fencing;
- requested resource governance;
- exact candidate identity;
- verification/evidence binding;
- crash-safe promotion/integration;
- recovery/reconciliation.

Productivity features do not become kernel authority merely because MAR ships them. Provider/model/session/subagent/context strategy remains replaceable rather than a prerequisite for kernel correctness.

## Direct path

Ordinary bounded development may use MAR's ChatCode-replacement productivity surface without entering the governed task lifecycle:

```text
ChatWeb -> MAR productivity surface -> bounded tool execution -> candidate -> acceptance
```

No governed MAR task lifecycle is required unless a governed runtime property is actually needed. This preserves the fast happy path while removing ChatCode as a required dependency.

## MAR governed path

When governed execution is selected:

```text
ChatWeb -> accepted execution intent -> MAR kernel -> deterministic tool/executor
```

The executor may be replaceable, but adding a second AI reasoning layer is not the default architecture.

The external-harness seam added at `c94d359` remains valid because it proves MAR can host a replaceable executable without owning cognition. It must not be interpreted as a requirement to insert OMP/Codex/Claude/Gemini reasoning beneath ChatWeb.

AI-agent harnesses are optional delegated-worker experiments only. They require explicit goal authority and must not silently become the normal MAR path.

## Change rule

From this point, architecture changes are incremental by default.

Allowed without reopening architecture:
- bounded bug fixes;
- measured performance improvements;
- removal/delegation of compatibility cognition;
- simplification of lifecycle/state;
- UX improvements;
- narrow adapters that replace existing responsibility;
- verification/recovery hardening.

Not allowed by default:
- a new MAR planner;
- a new MAR model/provider layer;
- another durable session lifecycle;
- multi-agent orchestration inside MAR;
- replacing the kernel with a new framework;
- broad architecture rewrites justified only by conceptual elegance.

## Architecture reopen gate

The frozen target may be reopened only by a new explicit decision record with representative runtime evidence that:

1. a required invariant or product journey cannot be satisfied by the current split;
2. the deficit is not a local implementation bug or missing bounded capability;
3. the proposed change has a measurable benefit over incremental repair;
4. migration preserves current accepted safety evidence; and
5. the proposal states what existing responsibility/code will be deleted or retired.

Research alone is insufficient to reopen the top-level architecture.

## Improvement loop

All normal improvement work follows:

```text
measure -> choose one bounded deficit -> patch/delete -> verify -> benchmark -> keep or revert
```

A slice is successful only if it improves a measured property without expanding architecture responsibility unnecessarily.

Preferred direction:
- fewer states;
- fewer durable lifecycles;
- less model/provider code in MAR;
- lower block/cancel latency;
- stronger evidence per governed task;
- smaller kernel surface.

## Current consequence

Do not continue OMP authentication/model integration as the default MAR path.

The live OMP binary probe remains useful evidence that a real third-party executable can run inside the MAR sandbox. No further OMP work is required until an explicit delegated-worker goal asks for it.

Next MAR work should reduce compatibility cognition/lifecycle while preserving current behavior, one bounded deletion slice at a time.
