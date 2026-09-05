# Slice 014 — Verification / Result Contract

**Status:** IMPLEMENTED / VERIFIED

**Frozen architecture source:** MAR V1 Product Specification sections 5, 12 and 13; architecture freeze invariant that verification evidence is bound to the exact verified revision and relevant verification environment/profile identity; existing execution-attempt fencing and side-effect recovery model.

## Goal

Make technical verification authoritative over model self-assertion and publish a durable, integrity-checked result bound to the exact task candidate, Goal Contract, verification profile and relevant toolchain environment.

`completed_candidate` remains only the autonomous coding agent's assertion that implementation is ready for MAR verification. It does not itself authorize `VERIFIED`, `READY_TO_INTEGRATE`, or completion.

## Durable verification model

`internal/domain/verification.go`

Slice 014 adds immutable verification evidence containing:

- task ID;
- attempt ID + run_epoch;
- Goal Contract hash;
- base revision;
- exact candidate revision;
- verification profile ID + profile hash;
- relevant environment/toolchain JSON + hash;
- every required verification command and its pass/fail evidence;
- explicit acceptance evaluation for every Goal Contract criterion;
- verification verdict;
- creation time;
- SHA-256 integrity digest.

Command evidence records command identity, arguments, cwd, exit code, pass/fail, duration, output digest and a bounded output prefix.

Evidence validation cross-checks environment JSON/hash, output-digest shape, command exit/pass consistency, acceptance evidence references and final verdict consistency before persistence.

## Durable result contract

`internal/domain/verification.go`
`internal/store/verification.go`

A task result records the frozen product-result fields:

- task ID;
- monotonically allocated task-local result version;
- Goal Contract hash;
- base revision;
- final task revision;
- changed files/areas;
- evidence ID;
- verification executed;
- pass/fail evidence;
- explicit unresolved risks, including an explicit empty list when none remain;
- integration status;
- workspace disposition;
- resource summary;
- result verdict;
- creation time;
- SHA-256 integrity digest.

Result version allocation happens inside the same serializable SQLite transaction that persists the evidence/result and advances authoritative task state. The result integrity digest is computed only after that durable version is allocated.

## SQLite persistence — schema v7

`internal/store/sqlite.go`
`internal/store/verification.go`

Schema version 7 adds:

- `verification_evidence`;
- `task_results`;
- task/revision lookup indexes;
- foreign-key binding to durable task/attempt/evidence identity;
- unique task-local result versions.

Before evidence can change task state, the persistence transaction independently checks:

- current attempt authority (`attempt_id + run_epoch`);
- task is in `VERIFYING`;
- durable Goal Contract hash and base revision match evidence;
- durable verification profile ID matches evidence;
- every acceptance criterion in evidence matches the immutable Goal Contract by count and text;
- durable workspace head matches the candidate revision;
- evidence and result identities agree;
- evidence/result integrity checks pass.

Passing evidence/result transitions `VERIFYING -> VERIFIED`.

Failed required verification persists a failed evidence/result and transitions `VERIFYING -> BLOCKED`; it cannot become technically verified.

## Candidate sealing and side-effect recovery

`internal/verification/sealer_windows.go`
`internal/store/workspace.go`

Before verification, MAR seals task changes into an exact local Git candidate revision using the existing durable side-effect ledger and contained Git process path.

The seal effect is attempt/run_epoch bound and includes:

- workspace path;
- expected HEAD;
- exact changed paths;
- SHA-256 workspace-state identity;
- deterministic candidate commit message.

Changed candidates require Goal Contract `LocalGitWrite` authority. `.mar` and `.git` runtime/control artifacts are excluded from candidate content.

The sealer is recovery-safe:

- a previously applied seal resumes idempotently without creating a second candidate commit;
- a `DISPATCHED` uncertain seal reconciles Git truth before retry;
- a reconciled non-applied effect requires explicit re-arm/retry;
- direct successful candidate creation is durably observed as `APPLIED`;
- reconciliation checks expected parent, commit message, exact changed paths, authorized workspace-state hash and absence of candidate-vs-workspace drift before advancing durable workspace HEAD.

This closes the crash/race gap where a commit with the right parent/message/path names but different authorized bytes could otherwise be mistaken for the candidate.

## Verification profile and environment identity

`internal/verification/profile.go`
`internal/verification/verifier_windows.go`

Verification profiles are immutable registry values with deterministic hashes. Slice 014 profiles reuse the existing Coding ACI command policy and allow the required Go verification subcommands (`test`, `vet`, `build`) only.

The verifier uses the existing self-hosting-safe Coding ACI runtime and `RunCommand`; it does not create a parallel shell or orchestration system.

Environment identity is captured before and after required verification and binds:

- GOOS/GOARCH;
- enforced sandbox identity;
- each required executable path;
- executable SHA-256;
- executable size.

A toolchain/environment change while verification is running makes the verification fail with explicit unresolved risk.

## Authoritative verification flow

`internal/verification/verifier_windows.go`

The verifier performs:

1. validate current attempt authority;
2. load and hash-check the durable Goal Contract;
3. resolve/hash the configured verification profile;
4. seal the exact candidate revision;
5. prove runtime root == durable task workspace and candidate workspace is clean;
6. transition `RUNNING -> VERIFYING` under attempt fencing;
7. capture environment/toolchain identity;
8. execute every configured verification command through Coding ACI, rechecking attempt authority between commands;
9. capture the environment identity again;
10. prove candidate revision and workspace have not drifted, including untracked source changes;
11. evaluate every immutable acceptance criterion into explicit evidence references;
12. construct and integrity-seal evidence/result;
13. atomically persist the outcome and drive only the valid authoritative task transition.

The verifier does not accept the agent summary, checkpoint text, or `completed_candidate` status as verification evidence.

## Freshness / stale-evidence rejection

`EvidenceFresh` is the eligibility gate for later integration/result reuse.

Historical evidence remains durable for provenance but is rejected as fresh after any relevant drift in:

- Goal Contract hash;
- base revision;
- verification profile ID/hash;
- candidate/durable workspace revision;
- tracked workspace content;
- untracked workspace content;
- relevant environment/toolchain identity.

Failed evidence is never fresh/integration-eligible.

## Agent boundary

`internal/agent/loop_test.go`
`internal/agent/aci_integration_windows_test.go`

Slice 014 preserves the Slice 012 contract:

`finish_task(status=completed_candidate)` only ends the coding loop.

A durable SQLite-backed test proves that returning `completed_candidate` leaves the task in `RUNNING`; only the authoritative Slice 014 verifier can later enter `VERIFYING` and produce technical `VERIFIED` state.

## Acceptance evidence

Focused tests PASS for:

- candidate sealing into one exact revision;
- `.mar` runtime artifact exclusion;
- LocalGitWrite denial;
- idempotent recovery of an already-applied candidate seal;
- uncertain/non-applied seal reconciliation before explicit retry;
- rejection of candidate commits whose bytes do not match the authorized workspace-state identity;
- evidence/result integrity tamper detection;
- durable Goal/profile/acceptance identity checks in SQLite;
- passing verification -> durable `VERIFIED` result/evidence;
- failed required verification -> durable failed result/evidence + `BLOCKED`;
- stale attempt cannot publish a result;
- candidate tracked/untracked drift causes verification failure;
- profile, revision, environment and workspace drift make prior evidence stale;
- verification result/evidence survive SQLite close/reopen;
- `completed_candidate` cannot bypass verification.

Repository-wide verification after Slice 014, using Go 1.27.0 on Windows and `TEMP/TMP=D:\MAR\.mar\runtime\testtmp`:

- `go test -count=1 -timeout 180s ./...`: PASS;
- `go vet ./...`: PASS;
- Windows `go build ./cmd/mar`: PASS;
- `git diff --check`: PASS.

The host's registered Go installation was missing its normal binary during this session, so validation used the official Go 1.27.0 portable toolchain under `D:\MAR\.mar\runtime\go-portable\go`. This is a host-toolchain repair/workaround, not a relaxation of MAR validation.

The known race-detector host compiler limitation remains unchanged; Slice 014 does not claim a race-detector PASS.

## Explicit non-goals

Slice 014 does not implement:

- public MCP control surface;
- ChatWeb-facing submit/status/steer/input/cancel/result/inspect API;
- authoritative project integration/merge workflow completion beyond reporting integration status;
- autonomous self-hosting acceptance;
- a final `SELF_HOSTING_READY` claim.

Those remain owned by Slices 015–016 and existing frozen integration boundaries.
