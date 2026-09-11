# MAR V1.1 — Final Release Acceptance

**Status:** `PRODUCT_ACCEPTED`  
**Self-hosting:** `SELF_HOSTING_READY`  
**Owner acceptance date:** `2026-09-12`  
**Accepted implementation HEAD:** `a6fa0279631c7a8f05f484a5bba7b20b73e0188f`  
**Release-candidate record:** `docs/release/MAR_V1_1_RELEASE_CANDIDATE.md`

## Owner decision

The Owner explicitly approved **MAR V1.1** after real-use of the integrated candidate. This is the explicit Owner acceptance required by the release-candidate closeout and by `docs/handoff/CURRENT.md`; it closes the remaining product gate and establishes `PRODUCT_ACCEPTED`.

The acceptance applies to the integrated MAR V1.1 experience at the implementation HEAD above, including the bounded context/cognition architecture, six-tool canonical public MCP surface with callable-but-unlisted legacy compatibility aliases, React 19 + TypeScript + Vite Owner Console, workspace/task interaction fixes, independent GPT/Claude connection surfaces, Claude Web ready-state link actions, durable verification/integration authority, and the already-proven bounded self-hosting path.

## Release decision

MAR V1.1 is accepted for routine owner use. The previously demonstrated self-hosting path is promoted to `SELF_HOSTING_READY`; routine MAR development should use MAR itself as the normal control path, with ChatCode/direct operator coordination reserved for bootstrap, recovery, or cases where MAR itself is unavailable.

No remaining production-code blocker is known for V1.1. A stable named tunnel/custom hostname, additional visual refinement, new telemetry, provider additions, and other improvements are post-V1.1 backlog. They do not reopen this release unless a concrete regression invalidates the accepted behavior.

## Evidence boundary

Engineering evidence remains the evidence already sealed in the release-candidate and current handoff records: current-head clean Git before this documentation closeout, integrated Owner Console corrections, complete sequential Go regression/vet/build gates, UI build and browser interaction checks, bounded public MCP discovery, durable self-hosting verification/integration, and resource/fencing safeguards. This final file records the Owner decision; it does not alter runtime authority, transport credentials, sandbox policy, database state, or executable behavior.

The commit containing this file is the documentation-only release closeout commit. The accepted implementation remains the HEAD recorded above.
