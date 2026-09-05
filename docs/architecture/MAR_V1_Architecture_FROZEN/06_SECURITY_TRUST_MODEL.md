# MAR V1 — Security & Trust Model

## 1. Trust boundary

MAR V1 assumes one trusted OS user on one host.

All registered clients act on behalf of the same machine owner.

This is not a hostile multi-user isolation boundary.

---

## 2. Security objective

Even under a trusted owner, model-generated actions must remain bounded.

Threats include:

- prompt injection from repository content;
- accidental destructive commands;
- secret exfiltration;
- unrelated filesystem mutation;
- remote Git side effects;
- runaway process creation;
- network abuse.

---

## 3. Separation of identity and authority

Connection identity answers:

> Who/what client is calling?

Task authority answers:

> What may this task do?

Connection success must not imply unrestricted machine authority.

---

## 4. Outer MCP policy

Normal ChatWeb interface exposes high-level operations only:

- submit;
- status;
- steer;
- input;
- cancel;
- result;
- inspect.

Do not expose unrestricted filesystem/shell mutation as the default public surface.

---

## 5. Task capability scope

Each task receives explicit scope, e.g.:

- project root/worktree only;
- local file mutation allowed;
- command execution sandboxed;
- local Git write allowed;
- Git push denied;
- deploy denied;
- network denied/default restricted;
- secret access none unless brokered.

---

## 6. Worker sandbox

A task worktree is not a security sandbox.

The V1 sandbox/authority model must enforce, outside model obedience:

- read scope;
- write scope;
- process-tree ownership;
- Git authority;
- network profile;
- secret access policy.

Child processes inherit the task's process/resource authority.

Task-local Git operations must not imply authority to mutate project integration refs.



Worker-generated commands run in task-controlled execution environment.

The worker should not gain authority merely by writing an executable file.

Execution policy must prevent trivial write→execute privilege expansion outside the task scope.

---

## 7. Secrets

Do not expose long-lived secrets as broadly readable environment/files when avoidable.

Preferred model:

```text
Worker
  ↓ asks for named capability
Secret/Action Broker
  ↓ performs bounded action or injects short-lived credential
External service
```

V1 may initially support simple local credentials, but the architecture should avoid making unrestricted secret browsing part of worker tooling.

---

## 8. Git authority

Separate permissions conceptually:

- read repository;
- mutate task worktree;
- create local task commit;
- integrate locally;
- push remote;
- deploy.

V1 defaults:
- local mutation: allowed by project policy;
- task-local commits: allowed through bounded tooling;
- authoritative integration ref mutation by workers: denied;
- push: denied unless explicitly enabled;
- deploy: denied.

---

## 9. Prompt injection stance

Repository text is untrusted input to the model.

Security must not rely on “the model will ignore malicious instructions”.

Controls belong in:

- capability boundaries;
- sandbox;
- command policy;
- filesystem scope;
- network policy;
- secret separation.

---

## 10. Audit/evidence

Even single-user V1 should preserve task-local provenance:

- Goal Contract;
- model profile;
- commands;
- changed revisions;
- verification;
- integration result;
- approvals/steering events.

This is for debugging and accountability, not enterprise compliance.

---

## 11. Security non-goals

V1 does not claim:

- tenant isolation;
- malicious local-user isolation;
- enterprise RBAC;
- full zero-trust workstation security;
- guaranteed resistance to all supply-chain attacks.


---

## 12. Recovery-time workspace authority

Epoch/state fencing is not a filesystem security primitive.

When a mutable task is recovered into the same workspace, MAR must treat any still-running stale process tree as mutation-capable until termination or OS-enforced write revocation is proven.

V1 default policy is **kill-and-confirm before replacement**.

A stale process being logically non-authoritative is insufficient to permit a new writer on the same mutable workspace.
