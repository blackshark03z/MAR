# MAR V1 — Architecture Freeze Record

**Freeze status:** `ARCHITECTURE_FROZEN`  
**Frozen baseline:** MAR V1 Architecture Freeze Candidate R3  
**Final independent review verdict:** `ARCHITECTURE_READY_FOR_FREEZE`  
**Remaining blockers:** `NONE`  
**Contradictions:** `NONE`  
**Freeze decision:** `YES`

---

## 1. Frozen product boundary

MAR V1 is:

> A single-owner, single-machine, MCP-native autonomous coding runtime that supports multiple clients, projects and concurrent tasks while keeping the inner model↔tool loop local to the runtime.

MAR V1 is not a multi-user SaaS, distributed workflow platform, generic remote shell, cloud worker fabric or agent-swarm framework.

---

## 2. Frozen architectural topology

```text
ChatWeb / MCP Client
        ↓
Stable MCP Control Plane
        ↓
Durable Orchestrator
        ↓
Project Coordinator + Resource Governor
        ↓
Autonomous Agent Worker
        ↓
Isolated Mutable Worktree
        ↓
Verification
        ↓
Serialized Crash-Safe Integration
        ↓
Repository
```

---

## 3. Frozen invariants

The architecture is considered correct only while these remain true:

1. MCP is the control plane, not the inner coding loop.
2. One mutable task owns one isolated mutable workspace.
3. Parallel execution is allowed; authoritative project integration is serialized.
4. Task truth is durable and independent of ChatWeb transport/session.
5. Verification evidence is bound to the exact verified revision and relevant verification environment/profile identity.
6. Resource admission is based on workload plus CPU/RAM/disk/process pressure.
7. Goal Contract is immutable; material change creates a superseding task.
8. Public MCP surface remains bounded and task-oriented.
9. Every spawned process tree belongs to one task lifecycle.
10. Workspace retention/cleanup is deterministic and bounded.
11. `run_epoch` provides logical fencing only.
12. Replacement mutable execution on the same workspace cannot start until every prior mutation-capable process tree is confirmed terminated, or OS-level write authority is conclusively revoked; otherwise recovery blocks.
13. Effects with uncertain crash outcomes must be observed/reconciled before retry.
14. Integration uses a durable attempt with `expected_head` and compare-and-advance/CAS semantics.
15. MAR applies backpressure before host CPU/RAM/disk/process safety reserves are exhausted.
16. SQLite-backed Orchestrator state is the only durable coordination truth.
17. Semantic compatibility may be `COMPATIBLE`, `INCOMPATIBLE`, or `UNCERTAIN`; `UNCERTAIN` blocks/requires review.

---

## 4. Architecture reopen rule

Architecture is **closed**.

It may be reopened only if implementation or benchmark evidence proves at least one frozen invariant false or insufficient in a way that cannot be corrected locally.

Valid reopen evidence includes:

- lost work;
- duplicate physical mutation;
- stale worker mutation;
- unrecoverable crash ambiguity;
- false completion;
- unsafe authority escape;
- incorrect parallel integration;
- unbounded host resource consumption;
- structural inability to sustain the required autonomous coding loop.

The following do **not** reopen architecture by themselves:

- search latency;
- prompt quality;
- model quality;
- token cost;
- UI quality;
- test-selection tuning;
- context-ranking quality;
- worktree startup time;
- ordinary implementation defects;
- provider choice;
- parser/index optimization.

These belong to implementation/optimization.

---

## 5. Next phase

The next phase is:

`IMPLEMENTATION → T1–T17 BENCHMARK/ACCEPTANCE`

No additional broad architecture research is authorized before implementation.

The benchmark plan is the evidence gate for whether implementation satisfies the frozen architecture.

---

## 6. Review history summary

### R1
Verdict: `ARCHITECTURE_READY_WITH_REQUIRED_CORRECTIONS`

Required corrections:
- execution fencing/recovery transaction;
- enforceable autonomous-shell boundary;
- crash-safe integration transaction;
- hard resource/disk bounds.

### R2
Verdict: `ARCHITECTURE_NOT_READY`

Remaining blocker:
- logical `run_epoch` fencing did not itself revoke physical write capability of a stale OS process sharing the same mutable workspace.

### R3
Correction:
- logical fencing explicitly separated from physical mutation authority;
- same-workspace replacement requires kill-and-confirm or conclusive OS-level write revocation;
- otherwise recovery blocks;
- T14 strengthened to test physical mutation isolation.

Final verdict:
`ARCHITECTURE_READY_FOR_FREEZE`

---

## 7. Canonical documents

The frozen architecture is defined by:

- `01_PRODUCT_BRIEF.md`
- `02_PRODUCT_SPEC_V1.md`
- `03_ARCHITECTURE_V1.md`
- `04_EXECUTION_CONCURRENCY_MODEL.md`
- `05_RESOURCE_RELIABILITY_MODEL.md`
- `06_SECURITY_TRUST_MODEL.md`
- `07_ACCEPTANCE_BENCHMARK_PLAN.md`
- `08_ARCHITECTURE_DECISIONS.md`
- `09_RISKS_AND_OPEN_QUESTIONS.md`

This Freeze Record controls interpretation when review-history wording differs from the final frozen state.
