# Slice 010 — Windows Worker Sandbox

**Architecture:** FROZEN
**Bootstrap milestone:** `MAR_BOOTSTRAP_STABLE -> SELF_HOSTING_READY`
**Status:** COMPLETE / VERIFIED

## Goal

Replace model-controlled `TRUSTED_HOST` command execution with an OS-enforced Windows boundary that is sufficient for the later autonomous self-hosting loop without reopening the frozen Worker Authority Boundary.

## Enforcement model

### LPAC process boundary

Model-controlled commands run in a Windows **least-privileged AppContainer (LPAC)**.

- `PROC_THREAD_ATTRIBUTE_ALL_APPLICATION_PACKAGES_POLICY` opts the worker out of broad `ALL APPLICATION PACKAGES` ACL grants.
- Each task receives a deterministic, task-unique capability SID.
- The task capability is granted temporary read/write/execute access to the task workspace.
- Explicit runtime/read paths receive temporary read/execute-only access.
- ACL grants are restored after the command; restore failure is returned as command failure.
- Separate tasks use different capability SIDs, so one task cannot consume another task's workspace grant.
- Per-profile/per-path mutex entries are reference-counted and deleted after use rather than accumulating for daemon lifetime.

### Windows runtime compatibility capability

Go's Windows file runtime initializes Winsock even for ordinary file I/O. The LPAC therefore receives the built-in `registryRead` capability so WSA startup can read required Windows runtime configuration.

This does **not** grant a network capability. Acceptance probes verify:

- outbound/local TCP access remains denied;
- an ambient HKCU secret value is not exposed;
- protected `HKLM\\SAM` / `HKLM\\SECURITY` scopes remain inaccessible.

### Process containment

The existing Windows Job Object physical-fencing boundary remains in force.

- process starts suspended;
- LPAC attributes are attached at creation;
- process is assigned to a `KILL_ON_JOB_CLOSE` Job Object before resume;
- descendants remain in the task Job Object;
- timeout/cancellation terminates the Job Object and confirms zero active processes;
- only explicit stdin/stdout handles are inherited through `PROC_THREAD_ATTRIBUTE_HANDLE_LIST`.

### Environment / secret boundary

Sandbox command environment is constructed from a whitelist instead of inheriting `os.Environ()`.

- `USERPROFILE`, `HOME`, `APPDATA`, `LOCALAPPDATA`, `TEMP`, `TMP`, and `PWD` point inside the workspace runtime area;
- Go build/module/temp caches are workspace-local;
- `GOPROXY=off`, `GOSUMDB=off`, `GOENV=off`, and `GOTOOLCHAIN=local` prevent implicit network/toolchain acquisition;
- ambient API keys and unrelated parent environment variables are not copied into the worker.

## Typed Git broker

Git for Windows/MSYS cannot reliably operate as a normal model-controlled LPAC command because its POSIX compatibility layer expects host path semantics outside the worker boundary.

MAR therefore does **not** relax LPAC for Git.

Instead:

- generic `run_command git ...` is rejected;
- `git_status` and `git_diff` use a daemon-side typed read-only broker;
- the model cannot supply raw Git subcommands/options;
- diff paths are validated workspace-relative paths;
- broker process remains Job Object-contained and output-bounded;
- broker environment is sanitized and does not inherit ambient secrets;
- `GIT_OPTIONAL_LOCKS=0` suppresses optional index mutation;
- fsmonitor, hooks, external diff, textconv, pager, credential helper, and submodule execution paths are disabled/bounded by MAR-authored options.

This is consistent with the frozen contract: task-local Git inspection is exposed through bounded tooling, while worker authority remains strictly weaker than daemon authority.

## Host prerequisite: Windows NUL device

Git for Windows and the Go toolchain both open the Windows `NUL` device. LPAC cannot use it on this Windows 10 host until the host grants the AppContainer package groups the required NUL access.

MAR exposes two explicit commands:

- `mar sandbox-host-check --workspace <path>` — read/execute readiness probe; fails closed when NUL is unavailable.
- `mar sandbox-host-prepare --workspace <path>` — elevated host preparation followed immediately by the same readiness probe.

The NUL device security descriptor is reset by Windows on reboot, so readiness is checked when a `WindowsSandboxExecutor` is constructed. An executor whose readiness probe fails reports `TRUSTED_HOST`, not `ENFORCED_SANDBOX`.

Host preparation is an owner/admin operation. The worker cannot silently elevate itself.

## Self-hosting safety signal

`Runtime.SelfHostingSafe()` is true only when:

1. the model command executor reports `ENFORCED_SANDBOX`; and
2. the typed Git broker is configured.

This signal means the **Coding ACI execution boundary** is safe enough for the later autonomous loop. It does not mean the MAR bootstrap milestone is complete; Slices 011–016 are still required before MAR becomes its own primary coding worker.

## Acceptance evidence

Verified on Windows 10 Pro `10.0.19045`:

1. workspace write succeeds inside LPAC;
2. arbitrary host filesystem write is denied;
3. arbitrary outside-workspace file read is denied;
4. LPAC cannot consume a file that is readable only through `ALL APPLICATION PACKAGES`;
5. explicit read scope succeeds but remains non-writable;
6. task B cannot read task A workspace while task A's capability grant exists;
7. no network capability: TCP connection attempt is denied;
8. ambient parent environment secret does not cross the sandbox boundary;
9. `registryRead` supports required Windows runtime configuration without exposing the test HKCU secret or protected SAM/SECURITY hives;
10. descendant process is killed on timeout and cannot perform a delayed workspace mutation;
11. task capability ACL is removed from workspace root and files created during the command;
12. keyed profile/path lock registries contain no retained entries after command completion;
13. typed Git broker ignores repository-configured external diff/fsmonitor helpers;
14. typed Git broker environment does not inherit ambient secrets;
15. sandboxed typed Git status/diff integration: PASS;
16. native `go test` inside LPAC: PASS;
17. `sandbox-host-check` after elevated preparation: PASS;
18. `go test -count=1 -timeout 180s ./...`: PASS;
19. `go vet ./...`: PASS;
20. Windows `go build ./cmd/mar`: PASS;
21. `git diff --check`: PASS.

## Scope boundary

This slice does not implement:

- semantic context retrieval;
- autonomous model loop;
- semantic checkpoint/resume;
- result/verification contract;
- MCP control surface;
- final self-hosting acceptance.

Those remain Slices 011–016.

## Known environment limitation

The Go race detector remains unavailable because the host C compiler lacks the required 64-bit support. This is unchanged from the prior checkpoint and is not a passing race result.

Final implementation revision is the Git commit containing this document.
