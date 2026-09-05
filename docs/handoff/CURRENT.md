# MAR — Current Tech-Lead Handoff

**Architecture:** FROZEN
**Branch:** `master`
**Verified implementation HEAD:** `1af96dc`

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

## Current state

Repo implementation is verified through Slice 008.

Latest verification:
- `go test -count=1 ./...`: PASS
- `go vet ./...`: PASS
- Windows `go build ./cmd/mar`: PASS
- `git diff --check`: PASS
- T15 real local-file crash/reopen reconciliation: PASS
- Model Gateway targeted suite (tool calls, timeout/cancellation, bounded errors/output, no implicit retry): PASS

## Next slice

`009` Coding ACI / Tool Runtime.

Goal: expose the minimal bounded coding-tool surface required for self-hosting: workspace-scoped read/search, patch, Git diff/status, contained command/test execution, and compact observations suitable for the model loop.

Do not expose unrestricted host filesystem or raw machine authority to the worker.

## Bootstrap milestone

`MAR_BOOTSTRAP_STABLE -> SELF_HOSTING_READY`

Remaining bootstrap slices:
- 009 Coding ACI / Tool Runtime
- 010 Minimal Context Engine
- 011 Autonomous Agent Loop
- 012 Semantic Checkpoint + Resume
- 013 Verification / Result Contract
- 014 MCP Control Surface
- 015 Self-Hosting Acceptance

After Slice 015 passes, MAR becomes the primary coding worker for continuing MAR V1 development. ChatCode remains fallback/inspection/emergency repair.

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
