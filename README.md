# MAR

MAR is a single-owner, single-machine, MCP-native autonomous coding runtime.

The V1 architecture is frozen. Canonical architecture documents live under `docs/architecture/MAR_V1_Architecture_FROZEN/`.

## V1 execution goal

`Goal -> Durable Task -> Isolated Worker -> Verification -> Crash-safe Integration -> Result`

The intended owner experience is:

`Owner -> Tech Lead Web -> CADS framing -> MAR MCP -> Worker(s) -> verified/integrated result -> Owner feedback`

The owner may be non-technical. Normal flow should not require the owner to author Goal Contract fields, Git/base revisions, verification profiles, run epochs, or engineering oracles. The Tech Lead Web owns requirement framing and uses CADS; MAR owns durable execution, sandboxing, resource control, criterion-bound evidence, recovery and integration authority. A provider-backed autonomous brain remains optional for unattended execution.

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

`-brain web` does not require a model-provider API key. Local MCP stdio remains the canonical process-local transport. GPT/OpenAI and Claude use separate remote transports:

- **GPT / OpenAI** uses the official outbound-only [OpenAI Secure MCP Tunnel](https://developers.openai.com/api/docs/guides/secure-mcp-tunnels). MAR gives `tunnel-client` a loopback-only Streamable HTTP MCP target; MAR MCP does not need a public listener.
- **Claude Web** keeps the existing token-bound Remote MCP path. Start a temporary Web link in **Connections**, copy the generated MCP URL into Claude `Customize -> Connectors -> Add custom connector`, then enable that connector in the conversation. A persistent HTTPS route can be configured instead of Quick Tunnel.
- **Claude Desktop** remains an optional local stdio client, not a prerequisite for Claude Web.

For fully unattended cognition, use provider mode with the configured provider URL, API-key environment variable, and model.

## GPT / OpenAI Secure MCP Tunnel

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

The Console exposes four normal surfaces:

- **Work** — current/recent durable tasks, attention queue, result/evidence, MAR-measured token usage and Owner feedback;
- **Projects** — add a supported local repository and narrow project-level local file/local Git permissions;
- **Connections** — compare GPT/OpenAI Secure MCP Tunnel and Claude Remote MCP at a glance. Each card has independent status, identifier, lifecycle actions, last activity, concise recovery guidance, and collapsed diagnostics. Provider mode and Claude Desktop `.mcpb` remain secondary options.
- **Advanced** — raw task/runtime diagnostics for Tech Lead/debug use.

For unattended provider-backed cognition, configure provider URL, API-key environment variable and model, then launch:

```text
mar ui -db <data-root>/mar.db -data-root <data-root> -brain provider -provider-base-url <provider-url> -api-key-env OPENAI_API_KEY -model <model>
```

The API key value remains only in the named environment variable; the Console never returns it. Token counters show only usage MAR actually receives/measures. They do not estimate hidden ChatWeb conversation usage.

The temporary **Claude** Web link uses a 256-bit random capability token in the URL path and a loopback-only Streamable HTTP endpoint behind an outbound Quick Tunnel. The link is session-scoped; treat the full URL like a password. MAR waits for a token-bound public health round trip before showing the link as ready, and only actual MCP traffic promotes it to connected. This Claude capability URL is not used for GPT/OpenAI; GPT has exactly one owner-facing remote transport authority: Secure MCP Tunnel.

Representative Owner UAT starts in the Tech Lead Web conversation: describe one real product need in normal language, allow the Tech Lead to frame the bounded Goal/acceptance/oracles, let MAR execute it without manual prompt/report forwarding, then use the product and record accept/reject/comment against the exact durable candidate/result in the Console. Engineering PASS alone is not Owner Product Acceptance.

## Verification profiles

`go-standard` runs the full sequential Go test/vet/build profile inside the enforced worker sandbox. Use it for ordinary code Goals whose repository tests are compatible with that sandbox.

`go-docs` is intentionally narrower and is only for documentation-only Goals. It compiles every test package without executing tests (`go test -run '^$'`), then runs sequential `go vet` and `go build`. It exists because MAR's own host-security/integration tests intentionally require capabilities such as raw Git fixtures, sockets, or nested AppContainer setup that the candidate LPAC is forbidden to receive. `go-docs` never substitutes for the full host release/owner acceptance gate when MAR runtime or security behavior changes.

Engineering acceptance is criterion-bound and machine-observed. V1 supports `output_contains:<literal>` for criterion-specific command observations and `file_contains:<relative-path>:<literal>` for direct sealed-candidate file observations. A green test/vet/build profile by itself never promotes an arbitrary criterion to PASS. If the declared observation is absent or unsupported, the criterion is `UNVERIFIED` and cannot integrate. Owner real-use acceptance remains separate durable feedback on the exact integrated candidate/result.

## MCP control surface

The public task-oriented MCP surface is intentionally limited to:

`submit`, `status`, `steer`, `input`, `cancel`, `result`, `inspect`, `brain_turn`, `brain_respond`.

The first seven tools are durable task control/read operations. `brain_turn` and `brain_respond` are a typed reasoning relay for Web-brain mode: they do not expose direct filesystem, Git mutation, or command authority to the MCP client.

Low-level coding primitives such as repository reads/writes, Git inspection, and allowed command execution remain inside the worker runtime under the immutable Goal Contract and Windows sandbox.

## Project Brain V1

Initial repository context is selected deterministically with a bounded, model-free retrieval pipeline: BM25F-style lexical relevance, exact path/symbol ranks, local dependency graph propagation, Personalized PageRank, and reciprocal-rank fusion. Go uses the standard parser/AST; Python and JavaScript/TypeScript use lightweight bounded syntax/import scanners; other languages fall back to lexical/path retrieval.

The frozen V1 retrieval gate is MRR >= 0.950 and Recall@3 = 1.000 on `TestContextRetrievalBenchmarkV1`. The current implementation scores MRR 1.000 and Recall@3 1.000. No embedding model, vector store, Tree-sitter/SCIP dependency, or second Project-Brain database is added before V1 owner acceptance.

## Development rule

Architecture is closed. Implementation may only reopen architecture when benchmark, recovery, or owner real-use evidence proves a frozen invariant insufficient.

See `docs/architecture/MAR_V1_Architecture_FROZEN/00_ARCHITECTURE_FREEZE_RECORD.md`, `10_IMPLEMENTATION_ENTRY_CONTRACT.md`, and `docs/implementation/017_PROJECT_BRAIN_V1.md` before changing runtime design.
