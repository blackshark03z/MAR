# Slice 022 — V1.2 connection desired-state recovery

**Status:** `IMPLEMENTED / TARGETED_VERIFIED`

## Goal

Make owned connection-process failure converge toward the configured desired state without hiding prolonged failure, starting duplicate owned processes, or silently rotating a connection identity that is not stable.

## Secure Tunnel policy

- The persisted OpenAI Secure Tunnel `desired_running` flag remains the recovery authority.
- Confirmed unexpected tunnel-client exit schedules same-process recovery only when `desired_running=true` and MAR is not explicitly stopping/closing.
- Recovery preserves the configured tunnel ID/profile.
- Retry is bounded to three attempts with backoff (1s, 5s, 15s).
- Only one recovery loop may exist at a time.
- A replacement process must survive a short settle window before recovery is considered complete.
- Repeated failure exhausts the budget and becomes explicit `ERROR` with actionable diagnostics; it does not spin forever.
- Explicit Start/Restart opens a fresh recovery budget.

## Quick Tunnel policy

Quick Tunnel hostnames are temporary identities. MAR therefore does not silently restart a crashed Quick Tunnel and pretend the client endpoint is unchanged.

- Unexpected owned cloudflared exit clears the dead temporary URL and reports `ROUTE_LOST` with diagnostics.
- Explicit regeneration starts a new temporary route and clears the route-loss state.
- Stable/named routes remain detection-only unless MAR actually owns their external lifecycle.

## Acceptance evidence

- `TestOpenAITunnelCrashAutoRecoversDesiredStateWithoutStaleConnectionTruth`: PASS.
- `TestOpenAITunnelAutoRecoveryExhaustsBoundedBudgetWithoutSpin`: PASS.
- `TestOpenAITunnelRestartRequiresFreshReadiness`: PASS.
- `TestOpenAITunnelStopFailureRetainsProcessAuthorityAndBlocksRestart`: PASS.
- `TestRemoteBridgeUnexpectedQuickTunnelExitReportsRouteLostUntilExplicitRegeneration`: PASS.
- `TestRemoteBridgeRetriesDeadTemporaryRouteWithinBoundedStartup`: PASS.
- `go vet ./cmd/mar`: PASS.
- Build to `.mar/runtime/verify/mar-slice-d.exe`: PASS.

## Preserved invariants

- no second Secure Tunnel process is launched after an unconfirmed Stop failure;
- stale Connected/Ready truth is cleared on process replacement;
- Owner desired state remains durable in SQLite;
- recovery never changes the Secure Tunnel ID;
- Quick Tunnel route regeneration remains explicit because its public hostname can change;
- no infinite hidden self-healing loop.

## Stop rule

This slice ends with bounded owned-process recovery/degraded truth. It does not add general connection orchestration, external route ownership, or transport rewrites.
