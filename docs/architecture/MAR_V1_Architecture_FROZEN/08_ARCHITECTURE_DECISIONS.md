# MAR V1 — Architecture Decision Record

This document records decisions that should not be casually reopened.

---

## ADR-001 — Single owner / single host

**Decision:** MAR V1 targets one trusted OS user on one machine.

**Why:** Removes SaaS/RBAC/tenant complexity and keeps focus on autonomous coding quality.

**Consequences:** Multi-user hostile isolation is a non-goal.

---

## ADR-002 — MCP is control plane, not inner agent loop

**Decision:** ChatWeb submits goals and receives task state/results. Worker reasoning/tool loops execute behind MCP.

**Why:** Avoid network/tool round-trip on every file/test operation and allow browser disconnect.

---

## ADR-003 — Durable task state

**Decision:** Task truth is persisted independently of MCP transport and worker processes.

**Why:** Enables recovery, resume, disconnect tolerance and trustworthy state.

---

## ADR-004 — SQLite first

**Decision:** Use SQLite/WAL for V1 orchestration metadata.

**Why:** Single-machine product; simpler than distributed databases while sufficient for authoritative local state.

**Do not add Redis/Postgres unless evidence requires it.**

---

## ADR-005 — One mutable task, one isolated worktree

**Decision:** Concurrent mutable tasks never share a working tree.

**Why:** Prevents physical write races and preserves task-local Git history.

---

## ADR-006 — Parallel execution, serialized integration

**Decision:** Project integration has one authoritative writer.

**Why:** Prevents race conditions and creates one place to handle base drift and evidence invalidation.

---

## ADR-007 — Resource-weighted scheduling

**Decision:** Concurrency is governed by workload + live CPU/RAM/I/O state, not fixed task count alone.

**Why:** Eight waiting model calls are not equivalent to eight builds.

---

## ADR-008 — Process tree belongs to task

**Decision:** Child processes are owned and terminated as part of task lifecycle.

**Why:** Prevents orphan processes and hidden resource leaks.

---

## ADR-009 — Layered context retrieval

**Decision:** Start with Git/metadata + lexical + symbol/dependency retrieval. Semantic embeddings are optional.

**Why:** Keep RAM/CPU/storage low and avoid premature vector infrastructure.

---

## ADR-010 — Revision-bound verification

**Decision:** Evidence is valid only for the revision verified.

**Why:** Base drift can invalidate earlier tests and assumptions.

---

## ADR-011 — Goal Contract is immutable

**Decision:** Worker may not silently redefine acceptance or boundaries.

**Why:** Prevents autonomous execution from redefining success.

---

## ADR-012 — Small MCP public surface

**Decision:** Default ChatWeb-facing API is task-oriented rather than raw-machine-oriented.

**Why:** Better security, compatibility and orchestration semantics.

---

## ADR-013 — Build OS remains separate

**Decision:** MAR executes; Build OS defines/guards Goal, Boundary, Acceptance and reconciliation policy.

**Why:** Prevents MAR becoming a second process-heavy methodology/control framework.

---

## ADR-014 — V1 stops at acceptance

**Decision:** No architecture expansion unless benchmark/review proves a core assumption invalid.

**Why:** Prevent infinite improvement and preserve delivery speed.


---

## ADR-015 — Execution attempt fencing

**Decision:** Every mutable task execution has a durable `attempt_id` and monotonically increasing `run_epoch`. Only the current epoch may perform MAR-authorized mutation or lifecycle transitions.

**Why:** Prevent duplicate/stale workers after crash, hang or recovery.

**Implementation note:** SQLite CAS/transactions are sufficient for single-host V1; no distributed lease service.

---

## ADR-016 — Side-effect reconciliation

**Decision:** Effects with ambiguous crash outcomes use durable operation identity and observation/reconciliation before retry.

**Why:** Exactly-once cannot be generically guaranteed across filesystem/Git/process/external effects.

**Rule:** Do not blindly repeat an effect whose prior outcome is unknown.

---

## ADR-017 — Worker authority is enforced outside the model

**Decision:** Worktree isolation is not treated as a security sandbox. Filesystem, process, Git, network and secret authority are enforced by runtime/sandbox policy.

**Why:** Repository prompt injection must not gain full OS-user authority merely because the model obeys it.

---

## ADR-018 — Crash-safe integration transaction

**Decision:** Project integration uses a durable integration attempt with `expected_head`, candidate revision, current evidence identity and expected-head CAS advancement.

**Why:** Single-writer serialization alone does not make integration crash-atomic.

---

## ADR-019 — Hard resource envelope includes disk

**Decision:** CPU, RAM, disk and process growth are first-class governed resources.

**Why:** Cleanup after completion cannot prevent active tasks from exhausting the host.

---

## ADR-020 — Project Coordinator is reconstructable

**Decision:** The Project Coordinator is an evictable in-memory serialization/cache module, not a second authority.

**Why:** Prevent split-brain truth between orchestrator and project-local runtime state.

---

## ADR-021 — Semantic uncertainty blocks integration

**Decision:** Compatibility outcome may be `COMPATIBLE`, `INCOMPATIBLE` or `UNCERTAIN`. `UNCERTAIN` requires review/block.

**Why:** MAR does not claim perfect semantic-conflict detection.


---

## ADR-022 — Logical fencing does not replace physical writer termination

**Decision:** `run_epoch` fences logical MAR authority only. It does not by itself revoke physical filesystem mutation capability from stale OS processes.

For mutable recovery using the same workspace, MAR must confirm termination of the previous attempt's entire mutation-capable process tree before admitting replacement mutable execution, unless OS-level workspace write authority has been conclusively revoked.

If neither can be proven, recovery blocks.

**Why:** Prevent stale child processes or already-dispatched commands from corrupting the workspace used by a replacement worker.

**V1 simplification:** Prefer kill-and-confirm. Do not build live write-revocation machinery unless implementation evidence later requires it.
