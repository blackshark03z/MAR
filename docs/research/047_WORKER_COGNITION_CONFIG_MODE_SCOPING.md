# Worker Cognition Config Mode Scoping

Date: 2026-09-23
Status: VERIFIED
Baseline: f6dc2ef9091dd1dad808393b8dd1709c76934d9c
Architecture authority: docs/architecture/MAR_ARCHITECTURE_STABILITY_POLICY.md

## Goal

Remove MAR-owned agent configuration from worker modes that do not consume it, while preserving the compatibility Web loop's actual safety/budget inputs and Provider-mode compatibility.

## Change

TaskRunner now scopes AgentProfile and AgentConfig before constructing each worker StartRequest.

Provider mode:
- preserves the complete AgentProfile;
- preserves the bounded AgentConfig;
- provider compatibility behavior is unchanged.

Web mode:
- retains only AgentProfile.BaseInstructions;
- strips Model and ReasoningEffort;
- retains bounded AgentConfig because the compatibility agent loop still enforces turn/tool/token/time/context/request limits from it.

External Harness mode:
- receives an empty AgentProfile;
- receives an empty AgentConfig;
- continues to receive only the harness-specific executable/arguments plus governed execution truth.

No StartRequest field or JSON key was removed. Existing worker wire compatibility remains intact.

## Why Harness config can be removed

The BrainHarness branch returns through runExternalHarnessChild before MAR constructs:

- contextengine;
- model Gateway;
- agent Loop.

Therefore AgentProfile and AgentConfig are dead configuration for that path and represent unnecessary MAR cognition coupling.

## Evidence

Focused unit coverage:

- TestWorkerProviderConfigStripsProviderOnlyFieldsOutsideProviderMode: PASS;
- TestWorkerCognitionConfigIsModeScoped: PASS;
- TestTaskRunnerConfigAllowsWebBrainWithoutProviderCredentials: PASS;
- TestTaskRunnerConfigAllowsExternalHarnessWithoutAgentProfile: PASS.

Mode assertions:

- Provider keeps model, reasoning, base instructions and agent budget config;
- Web keeps base instructions and budget config but receives no model/reasoning selection;
- Harness receives zero AgentProfile and zero AgentConfig.

Real E2E:

- TestRuntimeE2EExternalHarnessWorkerVerifyIntegrate: PASS in ~23.12s;
- TestRuntimeE2EWebBrainMCPWorkerVerifyIntegrate: PASS in ~33.09s;
- combined orchestrator package run ~56.34s.

Both paths still reach candidate -> verification -> integration.

Full repository:

- go test ./...: PASS;
- wall time ~117.88s;
- cmd/mar ~9.17s;
- internal/orchestrator ~111.55s;
- remaining packages cached or PASS.

## Architectural consequence

The external harness execution path no longer carries MAR-owned model, reasoning, prompt-profile or agent-budget configuration.

The Web compatibility path remains intentionally narrower but still carries the two inputs it actually consumes:

- BaseInstructions for the MAR compatibility system prompt;
- AgentConfig for fail-closed execution budgets.

Those Web inputs are not classified as dead configuration in this slice.

This is a bounded responsibility deletion on the frozen architecture, not a new abstraction or lifecycle.
