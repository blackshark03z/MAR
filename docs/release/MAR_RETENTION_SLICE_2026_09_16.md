# MAR terminal workspace retention slice — 2026-09-16

## Scope

Bounded operational slice after `v1.2.0-requalification.3`. This slice closes automatic terminal workspace reclamation without changing the frozen V1 execution architecture, verification semantics, integration authority, or Owner checkout policy.

## Observed production reality before the fix

Runtime `.3` was healthy/aligned/trusted, but the live database contained 61 workspaces still in `READY` state. The observed distribution included:

- 23 `BLOCKED` tasks with no result and no unsafe attempt;
- 12 `CANCELLED` tasks with no result and no unsafe attempt;
- 10 `COMPLETE + VERIFIED + INTEGRATED + RETAINED` tasks with no unsafe attempt;
- 7 `BLOCKED + VERIFICATION_FAILED + NOT_INTEGRATED + RETAINED` tasks;
- 6 `BLOCKED` tasks with an attempt not yet `PHYSICALLY_TERMINATED`;
- 2 `BLOCKED + VERIFIED + integration BLOCKED + RETAINED` tasks;
- 1 `BLOCKED + UNVERIFIED + NOT_INTEGRATED + RETAINED` task.

All 10 retained COMPLETE workspaces had `task_result.final_revision == workspace.head_revision` and zero attempts outside `PHYSICALLY_TERMINATED`. They were therefore safe retention candidates, but the activated `.3` runtime had no production reclaimer chain wired into the daemon loop.

The underlying manual removal primitive already existed for terminal tasks, including CANCELLED/FAILED, but it was not called automatically. This allowed per-task source trees and task-local Go caches to accumulate until MAR previously crossed its 20 GiB managed-disk budget.

## Root cause

`workspace.Manager.RemoveTerminal` existed, and store-level removal already required a terminal task plus physical fencing. However, the production path at `.3` did not expose a bounded reclaimer through scheduler/daemon. No background lifecycle step selected safe terminal workspaces and invoked the removal primitive.

## Frozen correction contract

Automatic reclamation is deliberately narrower than generic deletion:

1. Only terminal task classes are eligible: `COMPLETE`, `CANCELLED`, or `FAILED`.
2. `BLOCKED` is never auto-reclaimed. This preserves recovery, diagnosis, blocked-choice, and retry semantics.
3. Every candidate must have no execution attempt outside `PHYSICALLY_TERMINATED` before it is selected; `BeginWorkspaceRemoval` rechecks that physical fence transactionally.
4. COMPLETE tasks additionally require the latest result to be `VERIFIED`, `INTEGRATED`, `RETAINED`, and `final_revision == workspace.head_revision`.
5. CANCELLED/FAILED tasks do not require a TaskResult; their terminal state plus physical fence is sufficient under the pre-existing removal contract.
6. Reclamation is bounded per daemon tick (`TerminalReclaimsPerTick`, default 2) and best-effort. A cleanup error is reported independently and does not block normal scheduling.
7. Physical removal is limited to the MAR-managed workspace path and Git worktree registration. The authoritative Owner checkout is not cleaned/reset/staged/checked out.
8. For COMPLETE tasks, durable result history is preserved by appending a new result version with `workspace_disposition=REMOVED` and removal evidence. Existing verification/integration evidence remains intact.
9. Workspace finalization is idempotent across a crash window after physical removal: replaying `FinishWorkspaceRemoval` does not create duplicate result versions.

## Live controlled proof

With zero active MAR tasks, a temporary test invoked the candidate `ReclaimTerminal(1)` path against the live MAR database. Exactly one safe retained COMPLETE workspace was reclaimed successfully (`reclaimed=1`, no error). The temporary probe source was removed immediately afterward. This proved the new chain can execute the existing physical removal primitive against real MAR state rather than only fixtures.

The probe used store safety gates and did not bypass physical-fence or result-identity checks.

## Qualification evidence

- Baseline retention patch targeted packages: `store`, `workspace`, `scheduler`, `orchestrator` — PASS.
- Terminal-scope candidate tests prove:
  - safe COMPLETE is selected;
  - CANCELLED/FAILED without TaskResult are selected;
  - BLOCKED is excluded;
  - a terminal task without physical termination proof is excluded.
- Final targeted packages after hardening: `store`, `workspace`, `scheduler`, `orchestrator` — PASS.
- Qualification group A: `cmd/mar`, `aci`, `agent`, `contextengine`, `domain`, `effects`, `integration`, `mcpedge`, `model`, `model/openaichat` — PASS.
- Qualification group B: `pathidentity`, `processctl`, `resourcegov`, `service`, `verification`, `worker` — PASS.
- `go vet -p 1 ./...` — PASS.
- `go build -p 1 ./...` — PASS.
- `git diff --check` — PASS.

## Release semantics

This slice may be considered code-qualified only after commit identity is bound to a release candidate binary and the activated runtime demonstrates backlog reclamation while remaining `HEALTHY`, `ALIGNED`, trusted, sandbox-ready, and worker-ready. Do not infer Product Accepted from this engineering slice. No remote push or deploy is authorized by this document.
