# MAR independent audit reconciliation — 2026-09-17

## Decision status

This record reconciles the Owner-supplied independent Claude audit with the exact current MAR v1.2.0-requalification.7 source/runtime evidence.

Current gates:

- Engineering Release Qualified: PASS.
- Runtime Activated: PASS.
- Installation Security Qualified: NOT QUALIFIED — remediation required.
- Product Accepted: deferred pending real project use.

Exact engineering/runtime release remains v1.2.0-requalification.7 at source 73d75558946ac2570d3d1319d1461b2126ce9049. This audit does not revoke that qualification because the exact release already passed Windows go-standard test/vet/build and a post-activation self-hosting CUJ reached VERIFIED, INTEGRATED, PHYSICALLY_TERMINATED, and workspace REMOVED.

## Confirmed findings

1. A local plaintext credential file named Tunnel_api.txt exists in the repository directory. It is not Git-tracked and no path-level Git history was found, but it is hidden only by .git/info/exclude rather than repository policy. Treat the credential as exposed and rotate it. Do not preserve or reproduce its value in documentation.
2. Installation ACLs are broader than the single-owner threat model. The MAR data root currently grants Authenticated Users Modify, and mar.db additionally grants Users Read/Execute. Because the SQLite database contains remote connector capability tokens, installation ACL hardening is required.
3. Canonical Git is currently clean with zero semantic diff. The audit observation of 73 dirty files is not current truth. However core.autocrlf=true and repeated LF/CRLF warnings show that line-ending policy remains environment-sensitive and should be made explicit/reproducible.
4. No canonical Windows CI workflow is present. Release qualification exists, but it is not yet easy for an external reviewer to reproduce from one documented script/runner path.
5. Owner Console frontend quality debt is real: npm test is a placeholder failure, no lint script is defined, and TypeScript strict mode is disabled.
6. Dependency vulnerability scanning is not currently a closed release gate; govulncheck/npm audit evidence is unavailable in the independent audit environment.
7. The independent auditor's inability to execute Windows Go verification is an environment limitation, not evidence that the release tests failed.

## Bounded follow-up decision

Create one bounded slice named MAR Installation Security & Reproducibility Hardening. Do not reopen MAR architecture.

Required scope, in order:

1. Credential remediation: rotate/revoke the exposed tunnel/control-plane credential, remove local plaintext copies, add repository ignore policy for the local credential filename/pattern, and scan Git history plus MAR recovery/runtime artifacts without exposing secret values.
2. Installation ACL hardening: define the single-owner ACL contract for .mar, mar.db, runtime, recovery, artifacts, and worker/sandbox subtrees; preserve only narrowly required worker/AppContainer access; add an installation diagnostic/regression that proves the ACL contract.
3. Line-ending reproducibility: define explicit .gitattributes policy, normalize only with semantic-diff proof, and prevent CRLF/LF noise from becoming a false release blocker again.
4. Canonical verification entry point: add one Windows release verification script that drives frontend checks/build plus Go test/vet/build, diff checks, and security preflight. CI should call the same script rather than duplicate release logic.
5. CI/security automation: add a Windows CI path for the reproducible gates, secret scanning, and approved dependency vulnerability scanning. Host-only/AppContainer acceptance that requires a prepared Windows runner may remain on a self-hosted runner.

## Explicit non-goals for this slice

- No architecture rewrite.
- No weakening of Job Object containment, sandboxing, ACI, fencing, integration CAS, or verification binding.
- No wholesale frontend strict-mode migration in this slice.
- No automatic deletion of ambiguous Git lock files without MAR-owned operation provenance.
- No claim of Product Accepted from engineering/security tests alone.

## Follow-up backlog after the bounded slice

- Make Owner JSON decoding reject trailing non-whitespace data after the first JSON value.
- Add canonical domain/service payload bounds for goal, acceptance, boundaries, non-goals, and total serialized contract size.
- Add frontend test runner and regression coverage for task state, connection state, usage aggregation, and mutation error paths; then progressively replace any with typed API responses and tighten TypeScript strictness.
- Extend connection security regression around capability-token rotation, stale routes, origin enforcement, and diagnostic redaction.

## Release interpretation rule

From this date forward, distinguish at least these gates:

1. Engineering Release Qualified.
2. Runtime Activated.
3. Installation Security Qualified.
4. Product Accepted.

A release may pass engineering/runtime qualification while installation security remains blocked. These statuses must not be collapsed into one generic stable/release-ready label.