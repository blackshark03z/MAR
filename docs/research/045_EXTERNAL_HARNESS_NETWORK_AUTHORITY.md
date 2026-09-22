# External Harness Network Authority Mapping

Date: 2026-09-23
Status: VERIFIED
Baseline: 298dcaa8f4dc72911734e187a8bebac3b9250c93

## Goal

Allow a replaceable external coding harness to use outbound Internet only when the immutable task Goal Contract explicitly grants `Authority.NetworkAllowed`, without changing MAR's existing default-deny sandbox or built-in agent network model.

## Boundary

The Windows LPAC command spec now has an explicit `NetworkAllowed` boolean.

Default behavior remains unchanged:

- `NetworkAllowed=false` grants no network capability;
- ordinary MAR sandbox executors remain network-denied;
- MAR built-in agent execution still uses brokered `network_fetch`, `git_remote_ref`, and `git_push_head` authority filtering rather than raw process networking.

Only the external-harness constructors used by `BrainHarness` map the task Goal Contract's `NetworkAllowed` flag into LPAC process capability.

When true, MAR adds the Windows `internetClient` capability only.

MAR does not add:

- `internetClientServer`;
- `privateNetworkClientServer`;
- ambient credential variables;
- unsanitized parent environment;
- additional filesystem grants.

## OS-level evidence

Focused processctl tests:

- `TestAppContainerSandboxEnforcesWorkerAuthorityBoundary`: PASS.
  - default LPAC still cannot connect even to the local TCP listener used by the authority probe;
  - outside-workspace read/write remains denied.

- `TestSandboxExplicitInternetClientAllowsPublicOutbound`: PASS.
  - host public TCP reachability is checked first;
  - LPAC with explicit `NetworkAllowed=true` successfully connects outbound to `example.com:443`.

Both passed in one focused run; package time ~2.31s.

## Worker mapping evidence

`TestRunChildExternalHarnessMapsTaskNetworkAuthorityToLPAC`: PASS for both branches.

- `network_allowed_false`: external harness cannot reach `example.com:443`;
- `network_allowed_true`: the same external harness can reach `example.com:443`;
- the helper reads the task-bound Goal Contract envelope to determine the expected authority;
- built-in MAR cognition remains bypassed.

Existing `TestRunChildExternalHarnessBypassesMAROwnedCognition`: PASS in the same focused run.

## Full regression

`go test ./...`: PASS.

Observed package timings from the uncached/full run include:

- `cmd/mar`: ~33.9s;
- `internal/aci`: ~152.8s;
- `internal/orchestrator`: ~198.1s;
- `internal/processctl`: ~81.4s;
- `internal/verification`: ~117.3s;
- `internal/worker`: ~67.4s;
- `internal/workspace`: ~93.4s.

Command wall time: ~208.3s.

## Security interpretation

This is intentionally narrower than "network unrestricted."

`internetClient` supplies outbound Internet client capability to an LPAC process when the accepted Goal Contract grants network authority. MAR still withholds private-network and server capabilities, and the child environment remains sanitized.

For the built-in MAR agent path, `NetworkAllowed` continues to expose MAR-authored bounded network tools; it does not enable raw network in generic `run_command`.

For the external harness path, raw outbound Internet is necessary for cloud-backed coding harnesses that themselves speak to their provider. That wider process capability is therefore isolated to `BrainHarness` and remains explicit/default-deny.

## Remaining qualification gap

Network transport is no longer the blocker for a cloud-backed external harness. Authentication/credential access is still intentionally unresolved: the harness environment remains sanitized and MAR has not granted ambient API keys, user-profile credential stores, or arbitrary host read access.

A real Codex/Claude/OMP adapter must solve authentication with the smallest explicit credential boundary before it can be called production-qualified.
