# R-023 — Measurement coverage matrix

**Status:** `RESEARCH_ONLY`  
**Baseline:** MAR v1.1.0  
**Code changes authorized:** none  
**Date:** 2026-09-12

## Purpose

Define which P0 benchmark metrics can already be measured directly from V1.1/external observer evidence, which are only proxies, and which remain blind spots.

This prevents research reports from silently converting an inferred value into an authoritative MAR metric.

## Evidence labels

- `DIRECT` — directly observed from durable/runtime/OS evidence with clear semantics.
- `EXTERNAL_DIRECT` — directly observed by the external Research Observer rather than MAR.
- `DERIVED` — reconstructed from authoritative timestamps/counters, with assumptions documented.
- `PROXY` — correlated approximation useful for research but not a causal metric.
- `BLIND` — current research stack cannot measure it reliably.

## Runtime and environment identity

| Metric | Coverage | Current source | Notes |
| --- | --- | --- | --- |
| registered MAR project HEAD | DIRECT | MAR `project_context` / Git | live self-hosting read path currently returns project/head |
| release tag/source identity | DIRECT | Git/release docs | source identity only; does not bind executable automatically |
| executable path/hash | EXTERNAL_DIRECT | observer/host file metadata when collector has access | must be captured before benchmark |
| executable build/source provenance | PROXY / BLIND | release manifest/docs where present | R-012 proved current artifact provenance is incomplete/stale for V1.1 |
| SQLite schema compatibility | DIRECT when binary is actually opened | store startup/schema | invalid binary/source pairing makes benchmark `NOT_RUN_RUNTIME_IDENTITY` |
| sandbox host readiness | DIRECT | `sandbox-host-check` | expensive active probe, not a static flag |
| Go/toolchain identity | DIRECT | source/profile/runtime probe | include in environment profile |

## Durable task lifecycle

| Metric | Coverage | Current source | Notes |
| --- | --- | --- | --- |
| task created/terminal state | DIRECT | SQLite task/result records | reliable |
| attempt start/end | DIRECT | execution_attempts | available for most historical tasks |
| attempt/run epoch | DIRECT | durable task/attempt identity | authority-critical |
| WebTurn created/responded | DIRECT | web_turns | reliable |
| model wait per durable WebTurn | DERIVED | created_at -> responded_at | excludes client/tool processing around those timestamps |
| verification command duration | DIRECT | verification command evidence | per command when verification completed |
| integration attempt timestamps | DIRECT | integration records | some stage timing available |
| every task-state transition timestamp | BLIND | no append-only full state history | R-013: do not infer precise stage decomposition from final updated_at |
| exact preflight/queue duration | PROXY / BLIND | partial task/workspace records | not sufficient for authoritative stage timing |

## Context and cognition

| Metric | Coverage | Current source | Notes |
| --- | --- | --- | --- |
| stored Web request/response bytes | DIRECT | durable WebTurn JSON | already collected externally |
| DecisionProjection composition | DIRECT for stored projection turns | offline context collector | repository/recent/contract byte classes measured |
| legacy replay factor | DERIVED | offline structural comparison | historical only; not current V1.1 defect |
| same-request recent/tail duplicate bytes | DERIVED with byte equality | offline collector | strong candidate metric |
| model input/output tokens | DIRECT when provider reports usage | Web response/resource summary | coverage may be incomplete |
| context-build wall time | BLIND | not persisted | P0-3 requires controlled external timing or future instrumentation |
| Git subprocess count per context build | SOURCE-STRUCTURAL / PROXY | code path | five snapshot Git commands/build by current implementation; actual runtime count not externally sampled yet |
| files/bytes scanned per context build | partially DIRECT in Pack, timing BLIND | context Pack metadata | useful but not enough to attribute wall time |
| Web Chat outer context size | BLIND | client/platform owned | MAR cannot derive from durable inner request bytes |
| browser renderer context/memory growth | EXTERNAL_DIRECT for process RSS, semantic cause PROXY | resource watcher | Chrome aggregate RSS currently measured; per-tab attribution absent |

## Transport and reconnect

| Metric | Coverage | Current source | Notes |
| --- | --- | --- | --- |
| local 8787/8788 readiness | EXTERNAL_DIRECT | active transport watcher | 5 s sampling when MAR active |
| local readiness transition duration | EXTERNAL_DIRECT | event watcher | bounded by sample cadence |
| local Owner API latency | EXTERNAL_DIRECT | transport watcher | available when surface is live |
| process ownership | EXTERNAL_DIRECT | observer path/parent evidence | avoids false cloudflared attribution |
| MCP request count/session count | DIRECT but in-memory | MAR connector telemetry | not durable across restart |
| exact remote HTTP status/outcome | BLIND | not durably recorded by MAR | required for P0-5/P0-6 |
| remote request duration | BLIND | no authoritative MAR event | required for transport A/B |
| protocol version per request/session | BLIND / partial | negotiation exists but not durable telemetry | needed for stateful/stateless comparison |
| disconnect reason/hop | BLIND without external tunnel metrics | current MAR telemetry | R-015 proposes cloudflared/admin correlation |
| cloudflared HA connections/streams/errors/RTT | EXTERNAL_DIRECT when metrics endpoint available | cloudflared Prometheus/admin | does not prove MAR origin failure by itself |
| exact ambiguous-ACK commit boundary | BLIND until fault-injection harness | benchmark only | correctness oracle is durable state after injected loss |

## Host and process resources

| Metric | Coverage | Current source | Notes |
| --- | --- | --- | --- |
| physical RAM load/available | EXTERNAL_DIRECT | resource watcher | active |
| Windows commit used/limit/% | EXTERNAL_DIRECT | resource watcher | active |
| Chrome aggregate RSS/process count | EXTERNAL_DIRECT | resource watcher | active; no per-tab attribution |
| MAR aggregate RSS/process count | EXTERNAL_DIRECT | resource watcher | active |
| disk free/used | EXTERNAL_DIRECT | resource watcher | active |
| short CPU sample | EXTERNAL_DIRECT | resource observer | active |
| subprocess creation rate by type | BLIND / source proxy | not currently sampled authoritatively | needed for Console/context-build A/B |
| per-process lifetime and spawn cause | BLIND | current observer | could be measured externally without MAR instrumentation |
| I/O bytes by MAR/SQLite/context builder | BLIND / OS proxy candidate | current observer does not persist detailed per-process I/O | useful but optional if wall-time/CPU evidence is decisive |

## SQLite and Owner Console

| Metric | Coverage | Current source | Notes |
| --- | --- | --- | --- |
| DB configuration (`WAL`, `FULL`, one connection per `store.Open()` handle) | DIRECT from source/runtime config | store.Open | Owner UI parent and mcp-stdio runtime use separate handles/processes |
| DB file/WAL size | EXTERNAL_DIRECT candidate | filesystem metadata | not yet part of standard snapshot |
| per-query latency | BLIND | no query timing telemetry | P0-1/P0-2 key gap |
| query count per endpoint | SOURCE-STRUCTURAL / PROXY | source inspection | useful hypothesis, not runtime fact |
| single-connection queue wait | BLIND | `database/sql` wait metrics not collected | strongest contention blind spot |
| Owner endpoint latency | EXTERNAL_DIRECT | transport probe for runtime endpoint | broader endpoint matrix not yet sampled |
| sandbox-check subprocess frequency | SOURCE-STRUCTURAL / PROXY | UI 2 s poll + runtime handler | actual process count still needs A/B measurement |

## Verification and integration

| Metric | Coverage | Current source | Notes |
| --- | --- | --- | --- |
| test/vet/build duration | DIRECT | verification command evidence | available on completed verification |
| command pass/fail/output hash | DIRECT | sealed evidence | authoritative |
| environment hash identity | DIRECT | verification evidence | duration of hashing itself is BLIND |
| freshness revalidation count | SOURCE-STRUCTURAL | verifier/integration flow | actual runtime duration BLIND |
| candidate/head cleanliness outcome | DIRECT | verification/integration result | cost timing BLIND |
| integration attempt state/timestamps | DIRECT | durable integration attempt | enough for coarse envelope |
| exact verification non-command overhead | DERIVED/BLIND | residual around command timestamps | keep as residual until measured |

## P0 benchmark readiness

### Ready with existing measurement stack

- runtime/project source identity (excluding executable provenance gap);
- durable task/attempt/WebTurn outcome;
- stored context bytes/composition;
- model wait from WebTurn timestamps;
- host RAM/commit/process RSS;
- local 8787/8788 readiness and coarse latency;
- verification command timing;
- final correctness/authority invariants.

### Requires external research collector extension, not MAR code

Prefer solving these outside V1.1 first:

- executable SHA/path/build metadata collection;
- subprocess creation/lifetime sampling;
- DB/WAL file-size trend;
- broader Owner endpoint latency;
- cloudflared/admin Prometheus metrics;
- per-process I/O counters where useful;
- process correlation during Console/context-build A/B.

### Genuine instrumentation blind spots

Only after external approaches are exhausted should V1.2 instrumentation be considered for:

- SQLite per-query / connection-wait latency;
- exact context-build stage duration inside a worker;
- authoritative remote MCP request outcome/duration/protocol identity;
- exact task-state transition history if stage decomposition proves necessary.

Even these are not implementation requirements yet. They are candidates only if the P0 benchmark cannot answer its decision question without them.

## Important measurement rule

When a report uses a proxy, label it explicitly. Examples:

- `10 polls/s × request size` is a **byte-touch proxy**, not measured disk throughput;
- `task wall - Web wait` is **RESIDUAL_UNATTRIBUTED**, not MAR overhead;
- Chrome aggregate RSS delta is a **browser-pressure signal**, not proof one ChatGPT tab caused the allocation;
- source-level query counts are **structural estimates**, not observed database timings.

## Current verdict

`MOST_P0_QUESTIONS_CAN_START_WITH_EXTERNAL_EVIDENCE`

The research stack already covers enough to start P0 A/B experiments without modifying MAR. The highest-value remaining blind spots are narrow. Do not add general-purpose telemetry to V1.1 merely because richer metrics would be convenient.
