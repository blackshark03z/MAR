# Goal

Close MAR V1 product usability after the Astra audit without reopening the frozen runtime architecture. The owner is non-IT and works primarily through a Tech Lead Web conversation. CADS helps the Tech Lead frame requirements, Critical User Journey, engineering concerns, acceptance criteria and verification oracles. MAR executes the accepted bounded work and preserves durable task/evidence/integration truth. The Owner Console is the local setup, connection, project, permission, continuity, usage/health and feedback surface; it is not the primary place where the owner authors engineering contracts.

# Primary Product Journey

Owner describes intent in normal language -> Tech Lead Web understands the product context and asks only owner-level questions -> Tech Lead applies CADS to produce a bounded Goal Contract with criterion-specific scenario/oracle checks -> Tech Lead submits through MAR MCP -> MAR executes autonomously in isolated workspaces, including safe parallelism where contracts permit -> MAR verifies criterion-specific evidence and serializes integration -> Tech Lead explains the result in normal language -> Owner uses the product and records accept/reject/feedback against the exact candidate/result.

# Owner Console Journey

Launch MAR Console -> see connection readiness -> add/select a local project and review supported permissions -> reopen current/recent work after restart -> inspect tasks that need attention -> inspect result/evidence when useful -> record Owner feedback. Advanced diagnostics may expose task IDs, revisions, verification profiles and raw evidence, but normal owner flow must not require that vocabulary.

# Hard Product Acceptance

1. Zero manual forwarding: the owner does not copy prompts/reports between Tech Lead Web and coding workers during the representative Goal.
2. Zero required developer vocabulary in normal flow: the owner is not required to choose Git/worktree/base revision/verification profile/run epoch or construct engineering acceptance/oracles.
3. Human attention only for human decisions: MAR/Tech Lead do not ask the owner for facts that can be derived from repository/runtime state.
4. Criterion-specific evidence: no acceptance criterion may become PASS without a bound scenario/oracle and an actual machine observation. V1 typed engineering oracles are `output_contains:<literal>` and `file_contains:<relative-path>:<literal>`; a merely green generic command is insufficient. Missing/unsupported observation is UNVERIFIED.
5. Safe integration: owner/editor work appearing during integration is preserved; uncertainty blocks instead of destructively synchronizing.
6. Connection/setup truth: ChatGPT, Claude/local MCP and provider modes show their real capabilities/readiness and do not claim connected/autonomous state without evidence.
7. Project continuity: the owner can add a supported project, see its readiness/permissions, and reopen current/recent tasks after MAR/Console restart without remembering task IDs.
8. Owner feedback is durable and bound to the exact task/result candidate being judged.
9. Technical release gate remains test/vet/build/diff-check plus the frozen T1-T17/recovery/security acceptance relevant to the current revision/environment.
10. Only explicit Owner real-use approval may establish PRODUCT_ACCEPTED / SELF_HOSTING_READY.

# P0 / P1 Remediation Status

## Closed in safety checkpoint `fb4c8e94e1931941b076140dd8adfd646445270a`

- P0 HTTP mutation boundary: loopback Host + startup-session token + Origin/Sec-Fetch-Site + JSON-only before MCP mutation.
- P0 integration owner-work race: no destructive `reset --merge`; staged/unstaged/untracked owner work is preserved or integration blocks for safe sync/review.
- P0 verification semantics checkpoint introduced PASS / FAIL / UNVERIFIED and removed blanket profile PASS. A post-checkpoint Astra follow-up found that prose oracles could still be attached to unrelated green commands; the current correction binds PASS to typed `output_contains` or sealed-candidate `file_contains` observations and persists the actual observation in evidence.
- `go-docs` admission is docs-only by candidate changed-path identity.
- P1 capability admission: unsupported network/remote-Git/deploy authority and unsupported non-Go project/profile combinations fail before worker dispatch.
- Full repository convergence gate passed before checkpoint.

## Current P1 product work

- Claude Web remote MCP is now the primary Web connection path. Secure token-bound Streamable HTTP, temporary outbound tunnel lifecycle, public-readiness health probe, Start/Stop/Copy URL Console controls, and observed `initialize`/`tools/list` telemetry are implemented without a second coordinator. Public internet E2E has listed exactly the nine MAR task tools through a real `trycloudflare.com` URL. Claude Desktop is optional only.
- Add Project + project readiness + durable project-level local file/local Git policy implemented and enforced by preflight.
- Current/recent task continuity + attention queue implemented from the same SQLite task truth.
- Durable Owner feedback implemented and bound to exact result/candidate; acceptance is rejected before a verified integrated result exists.
- Console normal flow no longer requires Owner-authored acceptance/profile/priority/authority; those remain engineering/MCP concerns.
- MAR-measured input/output/total model usage is carried into durable result resource summaries; hidden client usage is not guessed.
- Focused API/store/service/orchestrator tests PASS, including restart persistence, unauthorized mutation rejection, recent work continuity and premature acceptance rejection.
- Real desktop/narrow rendered Console has been exercised on the actual MAR UI + SQLite/MCP runtime.
- Post-reboot sandbox preparation is now surfaced as an Owner Console action: one **Prepare sandbox** action invokes the existing host preparation through Windows UAC and re-checks readiness automatically; the owner no longer needs to open an Administrator terminal for the normal flow. A real UAC preparation probe passed on the current Windows host.
- Final clean-candidate remote-Web full Goal, final release gate and Owner real-use UAT remain.

# Non-goals

- No MAR V1 architecture redesign unless evidence proves a frozen invariant insufficient.
- No multi-user/SaaS/RBAC/cloud worker platform.
- No automatic push/deploy by default.
- No decorative dashboard or open-ended polish loop.
- No fake token/client usage metrics where the client/provider does not expose measurement.
- No second task/coordination database outside MAR SQLite.

# Constraints

- Frozen architecture under `docs/architecture/MAR_V1_Architecture_FROZEN/` remains canonical.
- CADS is a development/Tech Lead standard, not a MAR runtime subsystem.
- SQLite remains execution coordination truth; CADS `TASK.md` is intent/progress context only and must not duplicate runtime state as authority.
- Owner Console setup/project/feedback mutations must use application-level validation and remain inside the single-owner local trust boundary.
- UI review fixes BLOCKER/HIGH journey defects before cosmetic polish.

# Material Decisions

- DR-0001 remains valid for adopting CADS without changing runtime authority.
- DR-0002 establishes the post-Astra responsibility boundary: Owner -> Tech Lead Web -> CADS -> MAR -> Worker/Product, with Owner Console as secondary operations surface.

# Current Safe Action

Converge the Claude-Web remote MCP slice with rendered Console review and full repository test/vet/build/diff-check, then checkpoint it separately. Build a clean candidate from that commit and run one representative Goal end-to-end through the public remote MCP URL (submit -> Web brain turns -> worker -> typed verification -> integration -> result), not merely tools/list. After that, launch the clean Owner Console for the owner to connect Claude Web and perform the final human journey/UAT. ChatGPT Web remains capability/plan dependent and must not be falsely labeled connected.
