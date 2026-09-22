# External Harness Kernel E2E

Date: 2026-09-22
Status: VERIFIED KERNEL SEAM / INTENT HANDOFF NOT YET VERIFIED
Baseline: 798e955ce7af3c5c241af4dfc71488629d528712

## Question

Can MAR's governed kernel accept a candidate produced by an executable outside MAR's built-in model/agent cognition loop, then preserve sandboxing, verification, integration and authority finalization?

## Test

Added `TestRuntimeE2EExternalHarnessWorkerVerifyIntegrate`.

The test uses the existing `BrainHarness` path with a test-local executable as a harness-neutral stand-in. The harness executable:

- runs as a separate process through MAR's enforced Windows LPAC sandbox;
- mutates only the task workspace;
- creates the requested candidate marker;
- does not use MAR model/provider/Web cognition;
- exits successfully and returns a bounded summary.

MAR then performs the ordinary governed post-candidate path:

```text
submit
-> workspace / attempt
-> worker child
-> external harness executable under enforced sandbox
-> completed_candidate
-> verification against sealed candidate
-> physical termination proof
-> integration
-> COMPLETE
```

## Verified evidence

Focused real E2E:

- `TestRuntimeE2EExternalHarnessWorkerVerifyIntegrate`: PASS in ~25.67s.
- authoritative project receives the marker only after integration;
- authoritative Git HEAD advances from the baseline;
- task result is `VERIFIED`;
- integration status is `INTEGRATED`;
- attempt authority finishes `PHYSICALLY_TERMINATED`;
- there is no pending WebTurn;
- `AgentTurns = 0`;
- `AgentToolCalls = 0`;
- `ModelTotalTokens = 0`.

Full repository:

- `go test ./...`: PASS;
- wall time ~121.4s;
- `internal/orchestrator` ~114.7s because the new sandboxed E2E runs in the package.

## What this proves

The governed execution kernel does not require MAR's built-in model/provider/Web cognition loop in order to:

- isolate an executing candidate producer;
- receive a completed candidate;
- bind it to the current attempt;
- verify the sealed candidate;
- require physical process termination evidence;
- integrate only a verified candidate.

This is strong deletion-test evidence for future cognition delegation.

## What this does NOT prove

The current external-harness configuration does not yet provide a verified **per-task intent handoff**.

Today `HarnessExecutable` and `HarnessArguments` are runtime-level configuration. The E2E harness is deliberately fixed-purpose, so it proves the kernel/execution seam but not that an arbitrary external coding harness receives the current task's Goal Contract, authority, task/attempt identity, acceptance obligations and relevant context through a stable harness-neutral contract.

Therefore:

- do not claim a real Codex/Claude/OMP harness is production-qualified yet;
- do not delete Web/provider cognition solely from this E2E;
- the next harness work should define the smallest task-bound handoff contract that can support multiple harness implementations without making MAR own model/provider/session behavior.

## Related tooling observation

The `target_busy:workspace:...raw-command` queueing observed during qualification is a ChatCode dispatcher/raw-command lease behavior, not MAR production code. Existing MAR research already records this distinction. Do not patch MAR around that tooling lease.
