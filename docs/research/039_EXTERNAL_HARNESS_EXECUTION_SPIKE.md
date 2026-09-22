# External Harness Execution Spike

Date: 2026-09-22
Status: VERIFIED SPIKE — CLOSED FOR DEFAULT-PATH EXPANSION

> Forward note: `docs/architecture/MAR_ARCHITECTURE_STABILITY_POLICY.md` supersedes the earlier suggestion to continue OMP authentication as the next default step. The OMP probe remains evidence that a real third-party executable can run under MAR containment. Further AI-harness/model work requires an explicit delegated-worker Goal.
Architecture authority: docs/architecture/MAR_CONDITIONAL_EXECUTION_KERNEL.md
Baseline: docs/research/038_CONDITIONAL_KERNEL_RESPONSIBILITY_INVENTORY.md

## Purpose

Prove that a governed MAR worker can execute a replaceable external coding harness while MAR retains containment and downstream verification/integration, without initializing MAR-owned context/model/agent/Web cognition for that execution path.

This is a deletion-enabling seam, not a new orchestration lifecycle.

## Implemented path

A third worker mode now exists:

- provider: compatibility MAR-owned provider cognition
- web: compatibility MAR-owned Web cognition
- harness: external executable selected by trusted runtime configuration

For harness mode:

1. StartRequest still carries the durable task/attempt/workspace execution identity.
2. The harness executable must be an explicit absolute path.
3. The external executable runs through WindowsSandboxExecutor (LPAC) with the governed workspace boundary.
4. Environment is a bounded non-secret Windows runtime whitelist only:
   SystemRoot, WINDIR, ComSpec, PATH, PATHEXT, USERPROFILE, LOCALAPPDATA, APPDATA, TEMP, TMP, ProgramFiles, ProgramData.
5. The harness branch returns before rpcClient/contextengine/model Gateway/agent Loop construction.
6. Successful harness exit is projected to completed_candidate.
7. Non-zero/error execution remains a worker process error and cannot be promoted.
8. TaskRunner and RuntimeConfig can carry harness executable/arguments while provider/Web behavior remains unchanged.

CLI/UI selection is intentionally NOT exposed in this spike.

## Evidence

Focused external-harness test:

- TestRunChildExternalHarnessBypassesMAROwnedCognition: PASS
- TestStartRequestBoundaryProjectionPreservesLegacyWireShape: PASS
- elapsed: ~0.4s package run

The external harness integration test:
- uses a real executable;
- runs it inside LPAC;
- leaves AgentProfile and AgentConfig empty;
- writes an artifact into the governed workspace;
- observes HARNESS_OK output;
- returns completed_candidate.

Regression:

- go test ./internal/worker ./internal/orchestrator: PASS
  - worker ~33.1s
  - orchestrator ~102.2s

Full repository:

- go test ./...: PASS after test-isolation fix
- final run ~30.0s wall time; internal/worker executed in ~22.6s
- all packages PASS; no external-harness.ok artifact remained in the repository after the run

## What this proves

MAR authority/isolation does not require MAR-owned model/provider/session cognition.

The worker process can host an external harness execution path while preserving the same candidate -> verification -> integration architecture.

This creates a concrete future deletion route for:
- provider routing/config owned by MAR;
- agent loop/session ownership;
- DecisionProjection transport used only for MAR cognition;
- WebTurn lifecycle and brain_turn/brain_respond surfaces.

Deletion is NOT authorized yet. First run at least one real harness adapter through this mode and verify:
- bounded mutation;
- candidate identity;
- verification/integration;
- cancellation/fencing;
- crash/recovery;
- no secret leakage;
- no hidden WebTurn/DecisionProjection dependency.

## Next bounded step

Use one real external harness executable/adapter in a disposable governed task. Prefer the smallest available harness with deterministic non-interactive execution. Do not integrate OMP/ChatCode-specific lifecycle into MAR.

Acceptance for the next step:
- same Goal Contract and verification profile;
- harness modifies a bounded fixture;
- MAR records candidate/verification/integration normally;
- zero WebTurn rows for the task;
- zero DecisionProjection RPC calls for the task;
- cancellation still physically terminates the harness tree;
- external harness code remains replaceable and outside MAR cognition packages.

If that passes, retire one compatibility cognition responsibility at a time. If it requires adding another durable session lifecycle to MAR, reject the adapter design.

## Live OMP binary follow-up

Post-commit live probe on 2026-09-22:

- Installed OMP v18.2.6 Windows x64 from the upstream GitHub release asset.
- Verified SHA-256 before execution:
  `1fbff31df4bba1ec8a48d74b392e4b4c8c12de0a7f53cf62c6e5436fdfebda28`.
- Host `omp --version`: `omp/18.2.6`.
- A temporary, non-committed worker test executed that real `omp.exe --version` through the MAR `BrainHarness` LPAC path.
- Result: PASS; the real third-party executable returned through `completed_candidate` in ~1.33s.
- The temporary local-install-dependent test was removed after evidence capture; repository returned clean.

A real OMP coding turn is currently blocked outside MAR by provider authentication/model configuration:
- no supported API-key environment variable was present;
- OMP had no stored auth/model state;
- one-shot `omp -p --no-session` reported that no models were available.

Existing `.codex`, `.claude`, and `.gemini` directories are not treated as credential authority for this integration. MAR does not read/copy those credentials. OMP's own supported auth flow must establish its credential store before a governed coding-task benchmark.

Therefore the current conclusion is stronger but still bounded:

1. MAR external-harness execution is technically compatible with the real OMP binary under LPAC.
2. MAR-owned cognition is not required to launch OMP.
3. Credential onboarding is now the only known blocker to a real OMP mutation/verification/integration trial.
4. Legacy MAR cognition remains compatibility code until that authenticated real-task trial passes.
