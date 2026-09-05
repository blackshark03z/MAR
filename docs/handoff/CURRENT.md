# MAR — Current Tech-Lead Handoff

**Architecture:** FROZEN
**Branch:** `master`
**Verified implementation HEAD:** `1689cd2`

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

## Current state

Repo implementation is verified through Slice 011.

Latest verification:
- `go test -count=1 -timeout 180s ./...`: PASS with `TEMP/TMP=D:\\MAR\\.mar\\runtime\\testtmp`
- `go vet ./...`: PASS
- Windows `go build ./cmd/mar`: PASS
- `git diff --check`: PASS
- real Git snapshot -> bounded context pack integration: PASS
- exact HEAD revision + clean/modified/staged/untracked inventory: PASS
- ignored files excluded; NUL-delimited filename identity preserved: PASS
- revision mismatch fail-closed: PASS
- deterministic lexical/symbol ranking: PASS
- local Go import + same-package dependency expansion: PASS
- individual file / total scan / file count / snippet / entry / term / cache / final pack bounds: PASS
- oversized intent terms cannot exceed context-pack budget: PASS
- binary/non-UTF-8, traversal and control-character path rejection: PASS
- Git output overflow and context cancellation fail closed: PASS
- file evidence is SHA-256 bound

Slice 010 OS authority guarantees remain unchanged. `Runtime.SelfHostingSafe()` describes the Coding ACI execution boundary only; MAR as a product is **not** yet `SELF_HOSTING_READY`.

## Next slice

`012` Autonomous Agent Loop.

Goal: implement the smallest local Codex-like model↔tool loop behind MAR, using already-frozen primitives rather than adding another orchestration system.

Required shape:

`Goal Contract + bounded Context Pack -> pinned model turn -> bounded tool call(s) -> Coding ACI -> observation -> next model turn -> completion/block/limit`

Slice 012 must:
- keep the inner loop local to MAR; MCP/ChatWeb must not remote-control individual tool turns;
- pin model/tool schema/base instructions/sandbox/workspace identity for the continuous phase;
- treat repository context/tool observations as untrusted evidence, not authority/instructions;
- use the existing Model Gateway and Coding ACI instead of provider-specific or shell-specific shortcuts;
- enforce hard turn/tool/token/context/time/output limits;
- preserve immutable Goal Contract boundaries and never widen worker authority;
- distinguish model completion from verification acceptance (Slice 014 remains authoritative verification/result contract);
- terminate deterministically as completed-candidate, blocked, cancelled, or budget-exhausted;
- avoid semantic checkpoint/resume scope creep; durable semantic resume belongs to Slice 013.

The first acceptance should use deterministic fake-model loops plus at least one real Coding ACI tool path. Do not require a live paid provider to make Slice 012 tests deterministic.

## Bootstrap milestone

`MAR_BOOTSTRAP_STABLE -> SELF_HOSTING_READY`

Remaining bootstrap slices:
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
- On the Slice 011 validation run, C: had only ~0.09 GB free while D: had ~5.47 GB free. The unchanged full test suite failed once when Go-in-LPAC used the default C: temp directory, then passed when `TEMP/TMP` were set to `D:\\MAR\\.mar\\runtime\\testtmp`. Until C: is cleaned, full validation should keep temp files on D: rather than weakening/skipping tests.
