# Slice 058 — Fail-Closed Worker Launch Environment

**Date:** 2026-09-23  
**Status:** VERIFIED / ACTIVATED
**Scope:** governed worker-process launch only; Trusted Owner Fast Path is unchanged.

## Goal

Close the remaining kernel blocker for fail-closed launch-environment defaults.

Before this slice, `ProcessRunner.Run()` used:

```
Environment == nil -> os.Environ()
```

and the production orchestrator constructed the worker child environment by copying the complete daemon environment and only adjusting `PATH`.

That meant a governed worker child could inherit unrelated ambient host variables, including credentials or secrets that were not part of the accepted Goal Contract or worker configuration.

Command sandboxes and the external-harness child already used bounded/sanitized environments. The gap was the worker child process that hosts the current compatibility Provider/Web/Harness runtime.

## Decision

### 1. Nil launch environment fails closed

`NewProcessRunnerWithBackends` now rejects `ProcessConfig.Environment == nil`.

There is no implicit fallback to `os.Environ()` in `Run()`.

An explicitly provided empty slice remains distinguishable from an omitted environment and is allowed for callers that intentionally construct such an envelope.

### 2. Production worker environment is a whitelist

`NewRuntime` constructs the worker child environment from a bounded Windows runtime whitelist:

- `SystemRoot`;
- `WINDIR`;
- `SystemDrive`;
- `ComSpec`;
- `PATHEXT`;
- user/runtime path variables required by current Windows worker compatibility;
- program-data/runtime path variables;
- `PATH`, preserving configured MAR worker path entries ahead of the host path.

Unrelated ambient variables are not copied.

### 3. Provider credential is explicit

Only in Provider brain mode, the exact environment variable named by `ProviderConfig.APIKeyEnv` is projected into the worker environment.

Web and external-harness modes do not receive the provider credential merely because it exists in the parent daemon environment.

### 4. Explicit extras are opt-in

`RuntimeConfig.WorkerEnvironmentExtras` allows explicit `KEY=value` entries for self-hosting/test or tightly controlled host integration.

Extras:

- must be valid `KEY=value` entries;
- may not duplicate/override a protected whitelist key or provider-key projection;
- are never populated from ambient variables automatically.

The self-hosting acceptance tests now use this explicit seam for their helper markers instead of relying on ambient inheritance.

## Security invariant

The worker process launch environment is now:

```
safe Windows runtime keys
+ bounded PATH
+ exact provider credential only when Provider mode requires it
+ explicit caller-owned extras
```

and never:

```
os.Environ()
```

by default.

This reduces ambient credential exposure without changing worker authority, sandbox capabilities, network authority, Git authority, or task lifecycle.

## Compatibility

- no SQLite migration;
- no new task/attempt state;
- no MCP surface change;
- no change to Trusted Owner Fast Path;
- Provider mode keeps its configured API-key env;
- Web mode remains provider-credential free;
- external harness still receives its separately sanitized sandbox environment;
- self-hosting tests pass their helper markers explicitly.

## Acceptance

1. `ProcessRunner` rejects a nil process environment.
2. worker launch no longer falls back to `os.Environ()`.
3. an unrelated ambient secret is absent from the constructed worker environment.
4. safe Windows runtime keys and bounded PATH remain available.
5. Provider mode receives only the explicitly named provider API-key env.
6. malformed or duplicate explicit extras fail closed.
7. Provider self-hosting E2E still passes.
8. External Harness self-hosting E2E still passes.
9. WebBrain self-hosting E2E still passes.
10. full repository test/vet/build/diff-check passes before release claim.

## Focused evidence

Current candidate evidence:

- `go test -count=1 ./internal/worker` — PASS (21.584 s);
- focused orchestrator environment/process-limit tests — PASS (0.053 s);
- `git diff --check` — PASS;
- Provider E2E `TestRuntimeE2EMCPSubmitWorkerVerifyIntegrate` — PASS (44.403 s);
- External Harness E2E `TestRuntimeE2EExternalHarnessWorkerVerifyIntegrate` — PASS (22.941 s);
- WebBrain E2E `TestRuntimeE2EWebBrainMCPWorkerVerifyIntegrate` — PASS;
- detached result: `provider=0, harness=0, web=0`.

## Release evidence

The first full-suite attempt at `7f931a972f4ffcdda63c25508513e79ee04dcdbc` exposed two classes:

- CLI T7 did not reach the provider because the self-hosting test binary relied on ambient `MAR_T7_CLI_HELPER`; this was a real test-harness regression from the new fail-closed boundary, not a reason to re-open ambient inheritance. The CLI-only runtime seam was changed to pass that marker through explicit `WorkerEnvironmentExtras`.
- a verification-package failure cluster did not reproduce in isolation; the whole verification package later passed in the final full gate.

After the explicit CLI helper fix, exact revision `2dee9f1e2129cce0c4a49dde10c18a1801c144b7` completed:

- full `go test -p 1 -count=1 -timeout 300s ./...` — PASS;
- `go vet -p 1 ./...` — PASS;
- `go build -p 1 ./...` — PASS;
- `git diff --check` — PASS;
- identical HEAD before/after and clean working tree.

Local HEAD and `origin/master` matched. The same exact revision was activated live and reported `HEALTHY / ALIGNED / trusted_for_release=true`, with manifest identity ALIGNED and the OpenAI Secure Tunnel connected / ready / healthy.

The fail-closed worker launch environment blocker is therefore closed.
