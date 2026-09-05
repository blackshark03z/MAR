# MAR V1 — Product Brief

## 1. Problem

ChatWeb models can reason well about software, but naïve MCP integration creates a slow and fragile loop:

`ChatWeb → MCP call → repository → MCP result → ChatWeb reasoning → repeat`

For non-trivial coding tasks this causes:

- excessive network/tool round trips;
- dependence on an open browser conversation;
- poor long-task durability;
- weak recovery after disconnect;
- unsafe same-worktree concurrency;
- uncontrolled CPU/RAM/process growth;
- duplicated work across tabs;
- semantic conflicts between parallel tasks;
- repository/worktree bloat;
- too much human forwarding between Tech Lead and coding worker.

Native coding agents avoid much of this by keeping the inner model↔tool loop close to the repository.

---

## 2. Product thesis

MAR moves the autonomous agent loop behind MCP.

ChatWeb should submit a high-level bounded Goal, not remote-control individual files.

```text
ChatWeb
   ↓
MCP Control Plane
   ↓
MAR Durable Runtime
   ↓
Autonomous Agent Loop
   ↕
Filesystem / Shell / Git / Tests
   ↓
Verified Result
```

The external MCP conversation may disconnect without invalidating the task.

---

## 3. Primary user

One developer operating one workstation.

The same owner may use:

- multiple ChatWeb tabs;
- multiple MCP-capable clients;
- multiple local projects;
- multiple concurrent autonomous tasks.

All clients act on behalf of the same trusted OS user.

---

## 4. Primary job-to-be-done

> “I give MAR a bounded software goal with acceptance criteria. MAR executes autonomously against the correct repository, verifies the result, survives interruptions, and returns evidence without requiring me to micromanage the coding loop.”

---

## 5. Core user experience

1. User discusses intent with ChatWeb.
2. ChatWeb produces a Goal Contract.
3. ChatWeb submits the Goal through MCP.
4. MAR performs preflight and resource admission.
5. MAR creates or assigns an isolated workspace.
6. Local agent worker executes the model↔tool loop.
7. MAR runs verification.
8. MAR produces a result with revision-bound evidence.
9. For parallel work, MAR serializes integration.
10. ChatWeb reviews the result and escalates only when human judgment is needed.

---

## 6. Product principles

### P1 — Goal-level control
ChatWeb operates at goal/decision level, not file-operation level.

### P2 — Durable autonomy
Task state survives browser disconnect and runtime restart.

### P3 — Isolation before parallelism
Parallel mutable tasks never share the same mutable workspace.

### P4 — Verification over self-assertion
“Done” is based on evidence, not only model output.

### P5 — Bounded machine impact
MAR must preserve workstation usability and prevent runaway process, RAM, CPU and disk growth.

### P6 — Simple local product
V1 optimizes for one owner and one machine. Multi-user SaaS concerns are excluded.

### P7 — Architecture stops when acceptance is met
No open-ended feature accumulation.

---

## 7. Success definition

MAR V1 succeeds when it can complete representative coding tasks with:

- minimal external MCP turns;
- no lost updates;
- no orphan processes;
- correct recovery;
- bounded resource usage;
- clean worktree lifecycle;
- revision-bound verification;
- low human intervention;
- acceptance quality competitive with a native autonomous coding workflow.
