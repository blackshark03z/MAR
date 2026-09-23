# Slice 059 — Durable-State Deletion Boundary

**Date:** 2026-09-24
**Status:** VERIFIED / ACTIVATED
**Scope:** governed checkpoint/retention boundary only; no task lifecycle, SQLite schema, MCP, or Trusted Owner Fast Path expansion.

## Goal

Close the final bounded kernel blocker: make deletion/reclamation stop at an explicit durable-state boundary.

MAR may reclaim reconstructible execution materializations, but must retain authoritative coordination and evidence facts. A private Git checkpoint ref is a temporary recovery authority while a checkpoint is COMPACTED or REHYDRATING; after durable REHYDRATED finalization it is no longer authority and should not remain pinned forever.

## Reproduced gap

Production SQL audit found no deletion path for authoritative task history:

- no production `DELETE FROM tasks`;
- no production `DELETE FROM execution_attempts`;
- no production `DELETE FROM task_results`;
- no production `DELETE FROM verification_evidence`;
- no production `DELETE FROM workspace_checkpoints`.

Project detach deletes only project policy/project rows, and SQLite foreign keys are enabled. Durable tasks/workspaces/checkpoints therefore prevent project deletion.

Workspace retention already removes materialization while keeping the workspace row and appending a result version.

The remaining leak was private checkpoint refs. Live pre-slice measurement:

- Git repo `D:\MAR`: 132 `refs/mar/checkpoints/...` refs;
- live MAR DB: 134 COMPACTED checkpoints with active ref names;
- live MAR DB: 16 REHYDRATED checkpoints with active ref names.

The DB count spans registered projects, so it is not expected to equal the ref count in the MAR repository. The important finding is that REHYDRATED rows still retained active recovery refs even though durable rehydrate had completed.

## Boundary

Automatic reclamation may delete/release:

- managed worktree materialization after existing physical-fence and result guards;
- positive-allowlist rebuildable caches;
- bounded activation/staging artifacts under existing retention policy;
- private checkpoint Git refs only after the checkpoint is durably REHYDRATED.

Automatic reclamation must not delete:

- tasks;
- execution attempts / authority history;
- task results;
- verification evidence;
- workspace checkpoint rows;
- final integration/evidence receipts.

COMPACTED and REHYDRATING checkpoint refs remain recovery authority and are never eligible for ref release.

## Implementation

No schema or lifecycle state is added.

### Candidate scan

Store selects only checkpoint rows satisfying:

```
state = REHYDRATED
AND ref_name LIKE 'refs/mar/checkpoints/%'
```

The existing scheduler checkpoint reconciliation budget is reused. No daemon, queue, or new durable worker is introduced.

### Safe Git ref release

For each candidate:

1. validate the durable ref namespace and snapshot OID;
2. open the registered project under the existing project lock;
3. inspect the exact ref using Git;
4. if the ref exists, require `ref OID == snapshot_revision`;
5. delete with expected-old-OID semantics:
   `git update-ref -d <ref> <snapshot_revision>`;
6. verify the ref is absent;
7. only then persist a unique release receipt in the existing `ref_name` column:
   `released:<old-ref>`.

The receipt avoids a schema migration and preserves the original ref identity. It also excludes the row from future active-ref candidate scans.

### Crash/idempotency

- crash before ref deletion: next reconciliation retries exact-OID validation/deletion;
- crash after ref deletion but before DB receipt: next reconciliation observes the ref already absent and writes the release receipt;
- after receipt: the row is no longer selected;
- ref OID mismatch: fail closed; do not delete the ref and do not write the receipt.

The checkpoint row, snapshot revision, timestamps and task/evidence history remain durable.

## Additional durable-deletion guards

Regression coverage also proves:

- project detach fails closed while a durable task references the project; the transaction rolls back policy deletion too;
- terminal workspace reclaim retains task, execution-attempt, verification-evidence and workspace-checkpoint rows;
- workspace reclaim appends the REMOVED result version instead of deleting prior results.

## Focused evidence

Current candidate:

- `go test -count=1 ./internal/store ./internal/workspace ./internal/scheduler` — PASS;
- store: PASS (~1.4 s);
- workspace: PASS (~34.6 s);
- scheduler: PASS (~1.8 s);
- `git diff --check` — PASS;
- successful rehydrate test proves ref deletion + durable release receipt + replay returns zero candidates;
- OID-mismatch test proves fail-closed behavior with the mismatched ref left untouched.

## Release acceptance

Before this blocker may be marked closed:

1. full exact-HEAD `go test -p 1 -count=1 -timeout 300s ./...` PASS;
2. `go vet -p 1 ./...` PASS;
3. `go build -p 1 ./...` PASS;
4. `git diff --check` PASS;
5. clean unchanged exact HEAD;
6. local HEAD == `origin/master`;
7. exact revision activated live as `HEALTHY / ALIGNED / trusted_for_release=true`;
8. post-activation live DB shows REHYDRATED active checkpoint-ref count converging downward while COMPACTED recovery refs are preserved.

## Release evidence

Exact code-bearing revision `28fba4e2ff91c2d475930c75f0af172a10f6e7b9` completed the full release gate with:

- `go test -p 1 -count=1 -timeout 300s ./...` — PASS;
- `go vet -p 1 ./...` — PASS;
- `go build -p 1 ./...` — PASS;
- `git diff --check` — PASS;
- identical HEAD before/after and a clean working tree;
- local HEAD == `origin/master`.

The same revision was activated live and reported `HEALTHY / ALIGNED / trusted_for_release=true`, manifest `ALIGNED`, with the OpenAI Secure Tunnel connected / ready / healthy.

Live deletion-boundary acceptance after activation:

- before activation: REHYDRATED active checkpoint refs = 16;
- after activation: REHYDRATED active checkpoint refs = 0;
- durable released receipts = 16;
- COMPACTED active recovery refs = 134 before and 134 after;
- physical `D:\MAR` checkpoint Git refs = 132 before and 116 after, exactly 16 released;
- runtime remained healthy/aligned after cleanup.

All 16 eligible live rows belonged to project `mar` at `D:\MAR`. No COMPACTED recovery authority was deleted.

The durable-state deletion-boundary blocker is closed. Together with Slices 057 and 058, the finite kernel closure list is empty. MAR architecture closure may therefore be marked STABLE, subject to the repository's final docs-only closeout HEAD remaining exact-qualified and activated.
