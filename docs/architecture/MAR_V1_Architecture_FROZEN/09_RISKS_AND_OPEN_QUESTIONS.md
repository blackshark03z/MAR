# MAR V1 — Risks & Open Questions

**R2 note:** The former P0 architectural gaps around execution fencing, side-effect recovery, worker authority, crash-safe integration, and hard resource bounds are now resolved at the design-contract level. Implementation must still prove them experimentally.

---

## 1. Freeze-gate validation questions

These are no longer requests for a new architecture. Limited re-review should verify that the R2 corrections are coherent.

### V1 — Execution fencing
Can any stale worker/attempt still mutate after a higher `run_epoch` becomes authoritative?

R3 freeze criterion: a higher mutable epoch cannot become active on the same workspace until every prior mutation-capable process tree is confirmed terminated or write authority is conclusively revoked.

### V2 — Side-effect recovery
Can MAR distinguish “not executed”, “executed but not observed”, and “observed” well enough to avoid blind duplicate effects?

### V3 — Worker authority
Is the selected Windows implementation able to enforce the frozen filesystem/process/Git/network/secret authority contract?

### V4 — Integration crash recovery
Can every crash point around `expected_head → candidate → verify → CAS advance → finalize` be deterministically reconciled?

### V5 — Resource boundedness
Can active tasks still exhaust disk/process resources before backpressure takes effect?

### V6 — Resume quality
Does Goal + semantic checkpoint + selected evidence restore enough model state without full transcript replay?

---

## 2. Major implementation risks

### R1 — Worktree/build amplification
Some toolchains create large build artifacts per worktree.

Mitigation:
- shared immutable dependency caches;
- isolated mutable outputs;
- lifecycle cleanup;
- per-project build strategy.

### R2 — Windows process containment edge cases
Some processes may escape naïve parent-child killing.

Mitigation:
- Job Object ownership;
- integration tests with Node/Python/browser/ffmpeg-style process trees.

### R3 — Test suites too expensive for every iteration
Full regression on each inner loop may destroy throughput.

Mitigation:
- targeted verification during iteration;
- required broader verification at completion/integration.

### R4 — Context engine overgrowth
Symbol graph/indexing can become its own product.

Mitigation:
- layered retrieval;
- lazy indexing;
- measurable benefit required for added complexity.

### R5 — Reviewer cost explosion
Independent review on every tiny task may waste tokens/time.

Mitigation:
- risk-tiered verification profiles.

---

## 3. Decisions intentionally deferred

These do not block architecture freeze.

### Language
Rust vs Go.

### First model provider
Provider-neutral abstraction; implementation can start with one.

### Exact MCP protocol mapping
Use official Tasks extension where host support is sufficient; otherwise keep MAR task handles explicit.

### Warm worktree pool
Optimization after correctness.

### Embeddings
Optional after lexical/symbol retrieval benchmark.

### Remote worker
Out of V1.

---

## 4. Explicitly rejected scope creep

Do not add to V1 without benchmark evidence:

- organizations/RBAC;
- cloud orchestration;
- Redis;
- Kafka;
- Kubernetes;
- generalized workflow DSL;
- dozens of AI roles;
- global knowledge graph;
- marketplace/plugin ecosystem;
- deployment automation;
- automatic remote push by default.

---

## 5. External reviewer challenge prompts

Ask reviewers to answer:

1. Which invariant is wrong or insufficient?
2. Which failure mode could still corrupt or lose work?
3. Where can two parallel tasks produce an incorrect integrated result without detection?
4. Which component is unnecessarily complex for one machine?
5. Which component is too weak to achieve native-agent autonomy?
6. What happens when model call, worker process, daemon, or client fails at every lifecycle stage?
7. Which resource can still grow without a hard bound?
8. Which security boundary depends incorrectly on model obedience?
9. Does any state exist only in memory when it should be durable?
10. What would force a redesign after implementation begins?
