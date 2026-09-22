# External Harness Task Intent Handoff

Date: 2026-09-22
Status: VERIFIED HARNESS-NEUTRAL HANDOFF
Baseline: 8ebf5ba02637f393f3f680f4833f6162a9e890a1

## Goal

Close the gap left by R-043: an external coding harness must receive the current immutable task intent without MAR taking ownership of the harness model, provider, session or reasoning topology.

## Contract

Each external-harness attempt now receives an ephemeral JSON input envelope.

Environment:

`MAR_HARNESS_INPUT=<absolute path to input.json>`

Envelope schema:

`mar-external-harness-input-v1`

Fields:

- schema;
- task_id;
- attempt_id;
- run_epoch;
- contract_hash;
- complete immutable Goal Contract.

The envelope is rebuilt from the worker StartRequest for every attempt. It is not stored as a second authority source.

## Safety properties

- The Goal Contract hash is recomputed before launching the harness.
- If the durable task contract hash is present and differs from the recomputed hash, execution fails closed.
- The envelope lives outside the candidate workspace, so it cannot become candidate/product content.
- Its temporary directory is added to the sandbox only as a read grant.
- The external harness receives only the envelope path through its sanitized environment.
- The envelope directory is removed after the harness process returns.
- The candidate workspace remains the process working directory.
- MAR continues to own sandboxing, attempt authority, verification, physical termination proof and integration.

The Goal Contract itself is not put directly into environment variables because the current contract has no byte bound and Windows process environments are bounded.

## Evidence

Unit binding:

- `TestExternalHarnessInputBindsDurableTaskIntent`: PASS.
- task_id / attempt_id / run_epoch / contract_hash are bound.
- the Goal Contract round-trips unchanged.
- a Goal Contract changed after its durable hash is rejected.

Real external-harness E2E:

- `TestRuntimeE2EExternalHarnessWorkerVerifyIntegrate`: PASS in ~26.19s.
- the harness helper reads `MAR_HARNESS_INPUT`;
- verifies schema and task/attempt/run-epoch identity;
- recomputes and verifies the Goal Contract hash;
- derives the candidate marker content from the task Goal rather than static harness arguments;
- attempts to overwrite the envelope and is denied by the sandbox;
- writes only the candidate workspace;
- MAR then verifies and integrates the candidate;
- MAR model/Web cognition remains unused.

Regression discovered and corrected:

An older worker-process test fixture used a literal placeholder `ContractHash: "hash"`. The new fail-closed handoff correctly rejected it. The fixture was corrected to use the real Goal Contract hash; production hash verification was not weakened.

Final regression:

- affected external-harness worker tests: PASS;
- focused harness E2E: PASS;
- final `go test ./...`: PASS;
- only `internal/worker` reran on the final pass (~22.8s); other packages were cached after the prior full run.

## Architectural consequence

MAR now has a harness-neutral task-intent handoff in addition to the already-proven governed kernel execution seam.

This closes the generic Goal Contract handoff gap. It does not yet prove a specific real coding harness such as Codex, Claude Code, ChatCode or OMP is qualified. A real harness still needs a thin adapter that reads this envelope and returns a candidate under MAR's sandbox/authority rules.

No model/provider/session abstraction was added to MAR.
