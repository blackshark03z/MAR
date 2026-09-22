# External Harness Config Decoupling

Date: 2026-09-23
Status: VERIFIED
Baseline: 0df75fd0781e1c38ace3d4a43e7225e38802635c
Architecture authority: docs/architecture/MAR_ARCHITECTURE_STABILITY_POLICY.md

## Goal

Remove the external-harness execution path's remaining dependency on the compatibility cognition projection object without changing the legacy worker wire contract.

## Previous coupling

StartRequest.HarnessConfig projected a single compatibility object containing:

- ProviderConfig;
- AgentProfile;
- AgentConfig;
- external HarnessExecutable;
- external HarnessArguments.

RunChild always created that object first. BrainHarness then received the mixed cognition/execution object merely to obtain the executable and arguments.

This was unnecessary after previous slices had already proven that BrainHarness uses zero MAR model/provider/agent cognition.

## Change

HarnessConfig now contains compatibility cognition only:

- ProviderConfig;
- AgentProfile;
- AgentConfig.

BrainHarness:

- branches directly from StartRequest.Provider.Mode();
- validates HarnessExecutable directly on StartRequest;
- calls runExternalHarnessChild without HarnessConfig;
- launches StartRequest.HarnessExecutable with StartRequest.HarnessArguments directly.

The flat StartRequest JSON keys remain unchanged. No new wire envelope, type hierarchy, lifecycle, DB state or durable schema was added.

## Evidence

Focused worker coverage:

- TestHarnessConfigContainsOnlyCompatibilityCognition: PASS;
- TestExternalHarnessInputBindsDurableTaskIntent: PASS;
- TestRunChildExternalHarnessBypassesMAROwnedCognition: PASS;
- TestRunChildExternalHarnessMapsTaskNetworkAuthorityToLPAC: PASS for both network_allowed=false and true.

Focused worker package time: ~1.16s.

Real E2E:

- TestRuntimeE2EExternalHarnessWorkerVerifyIntegrate: PASS in ~39.73s;
- TestRuntimeE2EWebBrainMCPWorkerVerifyIntegrate: PASS in ~40.20s;
- combined orchestrator package ~80.19s.

Both still complete candidate -> verification -> integration.

Full repository:

- go test ./...: PASS;
- wall time ~140.98s;
- cmd/mar ~12.42s;
- internal/orchestrator ~132.32s;
- internal/worker ~39.99s;
- remaining packages cached or PASS.

## Architectural consequence

The external harness execution seam no longer depends on MAR's compatibility cognition configuration object.

HarnessConfig now accurately represents only the legacy Provider/Web cognition surface, while external harness execution consumes the explicit StartRequest execution fields already present for backward-compatible transport.

This is a direct dependency deletion on the frozen architecture.
