# MAR vs ChatCode — Trusted Owner Fast-Path Benchmark

**Date:** 2026-09-23  
**Status:** MEASURED / PARTIAL COMPARISON

## Question

Is direct MAR still materially slower or more cumbersome than ChatCode for ordinary single-owner development, and what should be optimized next?

The comparison is intentionally split into:

1. MAR local/runtime work;
2. outer ChatWeb -> MCP round-trip cost;
3. ChatCode availability and historical direct-run evidence.

A speed winner is not claimed unless the same live workload is available on both systems.

## Environment

MAR live runtime at measurement time:

- exact source/runtime revision: `3d95663a34ffa262ca8179e95314467c1d144d1b`;
- runtime: `HEALTHY / ALIGNED / trusted_for_release=true`;
- OpenAI Secure Tunnel: connected / ready / healthy;
- repository: `mar`.

ChatCode desktop was launched successfully, but the live gateway rejected the benchmark connection with:

- status: `blocked`;
- reason: `ai_connections_max_limit_reached`;
- used: `3`;
- limit: `3`.

Therefore the current same-workload ChatCode leg is **NOT RUN**, not PASS/FAIL.

## Current live MAR outer round-trip sample

Five sequential rounds were executed from ChatWeb through the active MAR MCP connection. Each round performed:

1. `project.context`;
2. `project.find` for `ProjectReadResult`;
3. bounded `project.read`;
4. `project.git_status`;
5. `action.run git rev-parse HEAD`.

Observed milliseconds:

| Round | context | find | read | git_status | run | total |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 2788 | 2860 | 2736 | 3153 | 4741 | 16278 |
| 2 | 4388 | 2294 | 3602 | 4796 | 8858 | 23938 |
| 3 | 4072 | 2137 | 2881 | 6091 | 4854 | 20035 |
| 4 | 3959 | 2594 | 2709 | 5041 | 5149 | 19452 |
| 5 | 5508 | 2269 | 3731 | 3043 | 2545 | 17096 |
| **median** | **4072** | **2294** | **2881** | **4796** | **4854** | **19452** |

These are outer tool-call timings. They include ChatWeb/tool orchestration and transport; they are not local MAR service timings.

## Same-day MAR local/E2E evidence

A prior same-day direct MAR coding benchmark measured:

- attach: ~39.85 ms;
- find: ~3.13 ms;
- read: ~2.02 ms;
- patch + Go test: ~14.74 s;
- stage: ~43.74 ms;
- commit: ~152.71 ms;
- status: ~127.08 ms;
- total: ~15.11 s;
- non-test overhead: ~369 ms.

A separate six-round MAR microbenchmark measured roughly:

- find: 51.02 ms;
- read: 1.20 ms;
- apply+verify: 51.52 ms;
- Git status: 104.05 ms;
- total: 228.11 ms.

An attempted optimization variant measured 255.49 ms (+12%) and was reverted.

These measurements show that local deterministic MAR work is small relative to the current multi-second outer MCP calls.

## ChatCode evidence and limits

Current live ChatCode benchmark admission failed before workload execution because all 3 AI connection slots were already in use.

Earlier same-day direct ChatCode work also hit workspace/admission friction before a comparable E2E coding task could start. A separate historical fully-specified ChatCode smoke completed approximately:

- worktree: ~1 s;
- context: 4.32 s;
- tests: 3.12 s;
- cleanup: 0.56 s.

That fixture is not identical to the MAR E2E benchmark and is therefore directional context only, not a latency winner comparison.

## Finding

The dominant measured bottleneck for current direct MAR usage is **outer MCP round-trip count**, not local Git/filesystem/service execution.

This is consistent with recent bounded changes:

- Slice 052: combine context batch + Git status;
- Slice 053: return file SHA-256 with reads;
- Slice 054: combine Git stage + commit.

However, live ChatGPT exposed one concrete compatibility gap: the current conversation retained a frozen pre-Slice-052 input schema. Calling `context_batch` with the new `include_git_status` field failed client-side because that property was absent from the frozen schema.

Therefore a compound operation that depends on a newly added request field may be unavailable until the ChatGPT connector schema is refreshed, even when the MAR runtime already supports it.

## Decision

1. Do **not** add caches, local AI, new lifecycle machinery, or Go micro-optimizations from this benchmark.
2. Keep reducing outer calls only when representative workflow evidence supports it.
3. Add one frozen-schema-safe alias for the already-proven context+status composition:
   - `project.operation=context_status`;
   - reuse only request fields that already existed in the older `project` schema.
4. Keep `context_batch` default semantics unchanged so non-Git/research-only projects remain compatible.
5. Do not claim MAR is globally faster than ChatCode while the current same-workload ChatCode leg is blocked.
6. Do not open another productivity slice after this compatibility correction unless a new representative benchmark exposes a material gap.

## Exit

After the frozen-schema-safe alias is qualified and proven callable from the current stale-schema chat without reconnect, Trusted Owner Fast Path productivity optimization pauses. The next live ChatCode comparison should be rerun only when a connection slot is available.
