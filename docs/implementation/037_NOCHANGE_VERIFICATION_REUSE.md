# MAR No-Change Verification Reuse

**Status:** IMPLEMENTATION CANDIDATE  
**Date:** 2026-09-22  
**Scope:** one proven-pure reuse case; not a generic action cache.

## Decision

For an exact no-change candidate, MAR may reuse previously PASSed verification **command evidence** only when project, candidate revision, resolved profile, environment identity, command identity, and evidence integrity all match.

Current task acceptance is always evaluated again. Goal identity, TaskResult, integration result, workspace state, and lifecycle state are never reused.

Reuse is disabled when current acceptance references command indexes or uses an output_contains oracle.

## Safety

The reuse path still performs current attempt-authority validation, before/after environment capture, candidate-stability checks, fresh acceptance observation, new current-task VerificationEvidence creation, and normal expected-head integration.

Reused commands carry explicit provenance:
- Reused=true
- ReusedFromEvidenceID=<source evidence>
- DurationMS=0
- TaskResult receipts use REUSED[source-evidence-id]

Any mismatch or uncertainty falls back to ordinary command execution.

## Measured result

All measurements below are from the same Windows MAR host around exact stable revision `2ee0c7a6b2971094c5005734eea8c086bfff0d27`, using MAR's portable Go 1.27 toolchain with `GOEXPERIMENT=nojsonv2`.

Real no-change `go-docs` command baseline:

`go test -p 1 -run ^$ -timeout 180s ./...`

Result: **PASS, measured 32,033 ms** on the stable checkout.

Focused cross-task verifier reuse:

`TestVerifierNoChangeReusesCommandsAcrossTasks`

Result: **PASS, measured_reuse_overhead_ms=700**, with an assertion that the current task executed zero verification commands and current file-based acceptance was observed again.

Output-dependent fallback:

`TestVerifierNoChangeReuseFallsBackForOutputDependentAcceptance`

Result: **PASS**; current commands executed rather than reusing prior command evidence.

Comparing the real command-only baseline with the measured reuse verifier path gives a conservative measured reduction of approximately **97.8%** for the eligible no-change case. The comparison is conservative because the 32,033 ms figure excludes ordinary fresh-verifier bookkeeping, while the 700 ms reuse measurement includes verifier bookkeeping and fresh acceptance observation.

This number is specific to eligible no-change tasks. It is not a claim that all MAR verification becomes 97.8% faster.

## Complexity delta

- SQLite schema: unchanged.
- Background processes: unchanged.
- Lifecycle states: unchanged.
- Generic cache framework: none.
- Durable representation: two optional provenance fields inside existing command-evidence JSON.
- Failure/recovery model: unchanged; missing or invalid reuse evidence falls back to normal verification.

## Stop rule

Do not broaden this into affected-package verification or a global action cache without new representative evidence.
