# MAR V1.2 — Final Release

**Status:** `MAR_V1_2_PRODUCT_ACCEPTED`
**Release:** `v1.2.0`
**Release date:** `2026-09-13`
**Acceptance model:** metrics/invariant based
**Previous product release:** `MAR_V1_1_PRODUCT_ACCEPTED`

## Release decision

The Owner instructed MAR V1.2 to be sealed after the bounded V1.2 scorecard reached 10/10 mandatory gates PASS with zero detected safety/integrity regressions and the exact promoted runtime reported healthy, aligned and trusted for release. V1.2 is therefore closed as `STABLE / PRODUCT_ACCEPTED`; optional research, visual polish or new optimization must not reopen V1.2 unless a concrete regression invalidates a release invariant.

The canonical engineering evidence remains `docs/release/MAR_V1_2_METRICS_SCORECARD.md` and the implementation closeout remains `docs/release/MAR_V1_2_RELEASE_CANDIDATE.md`. The final release tag `v1.2.0` is the canonical source identity for this release. The final seal commit changes release/governance documentation only; it does not alter the already accepted production behavior.

## Accepted scope

V1.2 closes the bounded scope defined by the V1.2 research/implementation decisions: runtime/source provenance and stale-artifact rejection; fail-closed execution-child readiness; bounded sandbox-readiness caching; compact live Web usage observation; bounded connection desired-state recovery with explicit Quick Tunnel route-loss truth; and Web-wait capacity parking/reacquisition while preserving exact attempt/process authority.

No transport rewrite, database redesign, generic telemetry platform, new multi-agent fabric, cloud/distributed worker architecture or broad UI redesign is part of V1.2.

## Mandatory release evidence

- 10/10 mandatory V1.2 metrics/invariant gates PASS.
- Zero detected safety/fencing/integrity regression.
- Full repository test, vet and build gates PASS on the accepted implementation line.
- Remote transport/idempotency semantics: 9/9 PASS.
- Representative self-hosting: PASS.
- Live production runtime on `127.0.0.1:8787`: `HEALTHY`, sandbox ready, execution runtime ready, worker capacity available.
- Promoted runtime identity before final documentation seal: exact source `325dcbaff4e059eeec2d4f20637fb2bd5a87d2f7`, binary SHA-256 `AFF8066D3DA6FB24C153D155BA4AD3EB426738B298F41C80F6B37CE1EE0CFA27`, `ALIGNED`, `trusted_for_release=true`.

## Final promotion requirement

After this documentation seal is committed, build the stable binary from that exact final HEAD, write a release manifest with `release_version=v1.2.0`, promote/restart the canonical `8787` runtime, and verify:

1. runtime `source_revision` equals the final tagged Git revision;
2. `manifest_status=ALIGNED` and `status=ALIGNED`;
3. `trusted_for_release=true`;
4. `runtime_health=HEALTHY`;
5. sandbox and execution readiness remain true;
6. the Owner Console loads from the embedded React/Vite asset;
7. Git tag `v1.2.0` points to that same revision.

Only after those promotion checks pass is the release physically sealed. These are packaging/promotion checks, not a new V1.2 product-development cycle.

## Stop rule

V1.2 is frozen after the final promotion checks pass. Future observations go to telemetry/research. Any new capability or optimization belongs to a later bounded release (for example V1.3) unless it is required to repair a proven V1.2 regression.
