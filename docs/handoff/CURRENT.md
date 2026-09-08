# MAR — Current Tech-Lead Handoff

**Architecture:** FROZEN

**Branch:** `master`

**V1 stable implementation checkpoint:** `8740028c2540655ffeac65995080e1cab7260fc1`

**Project-context checkpoint:** `bc0ae694b8f9f382a419d4ab08676e0d13bd7b1b`

**Starting checkpoint for this stabilization pass:** `764a5648e8a19a48dbd864bc6b43cbbacd1e0d4f`

**Remote state:** local `master` is ahead of `origin/master`; nothing was pushed or deployed.

Git, the frozen architecture documents, `TASK.md`, and this handoff are continuity truth. Chat history is disposable working memory.

## Current verdict — 2026-09-08

`BLOCKED`, not `MAR_V1_STABLE` yet.

The repository implementation and full regression are clean. The remaining blockers are real host/owner gates:

1. The machine does not currently have `tunnel-client`; no real OpenAI Doctor/run/readiness or ChatGPT discovery can be claimed.
2. A real `tunnel_id`, its Platform/ChatGPT workspace association, Tunnels Read + Use permission, and the runtime `CONTROL_PLANE_API_KEY` still require Owner configuration.
3. Windows sandbox preparation currently fails closed; the latest check timed out in the AppContainer NUL probe, and the earlier probe returned `HRESULT 0x80070070`. C: now rounds to 0.00 GiB free, below MAR's hard disk reserve. Do not weaken the sandbox/resource boundary.
4. Owner real-use acceptance has not happened. Engineering tests and rendered UI evidence are not `PRODUCT_ACCEPTED` or `SELF_HOSTING_READY`.

## Completed in this pass

- Reconciled the existing project-context slice and committed it at `bc0ae69`.
- Added schema v13 singleton OpenAI tunnel configuration. It persists tunnel identity, profile, credential environment-variable name, optional client/admin paths, and desired-running state; it never persists secret values.
- Added one dedicated OpenAI Secure MCP Tunnel manager around the existing MCP backend. The private MCP target listens only on loopback and the manager runs official `init -> doctor --explain -> run` lifecycle steps.
- Added bounded owned-process Start/Stop/Restart, concurrent Stop-during-Start cancellation, crash detection, restart reconciliation, output redaction, admin `/healthz` and `/readyz` probing, and real MCP-activity telemetry.
- Hardened lifecycle failure handling at `8740028`: raw `tunnel-client` stderr can no longer escape through owner HTTP errors; configuration cannot change under an active identity; readiness/status cannot disagree during a fast start; and a failed physical Stop retains the process handle and blocks replacement instead of losing authority.
- Retired the experimental ChatGPT public Web-bridge route from runtime authority. The existing public HTTPS/Quick Tunnel manager is Claude-only; legacy `chatgpt-web` database data is ignored but retained for backward audit data.
- Reworked **Kết nối AI** into separate GPT and Claude cards with independent state, identifier, copy, lifecycle actions, last activity, errors, and collapsed diagnostics.
- Updated `README.md` with the official OpenAI setup/recovery flow and kept Claude instructions separate.

## Verification evidence

Executed with `TEMP/TMP=D:\MAR\.mar\runtime\testtmp` because C: is below MAR's disk reserve:

- `go vet ./...`: PASS
- `go build -o D:\MAR\.mar\runtime\mar-v1-stable.exe ./cmd/mar`: PASS
- `go test -p 1 -count=1 -timeout 180s ./...` at `c0fa4f2` before the lifecycle hardening: PASS
- current `8740028` required `cmd/mar` regression gate: HOST-BLOCKED/FAIL in `TestAcceptanceT7CLIStdioDisconnectLetsActiveWorkerReachSafeTerminal`; worker correctly rejected execution because the Windows sandbox host is not self-hosting-safe, so a current full-suite PASS cannot be claimed
- current tunnel tests repeated 30 times, owner UI tests, store/MCP/remote-bridge tests: PASS
- owner UI JavaScript syntax check: PASS
- `git diff --check`: PASS
- changed-file credential-pattern scan: PASS after removing key-shaped test fixtures
- candidate binary SHA-256: `85160B3C4726717572D658A7AD4F137DD44CFC06570BE56837C16D817155E94D`

An earlier isolated acceptance run remained in `WAITING_RESOURCE` when its temporary data root was on C:, correctly enforcing the frozen 2 GiB host reserve. Moving test temp to D: allowed a full PASS before the final tunnel hardening. C: then exhausted further; the current revision's T7 worker now reaches the stronger sandbox gate and fails closed because AppContainer preparation is not valid for this boot. This is a host blocker, not a passing current full regression.

## Rendered owner-surface review

The built candidate was run on isolated state at `127.0.0.1:8898`; the existing owner runtime was not modified.

- Two primary cards are visible together on desktop and reflow to one column at narrow width without horizontal overflow: PASS.
- GPT and Claude lifecycle, identifier, telemetry, and errors remain independent: PASS.
- Missing `tunnel-client` appears on the GPT card with an install/recovery action; it never reports Connected: PASS.
- Missing `cloudflared` appears only on the Claude card; it does not contaminate GPT status: PASS.
- Tunnel ID copy interaction gives visible `Đã sao chép` feedback: PASS.
- Owner real-use/acceptance: PENDING OWNER.

## OpenAI Secure MCP Tunnel owner steps

1. Free enough space on C: for Windows/AppContainer operations, then run the existing **Chuẩn bị sandbox** owner flow and confirm `sandbox-host-check` passes.
2. In OpenAI Platform tunnel settings, create/select a tunnel and associate the intended Platform organization and ChatGPT workspace. Ensure the operator has Tunnels Read + Use.
3. Install the latest official `tunnel-client`, either at `<data-root>\runtime\tunnel-client.exe`, on `PATH`, or at the absolute path selected in the GPT card.
4. Set `CONTROL_PLANE_API_KEY` in the MAR process environment. Do not paste or store its value in MAR configuration.
5. Open **Kết nối AI**, save the real `tunnel_id`, run **Chẩn đoán**, then start the GPT tunnel.
6. Confirm fresh health/readiness and select or paste that tunnel in the supported ChatGPT developer-mode app connection screen.
7. Execute one bounded real Goal, inspect durable result/evidence, and explicitly accept or reject the Owner journey.

## Next gate

`HOST_STORAGE_RECOVERY -> HOST_SANDBOX_PREP -> OPENAI_TUNNEL_DOCTOR_RUN -> CHATGPT_DISCOVERY -> REAL_BOUNDED_GOAL -> OWNER_REAL_USE_ACCEPTANCE`

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
- Keep full validation temporary files on D: until C: is safely recovered.
- The Go race detector remains unavailable because the installed host C compiler lacks required 64-bit support; this is an environment limitation, not a passing race result.
