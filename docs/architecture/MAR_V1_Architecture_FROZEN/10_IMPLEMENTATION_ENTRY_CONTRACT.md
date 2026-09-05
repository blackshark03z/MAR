# MAR V1 — Implementation Entry Contract

**Architecture status:** `FROZEN`

Implementation starts from the frozen MAR V1 architecture.

## Allowed implementation work

- choose Rust or Go;
- select first model provider/backend;
- implement MCP control surface;
- implement SQLite/WAL state;
- implement worker supervisor and process containment;
- implement worktree lifecycle;
- implement context/search/indexing;
- implement agent loop;
- implement verification;
- implement resource governor;
- implement crash-safe integration;
- implement T1–T17 benchmark harness;
- optimize latency, memory, disk, context quality and token usage.

## Not allowed without architecture-reopen evidence

- redesign task lifecycle;
- remove frozen fencing guarantees;
- allow multiple mutation-capable attempts on one mutable workspace;
- bypass serialized authoritative integration;
- move essential task truth into transport/in-memory-only state;
- weaken resource boundedness;
- widen worker authority implicitly;
- add distributed infrastructure, multi-user tenancy or swarm architecture as part of V1.

## Implementation decision rule

When an implementation problem appears:

1. first treat it as an implementation defect;
2. try a local correction consistent with frozen invariants;
3. benchmark;
4. reopen architecture only if evidence shows a frozen invariant itself is insufficient.

## Acceptance gate

V1 implementation is not complete until the T1–T17 acceptance suite passes and owner real-use acceptance confirms the workflow is materially better than manual ChatWeb↔worker forwarding.
