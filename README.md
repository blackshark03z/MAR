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

MAR intentionally fails closed instead of running model-controlled commands without the enforced sandbox.

2. Initialize MAR and register the project if they have not already been registered:

```text
mar init -db <data-root>/mar.db
mar project-add -db <data-root>/mar.db -id <project-id> -root <project-root>
```

3. Start the MCP edge with GPT Web as the coding brain:

```text
mar mcp-stdio -db <data-root>/mar.db -data-root <data-root> -brain web
```

`-brain web` does not require a model-provider API key. For fully unattended cognition, use provider mode with the configured provider URL, API-key environment variable, and model.

4. In ChatGPT, submit one bounded Goal Contract. Use `status` to follow the durable task. When `brain_turn_available` is true, call `brain_turn`, reason over the exact messages and offered tools, and answer that exact `turn_id` with `brain_respond`. Coding tool calls are executed by the isolated worker, not by the MCP process.

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
- **Connections** — capability/readiness for local MCP clients, ChatGPT bridge requirements and provider mode without pretending configuration means a live connection;
- **Advanced** — raw task/runtime diagnostics for Tech Lead/debug use.

For unattended provider-backed cognition, configure provider URL, API-key environment variable and model, then launch:

```text
mar ui -db <data-root>/mar.db -data-root <data-root> -brain provider -provider-base-url <provider-url> -api-key-env OPENAI_API_KEY -model <model>
```

The API key value remains only in the named environment variable; the Console never returns it. Token counters show only usage MAR actually receives/measures. They do not estimate hidden ChatWeb conversation usage.

Representative Owner UAT starts in the Tech Lead Web conversation: describe one real product need in normal language, allow the Tech Lead to frame the bounded Goal/acceptance/oracles, let MAR execute it without manual prompt/report forwarding, then use the product and record accept/reject/comment against the exact durable candidate/result in the Console. Engineering PASS alone is not Owner Product Acceptance.

## Verification profiles

`go-standard` runs the full sequential Go test/vet/build profile inside the enforced worker sandbox. Use it for ordinary code Goals whose repository tests are compatible with that sandbox.

`go-docs` is intentionally narrower and is only for documentation-only Goals. It compiles every test package without executing tests (`go test -run '^$'`), then runs sequential `go vet` and `go build`. It exists because MAR's own host-security/integration tests intentionally require capabilities such as raw Git fixtures, sockets, or nested AppContainer setup that the candidate LPAC is forbidden to receive. `go-docs` never substitutes for the full host release/owner acceptance gate when MAR runtime or security behavior changes.

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
