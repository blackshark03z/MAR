# MAR — Current Tech-Lead Handoff

**Architecture:** FROZEN

**Branch:** `master`

**V1 stabilization implementation checkpoint:** `89a99d443de67672d5cc30a8fb41dd72d9a16a56`

**Project-context checkpoint:** `bc0ae694b8f9f382a419d4ab08676e0d13bd7b1b`

**Starting checkpoint for this stabilization pass:** `764a5648e8a19a48dbd864bc6b43cbbacd1e0d4f`

**Remote state:** local `master` is ahead of `origin/master`; nothing was pushed or deployed.

Git, the frozen architecture documents, `TASK.md`, and this handoff are continuity truth. Chat history is disposable working memory.

## Current release gate — 2026-09-08, Codex takeover

Target verdict: `ENGINEERING_V1_STABLE_PENDING_OWNER_CONFIG`. Confirm it only when
`D:\MAR\.mar\runtime\v1-release-final.json` matches the current Git HEAD, every
release gate exits 0, and `git_clean` is true. The final full release gate runs
after this handoff commit, so its exact revision and executable hash belong in
that local evidence record rather than a self-referential commit hash here.
If the record is missing, stale, or failing, final release confirmation remains
UNVERIFIED. Do not infer a current full-suite PASS from the historical section.

`PRODUCT_ACCEPTED`, `MAR_V1_STABLE`, and `MAR_SELF_HOSTING_READY` remain unclaimed.
OpenAI account/workspace configuration and the real Web journey are still pending.

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
- `project_read`, `project_context`, recovery/fencing/isolation, OpenAI local MCP/health/activity/lifecycle/redaction, Claude-only separation, and Connection Hub regression are covered by the full suite. Earlier rendered UI evidence remains applicable: this takeover changed no UI source. No live Claude connection is claimed from those tests.
- Installed `tunnel-client` executable SHA256 `FCC85A69EC0AD82518E4F8964F60C45E31787957782A0FC9C1B0C44E82D61B9B` still matches the extracted verified v0.0.14 artifact; ZIP checksum also matches the saved official checksum file. `help quickstart`: PASS. No reinstall was needed.
- Official `tunnel-client doctor --profile mar-openai --explain` stops at missing profile (`profile_load`). No runtime profile exists. `CONTROL_PLANE_API_KEY` is absent in process/user/machine environments (presence-only inspection). The only nonempty persisted preview tunnel ID has a sequential fixture marker and is not verified Owner configuration. No secret value was printed, stored, or committed.
- Real tunnel healthz/readyz, authenticated lifecycle/activity, Platform association, ChatGPT workspace association, Tunnels Read + Use, and ChatGPT discovery are PENDING_OWNER_CONFIG. The current Codex tool inventory exposes no MAR connector. No fake profile or new transport was introduced.

### Next action and self-hosting transition

1. Supply a real tunnel ID, associate the intended Platform organization and ChatGPT workspace, and grant Tunnels Read + Use. Supply `CONTROL_PLANE_API_KEY` only in the MAR process environment; do not put the value in chat, files, Git, or MAR settings.
2. Use the existing GPT Connection Hub to configure, diagnose, and start the official tunnel. Confirm actual health/readiness/activity, then discover MAR tools in ChatGPT and execute one simple read.
3. Run one small real Goal through Web Brain -> MAR MCP -> MAR worker -> verify -> integrate; preserve its durable evidence. Obtain explicit Owner real-use acceptance separately.
4. Immediately perform the first bounded V1.1 task through MAR itself. Only after its Goal, isolated edits, verification, integration, and handoff evidence pass may `MAR_SELF_HOSTING_READY` be declared. Direct Codex coordination then remains bootstrap/emergency recovery only.

No remaining code blocker is proven by this pass; final engineering confirmation is subject to the revision-bound release record above. The unavailable authenticated OpenAI configuration prevents the real MAR task and self-hosting transition. No V1.1 work has started. Backlog: first small diagnostics/docs task through MAR after that gate; no new product scope.

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

`HOST_SANDBOX_PREP -> OPENAI_OWNER_CONFIG -> OPENAI_TUNNEL_DOCTOR_RUN -> CHATGPT_DISCOVERY -> REAL_BOUNDED_GOAL -> OWNER_REAL_USE_ACCEPTANCE`

Do not claim `MAR_V1_STABLE`, `PRODUCT_ACCEPTED`, or `SELF_HOSTING_READY` until every gate above passes. Do not add another GPT transport or bypass the frozen resource/sandbox policy.

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

- Use the hash-verified portable Go under `D:\MAR\.mar\runtime\go-portable` if Go is unavailable from PATH.
- C: storage recovered to approximately 21.18 GiB free during the latest continuation; D:-hosted test temp remains a valid low-risk regression location.
- The Go race detector remains unavailable because the installed host C compiler lacks required 64-bit support; this is an environment limitation, not a passing race result.
