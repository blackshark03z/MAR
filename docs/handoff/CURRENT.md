# MAR — Current Tech-Lead Handoff

**Architecture:** FROZEN
**Branch:** `master`
**Verified implementation HEAD:** `761376f`

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

## Current state

Repo implementation is verified through Slice 009.

Latest verification:
- `go test -count=1 ./...`: PASS
- `go vet ./...`: PASS
- Windows `go build ./cmd/mar`: PASS
- `git diff --check`: PASS
- T15 real local-file crash/reopen reconciliation: PASS
- Model Gateway targeted suite (tool calls, timeout/cancellation, bounded errors/output, no implicit retry): PASS
- Coding ACI path/hash/tool-dispatch tests: PASS
- ACI -> Windows Job Object -> Git status/diff integration: PASS
- Command output flood bound: PASS
- `ContainedHostExecutor.SelfHostingSafe()`: correctly FALSE

## Next slice

`010` Windows Worker Sandbox.

Goal: replace TRUSTED_HOST-only command execution with an OS-enforced sandbox boundary sufficient for autonomous self-hosting: task-workspace write scope, no arbitrary host filesystem write, controlled read scope, process containment, restricted/default-deny network, and no ambient secret access.

This is an implementation of the frozen Worker Authority Boundary, not a new architecture generation. `SELF_HOSTING_SAFE` must remain false until this slice is proven.

## Bootstrap milestone

`MAR_BOOTSTRAP_STABLE -> SELF_HOSTING_READY`

Remaining bootstrap slices:
- 010 Windows Worker Sandbox
- 011 Minimal Context Engine
- 012 Autonomous Agent Loop
- 013 Semantic Checkpoint + Resume
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

## Session rotation protocol

A new Tech-Lead chat should:
1. connect ChatCode;
2. select project `MAR`;
3. read this file;
4. run `git_status`;
5. inspect only the frozen docs and implementation files relevant to the next slice;
6. continue from the recorded verified HEAD.

Do not require replay of previous chat history.

## Known environment limitation

Go race detector is currently unavailable because the host C compiler lacks required 64-bit support. This is a host toolchain limitation, not a passing race result.
