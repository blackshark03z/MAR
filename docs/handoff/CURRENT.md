# MAR — Current Tech-Lead Handoff

## Hybrid simplification architecture decision — 2026-09-21

Independent adversarial review concluded that MAR is over-engineered in lifecycle/storage representation for the current single-owner/single-host deployment, while its earned safety kernel remains valuable. The accepted canonical amendment is `docs/architecture/MAR_HYBRID_SIMPLIFICATION_DECISION.md`; the migration roadmap is `docs/roadmap/MAR_V2_HYBRID_SIMPLIFICATION.md`.

Target: **persist facts and irreproducible intent; reconstruct execution materializations**. Git owns reconstructible source checkpoints, SQLite owns coordination/authority/result facts, isolated worktrees exist while mutation is active, rebuildable caches are disposable, resource admission reclaims safely before denial, and long-term runtime promotion uses immutable versioned binaries plus a current pointer instead of overwriting a running executable.

Phase 0 and Phase 1 are now closed on the production line through `2612d9029a83a280b5aeefac2fefeacd6d8d4481`; that exact runtime is `HEALTHY / ALIGNED / trusted_for_release=true` and local/remote source identity is converged. Phase 0 is measured in `docs/research/034_HYBRID_SIMPLIFICATION_PHASE0_BASELINE.md` / `.json`: `.mar` = 16.882 GiB after prior cache cleanup; 114 live workspace paths remain; 104 BLOCKED workspaces are already physically terminated yet retain ~7.05 GiB. Phase 1 is recorded in `docs/implementation/035_HYBRID_PHASE1_CLEANUP_FIRST_RESOURCE.md` and provides cleanup-before-denial plus safe rebuildable-cache reclamation. Phase 2 core was integrated, pushed and activated at `72982a42043da7b1f149da3b1ae23a16e1d90d0b`; that runtime reached `HEALTHY / ALIGNED / trusted_for_release=true` with SQLite schema 16 active/supported. The implementation is documented in `docs/implementation/036_HYBRID_PHASE2_CHECKPOINT_REHYDRATE.md`: resumable BLOCKED workspace state is preserved as immutable Git snapshots plus durable SQLite checkpoint facts, only fail-closed-safe worktrees are compacted, and resume rehydrates from the snapshot. Live dry-run then showed the dominant remaining blocker was ACI-owned ignored scratch (`.mar/go` plus tiny `.mar/runtime` state), so the source line narrows the policy to treat only source-proven task-local scratch subtrees as reconstructible; arbitrary ignored material remains fail-closed. A live external-helper pilot exposed a coordination fact: scheduler crash-reconciliation correctly races an out-of-process CHECKPOINTING transaction back to READY. The bounded follow-up therefore keeps backfill inside the authoritative scheduler: only while there are no waiting tasks and the resource governor grants an idle-exclusive gate, at most two safe BLOCKED workspaces are compacted per tick.

## Current post-V1.3 release line — 2026-09-21

The historical bounded V1.3 B–D release checkpoint remains `ff2fcc6ad2eed9118e8f11a0c70adb44428d437a`. Slices **B, C, and D are complete/verified/integrated**; their implementation records are `030`, `031`, and `032`. Re-measurement in `docs/research/033_V13_BD_REMEASUREMENT_AND_STOP_RULE.md` decided `DO_NOT_OPEN_SLICE_E_F_NOW`, so E/F remain closed absent new representative evidence.

The repository has since advanced on a **post-V1.3 capability line** containing later production changes: owner permission/network/remote-Git behavior, cognition-delta adapter/guidance, and bounded local-path attach/list access (including `research_only` fail-closed access for non-Git research paths). The base entering this reconciliation is `3e44b765eae34a2c1c1c1d3cc874385dceae18bd`.

**Current stable release truth:** the finite architecture/kernel closure is complete through code-bearing revision `28fba4e2ff91c2d475930c75f0af172a10f6e7b9`. That exact revision completed full `go test -p 1 -count=1 -timeout 300s ./...`, `go vet -p 1 ./...`, `go build -p 1 ./...`, and `git diff --check` with all exit codes 0, unchanged HEAD and a clean tree; it was activated live as `HEALTHY / ALIGNED / trusted_for_release=true` with manifest `ALIGNED` and Secure Tunnel connected/ready/healthy. Slice 056 remains proven from the already-open stale-schema ChatGPT conversation; Slice 057 closes governed publication identity/CAS gaps; Slice 058 closes ambient worker-launch environment inheritance; Slice 059 closes the durable-state deletion boundary. Slice 059 live acceptance released REHYDRATED active checkpoint refs 16 -> 0 while preserving COMPACTED recovery refs at 134 and reducing physical `D:\MAR` checkpoint refs 132 -> 116. This release-closeout is documentation-only; the final stable invariant is that the current `master`, `origin/master`, and live runtime source revision are identical and that the exact closeout HEAD has passed the release gate.

**Next safe action:** use MAR's stable direct fast path for ordinary project work and the governed kernel only when its runtime properties are actually required. Do not open another architecture, kernel, or fast-path expansion slice absent new representative failing evidence under `MAR_ARCHITECTURE_STABILITY_POLICY.md`. The live ChatCode same-workload leg remains optional measurement when a connection slot becomes available and does not block stability.

## Connection usability correction — 2026-09-19

Owner real-use proved that a healthy public MCP route can exist while the current Web client has not initialized/discovered MAR. The bounded correction is `docs/implementation/028_CONNECTION_USABILITY_TRUTH.md`: Streamable HTTP connection truth is now derived as `ROUTE_READY -> CLIENT_ATTACHED -> USABLE`; route readiness alone is not client usability. OpenAI Secure MCP Tunnel remains the primary stable GPT path; temporary Server URL/Quick Tunnel remains fallback/debug. Canonical MCP tool names are unchanged and no new gateway tool is authorized by this slice.

## Forward architecture update — 2026-09-19

The repository entered this SoT consolidation clean/aligned on `master` at `ef271ca967cbc66471f6796038f4ccfda53c6598`.

Canonical forward architecture is now:

- `docs/architecture/MAR_ARCHITECTURE_CONSTITUTION.md`;
- `docs/architecture/MAR_EXTERNAL_COGNITION_CONTRACT.md`;
- `docs/roadmap/MAR_V1_3_PERFORMANCE_AND_SIMPLICITY.md`.

Long-term product position: **MAR = Durable Local Execution Kernel for External Cognition**. CADS defines how work is framed/judged; external cognition owns reasoning; MAR owns durable execution truth, authority, verification, integration, recovery and evidence. The accepted V1/V1.2 safety mechanisms remain frozen/earned; the new Constitution governs future evolution and resolves older provider-specific **Web Brain** terminology.

The next bounded line is **MAR V1.3 — Performance & Simplicity**. Priority is delta/event context, then telemetry-proven compound deterministic operations/observation barriers, then fast repair feedback while final authoritative verification remains unchanged. Incremental Project Intelligence, caching and protocol/provider expansion are conditional on measured need. No V2 rewrite is authorized by this handoff.

Older release/handoff material below remains historical evidence for the release/checkpoint it describes and must not be read as the current forward-architecture authority.

**Architecture:** FROZEN

**Branch:** `master`

**Current engineering baseline:** `MAR_V1_2_PRODUCT_ACCEPTED`

**Current V1.2 implementation HEAD:** `e5fe06e2de416fabff38087d8435516f1bacdb38`

**Current product release:** `MAR_V1_2_PRODUCT_ACCEPTED`

**Self-hosting status:** `SELF_HOSTING_READY`

## MAR V1.2 engineering release candidate — 2026-09-13

**Final release decision — 2026-09-13:** Owner instructed V1.2 to be sealed from the metric-bound release-ready checkpoint. The canonical final release record is `docs/release/MAR_V1_2_RELEASE.md`; release identity is `v1.2.0`. After final promotion verification, V1.2 is frozen and new optimization/research belongs to a later bounded release unless a concrete regression invalidates a V1.2 invariant.

The bounded V1.2 implementation is engineering-stable and its metric-bound release result has been Owner-accepted for sealing. Core scope is closed: runtime/source provenance and stale-artifact rejection; explicit execution-child readiness and fail-closed mutation admission; bounded sandbox-readiness caching; compact live Web usage observation; bounded Secure Tunnel recovery with explicit Quick Tunnel route-loss semantics; and Web-wait capacity parking/reacquisition that preserves exact attempt/process authority.

Accepted implementation HEAD is `e5fe06e2de416fabff38087d8435516f1bacdb38`. Release evidence includes full repository tests, full vet/build, real Windows sandbox Web-wait E2E, representative T1 self-hosting PASS, 9/9 current-source transport semantics PASS, Quick Tunnel replacement PASS, and live 30-poll sandbox-cache evidence with zero observed sandbox-check children. The detailed engineering record is `docs/release/MAR_V1_2_RELEASE_CANDIDATE.md`.

V1.2 is now the current Owner-accepted product release, evaluated **by metrics/invariants rather than visual-UAT novelty**. The canonical scorecard is `docs/release/MAR_V1_2_METRICS_SCORECARD.md`: 10/10 mandatory gates PASS, zero detected safety/integrity regressions, sandbox-probe churn reduced from 5/5 launches to 0/30 live polls, active-task live-usage read amplification reduced by ~88% on the fixed fixture, execution-child admission fails closed, and the two-waiter capacity stress changes from third-task blocked to admitted without weakening fencing. Tech Lead verdict: `V1_2_METRICS_ACCEPTED_FOR_RELEASE`. The exact final runtime has already been rebuilt/promoted and verified `ALIGNED / trusted_for_release=true` at `8787`; UI appearance is a regression check only because broad UI redesign is outside V1.2 scope.

## MAR V1.1 Owner acceptance — 2026-09-12

Owner explicitly approved **MAR V1.1** after real-use of the integrated release candidate. This closes the only remaining product gate recorded by the V1.1 release candidate and promotes the integrated V1.1 experience to `PRODUCT_ACCEPTED`. The accepted implementation HEAD is `a6fa0279631c7a8f05f484a5bba7b20b73e0188f`; the final acceptance record is `docs/release/MAR_V1_1_RELEASE.md`.

The already-proven bounded self-hosting path is therefore also promoted to `SELF_HOSTING_READY` for routine MAR development. Optional stable named-tunnel/custom-host setup, further visual polish, and future feature work are post-V1.1 backlog and are not release blockers. Historical sections below that say Owner UAT or product acceptance is pending describe earlier checkpoints and are superseded by this section.


## React Owner Console interaction closeout — 2026-09-11

Owner UAT found three real interaction defects after the React migration: the Tasks list could retain horizontal scroll and clip the beginning of titles; the Windows-native folder picker could remain behind Chrome and leave `Chọn thư mục` apparently hung; and the Claude Web card reported route readiness without exposing the capability URL or useful ready-state actions. Source commit `f882de9ed6e9cb7aa36c1969d4bf787316e26b50` closes these defects without changing MAR execution authority.

Tasks now enforce vertical-only list scrolling. Workspace registration uses a same-origin/session-protected React folder browser backed by `/api/projects/browse`; direct path entry remains supported, successful add reads `project.id` from the backend response, resets the form, and selects the resulting workspace. Claude Web now renders the active stable/temporary capability URL, exposes `Sao chép link`, surfaces diagnose feedback, retains details, and only shows `Kết nối` while the route is not ready. GPT fallback and Claude remain independent capability paths.

Verification: React TypeScript/Vite build PASS; Owner UI-only tests PASS; browser interaction smoke PASS for task-list overflow and workspace browse/add; exact full sequential repository tests PASS; `internal/verification` recheck PASS; `go vet -p 1 ./...` PASS; `go build -p 1 ./...` PASS; `git diff --check` PASS. Final source binary from the source commit is `D:\\MAR\\.mar\\runtime\\mar-head-f882de9.exe`, SHA-256 `63A10638A4CC6AD2F4C4FA3F1FF045D0A7FFF371A6E742C8CBD29DE55A2B5295`. After restart the temporary remote bridge completed its bounded startup and returned `LINK_READY`; final browser DOM evidence showed the Claude capability URL plus copy/diagnose/detail actions and no connect/play action while ready. Product acceptance still requires Owner real-use confirmation.

## React Owner Console workspace/layout UAT correction — 2026-09-11

Owner UAT after the React migration exposed two real surface defects: sidebar icon/text columns were visually inconsistent and workspace selection was not obvious/usable from the Workspaces surface. Source commit `97ca22e` fixes these without changing MAR backend authority. Sidebar navigation now uses a fixed 24px icon column with zero measured icon/text alignment spread; the topbar and content grid share the same 230px shell boundary; workspace cards expose explicit `Chọn / Đang chọn` state synchronized with the persistent topbar selector; folder picker/add-project failures are surfaced instead of failing silently; selected workspace can be cleared; stale local workspace selection is reconciled; hash back/forward navigation updates React view state; sidebar system health now derives from runtime truth instead of a hard-coded healthy label. Connection/status chips and desktop widths are bounded to avoid overflow.

Browser/CDP interaction evidence on the live preview: Workspaces route rendered the selector and one selectable card; clicking `Chọn` changed topbar scope from empty to `mar`, produced exactly one selected card, and navigation to `#/tasks` rendered the Tasks view. Computed sidebar columns were `24px + text`. A seven-page desktop layout audit at 1647x920 reported zero horizontal overflow, exactly one active nav item, main content beginning at x=230, Live four-column KPI layout, Tasks three columns, Workspaces 340px + flexible content and Connections two columns. Responsive audits at 1366, 1024 and 768 also reported zero horizontal overflow with the expected fixed-to-static sidebar and multi-column-to-single-column transitions.

Verification after the correction: React `npm run check` PASS; React production build PASS; targeted `go test -count=1 ./cmd/mar` PASS; exact full sequential `go test -v -p 1 -count=1 -timeout 420s ./...` PASS; `go vet -p 1 ./...` PASS; `go build -p 1 ./...` PASS; `git diff --check` PASS. `.gitattributes` pins React source/dist HTML to LF so Windows checkout/build cannot reintroduce the prior CRLF/trailing-whitespace packaging failure. Product acceptance remains pending explicit Owner visual/use acceptance.

## Owner Console React migration — 2026-09-11

Owner Console presentation has been migrated from the accumulated single-file v3-v7 HTML/CSS/JavaScript stack to a React 19 + TypeScript + Vite frontend with Lucide SVG icons. The integrated source commit is `e2424215fbd5d303f5335d4ae10019a4ca4f606d`, fast-forwarded from authoritative base `680dfc039e696393292e1886337d474be4c1239e` with no merge commit. MAR backend authority, SQLite truth, task lifecycle, sandbox, MCP, Decision Projection, budgets and existing `/api/*` contracts are unchanged.

Frontend source lives under `ui/owner-console/`. Vite builds production assets into `cmd/mar/owner_ui_dist/`; Go embeds and serves that static bundle from the same `mar.exe`, so Node/Vite is a build-time dependency only and is not required at runtime. The default React route is Live Operations. The UI keeps separate Live Operations, Tasks, Workspaces, Connections, Usage and Diagnostics surfaces; Tasks remains a three-column owner workflow; Lucide replaces production Unicode/emoji glyph icons.

Verification on the isolated candidate: `npm run check` PASS; `npm run build` PASS; targeted Owner Console tests PASS; Windows/process-focused packages PASS; exact full sequential repository tests PASS; `go vet -p 1 ./...` PASS; `go build -p 1 ./...` PASS. `node_modules`, TypeScript build cache and the managed-Go test fixture are ignored and were not committed. The embedded production bundle is committed so release builds remain deterministic without requiring Node at runtime.

Engineering status remains `ENGINEERING_STABLE_PENDING_OWNER_UAT`. React migration fixes the UI maintainability/icon-system root cause, but visual/product acceptance still requires Owner real-use after the integrated binary is restarted and smoke-tested.
## Owner Console visual landing correction — 2026-09-11

Owner UAT of the integrated v7 runtime found a real acceptance defect: the new Live Operations and three-column Tasks surfaces existed, but the static HTML still marked the legacy `Overview / Operations` view and Overview navigation item as the default active landing state. A normal refresh therefore looked materially unchanged from v6 even though the new components were present.

Source commit `5b352b1` corrects the product entry point without changing backend authority or telemetry: `Live Operations` is now the default active navigation/view, legacy Overview remains available as a secondary page, and a regression explicitly fails if `view-overview` becomes the default again. The correction passed targeted `cmd/mar`, full sequential repository tests, `go vet -p 1 ./...`, and `go build -p 1 ./...`. A live preview on `127.0.0.1:8787` returned HTTP 200 and confirmed `LIVE_NAV_ACTIVE=True`, `LIVE_VIEW_ACTIVE=True`, `OVERVIEW_ACTIVE=False`.

This correction closes the specific "looks unchanged after refresh" defect. It does **not** by itself establish `PRODUCT_ACCEPTED`; Owner must still visually use Live Operations and Tasks and accept/reject the final experience.

**Baseline closeout source:** `84c39c168f6bb6560721a899216d6acb4807aab3` (`Stabilize owned tunnel process lifecycle`). The Owner Console v5/process-lifecycle closeout was already sealed and fully VERIFIED in the preceding self-hosted task; this repair carries that reviewed handoff forward while fixing the transient integration-retry defect that prevented authoritative promotion. `ENGINEERING_STABLE` is an engineering claim only: Owner Console product-experience acceptance remains dependent on explicit Owner real-use feedback and is not promoted to `PRODUCT_ACCEPTED` by tests.

## Owner Console v7 release UI — 2026-09-11

Owner Console v7 is engineering-integrated at `cf3b39d89b8d1c14eb645bea591630a6dbc45494` from authoritative base `3e60aaba219d4a634b0c8bba687be221026e9d58`. The release keeps the frozen MAR execution/authority architecture unchanged and converges the owner-facing surface toward the approved bright, low-density operations layout.

Primary release scope: fixed light app shell and compact top bar; Live Operations with four answer-first summaries (system health, AI connections, active flows, tokens today), realtime token chart, active-flow panel, separate OpenAI/Claude zones and quick stats; Tasks as a desktop three-column workflow with list/filter/search, selected-task detail and a real create-task panel. Create-task uses registered project HEAD/policy and the existing `/api/tasks` path, exposes only truthful `Tự động · MAR scheduler`, keeps network disabled, and does not create a second task-authority path. Secondary pages retain the same shared visual system without fabricated telemetry. Design source-of-truth: `docs/design/OWNER_CONSOLE_V7.md`.

Verification on the exact integrated content before commit: targeted `go test -count=1 ./cmd/mar` PASS; full sequential `go test -p 1 -count=1 ./...` PASS across all repository packages; `go vet -p 1 ./...` PASS; `go build -p 1 ./...` PASS. The working-tree scope was exactly `cmd/mar/owner_ui.html`, `cmd/mar/owner_ui_test.go`, and `docs/design/OWNER_CONSOLE_V7.md` before sealing. No network, remote Git write, deploy, credential handling, transport redesign, provider guessing, synthetic sessions/tokens, or `Tunnel_api.txt` modification was introduced.

Recovery provenance: the original MAR UI task exhausted the already-integrated task-wide model budget and failed closed. Its isolated workspace was preserved. During operator recovery, an independent local Owner Console implementation already present on authoritative `master` was found to be deeper than the isolated candidate (four Live summaries, quick stats, richer create-task flow and corresponding regression markers). It was not overwritten. That authoritative local implementation was first targeted-tested, then passed the complete repository gate above, and was sealed directly as `cf3b39d...`. The older isolated candidate `92123c6...` is superseded evidence only and must not be promoted over the integrated v7 source.

Engineering status is `ENGINEERING_STABLE_PENDING_OWNER_UAT`. Tests establish engineering confidence only; explicit Owner real-use acceptance of the v7 experience is still required before `PRODUCT_ACCEPTED`.
## MAR V1.1 context/cognition architecture amendment — 2026-09-10

**Architecture verdict:** `ARCHITECTURE_EVOLVE`. **Architecture freeze:** `YES`. **Remaining architecture blockers:** `NONE` for the supported bounded-external-episode / optional-internal-provider contract.

This is a **documentation-only architecture amendment**. It records the Owner-approved conclusion of the independent Astra audit; it does not by itself change source/runtime behavior. MAR remains the owner of the single execution/tool loop, durable coordination truth, observable transcript/evidence and current decision state. ChatGPT Web is a non-authoritative Tech Lead/reviewer and optional bounded external cognition adapter. Normal reasoning requests must be rebuilt from bounded revision/state-bound Decision Projections inside the existing context/service boundary instead of replaying the full transcript. Large source/log/diff/tool output belongs in durable artifact payloads referenced by bounded handles. Web cognition uses bounded episodes; MAR does not claim constant browser memory for an indefinitely growing third-party chat. Long unattended reasoning may use optional internal providers through the existing `model.Provider` / `Gateway`; API billing is not mandatory for Web-assisted operation.

Audit truth to preserve during implementation: context amplification/full-history replay, large request/response echo and stale pending-turn selection are confirmed MAR defects; tool schema size alone is not the dominant measured contributor. Exact Chrome OOM attribution remains unproven and must be validated separately with actual Chrome renderer/heap plus Windows commit/pagefile measurements.

## P0 Slice 4 — bounded Web episodes and task-wide convergence budgets — 2026-09-11

Slice 4 is integrated at `e8daa0fcaca5e301e506e338b9744397f71f89a1`. The implementation keeps MAR/SQLite as the only durable coordination authority and adds bounded derived accounting rather than a second episode service or transcript store.

Web cognition defaults are now 12 external reasoning decisions, 40 attributable control-plane calls, and 512 KiB cumulative bounded MAR request/response payload per episode. Exhaustion fails closed before the next unsafe model/tool action and produces a compact continuation receipt bound to task/attempt/run_epoch, Goal/Decision Projection identity, checkpoint/revision state, counters, reason, and next valid action. Fresh episodes resume from durable Decision Projection/checkpoint truth without browser transcript replay.

Task-wide convergence defaults are 24 model decisions, 64 worker tool calls, 300000 reported/estimated model tokens, 30 minutes active execution, and 3 automatic attempts/replacements. Budget truth is reconstructed from durable attempts/WebTurns/checkpoints across reconnect/restart. Owner-approved `blocked_choice` can authorize one bounded manual continuation after the automatic attempt cap only when the prior attempt has physical termination proof; it does not erase other cumulative budget exhaustion. Repeated no-progress is guarded from durable progress state and remains fail-closed.

Release evidence for the integrated source: exact built-in `go-standard` profile PASS (`go test -v -p 1 -count=1 -timeout 180s ./...`, `go vet -p 1 ./...`, `go build -p 1 ./...`) plus `git diff --check` PASS. Required regression coverage is present for decision/call/byte boundaries, continuation receipts, restart reconstruction and stale-turn exclusion, replacement accumulation, no-progress handling, Decision Projection resume without historical transcript replay, real-worker failure evidence propagation, and MCP submit/steer/input/verify/integrate E2E semantics.

Bootstrap note: the long-running Slice 4 development task itself had already accumulated usage under the pre-Slice-4 unbounded runtime (45 model decisions, 132 worker tool calls, approximately 920k reported tokens) before the new cumulative limits existed. Once the new guard became active, that task correctly stopped instead of admitting another coding worker. The already verified isolated candidate was therefore promoted by an operator fast-forward only after confirming authoritative master still equaled the exact base `aca40c5c2360cef9a5b5b6741c83573b2a43dc7a`; the integrated commit is the exact verified candidate above, with no merge commit or content rewrite.

Known follow-up: an unclean MAR restart can still lose in-memory `TerminationProof` even when the Windows Job Object has already killed the worker tree, leaving a logically fenced task unable to prove physical termination after process restart. That recovery mechanism is intentionally not implemented in Slice 4 and remains a separate bounded follow-up.

**Slice 4 follow-up (completed below):** public MCP contraction. The contraction was required to reduce the normal Web-facing surface while preserving internal authority boundaries and diagnostics; its completed evidence and current next action are recorded in the following P0 public MCP contraction section. Slice 4 engineering verification does **not** imply `PRODUCT_ACCEPTED`.

**Context/OOM hardening implementation order:** (1) P0 current/stale-turn truth + small receipts; (2) P0 durable observation/artifact capture; (3) P0 bounded Decision Projection; (4) P0 Web episode + task-wide budgets; then public MCP contraction, Console telemetry/index work and optional provider continuation. Do not add a second orchestrator/gateway/projection service/database/agent framework or mandatory API path. Owner Console visual polish is paused until these P0 hardening slices are complete and verified.

## P0 public MCP contraction — 2026-09-11

The public MCP contraction is engineering-complete on source HEAD `95d93829a7c0061c8942d90b5257db5d36a183c9`. The exact implementation candidate was sealed at `06f042b10365aae2e0139c30534d1df87a037e11` from base `f57de009aa5e78fbf2e8a8254c4b29b3b5933bfb`; a follow-up regression-only correction at `95d93829a7c0061c8942d90b5257db5d36a183c9` updated the legacy remote-bridge test contract from the former 11-tool count to the new canonical six-tool count. No production authority path changed in that correction.

Normal `tools/list` now exposes exactly six canonical typed domain tools: `project`, `submit`, `task`, `control`, `brain_turn`, and `brain_respond`. Legacy names `project_context`, `project_read`, `status`, `result`, `inspect`, `steer`, `input`, and `cancel` remain callable for cached-client compatibility in this release but are unlisted. A receiving middleware rewrites those legacy calls to the corresponding canonical tool/operation before registry dispatch, so canonical and compatibility calls share the same existing Backend/service methods and do not create a second business-logic or authority path.

The serialized listed tool surface is regression-bounded to `<= 24576` bytes. Canonical `project` preserves bounded project context/read behavior; canonical `task` preserves status/result/inspect read semantics; canonical `control` preserves steer/input/cancel idempotency, validation, state and fencing semantics. `brain_turn` / `brain_respond` retain attempt/run-epoch/turn integrity and stale-turn rejection. Slice 3 Decision Projection, Slice 4 episode/task-wide budgets, sandbox, physical termination fencing, verification freshness, integration CAS, controls and SQLite durable authority remain unchanged.

Final source evidence at `95d93829a7c0061c8942d90b5257db5d36a183c9`: focused `internal/mcpedge`, `internal/service`, `internal/orchestrator`, `internal/store` regressions PASS; `TestRemoteBridgeExposesIndependentGPTAndClaudeLinks` PASS after the count-only contract correction; full sequential `go test -count=1 -p 1 -timeout 180s ./...` PASS across the repository; `go vet -p 1 ./...` PASS; `go build -p 1 ./...` PASS; `git diff --check` PASS.

Recovery truth: the original MAR coding task reached a fully implemented and fully verified workspace but exhausted the new task-wide worker budget at the final `completed_candidate` transition. The budget was not raised or bypassed. A manual continuation was unable to open another worker because cumulative task budget remained exhausted. Operator recovery therefore sealed the exact already-verified isolated workspace and fast-forwarded `master` only after confirming the authoritative owner worktree was clean at the exact base revision; no merge commit or content rewrite was introduced. The integrated full-suite rerun then found only the stale 11-tool remote-bridge expectation described above, which was corrected and followed by a complete green source gate.

**Compatibility policy:** keep the eight legacy aliases callable-but-unlisted for this release so cached ChatGPT/plugin clients do not break abruptly. Do not re-expand `tools/list`. Alias removal, if desired, belongs after real-use acceptance/release freeze with evidence that active clients use the canonical surface.

**Next P0 action:** run one short Owner/ChatGPT real-use bounded-context acceptance against the six-tool surface, confirm normal context size/tool discovery and cached-client compatibility behavior, then freeze the release if accepted. Engineering tests do **not** establish `PRODUCT_ACCEPTED`.

## Owner Console v6 convergence candidate — 2026-09-10

A bounded self-hosted UI convergence is in progress from base `7e3ad89d9aaa3447dcfb87fdc24573a64e0623b1`. The candidate implements the approved light operations direction in production source: compact aligned header, truthful OpenAI/Claude identity and progressive disclosure, realtime input/output chart from bounded MAR observations only, active-flow focus, compact Tasks master-detail with search/status filtering, and the truthful V1 execution selector `Tự động · MAR scheduler`. No named-worker routing, synthetic telemetry, provider guessing, architecture change, network action, push, deploy, or credential handling is introduced. The design contract is `docs/design/OWNER_CONSOLE_V6.md`.

This v6 work remains a **candidate** until targeted Owner UI/JavaScript checks and the sealed revision-bound `go-standard` profile pass and MAR authoritatively integrates it. The existing engineering baseline remains stable while this candidate is evaluated. `PRODUCT_ACCEPTED` remains strictly dependent on Owner real-use acceptance after integration.

## Verified integration retry recovery — 2026-09-10

A sealed candidate had already passed complete MAR verification, but authoritative integration correctly blocked on a transient owner-worktree precondition. After that external precondition was resolved, the generic `blocked_choice` recovery path incorrectly used `RecoverForReplacement`, incrementing `run_epoch` and launching a new coding worker against the already-sealed workspace. Context construction then failed closed because the task contract still expected the original base revision while the workspace HEAD was the sealed candidate.

The bounded repair routes only `BLOCKED` tasks whose latest result is fresh `VERIFIED` with `integration_status=BLOCKED` to an integration-only retry path. Before mutation it revalidates exact result/evidence/candidate/base identity, workspace identity, physical termination of every execution attempt, authoritative symbolic-ref HEAD, clean owner worktree, and candidate ancestry. Only then does it prepare a new integration attempt from the same verified result and drive authoritative integration. Unsafe or stale retries remain blocked; genuinely blocked coding tasks continue through the existing replacement-worker path. No verification, sandbox, process-fencing, connector, or transport authority is weakened.

Targeted self-hosted evidence: `internal/store`, `internal/integration`, and `internal/orchestrator` PASS. `TestRetryBlockedVerifiedIntegrationReusesCandidateWithoutNewWorker` proves dirty-worktree block -> clean prerequisite -> same verified candidate integrates without changing the coding attempt/run epoch or resource/evidence identity. `TestRetryBlockedVerifiedIntegrationRejectsStaleEvidenceWithoutReplacement` proves stale evidence fails closed, and `TestDaemonBlockedChoiceResumesReplacementExactlyOnce` remains green for generic coding recovery. Final sealed full-repository test/vet/build verification and authoritative integration are still required for this repair candidate.

**V1 stabilization implementation checkpoint:** `89a99d443de67672d5cc30a8fb41dd72d9a16a56`

**Project-context checkpoint:** `bc0ae694b8f9f382a419d4ab08676e0d13bd7b1b`

**Starting checkpoint for this stabilization pass:** `764a5648e8a19a48dbd864bc6b43cbbacd1e0d4f`

**Remote state:** local `master` is ahead of `origin/master`; nothing was pushed or deployed.

**Product transport decision — 2026-09-09:** `OPENAI_SECURE_TUNNEL_PRIMARY`

ChatGPT/GPT uses OpenAI Secure MCP Tunnel as the normal primary path. Saving a valid Tunnel ID arms durable desired-running state; MAR reuses the same tunnel identity and attempts automatic recovery on later launches when the local client and credential prerequisite are present. Server URL / Quick Tunnel remains an explicitly temporary fallback/debug path and is not presented as stable primary. Claude Web remains independent from GPT Secure Tunnel state and keeps its separate capability URL/telemetry path. Tunnel API-key values are environment-only and must never be persisted or rendered.

Git, the frozen architecture documents, `TASK.md`, and this handoff are continuity truth. Chat history is disposable working memory.

## Remote connector 502 lifecycle recovery — 2026-09-10

Owner real-use exposed a remote MAR availability defect: a direct `project_context` call from ChatGPT returned `502 Upstream/external service error` even though the local repository and Owner Console were healthy. Local Git truth remained HEAD `0f01136c94f1747948c61e1d91111f2287e618f4`; the older `71a884...` value was only a stale fallback from the failed remote read.

Root cause was physical child-process ownership, not project context. Repeated hard restarts of the Owner UI had left multiple `D:\MAR\.mar\runtime\cloudflared.exe` Quick Tunnel processes alive, plus an orphan `tunnel-client.exe run --profile mar-openai` whose parent MAR process no longer existed. Both connector profiles were still temporary-link capable and the OpenAI tunnel configuration remained present with `desired_running=true`. A replacement MAR process therefore had no authoritative handle over the stale tunnel children and could encounter stale public routes / tunnel-client profile state.

The bounded fix adds one shared infrastructure-process primitive. On Windows, both `cloudflared` and `tunnel-client` now start inside a Job Object with `KILL_ON_JOB_CLOSE`; abrupt MAR termination therefore causes Windows to terminate the owned child tree even when normal Stop cleanup cannot run. Other platforms retain explicit process termination behavior. No connector protocol, capability token, sandbox policy, or verification authority changed.

Regression evidence: `cmd/mar + internal/processctl` targeted tests PASS; `TestOwnedCommandJobCloseKillsChild` proves a child exits when its Job Object handle closes; `git diff --check` PASS. Runtime acceptance then removed only stale MAR-owned tunnel processes, launched the candidate, and observed exactly one `cloudflared` plus one `tunnel-client`, both parented by the current MAR process. A deliberate hard kill of candidate MAR PID 16736 caused child PIDs 10860 and 528 to disappear automatically; relaunch produced a new healthy tree `MAR 3092 -> cloudflared 13760 + tunnel-client 2312` with Owner Console HTTP 200. The recovery was committed at `84c39c168f6bb6560721a899216d6acb4807aab3`; that exact candidate passed the complete managed-Go sequential repository regression, vet, build and diff-check before commit. The current self-hosted repair preserves this process-lifecycle recovery while fixing only the later verified-integration retry defect.

## Owner Operations Console v5 light + multi-flow redesign — 2026-09-09

Owner real-use of the v4 candidate requested two concrete changes: move to a modern light visual system and make concurrency understandable when multiple GPT/Claude connection paths and MAR execution flows exist at the same time. v5 therefore stops using a single ambiguous `connected` number and separates **routes**, **authoritative sessions**, and **live execution flows**.

`docs/design/OWNER_CONSOLE_V5.md` is the current design/observability contract. The sidebar includes a dedicated **Live Operations** view. Owner feedback then required further decluttering: Overview now keeps only the operational answer, five short metrics, Current Work, Action, Connections and 7-day usage. Realtime detail is moved out of Overview. Live Operations now places one shared live-token monitor at the top and splits connection telemetry into separate **GPT** and **Claude** zones, each with routes, sessions, requests, live paths and recent provider events, followed by the execution-flow table.

A live execution flow is one active `task_id + run_epoch`. `run_epoch` is now exposed in Owner task telemetry so retries/replacement attempts cannot be visually conflated. Stateful MCP routes contribute authoritative session counts; stateless transports remain unknown, so a mixed system can correctly display a value such as `5 + ?` instead of converting the unknown route to zero. Route readiness remains separate from session/activity truth.

The light visual system is intentional and OS-theme independent: page background `#F6F8FB`, white surfaces, slate text, blue primary action, emerald/amber/red semantic statuses, soft borders/shadows and tables for multi-flow density. This avoids the dark/card-heavy appearance rejected in owner UAT and scales better when flow count grows.

Live Web Brain usage still comes only from durable WebTurn responses for the exact task/run epoch and remains labelled estimated (`~`), not provider billing. WebTurn records currently do not contain the responding connector identity, so v5 explicitly leaves per-flow provider attribution unavailable rather than guessing GPT vs Claude. Durable day/week/30-day Usage remains TaskResult-based and separate.

Current refinement evidence: targeted `cmd/mar + internal/store + internal/service` tests PASS, Owner JS syntax PASS and `git diff --check` PASS after the declutter/provider-zone change. The candidate is live on `127.0.0.1:8787` HTTP 200 with GPT/Claude zone and live-token markers present. The subsequent recovery baseline at `84c39c168f6bb6560721a899216d6acb4807aab3` passed the complete managed-Go sequential repository regression, vet, build and diff-check with v5 included, and the preceding self-hosted closeout sealed and fully VERIFIED the v5 handoff candidate. This handoff therefore records v5 as `ENGINEERING_STABLE`; explicit Owner real-use acceptance of the v5 experience remains separate and pending.

## Owner Operations Console v4 live-observability refinement — 2026-09-09

Owner real-use rejected the v3 surface as still too static/card-heavy and specifically required realtime connection observation and token usage. v4 keeps the v3 interaction architecture but changes the visual hierarchy to an operations/event-stream surface and adds a process-safe live telemetry path.

`docs/design/OWNER_CONSOLE_V4.md` is the current interaction/observability contract. Overview now uses a compact summary rail plus one dominant Live Operations panel with current connection activity, active-turn token totals, observed token-rate, completed model-turn count, a small token trend and a bounded Event Stream. Current Work/Action Center/Connections are rendered as operational lists with less nested card chrome.

Realtime connection events are derived from the existing non-overlapping 2-second authoritative runtime/task snapshots: status transitions, request deltas, authoritative session-count changes, last-activity changes and task product-stage changes. The Event Stream is seeded from the first real snapshot so known GPT/Claude state appears immediately instead of showing an empty waiting panel.

Live token telemetry is deliberately separate from durable Usage. For Web Brain executions, MAR reads the already-durable SQLite `web_turns` for the exact task/run epoch and aggregates completed `TurnResponse.Usage` values before the final TaskResult exists. The source is `WEB_TURN_DURABLE_ESTIMATE`: it is durable and process-safe but estimated runtime budgeting, not provider billing. UI labels live totals/rates with `~`. Provider-mode executions without an authoritative cross-process per-turn source remain unavailable instead of being inferred. Today/week/30-day usage continues to come only from latest durable TaskResult ResourceSummary values.

A Web Brain turn temporarily uses technical `INPUT_REQUIRED` while waiting for the AI response. v4 now distinguishes this as **Waiting for AI** and suppresses Owner attention for it; only a genuine agent `request_input` remains **Needs your input**.

Targeted evidence before final release closeout: `cmd/mar + internal/store + internal/service` PASS; Owner UI JS parse PASS; diff-check PASS. Browser-level desktop/compact UAT on the real local runtime PASSed layout/overflow, and a browser-only synthetic delta verified the client renderer: active-turn total moved from ~1,000 to ~1,500, completed turns to 2, Event Stream emitted `+500 estimated tokens`, and the sparkline rendered. No backend/task mutation was used for that renderer probe. Full revision-bound regression/release record still must be refreshed after the v4 commit before claiming engineering-stable.

## Owner Operations Console v3 — 2026-09-09

**Design baseline:** `docs/design/OWNER_CONSOLE_V3.md`

**Status before final current-head release binding:** `V3_CANDIDATE_BROWSER_ACCEPTED`

Owner real-use on Console v2 showed that feature completeness was not enough: the Overview could say `Healthy` while GPT had an actionable error, historical engineering failures inflated the owner attention count, and the desktop sidebar visually broke. The v3 redesign is therefore interaction-led rather than CSS-led. Its accepted principles are answer-first, action-first, directed browsing, progressive disclosure, semantic status separation, evidence-bound telemetry, calm hierarchy, and WCAG 2.2 AA-oriented interaction behavior.

The Overview now answers operational readiness before showing analytics. Aggregate state is derived from fresh runtime telemetry, sandbox readiness, and the primary GPT/Claude connection paths: `Operational`, `Degraded`, `Action required`, or `Telemetry unavailable`. It can no longer report a healthy/operational answer solely because the sandbox is ready. On the current real MAR state, the browser-rendered answer is `Degraded` with the explanation `GPT · OpenAI Secure Tunnel: Lỗi · Claude Web: Link sẵn sàng`, matching actual subsystem state.

Overview Action Center is intentionally narrower than the broader engineering attention model. It shows actionable-now owner/system conditions: sandbox preparation, provider recovery/configuration, unsupported workspace readiness, and tasks in `INPUT_REQUIRED`. Historical `BLOCKED`/`FAILED`, unverified results, unresolved risks, or non-integrated results remain visible in Tasks/Diagnostics but do not inflate the Owner action counter. On the current real state the old `10` attention items became `1` actionable Owner item without deleting any durable evidence.

Tasks now expose owner-facing stages (`Preparing`, `Working`, `Checking result`, `Needs your input`, `Completed`, `Could not complete`) while preserving exact technical state as secondary evidence. A selected task explains whether Owner action is required and what happens next; `INPUT_REQUIRED` gets one primary `Gửi và tiếp tục` action. Connections now separates `Configuration`, `Transport`, and `Live activity`; `Link ready` is explicitly not equivalent to `Connected`, and transports without authoritative session identity continue to show session count unavailable rather than an inferred number. First-time ChatGPT setup and deep diagnostics use progressive disclosure.

The sidebar defect was found mechanically during browser UAT. The fixed `nav` was nested inside a sticky/backdrop-filter header, which created an unexpected containing block: Chrome measured the desktop nav at only ~36px high while its buttons overflowed outside it. v3 moves `<nav>` outside `<header>` instead of masking the symptom with CSS. Browser/CDP acceptance on the real live Console at desktop viewport ~1582x904 then measured nav `248x831`, bottom exactly at viewport bottom, main content starting at x=248, zero horizontal overflow, and every navigation label with `scrollWidth == clientWidth`. At compact 800x1000, nav becomes horizontal, main starts at x=0, and horizontal overflow remains zero.

Browser interaction acceptance also verified that the Connections KPI routes to the Connections view and updates `aria-current`; real Overview state rendered `Degraded`; current Owner action count rendered `1`; and the live v3 Console remained HTTP 200. Targeted JavaScript syntax, `cmd/mar`, `internal/mcpedge`, `internal/store`, and `git diff --check` gates passed after the redesign. No token/session/provider metric was invented, and `Tunnel_api.txt` remained untracked and untouched.

The professional design research used for this baseline reinforces the same constraints: operations dashboards should answer concrete questions and move from overview to detail; status/focus/target interactions require explicit accessible semantics; and telemetry names/availability must preserve what is actually measured rather than infer missing data. This redesign does not reopen MAR's frozen execution architecture.

## Owner Console startup availability fix — 2026-09-09

Owner real-use immediately exposed a runtime packaging gap after the Console v2 engineering release: `http://127.0.0.1:8787` returned `ERR_CONNECTION_REFUSED` because no Owner UI process was listening after the release-gate process exited. The stable binary itself remained valid. The immediate recovery launched `D:\MAR\.mar\runtime\mar-v1-stable.exe ui` against `D:\MAR\.mar\mar.db` / `D:\MAR\.mar` / managed Go and verified HTTP 200 plus `<title>MAR Console</title>` on loopback.

The bounded product fix adds `scripts/start-owner-console.ps1`: an idempotent Windows launcher that verifies an existing MAR Console, fails closed if another process owns port 8787, otherwise starts the stable Owner UI with the persisted data root and managed toolchain, waits for an actual MAR HTML response, and optionally opens the browser. The current single-user installation registers this launcher at `%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\MAR Owner Console.lnk` so the Console is available after sign-in without manual terminal work. This launcher does not create a second daemon/task authority; it starts the same `ui` runtime only when the loopback Console is absent.

Runtime acceptance evidence for this fix: PowerShell parser check PASS; invoking the launcher while the Console was already live preserved the same listener PID; an absent-listener restart harness stopped owned MAR UI PID 16052 and the launcher established a new MAR UI PID 14736 with HTTP 200 and the expected MAR title; the production Console was then restored as PID 2900 and again returned HTTP 200. The Startup shortcut target/arguments were re-read after creation and matched the repository launcher. This closes the observed `ERR_CONNECTION_REFUSED` packaging/startup gap without changing MAR's frozen execution architecture.

## Owner Operations Console v2 — 2026-09-09

**Accepted implementation base before this slice:** `71a884a3e17d42e0b39c4b8e525ff40d9444f383`

**Status:** `ENGINEERING_STABLE`

The already accepted V1 Secure Tunnel + ChatGPT plugin path remains `PRODUCT_ACCEPTED`. This slice does not reopen transport architecture; it adds the Owner Operations Console v2 and observability surface on top of that accepted base.

The Console now exposes `Overview / Tasks / Workspaces / Connections / Usage / Diagnostics`, a persistent workspace selector, native Windows folder picker, actionable Attention Center, fresh/stale telemetry semantics, and WCAG 2.2 AA-oriented interaction behavior. Token usage is aggregated only from durable MAR `ResourceSummary` counters, with input/output/total for today, local calendar week, all time, and the last 30 days. Missing token counters remain explicitly unavailable instead of being inferred as zero, and provider attribution is explicitly `UNAVAILABLE` because current durable usage does not contain provider identity.

GPT/Claude connection telemetry is intentionally transport-aware. OpenAI Secure MCP Tunnel is stateless, so MAR does not fabricate an active-session count from connectivity or recent activity. Stateful Streamable HTTP connectors track observed `Mcp-Session-Id` values, expire them with the runtime 30-minute MCP session timeout, remove them immediately on explicit `DELETE`, and cap tracked identities at 256; if that bound would be exceeded the count fails closed instead of publishing a partial metric. Request counters are runtime-local and labeled as `requests / run`.

Attention Center combines real task state, verification verdict, unresolved risks, integration state, workspace readiness, sandbox state and GPT/Claude connection state. Task attention includes severity, reason and next action. Polling is bounded so the 2-second operational cycle and 30-second usage cycle cannot overlap themselves when backend requests are slow; failed runtime refresh marks telemetry stale and never preserves a stale `CONNECTED` claim as live.

Final engineering evidence for this slice:

- Windows elevated sandbox preparation completed and `sandbox-host-check` returned `sandbox_host_ready: true` on `D:\MAR\.mar\runtime\host-readiness`.
- Isolated `TestRuntimeE2EWebBrainMCPWorkerVerifyIntegrate`: PASS after host preparation.
- Final low-memory sequential regression using managed Go: **all 21 repository packages PASS**, including `cmd/mar`, `internal/aci`, `internal/orchestrator`, `internal/processctl`, `internal/verification`, `internal/worker`, and `internal/workspace`.
- Managed-Go `go vet ./...`: PASS.
- Release build to `D:\MAR\.mar\runtime\mar-owner-console-v2.exe`: PASS.
- `git diff --check`: PASS.
- Owner UI JavaScript syntax check: PASS.
- Changed-file credential-pattern scan: PASS.
- `Tunnel_api.txt` remained untracked and intentionally untouched.

The first unconstrained parallel `go test ./...` attempt hit the Windows commit/pagefile limit rather than a code assertion. The release gate therefore uses the same tests sequentially (`-p 1` / package-by-package), which passed completely without changing verification policy. No fake metrics, network push or deployment were introduced.

## Current release gate — 2026-09-08, self-hosting closeout

The runtime-source engineering release gate passed at `0716bcc9037e8a5f116d6e6c425453b60972bd6b`: vet, build, full sequential regression, real sandbox-host check, and diff-check all exited 0, with clean Git identity and binary SHA256 `C91F118B33C809DFB728AD6428BFBCD469C1F3A019CDC34DCE804F3094C34812` recorded locally. MAR later advanced `master` by one docs-only self-hosted integration and this handoff closeout will advance it once more. Therefore the final current-head claim still requires one last `D:\MAR\.mar\runtime\v1-release-final.json` refresh after this handoff commit. The record must match that final Git HEAD, every gate must exit 0, and `git_clean` must remain true.

The V1 Secure Tunnel + ChatGPT plugin path is already `PRODUCT_ACCEPTED` at the accepted pre-Console-v2 checkpoint. Owner Operations Console v2 is engineering-stable by the 2026-09-09 evidence above; explicit Owner real-use feedback remains the product-experience gate for the new Console surface only. Secure Tunnel is the current GPT primary path, while Server URL / Quick Tunnel is fallback/debug.

### Historical transport decision — 2026-09-08 (superseded by 2026-09-09 Secure Tunnel primary)

Owner selected **MCP LINK** as the temporary/current GPT path instead of requiring OpenAI Secure MCP Tunnel. The existing hardened Streamable HTTP bridge treats `chatgpt-web` and `claude-web` as peer connector profiles with independent capability URLs, stable/temporary settings, secret-link rotation and telemetry. The public Quick Tunnel process may be shared infrastructure, but traffic/state cannot promote the other connector. Secure MCP Tunnel implementation remains intact as an optional advanced/future path.

Owner real-use feedback exposed a V1 UX defect: opening MAR and then pressing **Create Link** made the connection flow feel needlessly slow. The bridge now auto-starts in the background whenever either web connector prefers temporary mode, reports `CONNECTING` truthfully, serializes concurrent Start/Restart calls, and rotates away from a dead/unresolvable Quick Tunnel after a bounded readiness attempt instead of waiting the old full 90-second window on one hostname. A real no-click candidate run reached `LINK_READY` for both GPT and Claude after automatically recovering from bad Quick Tunnel DNS. Anonymous `trycloudflare.com` remains a temporary/random hostname by design; a truly restart-stable full URL still requires the existing Stable mode with an owner-controlled public HTTPS base (for example a named tunnel/custom hostname).

Real public-Web evidence is now present. A live GPT Quick Tunnel accepted `Origin: https://chatgpt.com`; MCP `initialize` + `tools/list` succeeded through the public Internet with exactly 11 MAR tools, and GPT telemetry moved to `CONNECTED`. Public `project_context` returned project `mar`, exact HEAD and the persisted local-only policy without asking the Owner for Git vocabulary. A real public-link task then traversed submit -> Web Brain turns -> isolated worker mutation. It correctly failed closed at verification because the runtime had fallen back to `C:\Program Files\Go\bin\go.exe`, and Coding ACI could not grant sandbox read access to `C:\Program Files\Go` (`Access is denied`). Its criterion-specific file oracle nevertheless PASSed; the failed result remains durable provenance and was not integrated.

The managed Go prerequisite was restored at `D:\MAR\.mar\runtime\go-portable\go` from the installed Go 1.27.0 tree. Source and managed `go.exe` SHA256 both equal `7D828191BA32519A9C9361789AB647486236ED45C660889196C7770A8FF1985C`. MAR was relaunched with that explicit managed toolchain; sandbox policy was not weakened and no ACL exception for Program Files was added.

The bounded retry task `task-4790c13cb09e4782b0b26ff4d5c585cb` then completed end-to-end through MAR: isolated worker created only `docs/handoff/SELF_HOSTING_SMOKE.md`; the `file_contains` oracle PASSed; managed-Go `go test -run '^$'`, `go vet`, and `go build` all PASSed; durable result `result-8819e6da8eda46438f3c5c11407d33b0` / evidence `evidence-0b3aa3f6acb042929d8b208c569b2f1e` were `VERIFIED`; authoritative integration was `INTEGRATED`; and task state became `COMPLETE`. MAR itself advanced `master` from `0716bcc` to `890d55f802a3d0f957b2f7b9b2def313feae7239` with commit message `MAR candidate task=task-4790c13cb09e4782b0b26ff4d5c585cb attempt=attempt-082ca722e5ec46c394abb6a9e789b476 epoch=1`. This closes the first bounded V1.1 self-hosting task gate. The successful retry used the same SQLite/daemon authority with a local MCP stdio Web-Brain relay because the Owner-session boundary prevented programmatic restart of the Quick Tunnel after MAR restart; the earlier public-link run already proved the remote GPT transport through real worker mutation. No second coordinator was created.

Post-commit release verification at `fc668f1` also exposed one T9 acceptance-test false negative: `TestAcceptanceT9ActualDaemonCrashReconcilesWithoutFalseCompletion` ended with an empty marker after the daemon/process tree was killed. The helper rewrote the marker with `os.WriteFile` every 20 ms, so kill could land after truncate and before write, producing `""` even when containment succeeded. The production recovery path was not changed. T9 passed 10/10 isolated before the test change; the helper was hardened to fixed-width in-place writes and then passed 30/30. The resulting `0716bcc` runtime source subsequently passed the full revision-bound release gate.

### Takeover and completed corrections

- Started at `287df0014c2be0e1c844092aa78c17629cc7ec87` on `master`. The sole dirty file was this handoff. Its installation/provenance, recovered-storage, and UI-discovery notes are retained below; no reset, stash, clean, destructive checkout, push, or deploy was performed.
- Ran the existing Windows `RunAs` sandbox preparation flow. Its whole-project post-check returned 1 because the readiness timeout included project-wide ACL propagation. The isolated readiness probe then passed, confirming the host prerequisite.
- `273ac8f7dec30b33fee671c950e680da60f7a580` fixes that V1 defect by limiting NUL readiness ACL grants and cwd to the existing small `host-readiness` subtree. LPAC, capabilities, process containment, termination, and ACL cleanup remain enforced. A real OWNER RIGHTS fixture failed on the old root grant and passed after the correction. The rebuilt CLI now reports `sandbox_host_ready: true` with `-workspace D:\MAR` itself. Read-only review found no additional blocker in this change.
- `89a99d443de67672d5cc30a8fb41dd72d9a16a56` repairs the opt-in T1-T4 benchmark contracts, which predated criterion-specific oracles. Before correction all four correctly blocked as UNVERIFIED despite green commands. They now submit named behavior-test observations and assert durable acceptance evidence; runtime verification policy was not weakened.

### Verified evidence and scope

- Initial full `go vet`, build, `go test -p 1 -count=1 -timeout 180s ./...`, and diff-check passed on the takeover source before the two corrections. This is historical evidence, not the final-head release claim.
- Corrected sandbox regression: RED then GREEN. Corrected candidate vet/build and real root-workspace sandbox check: PASS.
- Explicit `MAR_RUN_SELF_HOSTING_ACCEPTANCE=1` T1-T4 at `89a99d4`: all PASS, 182.20 seconds total. Log: `D:\MAR\.mar\runtime\v1-taskclasses-89a99d4.log`. These exercise real isolated worker edits, a failing-test repair loop, sealed verification with typed observations, and authoritative local integration using a fixture model provider. They are not a live ChatGPT Goal or Owner acceptance.
- The final-head release record covers vet, build to `D:\MAR\.mar\runtime\mar-v1-stable.exe`, full default regression, real sandbox check, diff-check, clean Git identity, and executable SHA256. The default suite intentionally skips the separately executed opt-in T1-T4 benchmark; helper-process skips are not product acceptance.
- `project_read`, `project_context`, recovery/fencing/isolation, OpenAI local MCP/health/activity/lifecycle/redaction, GPT/Claude MCP-Link separation, and Connection Hub regression are covered by the relevant suite. No live GPT or Claude Web connection is claimed from engineering tests alone.
- Installed `tunnel-client` executable SHA256 `FCC85A69EC0AD82518E4F8964F60C45E31787957782A0FC9C1B0C44E82D61B9B` still matches the extracted verified v0.0.14 artifact; ZIP checksum also matches the saved official checksum file. `help quickstart`: PASS. No reinstall was needed.
- Official `tunnel-client doctor --profile mar-openai --explain` stops at missing profile (`profile_load`). No runtime profile exists. `CONTROL_PLANE_API_KEY` is absent in process/user/machine environments (presence-only inspection). The only nonempty persisted preview tunnel ID has a sequential fixture marker and is not verified Owner configuration. No secret value was printed, stored, or committed.
- Real Secure MCP Tunnel healthz/readyz, authenticated lifecycle/activity, Platform association, ChatGPT workspace association, and Tunnels Read + Use remain PENDING_OWNER_CONFIG for the optional tunnel path. They are no longer release blockers for the current MCP-Link path. No fake profile or second coordinator was introduced.

### Next action after self-hosting transition

1. Commit this handoff closeout, then run one final current-head release record refresh so the clean Git identity and all engineering gates are bound to the final docs-only HEAD.
2. Keep MCP Link as the current GPT path. On the next Owner session, MAR should prepare a temporary GPT/Claude link automatically; the Owner only copies the URL when ready, or supplies an owner-controlled Stable base when a fixed hostname across restarts is required. Perform one short Owner-driven task for product acceptance; do not bypass the startup-session credential boundary.
3. Obtain explicit Owner accept/reject feedback against a real integrated candidate. Only that may establish `PRODUCT_ACCEPTED`.
4. After the final release-record refresh succeeds, routine MAR development should use MAR itself; direct Codex/ChatCode coordination is bootstrap/emergency recovery only.

No remaining production-code blocker is proven. The first bounded V1.1 self-hosting task is complete and integrated. Missing Secure MCP Tunnel owner credentials do not block current use. The only remaining product gate is explicit Owner real-use acceptance; optional tunnel setup and further UX improvements are backlog, not V1 release blockers.

## Historical checkpoint — 2026-09-08, before takeover (superseded above)

`BLOCKED`, not `MAR_V1_STABLE` yet.

The repository implementation and full regression are clean. The remaining blockers are real host/owner gates:

1. A real `tunnel_id`, its Platform/ChatGPT workspace association, Tunnels Read + Use permission, and the runtime `CONTROL_PLANE_API_KEY` still require Owner configuration.
2. Windows sandbox preparation still fails closed at the AppContainer NUL probe and requires the existing elevated Owner preparation flow. Storage pressure has cleared: C: now has approximately 21.18 GiB vervangen free.
3. Owner real-use acceptance has not happened. Engineering tests and rendered UI evidence are not `PRODUCT_ACCEPTED` or `SELF_HOSTING_READY`.

## Completed in this pass

- Reconciled the existing project-context slice and committed it at `bc0ae69`.
- Added schema v13 singleton OpenAI tunnel configuration. It persists tunnel identity, profile, credential environment-variable name, optional client/admin paths, and desired-running state; it never persists secret values.
- Added one dedicated OpenAI Secure MCP Tunnel manager around the existing MCP backend. The private MCP target listens only on loopback and the manager runs official `init -> doctor --explain -> run` lifecycle steps.
- Added bounded owned-process Start/Stop/Restart, concurrent Stop-during-Start cancellation, crash detection, restart reconciliation, output redaction, admin `/healthz` and `/readyz` probing, and real MCP-activity telemetry.
- Hardened lifecycle failure handling at `8740028`: raw `tunnel-client` stderr can no longer escape through owner HTTP errors; configuration cannot change under an active identity; readiness/status cannot disagree during a fast start; and a failed physical Stop retains the process handle and blocks replacement instead of losing authority.
- Retired the experimental ChatGPT public Web-bridge route from runtime authority. The existing public HTTPS/Quick Tunnel manager is Claude-only; legacy `chatgpt-web` database data is ignored but retained for backward audit data.
- Reworked **Kết nối AI** into separate GPT and Claude cards with independent state, identifier, copy, lifecycle actions, last activity, errors, and collapsed diagnostics.
- Updated `README.md` with the official OpenAI setup/recovery flow and kept Claude instructions separate.
- Installed the current official Windows x64 `tunnel-client` v0.0.14 under the default managed runtime at `D:\MAR\.mar\runtime\tunnel-client.exe`. The release ZIP matched the official `SHA256SUMS.txt`, `gh attestation verify --repo openai/tunnel-client` passed, the installed executable hash matched the extracted artifact, and `help quickstart` exited 0. The binary is not Authenticode-signed; provenance relies on the official release checksum and GitHub artifact attestation.

## Verification evidence

The repository gates were executed with `TEMP/TMP=D:\MAR\.mar\runtime\testtmp` while C: was below MAR's disk reserve:

- `go vet ./...`: PASS
- `go build -o D:\MAR\.mar\runtime\mar-v1-stable.exe ./cmd/mar`: PASS
- `go test -p 1 -count=1 -timeout 180s ./...` at `c0fa4f2` before the lifecycle hardening: PASS
- current `8740028` required `cmd/mar` regression gate: HOST-BLOCKED/FAIL in `TestAcceptanceT7CLIStdioDisconnectLetsActiveWorkerReachSafeTerminal`; worker correctly rejected execution because the Windows sandbox host is not self-hosting-safe, so a current full-suite PASS cannot be claimed
- current tunnel tests repeated 30 times, owner UI tests, store/MCP/remote-bridge tests: PASS
- owner UI JavaScript syntax check: PASS
- `git diff --check`: PASS
- changed-file credential-pattern scan: PASS after removing key-shaped test fixtures
- candidate binary SHA-256: `85160B3C4726717572D658A7AD4F137DD44CFC06570BE56837C16D817155E94D`

An earlier isolated acceptance run remained in `WAITING_RESOURCE` when its temporary data root was on a full C:, correctly enforcing the frozen 2 GiB host reserve. Moving test temp to D: allowed a full PASS before the final tunnel hardening. C: now has sufficient space again, but the current revision's T7 worker still reaches the stronger sandbox gate and fails closed because AppContainer preparation is not valid for this boot. This is a host blocker, not a passing current full regression.

## Rendered owner-surface review

The built candidate was run on isolated state at `127.0.0.1:8898`; the existing owner runtime was not modified.

- Two primary cards are visible together on desktop and reflow to one column at narrow width without horizontal overflow: PASS.
- GPT and Claude lifecycle, identifier, telemetry, and errors remain independent: PASS.
- Before installation, missing `tunnel-client` appeared only on the GPT card with an install/recovery action and never reported Connected: PASS.
- After verified installation, a fresh isolated MAR sidecar discovered `D:\MAR\.mar\runtime\tunnel-client.exe` and reported `MISCONFIGURED` with the next action to enter a tunnel ID; it still did not claim Connected: PASS.
- Missing `cloudflared` appears only on the Claude card; it does not contaminate GPT status: PASS.
- Tunnel ID copy interaction gives visible `Đã sao chép` feedback: PASS.
- Owner real-use/acceptance: PENDING OWNER.

## OpenAI Secure MCP Tunnel owner steps

1. Run the existing elevated **Chuẩn bị sandbox** Owner flow and confirm `sandbox-host-check` passes.
2. In OpenAI Platform tunnel settings, create/select a tunnel and associate the intended Platform organization and ChatGPT workspace. Ensure the operator has Tunnels Read + Use.
3. Set `CONTROL_PLANE_API_KEY` in the MAR process environment. Do not paste or store its value in MAR configuration.
4. Open **Kết nối AI**, save the real `tunnel_id`, run **Chẩn đoán**, then start the GPT tunnel.
5. Confirm fresh health/readiness and select or paste that tunnel in the supported ChatGPT developer-mode app connection screen.
6. Execute one bounded real Goal, inspect durable result/evidence, and explicitly accept or reject the Owner journey.

## Next gate

`FINAL_CURRENT_HEAD_RELEASE_RECORD -> OWNER_REAL_USE_ACCEPTANCE`

The engineering/self-hosting path has real durable evidence. `PRODUCT_ACCEPTED` still requires explicit Owner acceptance. Do not add another GPT transport or bypass the frozen resource/sandbox/session policy.

## Frozen boundaries — do not reopen without concrete evidence

- MAR V1 frozen architecture is canonical only under `docs/architecture/MAR_V1_Architecture_FROZEN/`.
- MCP is the control plane, not the coding inner loop.
- SQLite is the durable coordination truth.
- One mutable task owns one isolated workspace.
- Logical fencing and physical process termination remain separate proofs.
- Worker authority remains OS-enforced and weaker than daemon authority.
- CPU/RAM/disk/process bounds and serialized authoritative integration remain mandatory.
- Ambiguous side effects reconcile before retry.
- Verification evidence remains revision/profile/environment bound.

## Environment notes

- Managed Go 1.27.0 is restored under `D:\MAR\.mar\runtime\go-portable\go`; managed/source `go.exe` SHA256 is `7D828191BA32519A9C9361789AB647486236ED45C660889196C7770A8FF1985C`. Keep MAR verification on this managed path; do not fall back to `C:\Program Files\Go` inside Coding ACI.
- C: storage recovered to approximately 21.18 GiB free during the latest continuation; D:-hosted test temp remains a valid low-risk regression location.
- The Go race detector remains unavailable because the installed host C compiler lacks required 64-bit support; this is an environment limitation, not a passing race result.


## 2026-09-15 real Web CUJ requalification: verified no-op integration

Runtime candidate `f760c0eddc972f6a25c77aa4deb80a1976b74a43` was activated on the canonical local runtime and reached `HEALTHY`, `ALIGNED`, `trusted_for_release=true`, `sandbox_host_ready=true`, and `worker_capacity_available=true` after repairing launcher-owned MAR-managed ACLs created during the SSD move.

A new read-only Web-brain CUJ, task `task-83ae4949634548249118faa11b1ab09d`, used profile `go-docs` with all mutation/network/deploy authority disabled. Verification itself PASSed completely: `go test -run ^$ ./...`, `go vet ./...`, and `go build ./...` all exited 0; acceptance oracle `file_contains:README.md:# MAR` PASSed; durable verification verdict was `VERIFIED`; candidate revision equaled base revision and `changed_areas=[]`.

The task then became `BLOCKED` only because authoritative integration required a clean registered checkout even though the verified candidate was a no-op (`final_revision == base_revision`). The registered checkout intentionally contained Owner work, so the old integration path incorrectly treated that Owner work as an integration blocker.

Frozen correction contract:
- A verified no-op candidate must still revalidate fresh verification evidence and authoritative HEAD identity.
- If `candidate_revision == expected_head`, integration must not require a clean Owner checkout.
- A no-op integration must not run `merge-base`, `update-ref`, `read-tree`, or any other Git mutation/synchronization step.
- It must finalize the durable integration result/task state through the normal store transition so recovery remains idempotent.
- Real revision-changing candidates retain the existing clean-worktree, descendant, CAS, and checkout-synchronization gates unchanged.

Regression coverage added in `internal/integration/manager_windows_test.go` proves both normal no-op integration and recovery from an already-dispatched no-op preserve a dirty Owner checkout with zero Git mutation calls.
