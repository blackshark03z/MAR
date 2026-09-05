# MAR implementation instructions

Before planning or implementation, read:

1. `docs/architecture/MAR_V1_Architecture_FROZEN/00_ARCHITECTURE_FREEZE_RECORD.md`
2. `docs/architecture/MAR_V1_Architecture_FROZEN/10_IMPLEMENTATION_ENTRY_CONTRACT.md`
3. the architecture document relevant to the current subsystem.

The MAR V1 architecture is FROZEN.

Do not redesign task lifecycle, execution fencing, workspace isolation, integration serialization, durable state, worker authority, or resource boundedness unless concrete implementation/benchmark evidence proves a frozen invariant insufficient.

Prefer small vertical slices with tests and revision-bound evidence.

No push or deploy by default.
