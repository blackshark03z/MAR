# MAR V1.2 — Metrics Scorecard

**Evaluation model:** objective metric / invariant based  
**UI role:** regression-only; V1.2 does not include a broad UI redesign  
**Compared against:** V1.1 accepted/research baselines  
**Accepted pre-seal live HEAD:** `325dcbaff4e059eeec2d4f20637fb2bd5a87d2f7`

## Decision rule

V1.2 is release-worthy when all mandatory gates below PASS, no safety/fencing invariant regresses, and the selected V1.2 bottlenecks show a measured improvement or a binary failure-mode correction against the V1.1 baseline. Visual novelty is not an acceptance criterion for this release.

## Mandatory scorecard

| Metric / invariant | V1.1 baseline | V1.2 measured result | Acceptance threshold | Verdict |
| --- | --- | --- | --- | --- |
| Owner `/api/runtime` sandbox-probe churn | 5 `sandbox-host-check` launches / 5 polls in every accepted-source repeat; structurally ~30 launches/min at 2 s cadence | 0 launches / 30 live production polls; sandbox readiness true for all 30 | 0 launches during 30 cached polls | PASS |
| Owner `/api/runtime` median latency (secondary, non-identical environments) | ~102–133 ms in accepted-source isolated benchmark | 14.21 ms median over 30 live production polls | Directional improvement only; not a hard release gate because host conditions differ | PASS / informational |
| Active-task live-usage read amplification | ~8.55 MiB extra process read I/O / 10 calls for one 19-turn active task | ~1.00 MiB / 10 calls on the same research fixture | >=75% reduction while preserving usage/integrity semantics | PASS (~88% reduction) |
| Execution-child death truth | Parent/route stayed reachable; remote submit after child death succeeded; task count 42 -> 43 and new task remained SUBMITTED/epoch 0 | `execution_runtime_ready` true -> false; remote submit rejected; DB task count 42 -> 42 | Zero new mutation-capable work admitted/persisted while execution runtime is unavailable | PASS |
| Web-wait worker-slot starvation | With `MaxConcurrentWorkers=2`, two waiting lifetimes occupied both slots/heavy leases; third READY task did not start | Two parked Web waiters release compute/heavy capacity; third task admitted in real Windows sandbox E2E; waiting worker resumes mutation only after capacity reacquisition | Third task must be admitted without creating a second writer; response must not resume mutation before reacquisition | PASS |
| Secure Tunnel recovery bound | No V1.2 bounded desired-state self-heal contract | Identity-preserving recovery bounded to 3 attempts with backoff; exhausted budget becomes explicit error | No unbounded restart loop; stable tunnel identity preserved | PASS |
| Quick Tunnel failure truth | Temporary route lifecycle could be mistaken for stable/recoverable route | Process loss becomes explicit `ROUTE_LOST`; no silent hostname rotation; replacement test with pending Web cognition PASS | No silent external-URL mutation | PASS |
| Remote transport/idempotency safety | V1.1 accepted semantics | Current V1.2 source: all 9 ambiguous-ACK, precommit-cancellation and session-expiry cases PASS | 9/9 PASS | PASS |
| Repository regression | V1.1 accepted full gates | `go test -p 1 -count=1 -timeout 420s ./...` PASS across all packages; `go vet -p 1 ./...` PASS; `go build -p 1 ./...` PASS | 100% required gates PASS | PASS |
| Representative self-hosting | V1.1 self-hosting ready | T1 tiny-fix PASS in ~33.7 s, 2 model turns, 2 tool calls | End-to-end mutation -> verification -> integration PASS | PASS |
| Runtime promotion convergence | Historical source/runtime mismatch was observed during V1.2 development | Pre-seal live `8787`: exact HEAD `325dcbaff4e059eeec2d4f20637fb2bd5a87d2f7`, SHA `AFF8066D3DA6FB24C153D155BA4AD3EB426738B298F41C80F6B37CE1EE0CFA27`, `ALIGNED`, `trusted_for_release=true`, `HEALTHY`, execution ready, worker capacity available, React/Vite asset active | Source HEAD == promoted artifact identity == live runtime identity | PASS |

## Quantitative summary

Mandatory gates: **10 / 10 PASS**.  
Safety/integrity regressions detected: **0**.  
Selected observer-cost improvement: sandbox child launches **5/5 -> 0/30**; live-usage read amplification reduced by **~88%** on the fixed 19-turn fixture.  
Selected capacity correction: third-task admission under two parked Web waits changed from **blocked -> admitted** while exact attempt/process fencing remained intact.

The `/api/runtime` latency change from ~102–133 ms to 14.21 ms is intentionally treated as secondary evidence because the old and new runs were not captured under identical host conditions. The process-launch count and fixed-fixture I/O delta are the stronger release metrics.

## Tech-lead evaluation

**Verdict: `V1_2_METRICS_ACCEPTED_FOR_RELEASE`.**

The bounded V1.2 goals are measurably achieved. No additional visual redesign or subjective UI acceptance cycle is required to evaluate the V1.2 engineering/product delta. The Owner Console should be checked only for regression (loads, truthful state, usable controls), not for visual novelty.

The remaining Owner decision, if retained as a product-governance gate, is simply accept/reject of this metric-bound release result. It should not require the Owner to rediscover backend improvements by looking at an essentially unchanged UI.
