# Slice 020 — V1.2 sandbox readiness cache

**Status:** `IMPLEMENTED / TARGETED_VERIFIED`

## Goal

Remove the proven Owner Console observer effect where every `/api/runtime` poll launches a `sandbox-host-check` subprocess, without weakening the sandbox readiness signal.

## Change

- Cache sandbox readiness inside the Owner backend.
- Ready results are revalidated every 5 minutes.
- Not-ready results are revalidated every 30 seconds so external preparation is discovered promptly.
- `POST /api/runtime/sandbox/prepare` always forces a fresh readiness probe after preparation.
- No new durable state, daemon, database table, or authority path is introduced.

## Acceptance

1. Repeated runtime polls inside the cache window execute one sandbox probe, not one probe per HTTP request.
2. Expired cache revalidates exactly once under the backend mutex.
3. Sandbox Prepare bypasses cached state and performs a fresh probe before returning success.
4. Existing Owner runtime semantics remain unchanged apart from reduced probe frequency.
5. Targeted tests, `go vet ./cmd/mar`, and build pass.

## Evidence

- `TestOwnerUISandboxReadinessCachesPollsAndRevalidatesAfterExpiry` PASS.
- `TestOwnerUISandboxPreparationUsesUACHelperAndRechecksReadiness` PASS.
- `go vet ./cmd/mar` PASS.
- `go build ./cmd/mar` PASS during verification; canonical runtime entrypoints were immediately restored afterward so verification output could not become a promoted artifact accidentally.

## Stop rule

This slice ends with bounded cache/revalidation behavior. Do not turn readiness caching into a general telemetry cache or runtime state subsystem.
