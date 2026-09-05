# MAR — Current Tech-Lead Handoff

**Architecture:** FROZEN
**Branch:** `master`
**Verified implementation HEAD:** `06210a4`

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

## Current state

Repo implementation is verified through Slice 010.

Latest verification:
- `go test -count=1 -timeout 180s ./...`: PASS
- `go vet ./...`: PASS
- Windows `go build ./cmd/mar`: PASS
- `git diff --check`: PASS
- Windows sandbox host readiness after elevated NUL preparation: PASS
- LPAC workspace write / outside read+write deny / default-deny network: PASS
- LPAC `ALL APPLICATION PACKAGES` opt-out: PASS
- task-unique capability cross-workspace isolation: PASS
- explicit runtime read-only scope: PASS
- ambient environment secret exclusion: PASS
- `registryRead` compatibility scope probe (required system read; HKCU secret + SAM/SECURITY denied): PASS
- LPAC descendant timeout -> Job Object kill -> no delayed mutation: PASS
- temporary ACL restoration and keyed-lock cleanup: PASS
- typed Git broker blocks repository-configured external helper/fsmonitor execution: PASS
- sandboxed typed Git status/diff integration: PASS
- native `go test` executed inside LPAC: PASS
- `Runtime.SelfHostingSafe()`: TRUE only when a ready LPAC executor **and** typed Git broker are configured

`Runtime.SelfHostingSafe()` here describes the Coding ACI execution boundary only. MAR as a product is **not** yet `SELF_HOSTING_READY`; Slices 011–016 remain.

## Next slice

`011` Minimal Context Engine.

Goal: implement the frozen layered context path with a bounded, revision-aware context pack:

`Goal/task intent -> repo/Git metadata -> lexical search -> symbol/dependency signals -> optional semantic retrieval -> context pack`

V1 must start with Git/metadata + lexical + symbol/dependency retrieval. Semantic embeddings remain optional; no global always-loaded vector database is authorized. Any indexes should be content-hash reusable, incremental, lazy-loaded, and shareable across identical worktree content.

The first implementation should prefer the smallest deterministic context engine sufficient for Slice 012's autonomous agent loop: bounded output, explicit source/revision identity, stable ranking, and no transcript-sized replay.

## Bootstrap milestone

`MAR_BOOTSTRAP_STABLE -> SELF_HOSTING_READY`

Remaining bootstrap slices:
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
- worker authority is OS-enforced and strictly weaker than daemon authority
- context retrieval is layered and bounded; vector infrastructure is optional, not a prerequisite

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
