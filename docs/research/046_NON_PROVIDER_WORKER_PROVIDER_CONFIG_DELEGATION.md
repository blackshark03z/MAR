# Non-Provider Worker Provider-Config Delegation

Date: 2026-09-23
Status: VERIFIED
Baseline: d5ce87b8ec317a7d91660b4a7a0e6443ea6f51da
Architecture authority: docs/architecture/MAR_ARCHITECTURE_STABILITY_POLICY.md

## Goal

Stop Web and external-harness workers from carrying provider-only runtime configuration that they do not use, while preserving Provider-mode compatibility and the existing worker start wire shape.

## Previous behavior

TaskRunner copied the complete runtime ProviderConfig into every worker StartRequest.

That meant Web/Harness worker start frames could contain:

- provider BaseURL;
- API-key environment-variable name;
- provider request timeout;

even though neither mode consumes those fields.

This was unnecessary compatibility-cognition coupling.

## Change

TaskRunner now normalizes ProviderConfig at the worker boundary:

- BrainProvider: ProviderConfig passes through unchanged;
- BrainWeb: only BrainMode=web crosses into StartRequest;
- BrainHarness: only BrainMode=harness crosses into StartRequest.

No StartRequest field or JSON key was deleted. Wire compatibility is unchanged.

No model/session/lifecycle abstraction was added.

## Evidence

Focused unit coverage:

- TestWorkerProviderConfigStripsProviderOnlyFieldsOutsideProviderMode: PASS;
- TestTaskRunnerConfigAllowsWebBrainWithoutProviderCredentials: PASS;
- TestTaskRunnerConfigAllowsExternalHarnessWithoutAgentProfile: PASS.

The first test proves:

- Provider mode preserves BaseURL, APIKeyEnv and RequestTimeout unchanged;
- Web/Harness modes zero BaseURL, APIKeyEnv and RequestTimeout while preserving BrainMode.

Real E2E:

- TestRuntimeE2EExternalHarnessWorkerVerifyIntegrate: PASS in ~33.79s;
- TestRuntimeE2EWebBrainMCPWorkerVerifyIntegrate: PASS in ~32.21s;
- package pair ~66.13s.

Both still reach candidate -> verification -> integration.

Full repository:

- go test ./...: PASS;
- wall time ~114.95s;
- cmd/mar ~9.70s;
- internal/orchestrator ~107.87s;
- remaining packages cached or PASS.

## Architectural consequence

Provider URL/API-key-name/request-timeout configuration is now scoped to the Provider compatibility path at the worker execution boundary.

Web and Harness workers no longer receive provider-specific configuration merely because the runtime supports Provider compatibility.

AgentProfile base instructions and AgentConfig budgets remain in the Web compatibility loop because they still govern behavior and fail-closed execution budgets; they are not treated as dead config in this slice.

This is a bounded responsibility reduction on the frozen architecture.
