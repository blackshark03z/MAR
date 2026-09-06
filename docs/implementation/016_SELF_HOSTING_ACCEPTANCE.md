# Slice 016 — Self-Hosting Acceptance

Status: **TECHNICAL BEHAVIORAL ACCEPTANCE PASS — OWNER REAL-USE + LIVE RESOURCE BASELINE PENDING**

Date: 2026-09-06

This report closes the frozen T1–T17 behavioral acceptance suite without claiming `SELF_HOSTING_READY`. Product acceptance remains gated on one owner real-use run and collection of live resource baselines that are not safely or honestly derivable from fault-injected tests.

## Final validation boundary

Final production-source acceptance after the last runtime change:

- `MAR_RUN_SELF_HOSTING_ACCEPTANCE=1 go test -count=1 -timeout 25m -run '^TestAcceptanceT1ToT4TaskClasses$' -v ./internal/orchestrator`: PASS
  - T1 tiny fix: 28.440s; 2 external MCP calls; 2 model turns; 2 tool calls; 220 model tokens
  - T2 medium repair loop: 38.812s; 2 external MCP calls; 5 model turns; 5 tool calls; 550 model tokens; failing-test evidence reached the repair turn
  - T3 large multi-file: 36.243s; 2 external MCP calls; 5 model turns; 5 tool calls; 550 model tokens
  - T4 deep refactor: 39.047s; 2 external MCP calls; 6 model turns; 6 tool calls; 660 model tokens
  - aggregate T1–T4: 18 model turns, 18 tool calls, 1,980 model tokens, 8 external MCP calls
- targeted T5–T17 behavioral matrix: PASS on the final production source
- `go test -p=1 -count=1 -timeout 30m ./...`: PASS across all repository packages
- `go vet ./...`: PASS
- `go build -o .mar/runtime/validation/mar.exe ./cmd/mar`: PASS
- `git diff --check`: PASS
- Go race detector: NOT RUN; host C compiler still lacks the required 64-bit support. This remains an explicit host limitation, not a passing race result.

`-p=1` is intentional for the repository-wide regression because measured Windows cold-build/LPAC contention made cross-package parallel test execution produce host-contention false negatives. Heavy application concurrency is tested explicitly by T5/T6 instead of being inferred from parallel test packages.

## Frozen T1–T17 matrix

| ID | Frozen behavior | Final evidence | Verdict |
|---|---|---|---|
| T1 | Tiny localized fix | `TestAcceptanceT1ToT4TaskClasses/T1-tiny-fix` | PASS |
| T2 | Medium bug with failure/repair loop | `TestAcceptanceT1ToT4TaskClasses/T2-medium-bug-repair-loop` | PASS |
| T3 | Large cross-file change | `TestAcceptanceT1ToT4TaskClasses/T3-large-multi-file` | PASS |
| T4 | Deep refactor | `TestAcceptanceT1ToT4TaskClasses/T4-deep-refactor` | PASS |
| T5 | Five parallel tasks / isolation / disk amplification | `TestAcceptanceT5FiveParallelTasksReceiveDistinctWorktreesAndMeasureDiskAmplification`; `TestAcceptanceT5FiveTasksSameProjectCanExecuteConcurrentlyWhenConfigured`; T13 integration evidence | PASS |
| T6 | Three projects / fairness / responsiveness | `TestAcceptanceT6ThreeProjectsAreFairAndLazilyProvisioned`; `TestAcceptanceT6ThreeProjectsRespectInteractiveResponsivenessCap`; `TestExecutionAdmissionBalancesProjectsBeforeFillingBacklog` | PASS |
| T7 | ChatWeb/MCP client disconnect | `TestAcceptanceT7CLIStdioDisconnectLetsActiveWorkerReachSafeTerminal` through real CLI stdio `CommandTransport` | PASS |
| T8 | Worker crash | `TestAcceptanceT8WorkerCrashDurablyBlocksRealRuntime` with real Runtime + SQLite + worker process | PASS |
| T9 | Daemon crash / restart | `TestAcceptanceT9ActualDaemonCrashReconcilesWithoutFalseCompletion` with real daemon process, marker mutation stop, SQLite reopen/reconcile | PASS |
| T10 | Cancellation with child process tree | `TestRunContainedCommandCancellationKillsDescendant` | PASS; zero orphan active descendants |
| T11 | Low-memory pressure | `TestDaemonMemoryPressureBlocksActualWorkerLaunch`; `TestMemoryPressureMonitorEvictsAfterInitialContextBuild`; `TestSupervisorAppliesHardCPUJobMemoryAndProcessLimits` | PASS |
| T12 | Authoritative base drift | `TestIntegrateRejectsAuthoritativeBaseDrift` | PASS; stale evidence rejected |
| T13 | Semantic conflict across different files | `TestAcceptanceT13RealSemanticConflictNeverSilentlyIntegratesSecondGoal` with real Git + SQLite + Integration Manager | PASS; conflict surfaced, second candidate not silently integrated |
| T14 | Stale worker physical + logical fencing | `TestLogicalFenceDoesNotPermitReplacementUntilPhysicalTermination`; `TestOldAttemptCannotPhysicallyMutateWorkspaceAfterReplacementAdmission` | PASS |
| T15 | Crash during side effect | `TestT15CrashAfterPhysicalLocalEffectDoesNotBlindRedispatch` | PASS; reconcile before any redispatch |
| T16 | Crash during integration | `TestRecoverDispatchedBeforeCASAdvancesOnceAndFinalizes`; `TestRecoverAfterCASBeforeFinalizeDoesNotDoubleAdvance` | PASS |
| T17 | Disk reserve pressure | `TestDaemonDiskPressureCancelsActiveExecutionAndReleasesLease`; `TestResourcePressureMonitorRunsWhileSchedulerStepIsBlocked`; `TestDiskReserveDeniesBeforeHostExhaustion`; `TestMARDiskBudgetIncludesReservations`; `TestPressureSnapshotBoundsSlowMARDiskScanAndFailsSafe` | PASS |

## Acceptance defects found and closed

Slice 016 acceptance found real product defects; tests were not weakened to make them pass:

- execution resource claims ended before worker/verification/integration lifetime;
- execution admission used a hard Job memory ceiling as an estimated RAM reservation, causing `WORKSPACE_READY` starvation under normal host-RAM fluctuation;
- verification descendants had a cancellation path without independently confirmed Job emptiness;
- host disk-reserve observation could be delayed by a non-cooperative recursive MAR disk scan;
- optional context cache eviction occurred only at context-build boundaries rather than under live pressure;
- verification did not initially inherit the same CPU/RAM/process hard envelope as the worker;
- MCP typed output validation failed on large `result`/`inspect` payloads; large control-plane results now use the SDK low-level handler while retaining bounded input parsing and valid structured JSON;
- worker protocol outer-frame encoding did not explicitly own its `json.RawMessage` payload, exposing a large/repeated-frame serialization failure;
- execution fairness initially allowed backlog ordering to monopolize slots;
- cancellation finalization initially reused an already-cancelled context;
- pressure monitoring initially shared the scheduler loop and could be delayed by a blocked scheduling operation.

All above defects have direct regression coverage.

## Metrics collected

- T1–T4 wall-clock: recorded above.
- External MCP calls: 8 total across T1–T4.
- Model turns: 18 total across T1–T4.
- Tool operations: 18 total across T1–T4.
- Model total tokens: 1,980 across T1–T4 scripted acceptance provider.
- T5 disk amplification observation: baseline repository bytes `29,686`; five task-worktree measured bytes `870`; measured ratio `0.029` in the acceptance fixture.
- Worktree cleanup correctness: PASS in workspace/concurrency regression suite.
- Orphan process count for T10: `0` after confirmed cancellation.
- Resume/reconcile correctness: PASS for T9, T15 and both pre/post-CAS T16 crash points.
- Verification pass rate for final T1–T4 run: 4/4.
- Behavioral frozen-suite acceptance rate: 17/17.
- Human intervention inside the scripted T1–T17 task executions: 0 task-level interventions.
- T2 intentional repair rework: one failing-test → repair → re-test cycle.
- T13 semantic integration conflicts surfaced: 1 acceptance conflict scenario, no silent second integration.
- T14 stale-attempt physical mutation rejection: 1 delayed-old-attempt scenario, no post-replacement mutation.
- T15 effect reconciliation: 1 crash-after-dispatch scenario, no blind duplicate effect.

## Required live metrics still pending

The frozen plan also requires numeric live baselines for:

- peak MAR daemon RAM;
- peak worker RAM;
- total CPU pressure;
- workstation responsiveness;
- minimum observed host free-disk reserve.

The technical suite proves the corresponding safety mechanisms (hard Job CPU/RAM/process caps, interactive heavy-job throttling, live memory cache eviction, independent disk-pressure monitor, reserve denial and active-work termination), but it does **not** fabricate numeric live-host baselines from fault injection.

Collect these during the owner real-use run. Record observed values without freezing arbitrary thresholds; the frozen architecture explicitly requires baseline measurement before performance targets are chosen.

## Ship gate

Technical behavioral acceptance is complete. MAR must still **not** be labeled `SELF_HOSTING_READY` until:

1. a real bounded Goal is submitted against a real project;
2. MAR runs it autonomously through worker → verification → result → integration/reject;
3. result/evidence is inspected by the owner;
4. live resource baselines above are recorded during that run;
5. the owner confirms the workflow is materially better than manual ChatWeb ↔ worker forwarding.

No further architecture work is part of the bootstrap gate unless the owner real-use run produces concrete contrary evidence.
