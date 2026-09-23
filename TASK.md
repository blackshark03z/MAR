# Current SoT — 2026-09-23

## Forward architecture — FREEZE AFTER BOUNDED FIXES

Canonical stability authority: `docs/architecture/MAR_ARCHITECTURE_STABILITY_POLICY.md`. Canonical kernel boundary: `docs/architecture/MAR_CONDITIONAL_EXECUTION_KERNEL.md`. ChatCode-replacement product boundary: `docs/architecture/MAR_CHATCODE_REPLACEMENT_TARGET.md`.

Forward target: **ChatWeb = default cognition/brain; CADS = intent/design/acceptance authority; MAR = the smallest trustworthy local execution bridge; external harnesses are replaceable executors; ChatCode is compatibility/bootstrap only and must not be a required forward dependency.**

**Trusted Owner Fast Path is the default normal-development path.** MAR exposes direct bounded project `action` operations for hash-guarded write, patch, argv-only run, Git stage/commit, and non-force push without creating a durable Goal/task/brain/verification lifecycle. The existing governed Goal path remains available when a task actually needs high-assurance isolation, exact-candidate verification, publication authorization, or recovery guarantees.

Architecture closure is finite rather than open-ended. The remaining blockers are bounded to publication/cancellation linearization, private non-canonical staging, fresh exact-OID verification, OID handoff/import, CAS-at-ref publication semantics, fail-closed launch environment defaults, and the durable-state deletion boundary. ChatCode replacement adds one product proof: a fresh Web client must complete the normal local-repository workflow through MAR with no ChatCode process/plugin in the path.

The MAR trust kernel must not depend on a specific model/provider/planner/session implementation. Above that kernel, MAR may retain, port, rewrite, or add productivity capabilities such as Project Brain, context assembly, research helpers, provider/session adapters, and ChatCode-inspired ergonomics when they materially improve the real owner workflow. These capabilities remain non-authoritative with respect to safety, verification, and publication. The external executable seam proved at `c94d359` remains a replaceable execution seam rather than a second safety authority.

Architecture improvement follows **REUSE BEFORE DELETE**: `identify proven capability -> port/rewrite/adapt where useful -> measure -> improve -> verify -> benchmark -> keep/revert`. Research by itself does not reopen the top-level architecture, but a useful existing capability is not removed merely because it is outside the trust kernel. Deletion requires either a proven replacement or measured evidence that the capability is unnecessary; simplification must not regress workflow capability or measured performance.

Current simplification progress:
- Web cognition no longer requires or persists a specific model identity; evidence: `docs/research/040_WEB_MODEL_IDENTITY_DELEGATION.md`.
- Web cognition no longer pins a model-specific reasoning effort; evidence: `docs/research/041_WEB_REASONING_CONFIGURATION_DELEGATION.md`.
- Provider compatibility still requires an explicit provider model and may carry explicit provider reasoning configuration.

These are bounded post-freeze responsibility-delegation slices, not architecture changes.
- DecisionProjection keeps its safety role, but the unused pre-loop repository context build before turn 1 is removed; evidence: `docs/research/042_FIRST_DECISION_PROJECTION_CONTEXT_BUILD_REMOVAL.md`. No worktree/revision cache was added.
- External harness kernel seam is E2E-proven without MAR model/Web cognition (zero agent/model usage); evidence: `docs/research/043_EXTERNAL_HARNESS_KERNEL_E2E.md`.
- Harness-neutral per-task intent handoff is now E2E-proven through a read-only ephemeral Goal Contract envelope bound to task/attempt/run-epoch/hash; evidence: `docs/research/044_EXTERNAL_HARNESS_TASK_INTENT_HANDOFF.md`. Real vendor/harness qualification remains separate.
- External harness network authority is now OS- and worker-proven: default deny remains intact, while `BrainHarness` maps explicit `NetworkAllowed=true` to LPAC outbound `internetClient` only; evidence: `docs/research/045_EXTERNAL_HARNESS_NETWORK_AUTHORITY.md`. Authentication/credential handoff remains separate and unresolved.
- Web/Harness worker start no longer carries unused provider URL/API-key-name/request-timeout configuration; Provider mode remains unchanged. Evidence: `docs/research/046_NON_PROVIDER_WORKER_PROVIDER_CONFIG_DELEGATION.md`.
- Worker cognition config is now mode-scoped: Provider keeps full compatibility profile/config; Web keeps only base instructions plus active fail-closed budgets; Harness receives zero MAR AgentProfile/AgentConfig. Evidence: `docs/research/047_WORKER_COGNITION_CONFIG_MODE_SCOPING.md`.
- External Harness no longer inherits MAR compatibility-cognition convergence ceilings (model decisions/tool calls/tokens), while semantic no-progress, cumulative execution time and attempt limits remain fail-closed. Evidence: `docs/research/048_EXTERNAL_HARNESS_CONVERGENCE_BUDGET_DECOUPLING.md`.
- External Harness execution no longer depends on the mixed compatibility HarnessConfig cognition projection; executable/arguments are consumed directly from StartRequest while the legacy wire shape remains unchanged. Evidence: `docs/research/049_EXTERNAL_HARNESS_CONFIG_DECOUPLING.md`.

Next MAR implementation work: complete practical ChatCode capability parity on the Trusted Owner Fast Path first, benchmark it against ChatCode, and improve measured bottlenecks. Do not route ordinary edits/tests/commits through the heavyweight governed lifecycle by default. Separately close bounded kernel fixes for the optional high-assurance path. Treat ChatCode as a reference implementation: preserve or port capabilities that already make the workflow fast and useful, then improve them behind MAR's stronger trust boundary. The current discovery slice is retained; the six-tool MCP surface is a starting shape, not a permanent feature ceiling. Do not open another broad architecture research loop, and do not delete working capability without replacement or measured justification.

## Architecture simplification decision

Independent architecture review has been accepted as an evidence-backed amendment to MAR's forward architecture. Canonical decision: `docs/architecture/MAR_HYBRID_SIMPLIFICATION_DECISION.md`. Canonical migration roadmap: `docs/roadmap/MAR_V2_HYBRID_SIMPLIFICATION.md`.

Verdict: MAR currently preserves valuable safety guarantees with too much durable lifecycle/storage machinery for the actual single-owner/single-host use case. Target architecture is **Hybrid MAR**, not Minimal MAR and not a rewrite.

Invariant: **Persist facts and irreproducible intent; reconstruct execution materializations.** A live per-task worktree is no longer durable resumability truth; Git checkpoints reconstruct source state, SQLite retains coordination/authority/result facts, and disposable execution materializations/caches are reclaimed.

**Current simplification scope:** Phase 0 baseline is complete in `docs/research/034_HYBRID_SIMPLIFICATION_PHASE0_BASELINE.md`. Phase 1 cleanup-first resource lifecycle is implemented as a scoped candidate in `docs/implementation/035_HYBRID_PHASE1_CLEANUP_FIRST_RESOURCE.md`: disk-budget/host-disk admission now attempts bounded terminal-safe reclamation plus positive-allowlist rebuildable-cache cleanup before preserving `WAITING_RESOURCE`. BLOCKED workspaces remain untouched. Phase 1 must still pass exact-candidate qualification after its scoped commit; do not open Phase 2 before that proof.

**Current line:** MAR V1.3 B–D remains the historical bounded stable release checkpoint, while the repository has since advanced on a post-V1.3 capability line. The base entering this reconciliation is `3e44b765eae34a2c1c1c1d3cc874385dceae18bd`. Later production changes include owner permission/network/remote-Git behavior, cognition-delta adapter/guidance, and bounded local-path attach/list access. These post-release changes require authoritative current-head `go-release` qualification before they may inherit a release-qualified claim.

Forward MAR architecture is now governed by:

- `docs/architecture/MAR_ARCHITECTURE_CONSTITUTION.md`;
- `docs/architecture/MAR_EXTERNAL_COGNITION_CONTRACT.md`;
- `docs/roadmap/MAR_V1_3_PERFORMANCE_AND_SIMPLICITY.md`.

Current direction: **MAR = Durable Local Execution Kernel for External Cognition**. Do not open a MAR V2 rewrite or add internal planner/multi-agent/memory infrastructure by default. Slice B (delta/event cognition context), Slice C (telemetry-proven compound deterministic operations), and Slice D (fast repair feedback) are complete for their bounded scopes. Re-measurement evidence is canonical in `docs/research/033_V13_BD_REMEASUREMENT_AND_STOP_RULE.md`; decision: `DO_NOT_OPEN_SLICE_E_F_NOW`. Slices B-D are the frozen current V1.3 optimization set. Slice E/F remain closed unless later representative evidence demonstrates a material remaining deficit with favorable complexity-adjusted benefit.

**Historical V1.3 release checkpoint:** MAR V1.3 is STABLE for the bounded B-D optimization scope at exact revision `ff2fcc6ad2eed9118e8f11a0c70adb44428d437a`. Its release closeout is COMPLETE: authoritative `go-release` PASS, result `VERIFIED`, integration `INTEGRATED`, unresolved risks `0`, exact-runtime `HEALTHY / ALIGNED / trusted_for_release=true`, and remote convergence proved for that revision. This remains historical release truth; it does not automatically release-qualify later production changes.

**Current post-V1.3 capability line:** Slices B, C, and D are already complete/verified/integrated. `docs/research/033_V13_BD_REMEASUREMENT_AND_STOP_RULE.md` decided `DO_NOT_OPEN_SLICE_E_F_NOW`. Keep E/F closed unless new representative evidence proves a material deficit. The next release action is current-head qualification of the accumulated post-V1.3 capability changes, not another V1.3 optimization slice.

The older V1 product goal/history below is retained as historical context and accepted-product intent. It does **not** override the 2026-09-19 architecture constitution or create implementation authority for a new V1.3 slice. A new implementation slice must still be opened as a bounded Goal with acceptance evidence.

# Historical V1 Goal

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

Run authoritative **current-head go-release qualification** for the post-V1.3 capability line. Bind acceptance to the production regressions for permission/network/remote-Git authority, cognition delta/guidance, scheduler/policy behavior, and bounded local-path attach/list. If and only if that exact HEAD is `VERIFIED + INTEGRATED`, promote the same revision to the live runtime, prove `HEALTHY / ALIGNED / trusted_for_release=true`, and confirm local/remote convergence. Do not open Slice E/F and do not inherit the historical `ff2fcc6...` release claim onto later code without this qualification.

## MCP Link UX contract — current V1 path

```text
UX_CONTRACT
PRIMARY_USER=The single owner connecting GPT or Claude to MAR without networking vocabulary.
PRIMARY_JOURNEY=Open Connections -> create/configure the public route -> copy the provider-specific MCP URL -> add it to GPT/Claude -> see route and actual MCP activity separately.
SCOPE_MODEL=GPT and Claude have independent capability URLs, stable/temporary settings, secret rotation and telemetry; Quick Tunnel lifecycle may be shared infrastructure.
STATES=Ready to start, Link ready, Connected, Idle, Route offline, Stable URL required, Bridge runtime missing.
SECURITY=Each full capability URL is secret; rotating one provider revokes only that provider path.
ACCEPTANCE=GPT traffic cannot promote Claude state and Claude traffic cannot promote GPT state.
```

## Optional Secure MCP Tunnel UX contract

```text
UX_CONTRACT
PRIMARY_USER=The single owner checking and operating GPT and Claude connectivity without networking expertise.
PRIMARY_JOURNEY=Open Connections -> understand both connection states -> configure/start or diagnose the affected transport -> copy the required identifier -> see live recovery or success.
PRIMARY_SURFACE=Owner Console > Connections.
INFORMATION_HIERARCHY=Provider status first; identifier and primary actions second; short remedy third; technical details behind Details.
SCOPE_MODEL=OpenAI/GPT and Claude are independent transports with independent configuration, lifecycle, telemetry, and errors.
PRIMARY_CONTROLS=Copy identifier, Start/Stop/Restart as applicable, Diagnose, and the minimum setup input.
ADVANCED_CONTROLS=Local target, health/readiness timestamps, profile, dependency path, and diagnostic output under Details.
STATES=Not configured, Connecting, Connected, Disconnected, Degraded, Misconfigured, Error; every non-ready state includes a next action.
BULK_DESTRUCTIVE=Not applicable; Stop affects only the selected transport and is explicitly labelled.
DISCOVERABILITY=Connections is a primary Console destination and both provider cards are visible together without scrolling at normal desktop height.
ACCESSIBILITY=Semantic buttons/labels, visible focus, status text in addition to color, live status announcements, responsive reflow, and no hover-only controls.
OWNER_PREFERENCE=NONE; the requested transport split and hierarchy are explicit.
```

```text
CREATE_FLOW_CONTRACT
TASK_GOAL=Optionally connect GPT to private MAR MCP through OpenAI Secure MCP Tunnel when the owner chooses that advanced transport.
LINEAR_OR_NONLINEAR=Linear onboarding until configured; routine lifecycle controls become direct after setup.
STEPS=Enter tunnel ID -> verify tunnel-client and local MAR MCP -> run Doctor -> Start tunnel -> confirm Connected -> copy tunnel ID into the supported OpenAI surface.
STEP_DEPENDENCIES=Tunnel ID, runtime control-plane credential, tunnel-client, and local MCP readiness must pass before Start can report Connected.
BACK_BEHAVIOR=Editing or leaving setup does not delete the saved tunnel ID or stop an already running connection.
NEXT_VALIDATION=Validate tunnel ID on save; dependency/local health/Doctor on Diagnose or Start; return errors beside the failing provider card.
FINAL_REVIEW_STEP=Not required because Start is reversible and does not publish or mutate repository state.
PRIMARY_COMMIT_ACTION=Start GPT tunnel.
CANCEL_EXIT_BEHAVIOR=Stop GPT tunnel terminates only the owned tunnel-client process and preserves configuration.
DRAFT_PERSISTENCE=Valid tunnel ID, profile name, credential environment-variable name, and desired-running state persist; secret values never persist.
POST_SUBMIT_DESTINATION=The same GPT card transitions through Connecting to Connected/Degraded/Error with near-realtime status.
```

```text
WORKSPACE_CONTRACT
PRIMARY_TASK=Understand and operate GPT and Claude connectivity in under three seconds.
PRIMARY_WORKSPACE=Two compact peer provider cards visible together.
PERSISTENT_REGIONS=Provider name, transport, normalized status, identifier, activity, concise error/remedy, and primary actions.
CONTEXTUAL_REGIONS=Setup fields when unconfigured; detailed diagnostics when Details is expanded.
NAVIGATION_MODEL=Existing Console Connections destination; no new global navigation.
LAYOUT_ARCHETYPE=Responsive two-column dashboard with per-card disclosure.
VIEWPORT_BUDGET=Each provider receives one half-width card at desktop widths and one full-width card in a narrow window; secondary connections follow below.
CONTENT_REPLACEMENT_STRATEGY=Status/remedy replaces stale setup messaging; advanced diagnostics remain collapsed.
ADVANCED_CONTROL_STRATEGY=Native Details disclosure inside the relevant provider card.
EXPECTED_SCROLL_BEHAVIOR=Primary provider cards fit near the top; secondary sandbox/desktop/provider details may continue below.
ARCHETYPE_RATIONALE=GPT and Claude must be compared at a glance but operated independently.
```
