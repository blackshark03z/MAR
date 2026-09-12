# MAR V1.2 — Engineering Release Candidate

**Status:** `ENGINEERING_STABLE_PENDING_OWNER_UAT`  
**Engineering closeout date:** `2026-09-13`  
**Accepted implementation HEAD:** `e5fe06e2de416fabff38087d8435516f1bacdb38`  
**Previous product release:** `MAR_V1_1_PRODUCT_ACCEPTED`

## Release decision

MAR V1.2 has completed its bounded engineering scope and is ready for Owner real-use acceptance. This record does **not** promote V1.2 to `PRODUCT_ACCEPTED`; V1.1 remains the accepted product release until the Owner explicitly accepts the live V1.2 experience.

The release keeps the frozen MAR authority model and durable task architecture. V1.2 hardens runtime truth and long-running Web operation without introducing a transport rewrite, database redesign, generic telemetry platform, new multi-agent fabric, cloud/distributed workers, or broad UI redesign.

## Bounded V1.2 scope shipped

- Runtime/source provenance binds release identity, source revision, executable SHA-256, Go/toolchain, SQLite schema and embedded Owner UI asset identity; stale/unbound artifacts fail release trust.
- Owner/remote reachability is separated from execution-child readiness; a dead execution child becomes explicit degraded truth and new mutation admission fails closed rather than creating orphan work.
- Sandbox readiness is cached with bounded revalidation and forced refresh after preparation, eliminating one sandbox probe subprocess per Owner runtime poll.
- Owner live-usage observation no longer hydrates full WebTurn request bodies; the measured 19-turn fixture reduced extra read I/O by about 88% while preserving accounting/integrity semantics.
- Owned Secure Tunnel recovery is bounded and identity-preserving; Quick Tunnel route loss is explicit rather than silently rotating an external hostname.
- Web Brain waiters cooperatively park scarce execution capacity while retaining exact attempt/process authority. A response may resume mutation only after the waiter reacquires parent-owned capacity; resident parked workers remain bounded.

## Engineering evidence

The implementation line passed the following release gates:

- deterministic Web-wait scheduler acceptance: two parked waiters release compute/heavy capacity so a third READY task is admitted;
- ProcessRunner acceptance: a Web response is not returned to the worker until capacity reacquisition completes;
- real Windows sandbox Web-wait E2E: three independent projects with `MaxConcurrentWorkers=2` proved third-task admission and correct post-response reacquisition/mutation on the same attempt;
- full `internal/worker` and `internal/orchestrator` regressions: PASS;
- full repository `go test -p 1 -count=1 -timeout 420s ./...`: PASS across all packages;
- full `go vet -p 1 ./...`: PASS;
- full `go build -p 1 ./...`: PASS;
- representative self-hosting T1 tiny-fix after repairing stale receipt parsing: PASS in ~33.7s with 2 model turns / 2 tool calls;
- current-source transport semantics: all 9 ambiguous-ACK, precommit-cancellation and session-expiry cases PASS;
- current-source Quick Tunnel replacement with pending Web cognition: PASS;
- live Owner runtime polling: 30 polls at the production surface kept sandbox readiness true with 0 observed `sandbox-host-check` child launches using the same psutil process-tree sampler that previously detected the observer effect;
- live promoted runtime before documentation closeout reported `HEALTHY`, `execution_runtime_ready=true`, `worker_capacity_available=true`, React/Vite UI active, and release identity `ALIGNED / trusted_for_release=true`.

## Runtime promotion boundary

The implementation candidate was already promoted and verified live during engineering closeout. Because this release-candidate document and handoff are a final source commit, the canonical `8787` runtime must be rebuilt from the exact documentation-closeout HEAD and re-promoted before Owner UAT. Owner review is valid only after live runtime identity again reports the exact final source revision as `ALIGNED / trusted_for_release=true`.

## Metrics-based product gate

V1.2 is not a visual redesign release. Product evaluation is therefore bound to `docs/release/MAR_V1_2_METRICS_SCORECARD.md`, not to whether the Owner Console looks materially different from V1.1. The mandatory scorecard is 10/10 PASS with zero detected safety/integrity regressions; the Tech Lead verdict is `V1_2_METRICS_ACCEPTED_FOR_RELEASE`.

If an explicit Owner governance decision is retained, it is an accept/reject decision on this metric-bound release evidence, not a requirement to rediscover backend improvements through subjective UI inspection. No known production-code blocker is open for the bounded V1.2 scope. Optional follow-up research or polish must not reopen this release unless a concrete regression invalidates one of the acceptance invariants above.
