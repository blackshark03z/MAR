# MAR bounded crash-recovery slice — 2026-09-16

## Status

Source candidate only until activation evidence is recorded. This slice follows the successful real Web no-op CUJ on `v1.2.0-requalification.2`, where task `task-f06c2d6282a54183887bf5a5069d11a2` reached `COMPLETE`, result `VERIFIED`, integration `INTEGRATED`, `changed_areas=[]`, and the authoritative Owner checkout remained byte-for-byte unchanged for every pre-existing dirty path.

## Problem closed by this slice

The prior restart path deliberately failed closed when an attempt was not already `PHYSICALLY_TERMINATED`. After an unclean daemon exit, MAR could revoke logical authority but could not reconstruct an OS-backed physical termination proof because the Windows Job Object handle lived only in daemon memory. This could leave an otherwise recoverable attempt permanently blocked.

## Bounded design

The frozen V1 architecture is unchanged. Worker process containment remains Windows Job Object based.

- Production workers use a deterministic named Job Object per `(task_id, attempt_id, run_epoch)`.
- `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` remains mandatory.
- A trusted recovery marker under `<data-root>/runtime/attempt-recovery` is written only after the worker process is created suspended and successfully assigned to the named Job Object, and before the worker is resumed.
- Marker absence, corruption, identity mismatch, or any uncertain kernel state remains fail-closed. MAR does not infer physical termination from PID absence.
- On daemon restart, an armed attempt may reopen its exact named Job Object. If processes remain, MAR terminates that Job Object and waits for zero active processes. If the armed named Job Object has already disappeared, the proof is derived from the exact kernel object lifecycle plus the durable post-assignment marker, not from process enumeration.
- Only after that proof is valid does the existing service path persist `PHYSICALLY_TERMINATED` with terminal status `recovered-after-daemon-restart`.
- Recovery does not automatically fabricate a task result or bypass the existing replacement/integration lifecycle.
- Historical attempts without the new marker keep the previous fail-closed behavior.

## Worker protocol hardening found during qualification

A grouped host qualification reproduced an intermittent Go 1.27 `encoding/json` failure: `jsontext.Value: invalid character '\x00'`. MAR already had an earlier RawMessage copy workaround from Slice 016, but the same failure resurfaced. Go 1.27 routes `encoding/json` through the JSON v2 implementation, so the outer worker frame no longer carries its inner JSON payload as `json.RawMessage`.

The internal worker frame protocol is bumped from version 1 to version 2. Inner payloads remain normal JSON bytes, but the outer frame carries them as owned `[]byte`; `encoding/json` therefore transports them as base64 and reconstructs the same bytes before inner `json.Unmarshal`. Parent and worker are the same MAR binary, so there is no mixed-version durable protocol state to migrate.

## Evidence

| Gate | Result |
|---|---|
| Recovery targeted packages (`processctl`, `orchestrator`, `worker`) before protocol hardening | PASS |
| Kernel recovery acceptance: reopen live named job | PASS |
| Kernel recovery acceptance: missing marker remains fail-closed | PASS |
| Kernel recovery acceptance: unverified handle loss reconstructs physical proof | PASS |
| Real SQLite T9 restart with armed named job | PASS; replacement admitted only after physical proof; no fabricated result |
| Qualification group 1: `cmd/mar`, `aci`, `agent`, `contextengine`, `domain`, `effects`, `integration`, `mcpedge`, `model`, `model/openaichat` | PASS |
| Qualification group 2 excluding worker outcome | PASS for `orchestrator`, `pathidentity`, `processctl`, `resourcegov`, `scheduler`, `service`, `store`, `testsupport`, `verification`, `workspace` |
| Grouped worker run before protocol v2 | Exposed intermittent `jsontext.Value` / NUL failure; not accepted as flaky |
| Exact four failing worker scenarios stress before protocol v2 | 5/5 PASS, proving intermittency rather than deterministic recovery regression |
| Full `internal/worker` after protocol v2 | 5/5 full package runs PASS, 86.8 s |
| Post-hardening overlap: `cmd/mar`, `orchestrator`, `processctl` | PASS |
| `go vet -p 1 ./...` | PASS |
| `go build -p 1 ./...` | PASS |
| `git diff --check` | PASS |

A monolithic full-repo test command was interrupted at the ChatCode job layer after 300 seconds without semantic progress. Its child test tree was explicitly terminated before split qualification. This is not recorded as a test PASS or FAIL. Qualification was therefore completed as bounded serialized package groups, with the worker package separately stress-run after the protocol hardening.

## Promotion state

No remote push or deploy is authorized by this record. The next gates are:

1. commit this successor source candidate;
2. build and manifest the exact candidate as `v1.2.0-requalification.3`;
3. atomically activate it with rollback available;
4. verify live identity `ALIGNED`, `trusted_for_release=true`, `HEALTHY`, sandbox ready, worker ready;
5. run a new bounded real Web smoke CUJ and verify the authoritative Owner checkout remains unchanged;
6. only then continue to the separate workspace retention/revocation slice.