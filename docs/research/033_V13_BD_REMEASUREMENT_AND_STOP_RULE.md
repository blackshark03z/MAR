# MAR V1.3 — B–D Re-measurement and Stop-Rule Decision

Status: RESEARCH / RELEASE-STOP INPUT
Date: 2026-09-19
Current base revision: 0c2cb9684bb5c934f7d993c2a3cbf6a4686ddf25

## 1. Question

After completed V1.3 Slices B–D, should MAR open conditional Slice E/F now, or stop adding core optimization complexity and proceed toward release qualification/runtime promotion?

This report does not authorize production changes. Changed repository scope: this report only.

## 2. Fresh current-source regression

Fresh B-D focused regression: PASS

The current source was exercised without repository mutation.

### Slice B — delta/event cognition context

Fresh current-source tests passed:

- `TestCognitionDeltaRecoveryFromCurrentState` — PASS;
- `TestCognitionDeltaReducesProjectionCost` — PASS;
- `TestBrainTurnDeltaModeIsOptInAndPreservesDefaultPayload` — PASS.

Fresh observed metric from the current test fixture:

- full request: 13,817 bytes;
- delta request: 1,057 bytes;
- ratio: 0.0765;
- 92.35% reduction in outward projection bytes.

### Slice C — compound deterministic same-file mutation

Fresh current-source focused ACI/agent/worker tests passed, including successful ordered replacement, no partial write on failure, batch bounds, primitive/compound byte identity, tool schema/authority compatibility, and worker compatibility.

The qualified paired metric remains:

- primitive same-file path: 3 `replace_exact` calls;
- compound same-file path: 1 `replace_many_exact` call;
- 66.7% reduction in worker mutation calls for the representative sequence.

One host Git/MSYS authority fixture skipped because nested AppContainer execution cannot host that fixture; this is an existing environment limitation, not a failed Slice C regression.

### Slice D — fast repair feedback

Fresh current-source DecisionProjection/service/orchestrator repair-path regressions passed, including bounded first-failure projection, durable first-failure selection, RETRY_WAIT physical-termination gating, retry-budget behavior, and verification-recovery safety.

The verifier tests that require host Git/MSYS fixtures skipped in the nested AppContainer, as expected. They are not counted as fresh PASS. The exact Slice D implementation revision `8a80955ebfaa8d79ee45c9792c58a9a94838549e` previously passed authoritative `go-standard` (`go test`, `go vet`, `go build`) and 7/7 acceptance before integration, and the later revision `0c2cb9684bb5c934f7d993c2a3cbf6a4686ddf25` changed documentation only. Therefore the current Go implementation remains the same qualified source reality.

The qualified paired Slice D metric remains:

- baseline early-fail profile: 3 verification commands continue after the first command fails;
- Slice D early-fail profile: 1 command executes, then returns repair feedback;
- 66.7% reduction in command invocations for the controlled first-command-failure fixture;
- historical failed `go-standard` evidence showed roughly 29.5–32.1 seconds of avoidable `vet + build` work after `go test` had already failed.

## 3. Component result summary

The three retained optimizations each show a material bounded benefit on the task pattern they were designed for:

- B: 13,817 -> 1,057 bytes, 92.35% reduction;
- C: 3 -> 1 deterministic same-file mutation calls, 66.7% reduction;
- D: 3 -> 1 verification commands on a controlled first-command failure, 66.7% reduction, with historical evidence of roughly 29.5–32.1 seconds of avoidable post-failure work.

All three preserve MAR durable authority rather than introducing a second coordinator, planner, task database, verification truth, or provider-specific kernel branch.

## 4. Recent real post-B–D task telemetry

Recent durable task results provide useful operational context:

### Slice D implementation/finalization

Task `task-f0fb0696b00b45929fdafbda3c573352`:

- creation -> result: 1,130.239 seconds;
- agent turns: 6;
- agent tool calls: 17;
- total model tokens: 107,864;
- final result: VERIFIED + INTEGRATED;
- full `go-standard`: PASS.

### Slice D documentation closeout

Task `task-06dcd2f843274fb9a467ac41271183cd`:

- creation -> result: 169.897 seconds;
- agent turns: 4;
- agent tool calls: 9;
- total model tokens: 67,333;
- final result: VERIFIED + INTEGRATED;
- `go-docs`: PASS.

### Slice C documentation closeout

Task `task-d06b468d0cf3492e83dae3015f9e21d8`:

- creation -> result: 1,200.068 seconds;
- agent turns: 5;
- agent tool calls: 10;
- total model tokens: 81,682;
- final result: VERIFIED + INTEGRATED;
- this task crossed run epoch 2 and included an external runtime-promotion prerequisite.

These data show that real Web-cognition/task latency can remain measured in minutes even after deterministic kernel optimizations. However, these tasks differ materially in scope, verification profile, run-epoch history, manual/Web response cadence, and external prerequisites. They are observational and not a valid apples-to-apples pre/post Verified Result Latency baseline. They must not be used to claim a percentage end-to-end latency improvement.

## 5. Remaining Slice E/F opportunity

Accepted R-021 context-cost research measured current context rebuilding as real but moderate rather than dominant on the MAR fixture:

- first `Engine.Build()`: about 783.7 ms;
- warm Build median: about 620.2 ms;
- immediate initial/turn-1 duplicate opportunity: roughly 0.6–0.8 seconds;
- five-turn structural estimate: roughly 3.9 seconds of total context-build work;
- repeated identical context output was common, but safe reuse requires stronger workspace freshness identity than Git revision alone.

The measured potential is not zero. It is also not large enough, on current evidence, to explain multi-minute real task completion. Opening broader incremental Project Intelligence, workspace-generation machinery, or cache/reuse infrastructure would add correctness and recovery surface for a currently moderate measured gain.

## 6. Complexity-adjusted decision

Decision: `DO_NOT_OPEN_SLICE_E_F_NOW`.

Reasoning:

1. B, C, and D each retain a material measured benefit in their targeted path.
2. Fresh current-source regressions preserve those behaviors; environment-dependent skips are explicitly separated from PASS claims.
3. Authoritative qualification/integration for the implementation slices remains green with zero unresolved risks.
4. The best accepted evidence for the next E/F-style context/cache target is roughly seconds, not minutes, on the measured MAR fixture.
5. Safer future reuse requires additional freshness/invalidations semantics, so complexity and failure-domain cost rise faster than the presently demonstrated benefit.
6. Real post-B–D task telemetry still contains large cognition/verification/manual cadence components that E/F would not directly remove.

This is a stop decision for further core optimization now, not a permanent ban. Reopen E/F only if later representative large-repository or real-product evidence shows a material context/intelligence deficit with a favorable complexity-adjusted payoff.

## 7. Release-stop assessment

The V1.3 core optimization program has enough evidence to stop opening new optimization slices at B–D:

- bounded context cost is materially reduced;
- recurring deterministic mutation chatter has a proven compound path;
- failed verification returns useful repair evidence earlier;
- no accepted authority/evidence/recovery invariant was intentionally weakened;
- the next researched cache/intelligence opportunity is currently moderate, not dominant.

The primary end-to-end KPI still lacks a clean controlled pre/post comparison across identical real task classes. That uncertainty is recorded rather than converted into a synthetic percentage claim. Given the complexity gate and existing component evidence, it does not justify opening E/F now.

Core optimization stop does not equal release completion. release completion still requires exact runtime activation/alignment and full release qualification for the current release candidate before V1.3 can be declared complete.

## 8. Next bounded action

Keep Slice E/F closed. Freeze B–D as the current V1.3 optimization set and proceed to the release lane:

1. authoritative current-head release qualification;
2. exact runtime activation/promotion;
3. prove source/runtime `ALIGNED`, `trusted_for_release=true`, and HEALTHY;
4. confirm remote/source convergence as required by release policy;
5. stop unless release evidence exposes a concrete blocker.

No additional optimization slice should be opened merely because another possible optimization exists.
