# Slice 051 — Trusted Owner Fast-Path Filesystem Primitives

**Date:** 2026-09-23  
**Status:** QUALIFIED CANDIDATE / ACTIVATION PENDING

## Goal

Close one practical ChatCode-parity gap in ordinary local development without widening MAR's trust kernel or forcing harmless filesystem work through host command execution.

Before this slice, the Trusted Owner Fast Path could create/replace/patch files, run bounded host commands, and perform bounded Git operations. Routine file organization such as creating a directory, renaming a file, or deleting a file required `action.run`. Because host execution is intentionally not network-sandboxed, `action.run` requires project `network_allowed=true`. That coupled local-only filesystem work to unrelated network authority.

## Change

The existing canonical `action` tool gains three bounded operations:

- `mkdir`: create exactly one project-relative directory whose parent already exists;
- `remove`: remove exactly one bounded regular file only when its exact SHA-256 matches `expected_sha256`;
- `rename`: rename exactly one bounded regular file to one absent project-relative destination only when the source SHA-256 matches `expected_sha256`.

These operations require only project `local_file_write`. They do not invoke a shell or child process and do not require `network_allowed`.

## Safety bounds

The slice intentionally does **not** add:

- recursive directory creation;
- recursive deletion;
- directory deletion;
- wildcard/glob mutation;
- overwrite-on-rename;
- symlink-file mutation;
- absolute/out-of-project mutation;
- shell interpretation;
- a new MCP tool;
- durable lifecycle/state.

File remove/rename remains bounded to the existing project read-size ceiling and uses the same registered-project path confinement. The source revision guard is exact SHA-256. A mismatched hash fails before mutation.

## Acceptance

1. `mkdir`, `remove`, and `rename` dispatch through the existing `action` domain tool.
2. The operations require `local_file_write` and do not require network authority.
3. `remove` and `rename` reject stale SHA-256 preconditions.
4. `rename` rejects an existing destination instead of overwriting it.
5. traversal remains fail-closed.
6. ordinary `action.run` still rejects when `network_allowed=false`; this slice does not weaken host-command policy.
7. focused `internal/service` and `internal/mcpedge` regressions pass.
8. full repository test/vet/build qualification is required before integration/release claims.

## Focused evidence

Current candidate evidence:

- managed `gofmt` on all changed Go files — PASS;
- `go test -count=1 ./internal/service ./internal/mcpedge` using MAR's managed Go toolchain — PASS;
- regression `TestProjectActionFilesystemFlowDoesNotRequireNetwork` proves local filesystem mutation succeeds with `NetworkAllowed=false` while host `run` remains rejected.

Full release-style qualification on the exact candidate working tree also PASSed:

- `go test -p 1 -count=1 -timeout 300s ./...` — PASS across all 23 packages;
- `go vet -p 1 ./...` — PASS;
- `go build -p 1 ./...` — PASS;
- `git diff --check` — PASS (Windows line-ending conversion warnings only).

T7 initially exposed host-sensitive acceptance flakiness while free physical RAM was near MAR's 1 GiB production reserve, including one immediate reserve rejection and later `WORKSPACE_READY` timeouts. Investigation proved the filesystem change was not the cause: clean base `2edc2f299dfc668d9aada56d280869411cfefe58` passed T7 repeatedly; service-only, Backend-interface-only, action-schema-only, description-only, dispatch-inclusive, and full-diff candidates all passed in controlled A/B runs. The actual coupling was in the `cmd/mar` T7 helper: a disconnect-safety acceptance test launched `mcp-stdio` with production resource defaults, so host RAM pressure could prevent the worker from reaching the behavior under test. A bounded internal-only `mcpRuntimeOptions` seam now lets this helper inject the same deterministic resource-governor/scheduler envelope already used by lower-level T7 coverage. No CLI flag or public surface was added, and production defaults remain unchanged at the existing 1 GiB free-RAM reserve / 256 MiB workspace RAM reservation. After the correction, exact T7 stress `-count=3` PASSed 3/3 (4.36s, 5.05s, 3.34s).

## Measured workflow effect

For these local-only filesystem actions the old fast-path workaround was either:

- unavailable while `network_allowed=false`; or
- one host-process invocation after widening network authority.

The new path is one typed MCP action with no child process and no network-authority dependency. The benefit is therefore primarily authority/ergonomics parity rather than a claimed wall-clock speedup. Broader ChatCode-vs-MAR latency benchmarking remains the next measured step after this capability slice is release-qualified.
