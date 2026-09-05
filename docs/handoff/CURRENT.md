# MAR — Current Tech-Lead Handoff

**Architecture:** FROZEN
**Branch:** `master`
**Verified implementation HEAD:** `c1924e2`

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

## Current state

Repo implementation is verified through Slice 012.

Latest verification:
- `go test -count=1 -timeout 180s ./...`: PASS with `TEMP/TMP=D:\MAR\.mar\runtime\testtmp`
- `go vet ./...`: PASS
- Windows `go build ./cmd/mar`: PASS
- `git diff --check`: PASS
- real Coding ACI autonomous loop (`write_file` -> typed Git status -> finish): PASS
- durable SQLite `attempt_id/run_epoch` authority validation before real ACI mutation: PASS
- logical fence stops stale attempt before subsequent model/tool dispatch: PASS
- Goal Contract authority filters mutation tool surface: PASS
- unclassified/unauthorized tools fail closed: PASS
- context revision + Goal-hash identity pinning: PASS
- explicit `finish_task` terminal protocol; mixed finish+mutation batch denied: PASS
- turn/tool/token/context/request/assistant/observation/time bounds: PASS
- provider output cap/length termination blocks tool side effects: PASS
- duplicate tool-call IDs blocked before unsafe execution: PASS
- repository/tool/test text remains untrusted evidence, not authority

`completed_candidate` is only an agent-loop result. Slice 014 remains the authoritative verification/result contract. MAR as a product is **not** yet `SELF_HOSTING_READY`.

## Next slice

`013` Semantic Checkpoint + Resume.

Goal: persist the minimum immutable/versioned semantic recovery state needed to reconstruct a bounded new model session after worker interruption without replaying the full transcript.

Frozen checkpoint minimum:
- Goal Contract hash;
- base revision;
- current revision;
- completed work;
- current hypothesis;
- changed areas;
- verification status;
- blockers;
- remaining work;
- next action;
- critical evidence references.

Required properties:
- checkpoint snapshots are immutable/versioned;
- atomic durable write;
- integrity hash/checksum;
- checkpoint is bound to task + authoritative attempt/run_epoch and stale attempts cannot publish a new checkpoint;
- recovery chooses the latest valid compatible checkpoint; corrupt/incompatible snapshots never override durable task truth;
- full transcripts/tool logs remain external artifacts and are not replayed by default;
- resume input is bounded Goal + latest valid semantic checkpoint + selected evidence/current context;
- same-workspace mutable replacement still requires Slice 002/003 physical/logical fencing before a replacement attempt becomes mutation-capable;
- do not implement final verification/result contract or MCP control surface here.

## Bootstrap milestone

`MAR_BOOTSTRAP_STABLE -> SELF_HOSTING_READY`

Remaining bootstrap slices:
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
- live model cognition, transcript artifacts and durable semantic checkpoints are distinct state layers

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
