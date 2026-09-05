# MAR — Current Tech-Lead Handoff

**Architecture:** FROZEN
**Branch:** `master`
**Verified implementation HEAD:** `d0b3290246c92524d6c4e0b142bf3e9a83310193`

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
- 015 MCP Control Surface — `e8bb5b9`

## Current state

Repo implementation is verified through Slice 015 plus the T16 authoritative-integration substep and the self-hosting runtime wiring substep of Slice 016. Slice 016 remains **IN PROGRESS** and MAR is not yet `SELF_HOSTING_READY`.

T16 integration checkpoint: `81c0042` — durable `integration_attempt`, serialized expected-head CAS, deterministic pre/post-CAS crash recovery, stale-evidence/base-drift blocking, and explicit integration-result integrity preservation.

Self-hosting runtime checkpoint: `d0b3290` — real worker process boundary, daemon/preflight/scheduler orchestration, cancellation watcher, fail-closed restart recovery, portable-Go sandbox grants/shared module cache, MCP runtime wiring, and end-to-end MCP→worker→verification→integration execution.

Latest repository-wide verification on the Slice 016 runtime checkpoint:
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
- official MCP Go SDK stdio server: PASS
- public MCP surface is exactly `submit/status/steer/input/cancel/result/inspect`: PASS
- low-level worker filesystem/shell primitives are absent from the public MCP surface: PASS
- durable `task_controls` schema/migration v8: PASS
- task control stream is monotonic, idempotent, integrity-bound and survives SQLite reopen: PASS
- control tamper is rejected: PASS
- concurrent clients append to one SQLite-backed coordination truth: PASS
- steering cannot rewrite the immutable Goal Contract: PASS
- `steer(kind=cancel)` uses the same authoritative cancellation path as `cancel`: PASS
- `input` requires `INPUT_REQUIRED` + current ACTIVE attempt and resumes atomically: PASS
- running cancellation logical-fences before final task cancellation: PASS
- final cancellation is rejected until physical termination is confirmed: PASS
- safe pre-attempt cancellation finalizes immediately: PASS
- durable integration schema/migration v9: PASS
- integration attempt identity/integrity survives SQLite reopen: PASS
- PREPARED/DISPATCHED integration is serialized per project in the V1 daemon process: PASS
- authoritative integration uses `git update-ref <ref> <candidate> <expected_head>` CAS semantics: PASS
- crash before CAS deterministically advances exactly once and finalizes: PASS
- crash after CAS but before durable finalize reconciles without a second ref advance: PASS
- authoritative base drift blocks integration before an attempt is created: PASS
- verification that becomes stale before integration dispatch is blocked before ref mutation: PASS
- integration result cloning preserves explicit empty-array identity required by `TaskResult` integrity: PASS
- worker runs in a separately killable Windows Job Object process tree via the internal `worker-run` protocol: PASS
- worker RPC is bounded to attempt authority + semantic checkpoint operations and rejects task/attempt/epoch escape: PASS
- abrupt worker exit returns no false success while preserving physical-termination proof when Windows confirms zero active processes: PASS
- daemon startup fail-closes orphaned/unproven attempts into recovery-required blocking without admitting a replacement mutable worker: PASS
- daemon startup recovery deduplicates each task within one reconciliation pass: PASS
- `mcp-stdio` now runs the bounded MCP control surface and durable daemon runtime over one SQLite coordination truth: PASS
- client stdio disconnect drains already-active bounded workers instead of treating disconnect as authority to kill mutation-capable work: PASS
- portable Go is granted explicitly to the LPAC sandbox; shared Go module cache is read-only granted while build/tmp caches remain task-local: PASS
- ACI command `cwd="."` correctly resolves to workspace root while `..`/absolute escape remains rejected: PASS
- real E2E test `TestRuntimeE2EMCPSubmitWorkerVerifyIntegrate` proves MCP submit → preflight → resource admission → isolated worktree → real worker child → model/tool loop → candidate seal → `go test/vet/build` → physical termination → serialized integration → authoritative project update: PASS
- E2E candidate changed only the requested `marker.txt`; final task state `COMPLETE`; final result `VERIFIED` with `integration_status=INTEGRATED`: PASS

`VerificationEvidence` and `TaskResult` are durable technical truth. The MCP edge is now a bounded task control plane over that durable kernel; it is not the coding inner loop and owns no independent coordination truth.

MAR as a product is **not** yet `SELF_HOSTING_READY`.

## Next slice

`016` Self-Hosting Acceptance.

Goal: close the remaining runtime wiring needed for one real MCP-submitted Goal to run autonomously through bounded context, worker execution, semantic resume, authoritative verification/result, cancellation/recovery and serialized integration/reject handling; then execute the frozen acceptance suite before any `SELF_HOSTING_READY` claim.

Frozen requirements:
- **T16 integration lane implementation: CLOSED at `81c0042`; retain it as a regression boundary while completing Slice 016.**
- prove the public MCP workflow reaches the existing autonomous worker without turning MCP into the inner coding loop;
- prove durable steering/input controls are consumed by the active/replacement runtime while Goal Contract authority remains immutable;
- prove cancellation reaches graceful interruption/contained process-tree termination and leaves zero orphan mutation-capable children (T10);
- preserve logical fencing + confirmed physical termination before mutable replacement (T14);
- preserve crash reconciliation/no blind duplicate local side effects (T15);
- close and verify serialized authoritative integration with durable `expected_head` validation, evidence identity and deterministic crash recovery (T16); if implementation is missing, treat that as a Slice 016 implementation defect rather than bypassing integration;
- reject stale verification after base/candidate/profile/environment drift, including base-branch drift (T12);
- exercise client disconnect, worker crash and MAR restart recovery without false completion or lost durable state (T7-T9);
- exercise concurrency/fairness/resource/disk-pressure behavior required by T5, T6, T11 and T17;
- surface semantic integration conflicts rather than silently integrating incompatible goals (T13);
- execute the frozen T1-T17 acceptance suite and collect required resource/workflow metrics;
- perform owner real-use acceptance: real project -> bounded Goal -> autonomous execution -> result/evidence inspection -> integrate or reject;
- do not claim `SELF_HOSTING_READY` until all mandatory hard acceptance conditions and owner acceptance pass.

Slice 016 may add the missing integration/runtime glue and benchmark harness required to satisfy these frozen acceptance conditions, but must not redesign the frozen architecture.

## Bootstrap milestone

`MAR_BOOTSTRAP_STABLE -> SELF_HOSTING_READY`

Remaining bootstrap slices:
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
