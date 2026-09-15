# MAR current handoff — 2026-09-16

## Current engineering truth

- Stable-use release: `v1.2.0-requalification.4`
- Exact source commit: `09745c865c164a776f19e102f8c17dc6a87a2a5b`
- Production binary SHA256: `C1176F1F47361AC15F7AA9EC29BDB29294846A8E7914D28006276EAEEA86358A`
- Runtime identity: `ALIGNED`, `trusted_for_release=true`, SQLite schema 14, Owner Console HTTP 8787 reachable.
- Engineering verdict: `RELEASE_QUALIFIED_AND_RUNTIME_ACTIVATED` for the bounded stable-use scope.
- Owner real-use acceptance is intentionally skipped until there is a real project to exercise; it is not inferred.

## Closed stable-use gaps

1. Owner Console reconnect: stopped/IDLE connection is no longer treated as ready, so the primary Connect action returns after stop/disconnect. Targeted connection tests PASS.
2. Verification profile admission: unsupported profiles such as `minimal` are rejected at the MCP boundary before backend submit. Live probe created zero tasks.
3. Verified no-op integration: a verified candidate equal to authoritative HEAD integrates without requiring a clean dirty Owner checkout and without Git mutation.
4. Unclean restart recovery: kernel-backed recovery can prove/force physical attempt termination before replacement; fail-closed behavior remains when proof is unavailable.
5. Terminal workspace retention: safe COMPLETE/CANCELLED/FAILED workspaces are reclaimed only after durable safety gates and physical fencing. BLOCKED workspaces remain retained.
6. Runtime/source/manifest mismatch, portable-Go/ACL qualification, host-vs-LPAC test classification, and frontend LF determinism were closed in the consolidated candidate and qualification chain.

## Runtime evidence

- Post-activation CUJ: `task-3fa746018aa24f02b67c1452ff9c281b`
- Result: `COMPLETE / VERIFIED / INTEGRATED`
- Acceptance oracle `file_contains:README.md:# MAR`: PASS
- Verification profile `go-docs`: test/vet/build all PASS
- Attempt authority: `PHYSICALLY_TERMINATED`
- Final workspace disposition: `REMOVED`; its physical workspace path no longer exists while durable result/evidence remains available.
- Retention activation reclaimed 22 historical terminal workspaces while preserving BLOCKED workspaces.
- Post-retention managed workspace footprint was about 0.247 GiB; task-local Go cache measured 0 MiB; recovery markers were negligible.
- Authoritative Owner checkout remained at `c2d2929a57fddff2868c2927a944e045819ba467` with the same seven pre-existing local paths and hashes throughout qualification/activation.

## Source of truth

- Exact release tag: `v1.2.0-requalification.4`
- Release branch: `codex/stable-use-requalification`
- Release evidence: Git notes ref `refs/notes/mar-release-evidence` on commit `09745c865c164a776f19e102f8c17dc6a87a2a5b`
- Slice records:
  - `docs/release/MAR_STABLE_USE_AUDIT_2026_09_15.md` — historical audit; its old `REQUALIFICATION_REQUIRED` verdict is superseded by the release evidence above.
  - `docs/release/MAR_RECOVERY_SLICE_2026_09_16.md`
  - `docs/release/MAR_RETENTION_SLICE_2026_09_16.md`

## Next gate

There is no known P0/P1/P2 engineering blocker in the bounded stable-use scope. The next product gate is Owner real-use acceptance when a suitable project exists. Until then, use `.4` as the engineering-qualified runtime and do not reopen frozen architecture without new runtime evidence.