# MAR — Current Tech-Lead Handoff

**Architecture:** FROZEN

**Branch:** `master`

**V1 stabilization implementation checkpoint:** `89a99d443de67672d5cc30a8fb41dd72d9a16a56`

**Project-context checkpoint:** `bc0ae694b8f9f382a419d4ab08676e0d13bd7b1b`

**Starting checkpoint for this stabilization pass:** `764a5648e8a19a48dbd864bc6b43cbbacd1e0d4f`

**Remote state:** local `master` is ahead of `origin/master`; nothing was pushed or deployed.

**Product transport decision — 2026-09-09:** `OPENAI_SECURE_TUNNEL_PRIMARY`

ChatGPT/GPT uses OpenAI Secure MCP Tunnel as the normal primary path. Saving a valid Tunnel ID arms durable desired-running state; MAR reuses the same tunnel identity and attempts automatic recovery on later launches when the local client and credential prerequisite are present. Server URL / Quick Tunnel remains an explicitly temporary fallback/debug path and is not presented as stable primary. Claude Web remains independent from GPT Secure Tunnel state and keeps its separate capability URL/telemetry path. Tunnel API-key values are environment-only and must never be persisted or rendered.

Git, the frozen architecture documents, `TASK.md`, and this handoff are continuity truth. Chat history is disposable working memory.

## Owner Operations Console v5 light + multi-flow redesign — 2026-09-09

Owner real-use of the v4 candidate requested two concrete changes: move to a modern light visual system and make concurrency understandable when multiple GPT/Claude connection paths and MAR execution flows exist at the same time. v5 therefore stops using a single ambiguous `connected` number and separates **routes**, **authoritative sessions**, and **live execution flows**.

`docs/design/OWNER_CONSOLE_V5.md` is the current design/observability contract. The sidebar now includes a dedicated **Live Operations** view. Overview is reduced to executive operational answers: aggregate readiness, live flows, authoritative sessions, provider-route readiness, owner action count and durable tokens today. Live Operations owns scalable realtime detail: provider summaries, execution-flow table, route/session table, Event Stream and active-turn token trend.

A live execution flow is one active `task_id + run_epoch`. `run_epoch` is now exposed in Owner task telemetry so retries/replacement attempts cannot be visually conflated. Stateful MCP routes contribute authoritative session counts; stateless transports remain unknown, so a mixed system can correctly display a value such as `5 + ?` instead of converting the unknown route to zero. Route readiness remains separate from session/activity truth.

The light visual system is intentional and OS-theme independent: page background `#F6F8FB`, white surfaces, slate text, blue primary action, emerald/amber/red semantic statuses, soft borders/shadows and tables for multi-flow density. This avoids the dark/card-heavy appearance rejected in owner UAT and scales better when flow count grows.

Live Web Brain usage still comes only from durable WebTurn responses for the exact task/run epoch and remains labelled estimated (`~`), not provider billing. WebTurn records currently do not contain the responding connector identity, so v5 explicitly leaves per-flow provider attribution unavailable rather than guessing GPT vs Claude. Durable day/week/30-day Usage remains TaskResult-based and separate.

Current pre-release evidence: v5 targeted `cmd/mar + internal/store + internal/service` tests PASS, Owner JS syntax PASS and `git diff --check` PASS. The v5 candidate is live on loopback HTTP 200 and a real Chrome headless run executed the UI JavaScript and showed seeded GPT/Claude/route Event Stream entries. A Chrome unique-profile headless mode on this installation returns zero-byte output and is not used as an acceptance oracle. Full repository regression, vet/build, commit and revision-bound release binding are still required before declaring v5 ENGINEERING_STABLE.

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
