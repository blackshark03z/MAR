# Slice 003 — Windows Process Supervisor / Physical Fencing

**Architecture:** FROZEN
**Status:** COMPLETE / VERIFIED

## Goal

Make R3 physical-fencing enforceable on Windows: a durable attempt may be marked `PHYSICALLY_TERMINATED` only after an OS-contained process tree is confirmed to have zero active processes.

## Design

- `go-winjob v1.0.0` wraps Windows Job Objects.
- Worker process starts **suspended**, is assigned to the Job Object, then resumes.
- `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` is enabled.
- MAR intentionally does **not** enable `BREAKAWAY_OK` or `SILENT_BREAKAWAY_OK`.
- Descendant processes inherit Job Object containment under normal Windows process creation semantics.
- `TerminateJobObject` is followed by Job Object accounting confirmation: `ActiveProcesses == 0`.
- Only then does the supervisor produce an unforgeable-by-construction `TerminationProof`.
- `TaskService` accepts this proof before recording `PHYSICALLY_TERMINATED`.

## Important failure semantics

- Logical `run_epoch` fencing remains separate.
- Timeout querying physical termination returns **no proof**.
- Emergency Job Object close is cleanup only and returns no proof.
- Failed process start defensively kills/waits any partially-created suspended process.
- If physical termination cannot be confirmed, replacement remains blocked.

## Acceptance

1. Root process is inside expected Job Object before user code runs.
2. A child spawned by the root is inherited by the same containment tree.
3. Termination kills parent and child.
4. Proof is valid only when Job Object reports zero active processes.
5. Zero/forged proof is rejected.
6. Service cannot unlock replacement from logical fence alone.
7. Valid OS termination proof enables durable physical termination and higher-epoch replacement.
8. A stale attempt that physically writes a workspace marker cannot mutate it after replacement admission.
9. No public `TaskService` bypass can directly assert physical termination without `TerminationProof`.
10. Existing Slice 001/002 tests continue to pass.
11. `go test ./...`, `go vet ./...`, build all pass on Windows.

## Source rationale

Microsoft Job Object semantics: processes associated with a job are managed as a unit and child processes are associated by default unless breakaway behavior is enabled. `go-winjob.Start` creates the process suspended, assigns it to the Job Object, and resumes it only after assignment.

## Evidence

- Parent + inherited child Job Object containment/termination test: PASS.
- Zero/forged `TerminationProof` rejection: PASS.
- Logical fence without physical proof keeps replacement blocked: PASS.
- OS-backed proof unlocks a higher-epoch replacement: PASS.
- Physical writer marker remains unchanged after replacement admission: PASS.
- Stale logical events are rejected after replacement admission: PASS.
- `go test ./...`: PASS.
- `go vet ./...`: PASS.
- Windows build `cmd/mar`: PASS.
- Final implementation revision is recorded by the Git commit containing this document.
