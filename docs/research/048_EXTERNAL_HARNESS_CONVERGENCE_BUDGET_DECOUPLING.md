# External Harness Convergence Budget Decoupling

Date: 2026-09-23
Status: VERIFIED
Baseline: 66c5e8bf0e6aa90a311477d532688afee232c97d
Architecture authority: docs/architecture/MAR_ARCHITECTURE_STABILITY_POLICY.md

## Goal

Prevent a replaceable external coding harness from being blocked by MAR compatibility-cognition ceilings that it does not consume, while retaining task-wide governed-execution convergence guards.

## Previous behavior

TaskRunner queried the task-wide convergence budget before every attempt and applied one generic stop reason to all brain modes.

The convergence budget combines two different classes of limits.

Compatibility cognition limits:

- model decision count;
- MAR worker tool-call count;
- MAR model token count.

Governed execution limits:

- semantic no-progress streak;
- cumulative active execution time;
- attempt count.

An external harness produces zero MAR model decisions, zero MAR agent tool calls and zero MAR model tokens. Nevertheless, a task could be prevented from entering BrainHarness because an earlier compatibility path had already exhausted one of those cognition counters.

## Change

TaskRunner now resolves convergence stop semantics by worker mode.

Provider and Web modes:
- behavior is unchanged;
- all existing convergence ceilings remain active.

External Harness mode:
- ignores model_decision_limit;
- ignores worker_tool_call_limit;
- ignores model_token_limit;
- still fails closed on no_progress;
- still fails closed on active_execution_limit;
- still fails closed on attempt_limit.

The TaskConvergenceBudget service/store is retained. This slice does not create a second budget system.

## Evidence

Focused regression:

- TestHarnessConvergenceIgnoresCompatibilityCognitionCeilingsButKeepsKernelGuards: PASS;
- TestTaskWideConvergenceGuardStopsRepeatedNoProgress: PASS;
- TestTaskWideConvergenceBudgetAccumulatesAcrossReplacementAttempts: PASS.

The harness-specific test proves that a budget with all cognition counters exhausted does not stop BrainHarness when execution/attempt capacity remains, while Web and Provider still stop on the same cognition exhaustion.

It separately proves Harness still stops on:

- no-progress;
- active execution exhaustion;
- attempt exhaustion.

Real E2E:

- TestRuntimeE2EExternalHarnessWorkerVerifyIntegrate: PASS in ~26.77s;
- TestRuntimeE2EWebBrainMCPWorkerVerifyIntegrate: PASS in ~32.80s;
- combined orchestrator package ~59.71s.

Full repository:

- go test ./...: PASS;
- wall time ~117.50s;
- cmd/mar ~10.08s;
- internal/orchestrator ~110.77s;
- remaining packages cached or PASS.

## Architectural consequence

MAR no longer treats external-harness work as if it consumed MAR-owned cognition.

The retained limits are properties of governed execution and convergence, not of a particular model/provider/agent implementation.

This removes lifecycle coupling without weakening attempt, duration or semantic no-progress protection.
