# Slice 057 — Private Integration Candidate Anchor + Exact OID Revalidation

**Date:** 2026-09-23  
**Status:** VERIFIED / ACTIVATED
**Scope:** optional governed high-assurance integration only; no fast-path change and no lifecycle/schema redesign.

## Goal

Close three bounded governed-publication gaps already named in the MAR SoT:

1. private non-canonical staging;
2. fresh exact-OID verification immediately before publication;
3. durable OID handoff from verified detached workspace state into authoritative integration.

This slice also audits the existing canonical publication primitive rather than replacing it: MAR already advances the authoritative branch with Git compare-and-swap semantics via:

```
git update-ref <ExpectedRef> <CandidateRevision> <ExpectedHead>
```

That CAS-at-ref primitive is retained.

## Reproduced gap

MAR mutation workspaces are detached Git worktrees; V1 intentionally creates no task branch. A verified changed candidate therefore normally remains reachable through the detached worktree HEAD until integration completes.

Before this slice, integration:

- persisted the verified candidate OID in SQLite;
- rechecked fresh evidence before dispatch and again before canonical ref advancement;
- checked the authoritative ref/head and Owner-work cleanliness;
- directly CAS-advanced the canonical ref to `CandidateRevision`.

Two gaps remained:

1. the repeated freshness checks compared only result ID + evidence ID and did not re-bind result version + final revision to the exact integration candidate OID;
2. there was no private Git ref anchoring the exact verified candidate between PREPARED integration and canonical publication. Crash recovery therefore still depended on the detached worktree/object remaining available.

## Decision

Use Git itself for a private, non-canonical handoff ref. Do not add another MAR durable state or database column.

For every changed-candidate integration attempt, derive:

```
refs/mar/integrations/<sha256(attempt-id)[:16-bytes-as-hex]>
```

Before `PREPARED -> DISPATCHED`:

1. resolve `CandidateRevision^{commit}`;
2. require the resolved commit OID to equal the persisted candidate OID;
3. create the private integration ref with create-only `git update-ref` semantics using an all-zero old OID;
4. verify the private ref points to that same exact candidate.

Recovery of a DISPATCHED attempt whose canonical ref is still at `ExpectedHead` repeats the anchor operation idempotently before attempting canonical CAS.

## Fresh exact-result binding

Every integration freshness check now requires all of:

- `fresh=true`;
- verdict `VERIFIED`;
- exact result ID;
- exact result version;
- exact evidence ID;
- exact task-result revision;
- exact `FinalRevision == CandidateRevision`.

A result that remains nominally “fresh” but reports a different candidate revision is blocked before any private or canonical publication side effect.

## Anchor cleanup

The private ref is:

- deleted if publication dispatch loses to cancellation/state conflict after staging;
- deleted when an integration attempt is durably BLOCKED;
- retained while canonical ref advancement succeeded but Owner checkout synchronization is still incomplete, so recovery keeps an independent candidate anchor;
- deleted after canonical ref + checkout synchronization are complete, immediately before durable integration finalization;
- absent for no-op integrations where `CandidateRevision == ExpectedHead`, because the canonical ref already anchors that exact commit.

No public branch is created.

## Crash/recovery properties

- crash after private-ref creation but before DISPATCHED: PREPARED recovery observes the same anchor and continues idempotently;
- crash after DISPATCHED but before canonical CAS: recovery requires/repairs the same private anchor before CAS;
- crash after canonical CAS: the canonical ref itself anchors the candidate; cleanup/finalization remains idempotent;
- sync conflict after canonical CAS: private anchor remains until recovery finishes synchronization;
- cancellation wins before dispatch: private anchor is removed and canonical ref remains unchanged.

## Safety / non-goals

This slice does **not**:

- add a task state or integration state;
- add a SQLite migration;
- add a lock service;
- change ordinary Trusted Owner Fast Path Git behavior;
- create a public/task branch;
- weaken Owner-work protection;
- change the existing expected-head canonical CAS;
- solve the separate fail-closed launch-environment or durable-state deletion-boundary work.

## Acceptance

1. a nominally fresh result with mismatched `FinalRevision` is blocked before private/canonical Git publication;
2. changed candidates are resolved to the exact persisted commit OID before dispatch;
3. a deterministic private integration ref anchors the candidate before dispatch/CAS;
4. recovery before CAS recreates/verifies that anchor idempotently;
5. canonical publication remains exactly one expected-head `update-ref` CAS;
6. successful integration deletes the private anchor before durable finalization;
7. Owner-work sync conflict retains the private anchor while the attempt remains DISPATCHED/recoverable;
8. cancellation that wins after private staging but before dispatch deletes the anchor and leaves the canonical ref unchanged;
9. no-op integrations create no private anchor;
10. focused integration tests and full repository release qualification pass before release claims.

## Focused evidence

Current candidate:

- managed `gofmt` on `internal/integration/manager_windows.go` and tests — PASS;
- `go test -count=1 ./internal/integration` — PASS (6.727 s);
- `git diff --check` — PASS;
- `TestRecoverDispatchedBeforeCASAdvancesOnceAndFinalizes` proves private stage -> one canonical CAS -> private delete -> COMPLETE;
- `TestIntegrationStopsBeforeCheckoutMutationWhenOwnerWorkAppearsAfterCAS` proves the private anchor remains while post-CAS checkout synchronization is recoverable;
- `TestPreparedAttemptRejectsFreshResultCandidateOIDMismatch` proves exact candidate OID binding;
- `TestPreparedCandidateAnchorIsCleanedWhenCancellationWinsBeforeDispatch` proves cancellation wins without canonical publication or private-ref leak.

## Release evidence

Exact revision `2b95bfd61ea1f198f7eed2253a69c49d9d44543a` completed the full exact-HEAD release gate with `test=0`, `vet=0`, `build=0`, `diff_check=0`, identical HEAD before/after, and a clean working tree. Local HEAD and `origin/master` matched that same revision before activation.

The same exact revision was activated live. Runtime reported `HEALTHY / ALIGNED / trusted_for_release=true`; manifest identity was `ALIGNED`; the OpenAI Secure Tunnel reported connected / ready / healthy.

This closes the previously listed governed-publication blockers for private non-canonical staging, fresh exact-OID verification, OID handoff/import, and CAS-at-ref publication semantics. The canonical expected-head `git update-ref` CAS was already present and was retained rather than redesigned.
