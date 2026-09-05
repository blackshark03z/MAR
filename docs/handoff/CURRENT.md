# MAR — Current Tech-Lead Handoff

**Architecture:** FROZEN
**Branch:** `master`
**Verified implementation HEAD:** `432a5c8`

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

## Current state

Repo implementation is verified through Slice 013.

Latest verification:
- `go test -count=1 -timeout 180s ./...`: PASS with `TEMP/TMP=D:\MAR\.mar\runtime\testtmp`
- `go vet ./...`: PASS
- Windows `go build ./cmd/mar`: PASS
- `git diff --check`: PASS
- semantic checkpoint schema/migration v6: PASS
- immutable task-local checkpoint versions + SHA-256 integrity: PASS
- checkpoint payload hard bound: PASS
- stale `attempt_id/run_epoch` cannot publish checkpoint: PASS
- corrupt newest checkpoint skipped for older valid snapshot: PASS
- checkpoint persists across SQLite close/reopen: PASS
- agent `checkpoint_task` publication: PASS
- checkpoint tool must be sole tool call: PASS
- hard resume byte bound before provider call: PASS
- replacement attempt receives prior checkpoint + fresh context in a two-message new model session: PASS
- no full transcript replay required for resume: PASS

Semantic checkpoint content is durable task memory but remains untrusted relative to the immutable Goal Contract and fresh evidence.

MAR as a product is **not** yet `SELF_HOSTING_READY`.

## Next slice

`014` Verification / Result Contract.

Goal: make verification authoritative over model self-assertion and produce a revision-bound durable result/evidence identity.

Frozen requirements:
- a task cannot become technically VERIFIED because the agent returned `completed_candidate`;
- verification evidence is bound to exact candidate revision + verification-profile hash + relevant environment/toolchain identity;
- acceptance evaluation and required verification must execute before VERIFIED/READY_TO_INTEGRATE eligibility;
- stale evidence must be rejected after revision/base/profile/environment drift;
- result must identify task ID, Goal hash, base revision, final task revision, changed areas/files, verification executed, pass/fail evidence, unresolved risks, integration status, workspace disposition, resource summary and verdict;
- remaining risk must be explicit;
- verification/result state must be durable and attempt/revision fenced;
- no MCP control surface scope creep; Slice 015 owns the external API;
- no final self-hosting claim; Slice 016 owns self-hosting acceptance.

Use the existing Coding ACI verification command path and durable task state. Do not create a parallel shell/orchestration system.

## Bootstrap milestone

`MAR_BOOTSTRAP_STABLE -> SELF_HOSTING_READY`

Remaining bootstrap slices:
- 014 Verification / Result Contract
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
- C: remains nearly full. Full validation should keep `TEMP/TMP=D:\MAR\.mar\runtime\testtmp` until C: is cleaned; do not weaken or skip tests to work around storage pressure.
