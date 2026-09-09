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

## Current release gate — 2026-09-08, self-hosting closeout

The runtime-source engineering release gate passed at `0716bcc9037e8a5f116d6e6c425453b60972bd6b`: vet, build, full sequential regression, real sandbox-host check, and diff-check all exited 0, with clean Git identity and binary SHA256 `C91F118B33C809DFB728AD6428BFBCD469C1F3A019CDC34DCE804F3094C34812` recorded locally. MAR later advanced `master` by one docs-only self-hosted integration and this handoff closeout will advance it once more. Therefore the final current-head claim still requires one last `D:\MAR\.mar\runtime\v1-release-final.json` refresh after this handoff commit. The record must match that final Git HEAD, every gate must exit 0, and `git_clean` must remain true.

`PRODUCT_ACCEPTED` remains unclaimed pending explicit Owner real-use acceptance. The engineering evidence now supports `MAR_V1_STABLE` and `MAR_SELF_HOSTING_READY` once the final current-head release record above is refreshed successfully. OpenAI Secure MCP Tunnel remains optional/backlog and does not block the current MCP-Link path.

### Current transport decision — 2026-09-08

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
