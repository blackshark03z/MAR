# R-010 — Stable-Source Benchmark Phase 0

**Status:** `RESEARCH_ONLY`  
**Baseline intent:** MAR v1.1.0  
**Code changes authorized:** none  
**Date:** 2026-09-12

## Research question

Can the frozen MAR V1.1 production source be benchmarked repeatedly today with the existing acceptance fixtures and release artifacts, without changing production code, and can the resulting measurements be treated as comparable performance evidence?

## Source identity check

The current research HEAD contains documentation-only research commits after `v1.1.0`. A production-path diff from `v1.1.0` to the current HEAD reported `PRODUCTION_DIFF=NONE` for Go source, module files, UI source/assets and scripts. The managed toolchain reports Go 1.27.0.

Therefore current canonical production source is suitable as the V1.1 source baseline for research, provided the executed artifact/harness is also identity-compatible.

## Attempt 1 — frozen T1 fixture without adaptation

The opt-in fixture was invoked as the isolated T1 subtest:

```text
MAR_RUN_SELF_HOSTING_ACCEPTANCE=1
go test -count=1 -timeout 10m -run '^TestAcceptanceT1ToT4TaskClasses$/^T1-tiny-fix$' -v ./internal/orchestrator
```

It failed before worker execution at the test-harness submit decoder. The current canonical `submit` MCP tool returns a compact receipt whose task identity is `task.task_id`; the older T1–T4 harness still deserializes the receipt into `domain.Task`, whose identity field is tagged `json:"id"`.

The observed response was valid current-contract data, but the harness interpreted the task ID as empty and aborted with `unexpected submit result`.

This is **benchmark-harness drift after public MCP surface contraction**, not evidence that the submitted task/runtime failed. Current T7 acceptance code already decodes the compact submit receipt with `json:"task_id"`, confirming the current contract.

## Attempt 2 — research-only Go overlay

A Go test overlay was created outside the MAR repository under `D:\MAR-Research\observer\benchmarks\t1`. The overlay changes only the T1–T4 test-harness receipt decoder from the legacy full `domain.Task` shape to the current compact `task_id` receipt. Canonical MAR production source is not modified.

With that adapter, T1 passed submit and reached worker execution, then correctly failed closed at the existing self-hosting safety gate:

```text
autonomous agent loop requires a self-hosting-safe coding runtime
```

The task became `BLOCKED`; its attempt was durably `PHYSICALLY_TERMINATED`. No model turn or mutation benchmark result was accepted.

## Host prerequisite check

Both the historical `mar-v1-stable.exe` and the current root `mar.exe` report the same host prerequisite failure:

```text
Windows sandbox host is not prepared:
AppContainer NUL probe failed: Access is denied.
```

This is an expected Windows host condition after the NUL device ACL prerequisite is reset (for example after reboot). MAR correctly refuses to downgrade `SelfHostingSafe()` merely to make the benchmark run.

Therefore T1 performance samples are currently `NOT_RUN_VALIDLY`, not PASS/FAIL performance evidence. Host sandbox preparation must be recorded as part of the benchmark environment profile before any mutation benchmark is admitted.

## Release artifact identity signal

A separate release-artifact check found:

- source at `v1.1.0`: `latestSchemaVersion = 14`;
- current source: `latestSchemaVersion = 14`;
- `.mar/runtime/mar-v1-stable.exe` SHA-256: `A53878CAE53A6AE0D936162DFF8284D80570B0EC8BA51F975C1CA68D4CD81748`;
- the default `scripts/start-owner-console.ps1` points to that `mar-v1-stable.exe`;
- starting that binary against the current durable database fails closed with `sqlite schema version 14 is newer than supported 13`.

This proves that the file currently named `mar-v1-stable.exe` is not schema-compatible with the accepted V1.1 durable store/source. It must not be used as a V1.1 performance baseline merely because of its filename.

The final V1.1 release record binds the accepted implementation HEAD but does not bind one exact release executable SHA-256. Historical handoff sections contain several binary hashes from earlier engineering points, so filename alone is insufficient release identity.

The retained runtime manifest `.mar/runtime/v1-release-final.json` makes the provenance gap explicit. It records a promotion at `2026-09-09T17:25:53.2379551Z` from source HEAD `0f01136c94f1747948c61e1d91111f2287e618f4`, with candidate `mar-v5-final-head.exe` SHA-256 `2F2D381E140B2440B8438F3D07E17574E1B8E71FD0ED1348EE9D56366C9F0A76`, promoted to the path `D:\\MAR\\.mar\\runtime\\mar-v1-stable.exe`. The later Owner-accepted V1.1 implementation HEAD is `a6fa0279631c7a8f05f484a5bba7b20b73e0188f`. Therefore the retained promotion manifest predates and does not identify the accepted V1.1 implementation.

The current on-disk `mar-v1-stable.exe` hash (`A53878...`) also does not match the candidate hash stored in that retained manifest, so the filename has accumulated at least one later replacement without a final V1.1 artifact manifest that binds it to `a6fa027...`.

This is classified as **RELEASE_ARTIFACT_IDENTITY_SIGNAL**, not yet as a production-code regression.

## Why the three blockers are separate

1. **Harness drift** — the benchmark parser did not track a bounded MCP receipt contract change.
2. **Host readiness** — Windows sandbox prerequisite is currently not prepared, so mutation/self-hosting benchmarks are invalid until the environment is valid.
3. **Artifact identity drift** — the default binary filename points to an executable that cannot open the schema-14 durable store.

Conflating these would create a false diagnosis such as "MAR T1 is slower/broken" when no valid T1 performance run has occurred yet.

## Research implications

Before stable-source mutation benchmarking is automated, validate these candidate invariants:

- benchmark admission must preflight sandbox readiness and return `NOT_RUN` when invalid;
- benchmark harness version must be bound to the public MCP contract it decodes;
- a release must bind source/tag, executable hash, supported DB schema and generated UI/assets as one artifact identity;
- the normal startup path must resolve to that bound artifact rather than a historically named executable;
- observer reports must record artifact hash + schema + source revision so incomparable runs are not mixed.

These are research candidates only. This document does not authorize rebuilding/replacing `mar-v1-stable.exe`, changing the startup script, modifying the MCP contract, patching T1–T4 in canonical source, or preparing the Windows sandbox host.

## Phase-0 verdict

`RESEARCH_SIGNAL`

A valid repeated T1 latency baseline has **not** yet been established. The correct next research path is:

1. continue read-only transport/resource/context observation;
2. establish artifact identity for the actually runnable schema-14 binary;
3. only after sandbox readiness is valid, run 10 comparable T1 trials using an identity-compatible harness/artifact;
4. keep all raw benchmark data outside Git and record only summarized evidence here.
