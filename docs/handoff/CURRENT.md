# MAR — Current Tech-Lead Handoff

**Architecture:** FROZEN
**Branch:** `master`
**Verified implementation HEAD:** `e65f74856e055ad64bf998b9750df39946947397`

Git + frozen docs + this checkpoint are continuity truth. Chat history is disposable working memory.

## Complete

- 001 Durable Task Kernel — `c778a4a`
- 002 Execution Attempt & Logical Fencing — `7753c8e`
- 003 Windows Process Supervisor / Physical Fencing — `c5fc013`
- 004 Resource Governor — `2aec14a`
- 005 Isolated Workspace Manager — `2efbcb8`
- 006 Durable Fair Scheduler — `ba9db93`
- 007 Side-Effect Ledger / T15 — `e31942c`
- 008 Model Gateway — `1af96dc`
- 009 Coding ACI / Tool Runtime — `761376f`
- 010 Windows Worker Sandbox — `06210a4`
- 011 Minimal Context Engine — `1689cd2`
- 012 Autonomous Agent Loop — `c1924e2`
- 013 Semantic Checkpoint + Resume — `432a5c8`
- 014 Verification / Result Contract — `e65f748`

## Current state

Repo implementation is verified through Slice 014.

Latest verification on the Slice 014 implementation commit candidate:
- `go test -count=1 -timeout 180s ./...`: PASS with `TEMP/TMP=D:\MAR\.mar\runtime\testtmp`
- `go vet ./...`: PASS
- Windows `go build ./cmd/mar`: PASS
- `git diff --check`: PASS
- durable verification/result schema + migration: PASS
- candidate sealing is effect-ledger reconciled and idempotent after crash/retry: PASS
- candidate reconciliation proves authorized workspace-state bytes, not only parent/message/path identity: PASS
- `completed_candidate` cannot directly produce technical VERIFIED: PASS
- stale `attempt_id/run_epoch` cannot publish verification/result: PASS
- verification evidence is bound to Goal hash, base revision, candidate revision, profile hash and environment/toolchain identity: PASS
- candidate/profile/environment drift invalidates evidence freshness: PASS
- tracked and untracked workspace drift invalidates evidence freshness: PASS
- failed verification cannot become VERIFIED and records explicit unresolved risk: PASS
- evidence/result integrity tamper is rejected: PASS
- durable Goal Contract/profile/acceptance identity is revalidated by the store before VERIFIED publication: PASS
- result/evidence survive SQLite close/reopen with valid integrity: PASS

`VerificationEvidence` and `TaskResult` are now durable technical truth. Agent self-assertion remains only a candidate assertion.

MAR as a product is **not** yet `SELF_HOSTING_READY`.

## Next slice

`015` MCP Control Surface.

Goal: expose the small ChatWeb-facing control plane over the durable MAR kernel without leaking low-level worker primitives into the public workflow.

Frozen requirements:
- required public operations are exactly the product-level capabilities `submit`, `status`, `steer`, `input`, `cancel`, `result`, and `inspect`;
- normal ChatWeb autonomous workflow must not require direct `read_file`, `write_file`, or unrestricted shell calls;
- low-level coding/file/process primitives remain inside the worker runtime;
- `submit` must preserve Goal Contract validation, base-revision validation, idempotent submission and durable-before-execution semantics already owned by the kernel/service layer;
- `status`, `result`, and `inspect` must expose bounded durable truth rather than reconstructing authority from chat history;
- `result` must surface the durable revision-bound `TaskResult`/verification identity from Slice 014;
- steering may add factual context, clarify priority, choose among explicitly blocked alternatives, request cancellation, or request additional verification;
- steering must never silently rewrite the immutable Goal Contract; a material goal/acceptance/boundary change requires a new superseding task;
- `input` may satisfy explicit task input requirements but must not widen authority or mutate immutable task intent;
- cancellation/steering/input must preserve attempt fencing, resource policy, process containment and side-effect reconciliation guarantees;
- multiple ChatWeb clients may observe/steer the same durable task without creating a second coordination truth;
- no parallel shell/orchestration system and no worker-authority widening;
- no final self-hosting claim; Slice 016 owns self-hosting acceptance.

Use the existing service/store/worker contracts. MCP is control plane, not the coding inner loop.

## Bootstrap milestone

`MAR_BOOTSTRAP_STABLE -> SELF_HOSTING_READY`

Remaining bootstrap slices:
- 015 MCP Control Surface
- 016 Self-Hosting Acceptance

After Slice 016 passes, MAR becomes the primary coding worker for continuing MAR V1 development. ChatCode remains fallback/inspection/emergency repair.

## Frozen boundaries — do not reopen without evidence

- MAR V1 frozen architecture
- single owner / single machine
- MCP is control plane, not inner loop
- one mutable task = one isolated workspace
- logical fencing separated from physical process containment
- SQLite is durable coordination truth
- hard CPU/RAM/disk/process envelope
- serialized authoritative integration
- uncertain side effects reconcile before retry
- worker authority is OS-enforced and strictly weaker than daemon authority
- context retrieval is layered and bounded; vector infrastructure is optional, not a prerequisite
- live model cognition, transcript artifacts and durable semantic checkpoints are distinct state layers
- verification evidence is revision/profile/environment bound and supersedes model self-assertion

## Session rotation protocol

A new Tech-Lead chat should:
1. connect ChatCode;
2. select project `MAR`;
3. read this file;
4. run `git_status`;
5. inspect only the frozen docs and implementation files relevant to the next slice;
6. continue from the recorded verified HEAD.

Do not require replay of previous chat history.

## Known environment limitations / host prerequisite

- Go race detector is currently unavailable because the host C compiler lacks required 64-bit support. This is a host toolchain limitation, not a passing race result.
- Windows LPAC self-hosting commands require the host NUL-device preparation from Slice 010. Windows resets that device security descriptor on reboot; `mar sandbox-host-check` must pass before the executor may report `ENFORCED_SANDBOX`, and `mar sandbox-host-prepare` requires owner/admin elevation when preparation is needed.
- The Windows package inventory reports Go 1.27.0, but the normal installed `go.exe` was missing during Slice 014 validation. Slice 014 was validated with a hash-verified portable Go 1.27.0 toolchain kept under `D:\MAR\.mar\runtime`; if `go` is absent from PATH, use/re-locate that D:-hosted toolchain rather than weakening validation.
- C: remains nearly full. Full validation should keep `TEMP/TMP=D:\MAR\.mar\runtime\testtmp` until C: is cleaned; do not weaken or skip tests to work around storage pressure.
