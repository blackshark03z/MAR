# MAR

MAR is a single-owner, single-machine, MCP-native autonomous coding runtime.

The V1 architecture is frozen. Canonical architecture documents live under `docs/architecture/MAR_V1_Architecture_FROZEN/`.

## V1 execution goal

`Goal -> Durable Task -> Autonomous Worker -> Isolated Worktree -> Verification -> Crash-safe Integration -> Result`

## Development rule

Architecture is closed. Implementation may only reopen architecture when benchmark or recovery evidence proves a frozen invariant insufficient.

See `docs/architecture/MAR_V1_Architecture_FROZEN/00_ARCHITECTURE_FREEZE_RECORD.md` and `10_IMPLEMENTATION_ENTRY_CONTRACT.md` before changing runtime design.
