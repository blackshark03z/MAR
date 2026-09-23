# MAR ChatCode Replacement Target

**Status:** ACCEPTED PRODUCT-BOUNDARY SUPPLEMENT  
**Date:** 2026-09-23  
**Scope:** MAR product surface; no kernel redesign.

## Objective

MAR must be usable directly from supported external cognition clients without requiring ChatCode as an intermediate runtime, gateway, workspace manager, coding authority, or publication authority.

Target:

```text
ChatGPT / Claude / future external cognition
                  |
                  v
            MAR MCP edge
                  |
                  +-- bounded project research
                  |
                  v
          MAR governed execution
                  |
                  v
             Windows / Git
```

ChatCode may remain temporarily available as a bootstrap/compatibility transport while direct MAR attachment is unavailable in a particular already-open client session. It is not part of the target architecture.

## Responsibility boundary

External cognition owns reasoning.

MAR owns the enforceable local boundary:

- project attachment and bounded read-only discovery;
- immutable Goal/authority input for mutation-capable work;
- attempt/run-epoch fencing;
- constrained Windows execution;
- exact candidate identity;
- independent verification;
- publication authorization;
- expected-head Git CAS;
- crash/restart reconciliation.

MAR may copy, port, adapt, or reimplement proven ChatCode ideas when they materially improve real workflow capability, speed, or ergonomics. Those capabilities belong in a productivity/convenience layer above the trust kernel unless they are themselves enforceable safety invariants. Matching feature count is not a goal; preserving and improving useful capability is.

## Initial replacement surface

Keep the canonical MCP discovery surface compact:

- `project`
- `submit`
- `task`
- `control`
- `brain_turn`
- `brain_respond`

The `project` tool is the only pre-submit repository-research domain surface. It must support enough bounded discovery for a Web Tech Lead to understand an arbitrary attached local repository without ChatCode:

- `context`
- `attach`
- `list`
- `read` with optional bounded line range
- `search` with bounded text matches

The current six-tool surface is the initial direct-MAR surface, not a permanent ceiling. Public productivity capabilities may be added, composed, or rewritten from proven ChatCode patterns when representative workflows show material benefit, provided they cannot bypass immutable Goal/authority, isolation, candidate identity, verification, publication authorization, or recovery. Low-level unrestricted mutation does not become kernel authority merely for parity.

## Replacement acceptance proof

MAR replaces ChatCode for the normal workflow only when a fresh supported Web client can, with ChatCode absent from the path:

1. connect directly to MAR;
2. attach or select an arbitrary local repository/path;
3. inspect context and efficiently list/search/read relevant source;
4. submit one bounded Goal;
5. complete external-cognition turns while the isolated worker performs permitted coding actions;
6. produce an exact candidate;
7. verify the exact candidate independently;
8. publish through MAR's authorized expected-head Git transition;
9. reconnect/restart and recover durable status/result truth;
10. finish without invoking a ChatCode plugin, gateway, process, workspace, or authority surface.

This proof supplements the architecture-freeze exit criteria; it does not weaken any kernel invariant.

## REUSE BEFORE DELETE

ChatCode is an allowed reference implementation for product capability, UX, context retrieval, research, workflow ergonomics, and execution convenience. MAR may copy the idea, port it, rewrite it cleanly, or improve it rather than rediscovering the same solution from scratch.

The migration rule is: **preserve proven capability -> port/rewrite/adapt -> benchmark in representative workflows -> improve -> delete old machinery only after a replacement is proven or measured evidence shows the capability is unnecessary.** Existing useful Project Brain, context, research, provider/session, or convenience functionality is not deleted merely because it is outside the trust kernel; it may live in the productivity layer with no authority over kernel safety decisions.

This amendment is a documentation/product-boundary decision only. It does not weaken the architecture-closure safety invariants or the seven bounded kernel fixes.

## Stop rule

Do not add features solely for vanity parity, but do not delete useful behavior solely to make MAR smaller. When ChatCode already demonstrates a useful capability, prefer reuse/porting and measured improvement before redesign. Prefer extending an existing typed domain tool when that keeps the surface coherent; add a new bounded surface when real workflow evidence justifies it.
