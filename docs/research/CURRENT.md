# MAR research current — 2026-09-16

Research is converged for the V1.2 stable-use scope. The frozen V1 architecture remains unchanged.

## Current conclusion

The historical stable-use audit findings have been dispositioned:

- P1 runtime identity/manifest mismatch — CLOSED.
- P1 portable-Go/ACL qualification and host-vs-LPAC classification — CLOSED.
- P1 reconnect/profile-validation/LF asset determinism — CLOSED.
- P2 unclean-restart physical termination recovery — CLOSED by `221a9dc6589241fa80b807bf6d9cde6a94bf134a`.
- P2 terminal workspace retention/disk accumulation — CLOSED by `09745c865c164a776f19e102f8c17dc6a87a2a5b`.
- Superseded CURRENT/TASK accumulation — addressed by this handoff-only documentation branch; it is not a runtime blocker.

Exact engineering release: `v1.2.0-requalification.4` / `09745c865c164a776f19e102f8c17dc6a87a2a5b`.

Use `docs/handoff/CURRENT.md` for the concise current state and the release/slice documents for evidence. Do not treat historical BLOCKED tasks or superseded research statements as open product requirements without fresh evidence.

## Deferred/non-blocking research

Optional transports, broader Console redesign, context-engine expansion, deeper performance optimization, and real-use product acceptance remain outside the completed stable-use qualification. Open a new bounded research slice only when a real measured need appears.