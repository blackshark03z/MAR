# MAR — Current Tech-Lead Handoff

Updated: 2026-09-16

## Current truth

- Architecture: **FROZEN V1**. Do not reopen without concrete runtime evidence.
- Engineering release: **`v1.2.0-requalification.7`**.
- Canonical source / local `master` / `origin/master` / release tag target: **`73d75558946ac2570d3d1319d1461b2126ce9049`**.
- Runtime source: **`73d75558946ac2570d3d1319d1461b2126ce9049`**.
- Runtime: **HEALTHY / ALIGNED / trusted_for_release=true**, SQLite schema 14.
- Release binary SHA-256: **`A809D8F0A4ED0DA3B27B11F1F551B4C71417354FCD0FA45F5DC5CC71A4FC8E84`**.
- Owner UI SHA-256: **`F1EF68C2551BE783AD478275DB49A5BFBE51F01592D424144140680EA7B2C3A0`**.
- Engineering verdict: **RELEASE_QUALIFIED_AND_RUNTIME_ACTIVATED**.
- Owner real-project acceptance: **DEFERRED**. Do not relabel as Product Accepted until a representative real-project CUJ is accepted by the Owner.

## Why `.7` exists

Task Convergence v2 Slice A first reduced historical BLOCKED noise and over-sensitive no-progress detection. A later recovery slice added periodic retry for transient physical-recovery misses. The first live runtime carrying that retry (`.6`, source `7e6b8d6ffe4fe8818447f4c2ea8f5648b17cae0a`) exposed a regression: periodic recovery scanned `TaskRunning` and could fence an attempt still owned by the current daemon.

`.7` fixes only that regression: periodic reconciliation skips tasks present in the live daemon `active` set, while startup reconciliation still runs before any active execution exists and remains fail-closed for stale attempts. The stale-attempt retry path and physical-proof requirements are unchanged.

## Qualification evidence

Exact candidate `73d75558946ac2570d3d1319d1461b2126ce9049` passed:

- targeted live-attempt regression: PASS;
- targeted transient stale-attempt recovery regression: PASS;
- all 21 repository package tests: PASS;
- `go vet -p 1 ./...`: PASS;
- `go build -p 1 ./...`: PASS;
- candidate tree clean before promotion;
- staging runtime identity: ALIGNED / trusted.

Post-activation self-hosting CUJ:

- task: `task-0db4eb285d554e03a5c59502eb5b8046`;
- base/final revision: `73d75558946ac2570d3d1319d1461b2126ce9049`;
- Web worker read-only phase: PASS (`README.md` contains `# MAR`, workspace Git clean);
- canonical `go-standard`: test / vet / build PASS;
- verification evidence: `evidence-54d7e1eb44fa4eef9790baef7b7df9ad`;
- result integrity: `07d411d90db832194c867d13a5b8eb2c881a75be7622196fbc9a5cc43add2687`;
- verdict: VERIFIED;
- integration: INTEGRATED (no-op);
- attempt: PHYSICALLY_TERMINATED;
- workspace: REMOVED;
- `changed_areas=[]`;
- `unresolved_risks=[]`.

Release evidence is also bound to `refs/notes/mar-release-evidence` and the annotated tag `v1.2.0-requalification.7` on GitHub.

## Convergence / BLOCKED interpretation

Historical BLOCKED counts are not a current `.7` failure rate. The audit that motivated Slice A found the old database was dominated by pre-`.4` development/requalification tasks. Owner Console now distinguishes historical/superseded BLOCKED/FAILED tasks from current actionable work using task `base_revision` versus registered project HEAD. `INPUT_REQUIRED` remains actionable.

The no-progress guard now uses semantic checkpoint progress rather than Git revision alone and requires repeated no-progress before blocking. Do not widen the global 30-minute / token / decision budgets again unless new `.7` real-project evidence shows they remain a practical blocker.

## Known non-blocking recovery edge

During operator recovery of the revision-changing recovery candidate, a crash after authoritative ref CAS left a zero-byte stale `.git/index.lock`. Git then reported many false `diff-files` changes until the stale lock was proven orphaned, removed, and index metadata refreshed. MAR correctly failed closed; no Owner work was overwritten and no SQL bypass was used.

There is intentionally **no automatic stale-lock deletion** in `.7`: lock presence alone cannot prove that another Owner Git process is dead. If this repeats in real use, open a separate bounded slice that introduces MAR-owned Git-operation identity/marker evidence before any automatic lock reconciliation.

## Next action

Use `.7` for normal project development. Collect real task outcomes and block reasons. Only open Convergence v2 Slice B (adaptive budget) or additional recovery work if repeated real-project evidence justifies it. Direct ChatCode/operator mutation is now bootstrap/emergency recovery, not the normal development path.