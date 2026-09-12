# R-027 — MAR V1.2 candidate requirements

**Status:** `RESEARCH_ONLY / CANDIDATE_SYNTHESIS`  
**Date:** 2026-09-12  
**Stable baseline:** MAR v1.1.0 remains frozen  
**Implementation authority:** none

## Purpose

R-001 through R-026 produced enough source, durable-history, external-observer and controlled fault-injection evidence to stop opening broad research topics.

R-027 converts those findings into a bounded V1.2 candidate set. It is intentionally stricter than a backlog: a candidate appears here only when a measured/reproduced problem exists or when release/health truth is provably insufficient.

No item in this document authorizes production implementation. A V1.2 implementation slice still requires an explicit implementation decision, bounded acceptance oracle and preservation of V1.1 safety invariants.

## Decision rule

Promote a candidate only when all are true:

1. the problem is measured or reproduced;
2. MAR materially contributes to it;
3. the expected benefit is user/runtime meaningful;
4. a bounded change can address it;
5. acceptance can prove the benefit;
6. safety/authority/recovery semantics remain intact;
7. complexity is justified by the measured effect.

If one of these is missing, keep the finding as research evidence rather than implementation scope.

# Priority P0 — release/runtime truth

## C-01 — Execution-runtime health truth and bounded child supervision

**Evidence:** `STRONG / LIVE_REPRODUCED`

R-026 accepted-source isolated fault injection proved:

- Owner UI parent remains reachable after `mcp-stdio` child death;
- `/`, `/api/runtime`, `/api/tasks` remain HTTP 200;
- remote `/health/<token>` remains 204;
- proxied task operations fail 502;
- no child respawn occurs after 8 seconds;
- a new remote submit can still persist successfully through the parent;
- the new task remains `SUBMITTED / epoch 0` because no execution daemon exists.

This is a real health/liveness truth gap. It is not a stale-writer safety escape.

### Candidate requirement

MAR must distinguish at least these states:

```text
owner_surface_reachable
remote_route_reachable
execution_runtime_ready
worker_capacity_available
```

`execution_runtime_ready=false` must never be presented as overall runtime healthy.

If the Owner UI parent owns the `mcp-stdio` child lifecycle, child exit must produce one of two bounded outcomes:

1. safe automatic restart/takeover after authority checks; or
2. durable degraded state with explicit queued/not-executing semantics.

A remotely submitted durable task may remain accepted while execution is unavailable **only if** the product truth explicitly says it is queued and not executing. Silent `SUBMITTED` accumulation behind a green/healthy surface is unacceptable.

### Acceptance oracle

Kill only the execution child while keeping parent/bridge alive.

Require:

- execution health becomes degraded within a bounded detection interval;
- no false `execution_runtime_ready=true`;
- no duplicate daemon authority;
- no stale worker gains authority;
- existing durable tasks are not lost;
- new submit behavior is explicit (`queued/degraded` or intentionally rejected), never falsely active;
- if automatic restart is implemented, authority lease/fencing proves only one daemon resumes execution.

A practical initial detection target is within two ordinary Console refresh intervals; exact SLA should be finalized during implementation design.

## C-02 — Release artifact/source provenance must be cryptographically bound

**Evidence:** `STRONG / PROVEN_GAP`

R-010/R-012 found:

- accepted V1.1 source/schema and disk `mar-v1-stable.exe` were not the same runtime generation;
- historical promotion manifest referenced an earlier source HEAD;
- root `D:\MAR\mar.exe` had no embedded `vcs.revision`;
- its hash did not match archived release artifacts;
- behavioral fingerprint showed the live root runtime serving legacy monolithic UI while accepted V1.1 source contains the React/Vite asset model.

Performance/eval numbers from an unbound binary are not trustworthy release evidence.

### Candidate requirement

Every promoted MAR runtime must have one immutable release identity record binding:

```text
release/version
source commit
production-tree identity
binary SHA-256
Go/toolchain identity
SQLite schema compatibility
build timestamp
UI asset/build identity
```

The executable should expose or embed source revision metadata. Release/launcher surfaces must make source/runtime mismatch observable.

### Acceptance oracle

Given an accepted release commit/tag:

- build artifact revision equals intended source;
- binary hash matches release manifest;
- schema compatibility is verified before start;
- asset model fingerprint matches source release;
- substituting a stale binary causes a clear identity/degraded signal;
- benchmark gate refuses performance/mutation baselines against mismatched runtime.

## C-03 — Sandbox readiness observation must not spawn a process per UI poll

**Evidence:** `STRONG / REPEATED_A_B`

R-016 accepted-source isolated A/B proved:

- each `/api/runtime` request invokes one real `sandbox-host-check` child;
- five polls produced five sandbox probes in every repeat;
- median runtime endpoint latency was roughly 102–133 ms;
- transient process tree increased from 2 to 5 processes;
- transient RSS peak rose roughly 29–31 MiB;
- the original 5-second Research Observer itself accidentally produced the same observer effect until corrected.

### Candidate requirement

Boot-scoped Windows sandbox readiness should be observed with a bounded cache/state mechanism rather than running a full sandbox probe on every UI refresh.

The cache must be fail-closed and have explicit invalidation/recheck on at least:

- MAR/host boot identity change;
- successful/failed sandbox preparation;
- worker/sandbox admission failure indicating readiness may be stale;
- explicit diagnostics request.

### Acceptance oracle

After one valid readiness observation, 30 normal `/api/runtime` polls must produce **zero additional sandbox-host-check child launches** while still reporting truthful readiness.

After invalidation, the next required check must revalidate the real Windows prerequisite before worker admission.

# Priority P1 — throughput and observation cost

## C-04 — External cognition wait must not monopolize scarce heavy execution capacity

**Evidence:** `STRONG STRUCTURAL + STRESS_PROVEN; PRODUCTION INCIDENCE LOW`

R-018 proved:

- Web Brain wait occurs inside the active worker lifetime;
- it retains worker slot, heavy resource lease and configured reservations;
- default MaxConcurrentWorkers is two;
- worker wait polls turn response every 200 ms;
- daemon control/status polling also runs every 200 ms;
- full WebTurn payloads can be reread through these paths;
- 368 historical waits had median 36.8 s, p95 233.7 s, max 18.4 min;
- historical pending concurrency never exceeded one;
- controlled daemon/resource-governor stress proves two waiting worker lifetimes consume both default slots/heavy leases and prevent a third READY task from starting until one releases.

### Candidate requirement

Waiting for external cognition must remain durable and fenced but should not consume scarce **heavy execution capacity** as if useful build/mutation compute were active.

Implementation design is intentionally not chosen yet. Valid solution families may include:

- park/yield a worker while retaining logical attempt identity;
- release only heavy resource claim while retaining a lightweight waiting process;
- checkpoint/terminate cleanly at Web wait and resume from durable state after response;
- separate waiting-worker accounting from active mutation/build capacity.

### Safety invariants

Any solution must preserve:

- exact `task_id + attempt_id + run_epoch + turn_id` binding;
- no second mutation-capable worker before physical authority is safe;
- stale response rejection;
- bounded active-execution/convergence budgets;
- no transcript replay requirement;
- restart recovery from durable state.

### Acceptance oracle

With `MaxConcurrentWorkers=2`:

1. two tasks enter durable external-cognition wait;
2. a third independent READY task arrives;
3. third task must obtain appropriate compute capacity without violating fencing;
4. responding to either waiting turn resumes the exact original durable task/turn;
5. no duplicate mutation authority exists at any point.

## C-05 — Owner live-usage/task summaries must not reread full historical cognition payloads

**Evidence:** `STRONG / REPEATED_A_B`

R-016/R-017 measured an accepted-source isolated fixture:

- 30 terminal tasks: `/api/tasks` median roughly 11–27 ms;
- one RUNNING task with 19 WebTurns / ~919 KiB historical turn payload: median roughly 38–46 ms;
- extra process read I/O was ~8.55 MiB per 10 calls, stable across three repeats;
- roughly ~0.855 MiB extra read I/O per task-list call;
- UI response increased only ~975 bytes.

The cost is therefore internal historical row loading/validation/decoding, not response payload size.

### Candidate requirement

Live task summaries must use the smallest truthful durable read needed for:

- pending-turn state;
- last activity;
- token usage/accounting;
- task/result/feedback summary.

Do not load historical `request_json` bodies when the surface only needs summary metadata/usage.

Prefer a narrow query/read projection before adding new durable aggregate tables.

### Acceptance oracle

On the same 19-turn fixture:

- task-list semantics/visible counters remain identical;
- no full request bodies are loaded solely for live-usage summary;
- process read I/O per call drops materially (candidate target: >=75% reduction versus the measured active fixture);
- response correctness and integrity semantics remain unchanged.

## C-06 — Owned connection processes should converge toward desired state or clearly degrade

**Evidence:** `SOURCE + FAULT-INJECTION; INCIDENT FREQUENCY UNKNOWN`

R-025/R-015 show:

- Quick Tunnel child exit is detected but same-process automatic restart is not currently scheduled;
- boot-time desired-state restoration is not equivalent to same-process self-healing;
- disposable fault injection proves external observation can distinguish origin-down/tunnel-alive from tunnel-process-down/origin-alive;
- current observer can classify these conditions without changing MAR production telemetry.

### Candidate requirement

For owned connection processes whose desired state is `running`, MAR should either:

1. boundedly restart/reconnect them with backoff and identity safety; or
2. transition to explicit degraded state with actionable diagnostics.

Do not spin indefinitely, rotate identities without policy, or hide repeated failure.

### Acceptance oracle

Inject owned tunnel-client/cloudflared exit while local MAR remains healthy:

- failure domain is truthfully classified;
- desired state remains durable;
- bounded retry/backoff behavior is visible;
- repeated failures stop/escalate rather than loop forever;
- stable connector identity is preserved where the transport supports it.

# Priority P2 — targeted performance improvements

## C-07 — Reuse/remove the immediate duplicate context build before DecisionProjection turn 1

**Evidence:** `MEASURED / MODERATE BENEFIT`

R-021 measured repeated accepted-source context builds:

- full Build median roughly 0.62–0.69 s;
- Git snapshot roughly 0.29–0.30 s;
- fixed-snapshot reread/hash/score still roughly 0.37 s;
- adjacent unchanged builds reconstruct identical packs;
- DecisionProjection mode structurally performs an initial build and another build before first actual turn.

### Candidate requirement

Research/implement only the narrow first-turn reuse/removal if source/worktree/Goal identity cannot change between those two points.

Do **not** introduce a broad revision-only context cache. Working-tree mutations can occur without HEAD change.

### Acceptance oracle

- first model turn receives the same valid context pack;
- no Git/worktree change can be missed;
- one full Build is eliminated on the safe path;
- mutation/read-command invalidation behavior remains correct;
- stale revision/worktree checks remain fail-closed.

## C-08 — Separate fast repair feedback from final authoritative verification

**Evidence:** `MEASURED / PROFILE-SENSITIVE`

R-019 reconstructed:

- successful go-standard verification median ~84.2 s;
- accepted V1.1 go-docs verification consumed ~259.1 s / ~61.4% task wall time;
- after failed `go test`, several go-standard suites still spent ~29.5–32.1 s running `vet + build` for complete evidence;
- integration itself is small (median ~1.25 s).

### Candidate requirement

Do not weaken final verification. Research a two-level workflow:

1. **repair feedback:** fail fast enough to return useful failure evidence to the worker;
2. **authoritative candidate verification:** run the complete required profile before VERIFIED/integration.

This can reduce repair-loop latency without weakening release evidence.

### Acceptance oracle

- failed test iteration returns materially sooner;
- final candidate still runs full profile and produces revision/environment-bound evidence;
- no candidate can become VERIFIED from partial/repair-only evidence;
- cache/isolation authority remains unchanged.

# Observe-only / not yet V1.2 requirements

## O-01 — External Web cognition representation amplification

R-020 reconstructs approximately 2.17x application/result representation from inner pending request to MCP `CallToolResult` because exact cognition input is carried in structured + compatibility text representations.

This is real representation amplification, but actual ChatGPT browser/client retention and renderer RSS remain unobservable. Do not redesign MCP payloads or remove compatibility representation merely from byte reconstruction.

Promotion requires external/client-side pressure correlation or a protocol-supported representation change with compatibility proof.

## O-02 — Broader context caching

Repeated context computation is real but moderate on the measured repository. Do not add workspace-generation machinery/global cache until larger-repository evidence shows material benefit beyond C-07.

## O-03 — SQLite pool/architecture changes

R-017 corrected the earlier model: Owner parent and execution child have separate SQLite handles/processes. Do not increase `MaxOpenConns`, weaken durability, or introduce a second coordination store based on UI read-cost findings.

## O-04 — MCP transport rewrite

Core transport semantics are strong:

- submit/brain response precommit cancellation: proven;
- postcommit lost-ACK idempotency: proven stateful + stateless;
- stateful session expiry reconnect: proven;
- real Quick Tunnel replacement with pending WebTurn: proven;
- transport semantics sentinel: 9/9 PASS.

Do not replace Streamable HTTP/MCP architecture without new evidence.

## O-05 — General OpenTelemetry/trace platform

External observer coverage is now sufficient for the P0/P1 questions. Keep OTel as a compatibility direction, not a dependency, until a specific blind spot cannot be answered externally.

# Recommended V1.2 implementation sequence if Owner later authorizes implementation

```text
Slice A — release/runtime truth
  C-02 artifact/source identity
  C-01 execution child health + bounded supervision/degraded semantics

Slice B — remove observer waste
  C-03 cached/invalidation-safe sandbox readiness
  C-05 narrow live-usage/task-summary reads

Slice C — waiting capacity
  C-04 external-cognition wait capacity design + failure injection

Slice D — connection self-healing
  C-06 bounded desired-state recovery

Slice E — low-risk speed
  C-07 first-turn context duplicate removal
  C-08 repair-feedback vs final verification split
```

The ordering intentionally puts truth/recovery before throughput tuning.

# Cross-slice acceptance suite

Before V1.2 can be called stable, rerun or add bounded coverage for:

1. stale-worker physical/logical fencing;
2. execution-child death while parent remains reachable;
3. durable submit while execution unavailable;
4. source/binary/manifest mismatch;
5. sandbox readiness invalidation and reboot semantics;
6. two Web waits + third READY task;
7. live usage on large WebTurn history;
8. stateful/stateless ambiguous ACK and precommit cancellation;
9. session expiry reconnect;
10. real tunnel process replacement with pending WebTurn;
11. tunnel/origin failure-domain classification;
12. final full verification + integration freshness;
13. representative T1/task baseline after runtime identity and sandbox prerequisites are valid.

# Current synthesis verdict

`RESEARCH_CONVERGED_TO_BOUNDED_V1_2_CANDIDATES`

The strongest V1.2 case is **not** a transport rewrite or a new agent framework. The evidence points to a smaller set of concrete engineering improvements:

- make runtime/release truth trustworthy;
- make execution child liveness explicit/self-healing;
- remove observability work that perturbs the runtime;
- stop external cognition waits from monopolizing scarce compute capacity;
- read summary telemetry without rereading cognition payloads;
- preserve the already-strong durable/idempotent transport semantics;
- apply only narrow, evidence-backed performance optimizations afterward.
