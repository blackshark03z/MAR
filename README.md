# MAR

MAR is a single-owner, single-machine **trustworthy local execution bridge** for external cognition. Read-only project discovery stays lightweight; mutation-capable work enters MAR's governed authority/isolation/verification/publication path. MCP and cognition surfaces are adapters, never durable task authority. ChatCode may remain a temporary bootstrap/compatibility transport during migration, but it is not a required forward dependency.

The accepted V1 safety architecture remains frozen under `docs/architecture/MAR_V1_Architecture_FROZEN/`. Forward architecture evolution is governed by `docs/architecture/MAR_ARCHITECTURE_CONSTITUTION.md`, `docs/architecture/MAR_EXTERNAL_COGNITION_CONTRACT.md`, and the conditional-entry amendment `docs/architecture/MAR_CONDITIONAL_EXECUTION_KERNEL.md`.

## Execution boundary

Normal CADS development may execute directly:

`CADS Design -> direct harness -> candidate -> Product Acceptance`

When explicit runtime properties require governance:

`CADS Design -> MAR -> replaceable harness -> revision-bound verification/integration -> Product Acceptance`

Inside MAR, the V1 governed execution goal remains:

`Goal -> Durable Task -> Isolated Worker -> Verification -> Crash-safe Integration -> Result`

MAR task/attempt/workspace/resource states apply only after a task enters this governed path. A normal direct task does not need a MAR task ID or lifecycle state.

Current Web clients such as ChatGPT/Claude and provider mode remain compatibility execution adapters. CADS/Tech Lead owns product framing and acceptance; the harness owns coding mechanics; MAR owns only the governed execution properties it actually enforces. New provider/model/session/cognition functionality is not forward kernel scope by default.

## First self-hosting run

1. Create a dedicated MAR-owned sandbox probe directory under the MAR data root, for example `<data-root>/sandbox-host-probe`. The probe directory must already exist before running the host check. Use this scratch directory instead of the project root so historical project ACL/worktree state cannot interfere with the prerequisite probe.

```text
mar sandbox-host-check -workspace <data-root>/sandbox-host-probe
```

If the check reports that the sandbox host is not prepared, run the following once from an **elevated Administrator terminal after each Windows boot** against that same probe directory, then re-run the check there:

```text
mar sandbox-host-prepare -workspace <data-root>/sandbox-host-probe
mar sandbox-host-check -workspace <data-root>/sandbox-host-probe
```

MAR intentionally fails closed instead of running model-controlled commands without the enforced sandbox. In the normal Owner Console journey, the **Connections** screen detects this prerequisite after a reboot and offers **Prepare sandbox**. MAR invokes the same preparation command through Windows UAC and automatically re-checks readiness after the owner approves the Windows permission prompt, so the owner does not need to open an Administrator terminal. The commands above remain the diagnostic/manual fallback.

2. Initialize MAR and register the project if they have not already been registered:

```text
mar init -db <data-root>/mar.db
mar project-add -db <data-root>/mar.db -id <project-id> -root <project-root>
```

3. Start the MCP edge with a Web/desktop Tech Lead client as the coding brain:

```text
mar mcp-stdio -db <data-root>/mar.db -data-root <data-root> -brain web
```

`-brain web` does not require a model-provider API key. Local MCP stdio remains the canonical process-local transport. For current V1 Web use, GPT/OpenAI and Claude each receive a separate token-bound **MCP Link** on MAR's Streamable HTTP bridge:

- **GPT / OpenAI** gets its own capability URL and realtime telemetry. Start the temporary bridge in **Connections**, copy the GPT MCP URL into the supported ChatGPT MCP connection surface, or configure a persistent HTTPS base route instead of Quick Tunnel.
- **Claude Web** gets a different capability URL on the same hardened bridge engine. Copy the Claude MCP URL into `Customize -> Connectors -> Add custom connector`, then enable that connector in the conversation.
- **OpenAI Secure MCP Tunnel** remains implemented and available as an optional advanced/future transport; it is not required for the current MCP Link V1 gate.
- **Claude Desktop** remains an optional local stdio client, not a prerequisite for Claude Web.

For fully unattended cognition, use provider mode with the configured provider URL, API-key environment variable, and model.

## Optional GPT / OpenAI Secure MCP Tunnel

1. Create or select a tunnel in OpenAI Platform tunnel settings and associate it with the Platform organization and ChatGPT workspace that should use MAR. Copy its `tunnel_id`.
2. Install `tunnel-client` from the Platform download link or the [official latest release](https://github.com/openai/tunnel-client/releases/latest). MAR searches `<data-root>/runtime/tunnel-client.exe` first, then `PATH`; an absolute path can also be entered under **Connections -> OpenAI / GPT -> Thiết lập & chi tiết**.
3. Put the runtime control-plane API key in `CONTROL_PLANE_API_KEY` before launching MAR Console. The setting may name a different environment variable, but the secret value itself is never stored in SQLite, returned by the runtime API, rendered by the Console, or passed on the command line.
4. In **Connections**, enter `tunnel_id`, save, run **Chẩn đoán**, then choose **Bắt đầu GPT tunnel**. MAR initializes the named local HTTP profile, runs `tunnel-client doctor --profile <profile> --explain`, and starts `tunnel-client run --profile <profile>` only after Doctor passes.
5. Copy the tunnel ID into the supported OpenAI connection surface. Keep the tunnel running while ChatGPT, Codex, or an API flow uses MAR.

The GPT card reports configuration, owned-process state, `/healthz`, `/readyz`, last successful readiness/activity, and the last redacted error. Actual MCP traffic also establishes connection truth when the local admin address is not discoverable. A saved desired-running state restarts the tunnel after MAR restarts, but the new process must produce fresh readiness or MCP traffic before MAR can report `CONNECTED`; stale state is never reused.

If the tunnel is not visible in ChatGPT, verify the tunnel-to-workspace association and Tunnels Read + Use permission in OpenAI Platform. If the process is running but degraded, use **Chẩn đoán** and inspect the collapsed details. `tunnel-client` requires outbound HTTPS to OpenAI and local access to the MAR MCP target; it does not require inbound Internet.

4. In an MCP-capable Tech Lead client, submit one bounded Goal Contract. Use `status` to follow the durable task. When `brain_turn_available` is true, call `brain_turn`, reason over the exact messages and offered tools, and answer that exact `turn_id` with `brain_respond`. Coding tool calls are executed by the isolated worker, not by the MCP process.

5. After completion, use `result` for the revision-bound verification/integration result and `inspect` for the full task, workspace, attempt, checkpoint, control, evidence, and pending-brain state.

## Owner Console / hands-on V1 UAT

The local Owner Console is MAR's setup and operations surface, not the primary place where the owner writes engineering contracts. It listens on loopback only, uses a startup-session token for mutations, and does not create a second task/integration authority.

Launch in Web-brain mode:

```text
mar ui -db <data-root>/mar.db -data-root <data-root> -brain web
```

Then open `http://127.0.0.1:8787`.

For the single-owner Windows installation, `scripts/start-owner-console.ps1` is the idempotent launcher used by the user Startup shortcut. It exits successfully when the verified MAR Console is already listening, refuses to reuse port 8787 if another process owns it, and otherwise starts the current stable binary with the persisted MAR database/data root and managed Go toolchain. This keeps the localhost Console available after Windows sign-in without creating a second runtime authority.

The Console exposes four normal surfaces:

- **Work** — current/recent durable tasks, attention queue, result/evidence, MAR-measured token usage and Owner feedback;
- **Projects** — add a supported local repository and narrow project-level local file/local Git permissions;
- **Connections** — compare GPT and Claude MCP Links at a glance. Each has its own capability URL, status, last activity, route/MCP telemetry, stable/temporary mode and secret-link rotation. OpenAI Secure MCP Tunnel, provider mode and Claude Desktop `.mcpb` remain secondary options.
- **Advanced** — raw task/runtime diagnostics for Tech Lead/debug use.

For unattended provider-backed cognition, configure provider URL, API-key environment variable and model, then launch:

```text
mar ui -db <data-root>/mar.db -data-root <data-root> -brain provider -provider-base-url <provider-url> -api-key-env OPENAI_API_KEY -model <model>
```

The API key value remains only in the named environment variable; the Console never returns it. Token counters show only usage MAR actually receives/measures. They do not estimate hidden ChatWeb conversation usage.

The temporary **GPT** and **Claude** Web links each use a different 256-bit random capability token in the URL path and the same loopback-only Streamable HTTP bridge behind an outbound Quick Tunnel. Treat each full URL like a password. MAR waits for a token-bound public health round trip before showing a link as ready, and only traffic observed on that connector promotes that connector to connected. Rotating one connector revokes only its old capability path; the other connector profile and telemetry remain independent.

Representative Owner UAT starts in the Tech Lead Web conversation: describe one real product need in normal language, allow the Tech Lead to frame the bounded Goal/acceptance/oracles, let MAR execute it without manual prompt/report forwarding, then use the product and record accept/reject/comment against the exact durable candidate/result in the Console. Engineering PASS alone is not Owner Product Acceptance.

## Verification profiles

`go-standard` runs the full sequential Go test/vet/build profile inside the enforced worker sandbox. Use it for ordinary code Goals whose repository tests are compatible with that sandbox.

`go-docs` is intentionally narrower and is only for documentation-only Goals. It compiles every test package without executing tests (`go test -run '^$'`), then runs sequential `go vet` and `go build`. It exists because MAR's own host-security/integration tests intentionally require capabilities such as raw Git fixtures, sockets, or nested AppContainer setup that the candidate LPAC is forbidden to receive. `go-docs` never substitutes for the full host release/owner acceptance gate when MAR runtime or security behavior changes.

Engineering acceptance is criterion-bound and machine-observed. V1 supports `output_contains:<literal>` for criterion-specific command observations and `file_contains:<relative-path>:<literal>` for direct sealed-candidate file observations. A green test/vet/build profile by itself never promotes an arbitrary criterion to PASS. If the declared observation is absent or unsupported, the criterion is `UNVERIFIED` and cannot integrate. Owner real-use acceptance remains separate durable feedback on the exact integrated candidate/result.

## MCP control surface

Normal `tools/list` exposes seven canonical domain tools:

`project`, `action`, `submit`, `task`, `control`, `brain_turn`, `brain_respond`.

`project` is the bounded read-only discovery surface. `action` is the Trusted Owner Fast Path for ordinary development: hash-guarded create/replace, exact-hash patch, argv-only bounded run, Git stage/commit, and non-force push gated by project policy, with no durable Goal/task/brain lifecycle. `submit`, `task`, and `control` retain the governed high-assurance workflow; `brain_turn` and `brain_respond` remain its external-cognition relay.

Use `action` by default for normal single-owner edit/test/commit work. Use the governed Goal path when independent candidate verification, publication authorization, or recovery guarantees are actually required.

## Project Brain V1

Initial repository context is selected deterministically with a bounded, model-free retrieval pipeline: BM25F-style lexical relevance, exact path/symbol ranks, local dependency graph propagation, Personalized PageRank, and reciprocal-rank fusion. Go uses the standard parser/AST; Python and JavaScript/TypeScript use lightweight bounded syntax/import scanners; other languages fall back to lexical/path retrieval.

The frozen V1 retrieval gate is MRR >= 0.950 and Recall@3 = 1.000 on `TestContextRetrievalBenchmarkV1`. The current implementation scores MRR 1.000 and Recall@3 1.000. No embedding model, vector store, Tree-sitter/SCIP dependency, or second Project-Brain database is added before V1 owner acceptance.

## Development rule

The earned V1 safety kernel remains closed unless benchmark, recovery, deployment or owner real-use evidence proves a frozen invariant insufficient. Future evolution must also obey the Architecture Constitution's measured-benefit / complexity gate; do not interpret stronger models or newer agent frameworks as automatic reasons to add MAR subsystems.

Before changing runtime design, read `docs/architecture/MAR_ARCHITECTURE_CONSTITUTION.md`, `docs/architecture/MAR_EXTERNAL_COGNITION_CONTRACT.md`, the relevant frozen V1 invariant/ADR, and the current bounded release roadmap. Current next line: `docs/roadmap/MAR_V1_3_PERFORMANCE_AND_SIMPLICITY.md`.
