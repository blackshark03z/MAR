# MAR Conditional Execution Kernel Boundary

**Status:** ACCEPTED — FORWARD ARCHITECTURE AMENDMENT  
**Date:** 2026-09-22  
**Basis:** direct ChatCode benchmark, CADS DR-0013, MAR Hybrid Simplification evidence, and observed BLOCKED/cancel/resource friction.  
**Decision type:** responsibility reduction; no V1 rewrite.

## Decision

MAR is an **optional governed execution kernel**, not the mandatory execution path for every CADS development task.

CADS owns product intent, design obligations and product acceptance. A compatible coding harness may execute ordinary bounded work directly. MAR is entered only when the accepted Goal requires runtime properties that the selected direct substrate cannot safely provide.

Current governed-property vocabulary shared with CADS:

- `isolated-mutation`;
- `durable-recovery`;
- `concurrent-writer-fencing`;
- `durable-execution-authority`;
- `resource-governance`; and
- `crash-safe-integration`.

MAR does not choose whether a product Goal needs these properties. Design/Tech Lead authority does. MAR must only claim a property it demonstrably enforces.

## Lifecycle scope

MAR task/attempt/workspace/resource lifecycle has meaning **only after a task has entered governed execution**. It is not a universal CADS development lifecycle.

Therefore a normal direct task may legitimately have no MAR task ID, workspace, run epoch, BLOCKED state, checkpoint, or integration record. Product completion still requires the applicable CADS acceptance oracle; absence of MAR state is not missing evidence when MAR was not required.

## Kernel responsibilities

Long-term MAR core is limited to governed-runtime responsibilities:

1. authority envelope and revocation;
2. OS/process/filesystem isolation and stale-writer fencing;
3. shared resource governance when requested;
4. consequential-effect fencing/reconciliation where MAR is selected to own the effect;
5. exact candidate/revision identity;
6. verification/evidence binding for MAR-governed candidates;
7. expected-head/CAS-style canonical integration when MAR owns promotion; and
8. crash/recovery/reconciliation for the governed execution.

## Harness responsibilities

The default owner workflow has one primary cognition layer: **ChatWeb**. MAR must not insert another AI reasoning layer by default.

The coding harness is a replaceable, non-authoritative executor. In the normal ChatWeb-led architecture this means deterministic tools/executors beneath the accepted execution intent. Provider/model selection, credentials, reasoning/session topology, context management, subagents, coding-tool ergonomics, LSP/DAP and similar agent mechanics are not target MAR-kernel responsibilities.

An external AI-agent harness may exist only as an explicitly delegated-worker mode. Its existence does not change the default ChatWeb-brain architecture and must not cause MAR to regain provider/model/session ownership.

Existing provider mode, Web-brain relay, Project Brain/cognition projection and related session surfaces remain supported compatibility behavior for current releases. This amendment does **not** authorize their immediate deletion. It closes them to expansion by default.

Any future harness integration must pass a deletion test:

> Adding the harness must let MAR delete or retire equivalent execution-harness responsibility while preserving kernel invariants.

If integration only adds another session lifecycle, compatibility layer or orchestration hop, do not integrate it.

Durable MAR truth must not require a harness-specific model ID, provider identity, prompt format, subagent topology or session ID in order to understand authority, candidate identity, evidence, integration or recovery.

## Migration rule

1. Preserve current V1/V1.x behavior as historical/release compatibility.
2. Keep provider/session/cognition/productivity features outside kernel authority by default. They may be retained, ported, rewritten, or improved in MAR's productivity layer when representative workflows show material value.
3. Measure responsibility/code paths before deletion.
4. Introduce replaceable harness boundaries where they improve portability or permit simpler implementations without regressing useful workflow capability.
5. Apply REUSE BEFORE DELETE: prefer a proven replacement or measured evidence of non-value before retiring useful compatibility/productivity behavior.
6. Re-run safety, recovery and exact-candidate evidence gates for every kernel reduction.

## Acceptance

This boundary is effective when:

- CADS can route ordinary work directly without creating MAR state;
- MAR documentation no longer describes itself as the mandatory coding path;
- MAR lifecycle is explicitly scoped to governed tasks;
- cognition/provider/session work is frozen as compatibility rather than forward kernel scope;
- existing MAR release/safety invariants remain unchanged by this documentation-only slice; and
- subsequent implementation slices measure deleted responsibility and reduced block/cancel/latency friction instead of merely adding adapters.

## Revisit

Expand MAR only when representative evidence shows a required governed property is missing or a commodity substrate cannot enforce it. Shrink MAR when a replaceable lower layer can supply the same property without weakening authority/evidence invariants.
