# 2026-09-17 — Audit reconciliation is current research truth

See `docs/decisions/MAR_AUDIT_RECONCILIATION_2026_09_17.md`. The Owner-supplied independent audit was reconciled against live `.7`: plaintext local credential handling and broad installation ACLs are confirmed security gaps; the reported 73-file dirty tree is not current (canonical `.7` is clean) although line-ending configuration remains environment-sensitive; inability of the external audit environment to run Windows Go does not invalidate the existing `.7` test/vet/build + post-activation CUJ evidence.
# MAR Research — Current

Updated: 2026-09-16

## Current research verdict

MAR `v1.2.0-requalification.7` is engineering release-qualified and runtime-activated at source `73d75558946ac2570d3d1319d1461b2126ce9049`. The next useful research input is **real project telemetry**, not another speculative optimization round.

## Findings already converted into product changes

1. Historical BLOCKED volume was misleading: the audited DB had 67 historical tasks, 41 BLOCKED, but all 41 BLOCKED tasks were created before `.4` activation. Old requalification/probe tasks must not be treated as the current release failure rate.
2. Convergence limits were too coarse for AI coding. In particular, revision-only no-progress classification could block workers that had produced meaningful analysis/test/evidence without a new commit. Slice A now uses semantic progress and a repeated no-progress threshold.
3. Owner Console attention needed revision context. Historical/superseded BLOCKED/FAILED work is now separated from current actionable work; durable records are preserved rather than deleted.
4. One-shot startup physical recovery was insufficient after transient named-Job recovery misses. Periodic recovery was added with a bounded interval and proof-only semantics.
5. Periodic recovery itself must not touch attempts owned by the current live daemon. `.7` closes this regression by excluding live `active` tasks from periodic reconciliation while retaining startup reconciliation.

## Deferred hypotheses

- Adaptive task budgets may improve long coding tasks, but no budget widening should ship until `.7` real-project measurements show repeated useful-progress tasks still hit time/token/decision ceilings.
- Automatic stale `.git/index.lock` recovery is unsafe without durable proof that the lock belongs to a dead MAR-owned Git operation. If the edge repeats, research a MAR-owned integration-operation marker rather than deleting locks by age/PID guesswork.

## Measurement to collect during normal use

For each new `.7` task, retain: final state, blocker category, active execution time, model decisions, worker tool calls, model tokens, attempt count, semantic progress streak, verification result, integration result, and whether Owner input was genuinely required. Compare only `.7+` tasks when estimating current block rate.

Stop research when the product path is working; do not turn MAR into an endless optimization project.