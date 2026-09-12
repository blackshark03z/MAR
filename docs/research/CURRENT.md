# MAR research — current state

**Mode:** `RESEARCH_ONLY`  
**Stable baseline:** MAR v1.1.0 remains frozen  
**Date:** 2026-09-13

## Current priority

Broad P0 research is **converged**. R-022 is the evidence ledger, R-027 is the candidate synthesis, and R-028 is the bounded V1.2 scope decision. Do not open additional broad research topics unless new evidence invalidates that decision.

Default next action:

```text
use R-028 as implementation entry contract
  -> open Slice A only when implementation is explicitly authorized
  -> keep Slices B-D sequential and acceptance-gated
  -> E1/E2 remain optional bounded follow-up
  -> preserve V1.1 safety invariants and stop V1.2 after acceptance
```

The only material unclosed proof is actual OS worker-process continuity during external route loss, currently `BLOCKED_BY_SANDBOX` on this boot. It does not block the candidate synthesis because transport/durable-state continuity is already proven across ACK loss, session expiry and real Quick Tunnel replacement.

## Confirmed architecture/source facts

These are source/durable facts, not performance conclusions:

- MAR durable task/attempt/WebTurn identity is independent from ordinary remote request lifetime.
- submit and brain-response paths have durable idempotency/conflict primitives.
- DecisionProjection removed the dominant legacy full-history replay pattern in measured later turns.
- SQLite is configured with WAL + synchronous FULL but the Go pool is intentionally limited to one open connection.
- Owner Console polls runtime/tasks every 2 seconds; runtime polling executes a real sandbox-host-check subprocess.
- task-list polling can structurally issue up to 91 SQLite reads per poll at the 30-task limit and live usage reads full WebTurn request/response rows.
- Web Brain wait keeps the active worker/heavy lease and polls for response every 200 ms; daemon active-task monitoring also polls every 200 ms.
- production defaults reserve ~256 MiB RAM + 256 MiB disk per execution lease and normally allow two concurrent workers.
- context construction may execute five contained Git commands per build and scan/hash up to 2,000 files / 8 MiB; DecisionProjection mode currently performs an initial build plus another build at turn 1.
- brain_turn transports the exact WebTurn request to external Web cognition; MAR-side bounded context does not prove Web-client conversation context is bounded.
- verification/integration repeatedly revalidates environment/candidate freshness around authoritative integration.

## Newly confirmed live/reconstructed signals

- Running runtime identity is invalid for V1.1 performance baselines: both live MAR processes use root `D:\\MAR\\mar.exe` SHA-256 `74FAFFC1...`; the binary has no `vcs.revision`, no matching archived runtime artifact hash, and its Owner UI serves legacy monolithic HTML rather than the accepted V1.1 React/Vite asset model. Classification: `RUNNING_RUNTIME_NOT_ACCEPTED_V1_1`; latency/mutation baselines remain `NOT_RUN_RUNTIME_IDENTITY`.
- External Web cognition amplification has now been byte-level reconstructed. Across 90 DecisionProjection turns, inner request volume 5.46 MB becomes ~11.83 MB in the current TextContent + StructuredContent application representation (`2.167x` aggregate). The accepted five-turn V1.1 task is `2.129x`. These are simulated application bytes, not measured HTTP wire bytes or hidden model context.
- Remote submit ambiguous-ACK recovery is now executable evidence on accepted source: a research-only HTTP harness dropped the first response after durable submit commit, then reconnected with a new MCP client/session and retried the same idempotency key. Both stateful and stateless modes returned the original task with `created=false`; no duplicate task identity was produced.
- `brain_respond` post-commit/lost-ACK recovery is also executable evidence: a real temporary task/WebTurn was durably responded, the task resumed `RUNNING`, the first ACK was injected away, and a new session retried the exact same response. Both stateful and stateless modes returned the original turn with `created=false`.
- Deterministic pre-TaskService cancellation is proven for submit and brain response in both stateful/stateless modes. Request cancellation reaches the backend; no hidden submit occurs; a pending WebTurn remains `INPUT_REQUIRED`; and reconnect retry succeeds normally. Stateful session expiry is proven too: after forced 1-second session expiry, a new client reconnects and retrying the same submit identity returns the original task with `created=false`; the automated transport semantics sentinel passes 9/9 cases. Real Quick Tunnel replacement is proven as well: cloudflared #1 is killed while a WebTurn is pending, durable state remains `INPUT_REQUIRED`, cloudflared #2 comes up on a new public hostname, and a new client responds to the original turn and resumes the same task. Actual OS worker-process continuity is `BLOCKED_BY_SANDBOX` on the current boot (`sandbox_ready=false`), so no mock is promoted as proof. Exact mid-transaction crash points remain optional deeper research.

## Material hypotheses awaiting live benchmark

1. `OWNER_CONSOLE_OBSERVER_EFFECT` — polling/process/DB work may measurably perturb MAR.
2. `WEB_WAIT_CAPACITY_STARVATION` — two waiting Web tasks may occupy both default worker slots while useful compute is idle.
3. `SQLITE/WAL_OBSERVER_PRESSURE` — corrected topology: Owner UI parent and mcp-stdio runtime use separate SQLite handles/processes. Frequent/full-payload reads may still create shared WAL/filesystem/host pressure, while Web-wait polling inside the runtime child does share that child's one-connection handle.
4. `CONTEXT_REBUILD_OVERHEAD` — unchanged/read-only turns may pay repeated Git scan/hash cost.
5. `OUTER_WEB_CONTEXT_AMPLIFICATION` — exact brain_turn payloads may still grow Web-chat/browser pressure despite bounded internal projection.
6. `VERIFICATION_INTEGRATION_COST` — safety/freshness revalidation may be a material wall-time share.
7. `CONNECTION_SELF_HEALING` — owned GPT/Quick Tunnel process crashes are detected truthfully but are not automatically restarted inside the same MAR process; boot-time desired-state restoration is not same-process recovery.
8. `STALE_CONNECTED_SEMANTICS` — request arrival can establish route health before application outcome, Secure Tunnel fallback without admin health has no activity TTL, and temporary Quick Tunnel public health is not continuously reprobed after startup.
9. `RUNTIME_CHILD_LIVENESS` — Owner UI/remote bridge parent and `mcp-stdio` execution child are separate roles; source inspection shows no parent-side child restart/watchdog. Remote parent service may remain reachable/persist tasks while daemon execution is absent, while restart recovery itself remains fail-closed.

## Measured performance calibration

- Context rebuild repeat-compute is now measured on accepted production source: 8 consecutive unchanged builds produced identical pack hashes; warm `Engine.Build()` median ~620 ms, warm Git snapshot median ~303 ms, 273 files scanned/build, five Git subprocesses/build. A five-turn task's structural six-build path is roughly ~3.9 s on this fixture, so context rebuild is real but **moderate rather than dominant** versus multi-minute Web cognition waits.
- Verification timing is now reconstructed automatically from durable command evidence. Successful `go-standard` suites (n=6) have median command time ~84.2 s; successful `go-docs` suites are 144.4 s and 259.1 s (n=2). The accepted V1.1 task spent 259.1 s / 61.4% of its 422.2 s wall time in verification commands versus 122.3 s / 29.0% waiting for Web cognition. However successful `go-standard` tasks have median verification share only ~4.0% because their Web waits are much longer. Verification optimization must therefore be profile/task-sensitive.
- Failed-test repair latency is also measurable: four `go-standard` suites continued `vet + build` for ~29.5–32.1 s after `go test` had already failed. Treat this as a fast-feedback research opportunity, not automatic waste; final authoritative verification must remain complete unless equivalent evidence is proven.
- Integration is currently small in the durable sample: 7/7 attempts COMPLETE, median ~1.25 s, max ~2.24 s. Do not prioritize integration micro-optimization over verification/Web wait/context transport based on current evidence.
- Owner backend polling has now been A/B tested on an accepted-source isolated runtime. `/api/runtime` at the UI's 2-second cadence creates exactly one real `sandbox-host-check` child per poll (5/5 in every repeat), with ~102–133 ms median endpoint latency and ~29–31 MiB transient process-tree RSS increase. `/api/tasks` on 30 terminal tasks is much cheaper (~8–29 ms median). A follow-up fixture with one RUNNING task carrying 19 WebTurns/~919 KiB made 10 task-list calls reread ~9.8 MiB versus ~1.25–1.29 MiB terminal-only: stable extra ~8.55 MiB/10 calls (~0.855 MiB/call), median latency ~38–45.5 ms, RSS mean +3.7–4.3 MiB, while response grew only ~975 bytes. Prioritize cached/adaptive sandbox readiness plus narrow live-usage summary queries before broad DB architecture changes.
- Verification cache cost is structurally explained by the current safety model: Go verification uses task-local writable `GOCACHE/GOMODCACHE/GOTMPDIR` under the isolated workspace, and runtime comments explicitly expect cold self-hosting verification. Any speed experiment must preserve this authority boundary; do not reintroduce a shared writable Go cache. Research cold-vs-warm task-local cost and safe seed/reuse designs instead.

R-027 records the evidence-backed candidate requirements. R-028 narrows them into the bounded V1.2 implementation-entry scope: core Slices A-D, conditional follow-up E1/E2, explicit out-of-scope items and a release stop rule. Production implementation is still unauthorized until an implementation slice is explicitly opened.

## Proven research signals

- historical legacy request replay factor was materially larger than current DecisionProjection population (~4.47x vs ~1.04x under the phase-0 structural comparison).
- measured current DecisionProjection requests contain exact same-request recent-evidence/protocol-tail duplication (~8.6% aggregate in the phase-0 sample).
- release artifact provenance is insufficient: existing stable binary/manifest identity does not cleanly bind to the later Owner-accepted V1.1 implementation source.
- historical host baseline can be near ~78% Windows commit pressure and ~4.5 GiB Chrome aggregate RSS even with MAR stopped; Chrome OOM cannot be assigned to MAR without correlated evidence.
- a live 2026-09-12 `/api/runtime` microbenchmark confirmed real sandbox-check process amplification: 10 synthetic runtime requests at 2 s cadence all returned 200 and produced roughly 11 additional observed `sandbox-host-check` process launches over a 20 s baseline window. Treat this as event-count evidence only; host pressure invalidated latency conclusions.
- live process topology at the same checkpoint showed the normal Owner UI parent -> `mcp-stdio` child relationship present.
- historical Web-wait reconstruction covered 368 responded turns across 27 tasks: median 36.8 s, p95 233.7 s, max 1103.6 s, ~7.7 h aggregate wait. Historical maximum concurrency was one pending turn, so two-waiter worker-slot starvation remains a stress hypothesis rather than an observed incident. The source-derived full-row polling path corresponds to ~25.7 GiB aggregate byte-touch proxy over those waits, explicitly not physical I/O.

## Tunnel failure-domain fault injection

Disposable Quick Tunnel fault injection now proves two externally distinguishable 502 classes without touching production MAR: stopping the local origin while `cloudflared` remains connected yields `HA=1`, `request_errors +1`, local origin unavailable and public 502 (`LOCAL_ORIGIN_UNREACHABLE`); stopping `cloudflared` while the local origin remains HTTP 200 makes the metrics endpoint disappear and the public route return 502 (`TUNNEL_PROCESS_OR_METRICS_UNAVAILABLE`). Restarting the origin alone restores the existing public route. This is sufficient to automate conservative tunnel/origin attribution; production incident frequency remains unmeasured.

## Research observer self-effect correction

The 5-second active transport watcher no longer calls `/api/runtime`. It now uses loopback TCP plus owned MAR process-role observation and requires an `ui` parent plus `mcp-stdio` child for `LOCAL_READY`. This removes a previously self-inflicted ~12 `sandbox-host-check` launches/minute. A 20-second post-restart verification window sampled 76 times and observed zero sandbox-host-check launches. Hourly snapshots retain one bounded runtime API observation.

## Measurement stack already active outside MAR

`D:\MAR-Research\observer` currently provides research-only:

- hourly durable/context/transport/resource snapshot;
- 24h aggregation;
- active local transport watcher;
- active host resource watcher.

See R-007, R-008, R-009 and R-023 for exact coverage and blind spots.

## Current live constraints

- Host validity is dynamic rather than permanently blocked. An earlier 46 s window saw Windows commit pressure oscillate from ~81% to 99.3% because of a separate non-MAR Python workload; a later gate sample was clean (~79.8% commit, ~25.6% median CPU). Heavy benchmarks must therefore evaluate the gate immediately before each run instead of inheriting the earlier invalid window.
- The authoritative live root runtime remains invalid for V1.1 latency/mutation baselines, but accepted-source **isolated** component/backend benchmarks are now possible: a disposable binary built under `D:\MAR-Research` carries VCS revision `d1e04e...`, production diff from V1.1 is empty, and a port-only research overlay prevents collision with live 8787/8788.
- The external benchmark gate now classifies current conditions automatically. Latest checkpoint: `structural_read_only=RUN`, `latency_baseline=NOT_RUN_RUNTIME_IDENTITY`, `mutation_baseline=NOT_RUN_RUNTIME_IDENTITY`. Running UI/child share root `D:\MAR\mar.exe` SHA-256 `74FAFFC1...`; Go build metadata contains no `vcs.revision`, no `.mar/runtime` artifact matches that hash, and the live `/` surface still serves the legacy monolithic Owner Console rather than the React/Vite asset model present in accepted V1.1 source. Runtime identity is therefore behaviorally stale, not merely unbound. Host pressure may independently move above/below threshold, and sandbox readiness remains false.
- External Web cognition amplification is now byte-reconstructed: 90 DecisionProjection turns total ~5.46 MB inner requests but ~11.83 MB simulated `brain_turn` CallToolResult representation (~2.17x); the accepted five-turn V1.1 task reconstructs ~254 KB inner -> ~542 KB outer (~2.13x). This is application/result representation, not measured HTTP wire bytes or hidden model context.
- Context-build cost is now independently decomposed with an external Go overlay: repeated full `Engine.Build()` median ~691 ms on this source tree, production `GitRepository.Snapshot()` median ~294 ms, and repeated build against a frozen snapshot still ~369 ms while scanning 273 files. The analysis cache therefore does not remove the dominant reread/hash/score work; Git snapshot and source processing are both material, but neither alone explains multi-minute task latency.
- Runtime-child liveness C1/C2 is now executable evidence on an accepted-source isolated runtime. Killing only `mcp-stdio` leaves Owner root, `/api/runtime`, `/api/tasks` at 200 and remote health at 204, while proxied task status returns 502 and no child respawn occurs after 8 seconds. A fresh remote MCP client can still submit successfully through the parent; the research DB grows by exactly one task, which remains `SUBMITTED / epoch 0` after 8 seconds with no execution child. Classification: `RUNTIME_CHILD_LIVENESS_GAP_CONFIRMED` — availability/health-truth issue, not stale-writer safety escape.
- Web-wait W4 capacity semantics are now stress-proven through real `Daemon.launchReady()` + resource-governor logic: with the default two-worker shape, two blocked worker lifetimes occupy both active slots/heavy leases and the third READY task cannot start; after one blocked lifetime releases, the third task is admitted and resources cleanly converge. Combined with source evidence that Web cognition waits stay inside `RunWorkspaceReady()`, `WEB_WAIT_CAPACITY_STARVATION` is confirmed as a stress risk, while historical production concurrency of pending Web turns remains max=1.
- P0 performance benchmarks are not valid until P0-0 proves execution-runtime liveness; Owner UI/route reachability alone is insufficient.
- mutation benchmark T1 is not valid until runtime identity and sandbox-host prerequisite are correct.
- the historical T1 harness also has public-contract parser drift; do not treat its failure as a MAR regression.
- a ChatCode raw-command probe job (`job-18d485a16289aa50-c2`) remains RUNNING and is flagged by ChatCode resource status as suspected stalled. It can hold the MAR workspace raw-command target, so do not use blocked raw-command probes as MAR performance evidence.
- despite that external command lease, MAR's own read-only self-hosting surface is currently reachable: a direct `project_context(project_id=mar)` call returned project `mar` and Git HEAD `d1e04e48898db87669fb1d4852448cbbdc08cea0` on 2026-09-12. Treat this only as `PUBLIC_READ_SURFACE_REACHABLE`; it does not prove the child daemon/scheduler is execution-ready because this read can be served independently of worker progress.

## Rules for next session

- Read this file + R-022 + R-023 before opening more research.
- Do not modify production code.
- Do not classify a benchmark as regression when runtime identity, sandbox readiness, fixture compatibility or environment validity is unresolved.
- Label proxies explicitly; do not call residual wall time `MAR overhead`.
- Safety invariant failure needs one reproduction; latency claims need comparable repeated samples.
- Prefer extending the external observer before proposing V1.2 instrumentation.
