# MAR

MAR is a single-owner, single-machine, MCP-native autonomous coding runtime.

The V1 architecture is frozen. Canonical architecture documents live under `docs/architecture/MAR_V1_Architecture_FROZEN/`.

## V1 execution goal

`Goal -> Durable Task -> Autonomous Worker -> Isolated Worktree -> Verification -> Crash-safe Integration -> Result`

## MCP control surface

The local MCP edge is available over stdio:

```text
mar mcp-stdio -db .mar/mar.db
```

The public task-oriented surface is intentionally limited to:

`submit`, `status`, `steer`, `input`, `cancel`, `result`, `inspect`.

Low-level coding primitives such as repository reads/writes and command execution remain inside the worker runtime rather than the public MCP workflow.

## Development rule

Architecture is closed. Implementation may only reopen architecture when benchmark or recovery evidence proves a frozen invariant insufficient.

See `docs/architecture/MAR_V1_Architecture_FROZEN/00_ARCHITECTURE_FREEZE_RECORD.md` and `10_IMPLEMENTATION_ENTRY_CONTRACT.md` before changing runtime design.
