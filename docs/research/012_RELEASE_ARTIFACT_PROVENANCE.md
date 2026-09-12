# R-012 — Release Artifact Provenance and Runtime Identity

**Status:** `RESEARCH_ONLY`  
**Baseline:** MAR v1.1.0 accepted source  
**Code changes authorized:** none  
**Date:** 2026-09-12

## Research question

How can MAR measurement, startup and future release validation prove that the executable being observed is the executable produced for the accepted source/release, rather than trusting a historical filename such as `mar-v1-stable.exe`?

## Why this became P0 research

R-010 attempted a stable-source benchmark and discovered that source identity alone is insufficient. The canonical source can be unchanged while the executable selected by the startup path is older than the durable database schema and accepted implementation.

Any latency, reconnect, resource or correctness measurement is suspect if the observer cannot bind:

```text
accepted source/tag
      ↕
release manifest
      ↕
actual executable bytes
      ↕
supported durable-store schema
      ↕
running process
```

## Current empirical evidence

Accepted V1.1 truth:

- tag/source `v1.1.0` has SQLite `latestSchemaVersion = 14`;
- final Owner acceptance records implementation HEAD `a6fa0279631c7a8f05f484a5bba7b20b73e0188f`;
- current production source remains identical to the V1.1 tag outside research documentation.

Retained runtime release manifest:

`D:\MAR\.mar\runtime\v1-release-final.json`

records:

- source/head: `0f01136c94f1747948c61e1d91111f2287e618f4`;
- candidate artifact: `mar-v5-final-head.exe`;
- candidate SHA-256: `2F2D381E140B2440B8438F3D07E17574E1B8E71FD0ED1348EE9D56366C9F0A76`;
- production path: `D:\MAR\.mar\runtime\mar-v1-stable.exe`;
- promoted at: `2026-09-09T17:25:53.2379551Z`.

Current on-disk executable observations:

- `.mar/runtime/mar-v1-stable.exe` SHA-256: `A53878CAE53A6AE0D936162DFF8284D80570B0EC8BA51F975C1CA68D4CD81748`;
- root `mar.exe` SHA-256: `74FAFFC1E2FCF83EA0F317D6476F30CA7D6FDD65B2CFC03F8775F65ABA048118`.

The current `mar-v1-stable.exe` hash therefore matches neither the retained manifest candidate hash nor an artifact identity bound by the final V1.1 acceptance record.

The default `scripts/start-owner-console.ps1` selects `.mar/runtime/mar-v1-stable.exe` by filename. Starting that executable against the current durable store fails closed:

```text
sqlite schema version 14 is newer than supported 13
```

This is direct evidence that the default startup target is not compatible with the accepted V1.1 durable store.

### Live runtime behavioral identity — 2026-09-12

The currently running Owner UI parent and `mcp-stdio` child both execute `D:\\MAR\\mar.exe` with SHA-256 `74FAFFC1E2FCF83EA0F317D6476F30CA7D6FDD65B2CFC03F8775F65ABA048118`. Go build metadata is readable but contains no `vcs.revision`, and no executable under `.mar/runtime` matches that root binary hash.

A stronger behavioral fingerprint shows the running binary is not the accepted V1.1 runtime: its `/` surface serves a ~149 KiB monolithic HTML/CSS Owner Console and does not reference the React/Vite asset model, while accepted V1.1 source serves `cmd/mar/owner_ui_dist/index.html` with hashed `/assets/index-*.js` and `/assets/index-*.css` resources. Therefore the current live runtime is not merely `BINARY_UNBOUND`; it is behaviorally stale relative to the accepted V1.1 source.

This makes current latency/mutation measurements invalid as a V1.1 performance baseline even when host resource pressure is otherwise acceptable.

## Classification

Current research classification:

```text
accepted_source        = KNOWN
accepted_impl_head     = KNOWN
runtime_manifest       = PRESENT_BUT_PRE_ACCEPTANCE
startup_target_path    = KNOWN
startup_target_hash    = KNOWN
startup_target_schema  = INCOMPATIBLE_WITH_DB_14
artifact_release_bind  = UNAVAILABLE
verdict                = ARTIFACT_PROVENANCE_GAP
```

This is not classified as a source-code regression. It is a packaging/release identity gap.

## Observer requirement

Every controlled benchmark/report should eventually include one bounded **Runtime Identity Record** before metrics are considered comparable.

Candidate fields:

```text
measurement_schema
observed_at
release_tag
accepted_implementation_head
current_source_head
production_diff_from_release
release_manifest_path
manifest_source_head
manifest_binary_sha256
startup_target_path
startup_target_sha256
durable_db_user_version
binary_schema_compatibility
running_pid
running_executable_path
running_executable_sha256
identity_verdict
```

Do not record secrets, user identifiers, path tokens or environment values.

## Identity verdicts

Candidate analytical statuses:

- `ALIGNED` — accepted source, manifest, startup target and running bytes are bound and compatible;
- `DOCS_ONLY_AHEAD` — source HEAD differs only by explicitly research/docs-only commits while production identity remains release-bound;
- `MANIFEST_STALE` — retained manifest predates accepted implementation;
- `BINARY_UNBOUND` — executable hash is not identified by the accepted release record;
- `SCHEMA_INCOMPATIBLE` — selected binary cannot safely open the durable store version;
- `RUNNING_BINARY_MISMATCH` — running executable differs from expected startup/release target;
- `INSUFFICIENT_EVIDENCE` — identity cannot be established without guessing.

Multiple statuses may coexist; the compact report should surface the highest-consequence one.

## Safe phase-0 automation

Without modifying MAR, the external Research Observer can safely automate most identity checks:

1. read current Git HEAD/tag and production-path diff;
2. read final release acceptance metadata from repo docs;
3. read retained runtime manifest if present;
4. hash executable files on disk;
5. read SQLite `PRAGMA user_version` read-only;
6. observe running process executable path/hash when accessible;
7. compare evidence and emit an analytical verdict.

What it cannot reliably derive from arbitrary historical binaries is the maximum supported schema version unless the binary is probed or future builds embed/query explicit build metadata.

A compatibility probe must never point an unknown binary at the authoritative DB if there is any migration/write possibility. Prefer a read-only/snapshotted store or future explicit build metadata.

## Future release-contract hypothesis

A future release system may benefit from one immutable manifest generated at the exact accepted build, containing at least:

```text
release/version
source commit
dirty=false
binary SHA-256
supported/current DB schema
embedded UI asset identity
Go/toolchain identity
verification/evidence reference
build timestamp
```

The startup path could then refuse or warn on a hash/schema mismatch instead of relying on a filename. This is only a research hypothesis; it is not authorized implementation scope.

## Relationship to monitoring

Runtime identity must be established **before** rolling performance baselines are compared. Otherwise an observer can report a false latency/context regression when the real change is that a different executable was launched.

Recommended analytical ordering:

```text
IDENTITY
  ↓
ENVIRONMENT READINESS
  ↓
TRANSPORT / RESOURCE HEALTH
  ↓
TASK / CONTEXT / LATENCY METRICS
  ↓
REGRESSION ANALYSIS
```

If identity is `SCHEMA_INCOMPATIBLE` or `BINARY_UNBOUND`, mutation benchmarks should be `NOT_RUN` rather than contaminating the baseline.

## Live runtime behavioral fingerprint — 2026-09-12

The currently running Owner UI parent and `mcp-stdio` child both execute `D:\\MAR\\mar.exe` with SHA-256 `74FAFFC1E2FCF83EA0F317D6476F30CA7D6FDD65B2CFC03F8775F65ABA048118`.

Go build metadata for that executable contains no `vcs.revision`, and an exact SHA-256 inventory across `.mar/runtime` found no matching archived executable. More importantly, the running Owner surface behaviorally diverges from the accepted V1.1 source: the running `/` response is a ~149 KiB monolithic HTML/CSS document with no `/assets/index-*.js` React/Vite asset reference, while accepted V1.1 source serves the React/Vite bundle from `cmd/mar/owner_ui_dist/index.html`.

This upgrades the runtime-identity classification from merely `BINARY_UNBOUND` to:

```text
BINARY_UNBOUND
+ RUNNING_UI_ASSET_MODEL_MISMATCH
= RUNNING_RUNTIME_NOT_ACCEPTED_V1_1
```

This remains a packaging/runtime-identity finding, not a source regression. Performance baselines taken against this process must remain `NOT_RUN_RUNTIME_IDENTITY`.

## Phase-0 verdict

`ARTIFACT_PROVENANCE_GAP_CONFIRMED`

The research evidence is sufficient to justify automated artifact-identity measurement. It is **not** yet authority to rebuild, replace or rename any V1.1 executable or startup path.
