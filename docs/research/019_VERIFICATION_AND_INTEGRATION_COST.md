# R-019 — Verification and integration cost decomposition

**Status:** `RESEARCH_ONLY`  
**Date:** 2026-09-12  
**Verdict:** `VERIFICATION_COMMAND_COST_CONFIRMED_MATERIAL; INTEGRATION_COST_SMALL_IN_CURRENT_SAMPLE; OPTIMIZATION_POLICY_UNPROVEN`

## Research question

How much MAR wall time is spent proving candidate freshness/correctness and safely integrating it, and which parts are essential safety cost versus potentially redundant repeated work?

This document does not authorize weaker verification, skipped freshness checks, parallel command execution, cached evidence reuse, or integration-policy changes.

## Current built-in verification profiles

### `go-standard`

Runs sequentially with `-p 1`:

1. `go test -v -p 1 -count=1 -timeout 180s ./...`
2. `go vet -p 1 ./...`
3. `go build -p 1 ./...`

### `go-docs`

Documentation-only change scope still runs:

1. `go test -p 1 -count=1 -run ^$ -timeout 180s ./...`
2. `go vet -p 1 ./...`
3. `go build -p 1 ./...`

The verifier executes every configured command sequentially and records command-level duration/output evidence. A failed earlier command does not currently short-circuit later profile commands.

This favors complete evidence over minimal failure latency.

## Hidden verification cost outside the three profile commands

Verification also performs freshness/identity work before, during and after the command suite.

Important operations include:

- load task and verification profile;
- resolve sealed candidate;
- validate candidate/workspace identity;
- Git `HEAD` / diff / clean-worktree checks;
- transition task into VERIFYING;
- capture environment identity before commands;
- execute all profile commands;
- capture environment identity after commands;
- revalidate candidate head/cleanliness;
- evaluate acceptance oracles;
- persist verification evidence and result.

Therefore `sum(command durations)` is not the full verification wall time.

## Environment identity hashing

`hostEnvironmentSnapshot(profile)` resolves each distinct verification executable, opens the executable file and SHA-256 hashes its bytes, then binds:

- GOOS;
- GOARCH;
- sandbox isolation identity;
- tool path/name/size/hash.

Because `go-standard` uses the same Go executable for all three commands, each snapshot deduplicates the executable within that snapshot and hashes `go.exe` once.

The verifier captures this snapshot both before and after the command suite.

## Freshness revalidation during integration

Integration does not trust a previously verified result blindly. It calls the fresh-result gate repeatedly around material integration boundaries.

For the normal successful path, source inspection shows `LatestFreshResult()` can be called:

1. before preparing a new integration attempt;
2. again while a PREPARED attempt is being driven before dispatch;
3. again after DISPATCHED but before authoritative ref advancement.

`LatestFreshResult()` calls verification `EvidenceFresh()`, which in turn:

- checks task/result/evidence/profile identity;
- validates workspace candidate revision;
- runs candidate-head/cleanliness checks;
- captures current verification environment identity again and compares its hash with the persisted evidence.

Combined with the verifier's before/after snapshots, a successful candidate can structurally hash the same verification tool binary roughly **five times** across verify + integrate, subject to exact recovery/branch path.

This repetition is an intentional freshness defense today. It is not automatically waste.

## Git freshness checks also repeat intentionally

Candidate freshness validation uses Git observations such as:

- `rev-parse --verify HEAD`;
- changed/diff path checks;
- clean-worktree checks.

Integration then additionally checks authoritative ref/head/cleanliness and ancestry, and rechecks these at critical PREPARED/DISPATCHED transitions before CAS-style ref advancement.

This protects against source/environment drift and crash windows. Any optimization must preserve the same material decision boundaries rather than merely reducing command count.

## Existing durable timing evidence

Verification command evidence already persists `DurationMS` for every profile command.

Therefore the Research Observer can measure without production instrumentation:

- test duration;
- vet duration;
- build duration;
- pass/fail per command;
- profile-level command sum.

What is not yet directly separated durably is:

- environment snapshot/hash duration;
- Git freshness-check duration;
- SQLite/evidence persistence duration;
- integration freshness-gate duration;
- authoritative Git CAS/update duration;
- connection-pool wait caused by unrelated observability/polling.

Keep these as `RESIDUAL_UNATTRIBUTED` until measured.

## Durable historical measurement — 2026-09-12

An external read-only collector now reconstructs verification/integration timing directly from durable MAR evidence. Performance summaries use only verification suites whose durable verdict is `PASS` and whose configured commands all report `passed=true`; failed suites remain visible for repair-loop analysis but do not contaminate successful latency baselines.

### Successful verification suites

Current durable sample:

```text
go-standard:
  PASS suites                         6
  median command-suite time        84.2 s
  max command-suite time          142.4 s
  median test                       48.6 s
  median vet                        24.4 s
  median build                       8.2 s

go-docs:
  PASS suites                         2
  command-suite times            144.4 s, 259.1 s
  median of this two-run sample    201.7 s
```

The `go-docs` sample is too small for a stable percentile claim. A historical third `go-docs` evidence row completed in only 32 ms because all three commands reported `passed=false` while using an older host Go path; it is retained as failed evidence and explicitly excluded from the successful performance baseline. It is **not** evidence of a 32 ms warm-cache verification path.

### Accepted V1.1 task decomposition

For accepted self-hosting task `task-b06d9ab9f1834e32996e6b5faef3f6e0`:

```text
task wall time                    422.176 s
Web cognition wait               122.269 s   29.0%
verification command sum         259.097 s   61.4%
RESIDUAL_UNATTRIBUTED             40.810 s    9.7%

verification commands:
  go test -run ^$ ./...           194.730 s
  go vet ./...                     48.964 s
  go build ./...                   15.403 s
```

This is the clearest current example where authoritative verification dominates task wall time. The result must not be generalized to all MAR tasks: among the six successful `go-standard` suites with task timing available, median verification-command share is only about **4.0%**, while median Web-wait share is about **65.6%** because several tasks are much longer cognition-heavy runs.

Therefore the optimization target is **task/profile-sensitive**, not a blanket weakening of verification.

### Failed-test evidence tail

Four historical `go-standard` suites have `go test` fail while subsequent `go vet` and `go build` still run successfully. The post-test-failure `vet + build` tail is about:

```text
29.5 s
30.8 s
32.1 s
32.1 s
median ≈ 31.5 s
```

This is measurable delay before repair feedback. It is not automatically waste: complete vet/build evidence may reveal additional defects or preserve stronger authoritative evidence. The right research question is whether MAR should distinguish a fast **repair-feedback gate** from final authoritative verification, while keeping the final acceptance gate unchanged.

### Integration attempt timing

Durable `integration_attempts.created_at -> updated_at` gives a bounded integration wall span for seven successful attempts:

```text
COMPLETE attempts                   7
median integration duration      1.251 s
max / current p95 sample         2.244 s
accepted V1.1 integration        2.244 s
```

In the current sample, integration itself is orders of magnitude smaller than verification command time. Repeated freshness checks remain correctness-sensitive and should not be removed, but integration is **not currently the first speed target**.

### Why cold verification is structurally plausible

Source inspection confirms verification commands run through the task's self-hosting-safe ACI runtime. For every Go command, ACI sets:

```text
GOCACHE    = <task-workspace>/.mar/go/build
GOMODCACHE = <task-workspace>/.mar/go/mod
GOTMPDIR   = <task-workspace>/.mar/go/tmp
GOPROXY    = file://<trusted shared module-download seed> when configured
```

The runtime itself explicitly notes that self-hosting verification frequently starts from a **task-local cold build cache** and therefore raises the command timeout rather than assuming the old two-minute fallback.

This isolation is intentional: it prevents weaker workers/tasks from sharing writable Go cache state and preserves deterministic task-local authority. The shared module source is exposed as a read-only file proxy; the writable extracted module/build caches remain task-local.

Consequences:

- a task that already ran relevant Go commands may partially warm its own cache before final verification;
- a docs-only or otherwise non-Go worker path can reach final verification with a cold build cache;
- cache warmth does not automatically carry across different task workspaces;
- replacing this with one shared writable build cache would reopen authority/contamination risks and is **not** an acceptable optimization shortcut.

The next cache experiment should therefore compare cold versus warm task-local caches and investigate safe seeding/reuse mechanisms that preserve task isolation, rather than restoring shared writes.

### Current interpretation

Measured priority is now:

1. preserve final verification correctness;
2. investigate `go-docs` / short-task verification cost and cache/environment identity;
3. benchmark fast repair feedback after a failed primary test versus complete evidence;
4. keep measuring `go-standard` verification, but do not assume it dominates long cognition-heavy tasks;
5. deprioritize integration micro-optimization unless future samples change its scale.

## Benchmark matrix

### V0 — tiny valid candidate

Use a stable fixture with a small source change and record:

- total verification wall time;
- test/vet/build durations;
- residual verification overhead;
- integration wall time.

### V1 — medium candidate

Repeat with a representative multi-file change.

### V2 — intentionally failing test

Measure the cost of continuing vet/build after a known failed test versus a research-only fail-fast simulation. No production policy change follows from this alone; complete evidence may justify the extra time.

### V3 — docs-only candidate

Measure `go-docs` wall time and determine whether vet/build dominate documentation-only releases.

### V4 — warm versus cold caches

Run identical disposable fixtures under controlled warm/cold Go cache conditions. Cache state must be part of benchmark identity or comparisons are invalid.

### V5 — Console closed/open

Repeat V0/V1 with Owner Console closed and open to detect R-016/R-017 contention during verification persistence/freshness reads.

## Metrics

Track:

```text
verification_wall_ms
command_test_ms
command_vet_ms
command_build_ms
verification_residual_ms
integration_wall_ms
verify_plus_integrate_ms
verify_share_of_goal_pct
environment_snapshot_count
fresh_result_gate_count
candidate_git_check_count
final_outcome
```

When possible also track:

- bytes hashed for environment identity;
- Git command count/duration;
- sql.DB wait around evidence/result/integration operations;
- warm/cold cache identity.

## Research hypotheses

### H1 — Complete command suite may dominate tiny-task latency

If a tiny edit spends far more time in broad test/vet/build than coding, that is not necessarily wrong: verified correctness is a product feature. But it changes where optimization effort should go.

### H2 — Fail-fast versus complete evidence is an explicit trade-off

For a candidate whose first required test fails, running vet/build can provide more diagnostic evidence but delays return-to-repair. Benchmark whether that evidence materially helps the next repair turn.

Potential future designs could distinguish:

- fast repair feedback inside worker loop;
- full authoritative verification only for a candidate believed ready.

MAR already has some separation between worker commands and final verifier; research should preserve it rather than weakening the final gate.

### H3 — Repeated freshness hashing may be optimizable only within one bounded authority window

A future implementation might avoid rehashing an unchanged tool binary at every internal sub-step if one cryptographically/OS-bound environment observation can be proven valid across the same short operation boundary.

However file replacement, toolchain drift and crash/recovery semantics make naive caching unsafe. No cache should survive a boundary unless its freshness identity and invalidation are explicit.

### H4 — Integration safety checks should be optimized by observation batching, not removal

If repeated Git queries become material, research whether a single bounded broker observation can return multiple identity facts atomically/consistently enough for one decision boundary. Do not simply remove rechecks around CAS/crash-sensitive transitions.

## Acceptance for any future verification optimization

Must preserve:

- criterion-specific acceptance evidence;
- candidate revision binding;
- environment/tool identity freshness;
- sandboxed verifier authority;
- stale candidate/environment rejection;
- authoritative-base drift rejection;
- crash-safe integration semantics;
- no false VERIFIED or COMPLETE state.

And must demonstrate material improvement on comparable fixtures in at least one of:

- verification wall time;
- repair-loop time after failed candidate;
- integration wall time;
- host resource consumption;

without increasing incorrect acceptance or stale integration.

## Phase-0 decision

`VERIFICATION_COMMAND_COST_CONFIRMED_MATERIAL; PROFILE_SENSITIVE`

Durable evidence proves that verification can dominate short/docs-only task latency and that failed-test complete-evidence tails add roughly half a minute before repair feedback in the observed `go-standard` failures. At the same time, successful long cognition-heavy tasks can spend far more time waiting for Web reasoning than verifying. Integration attempts are currently small (~1–2 s).

The next research step is therefore **not** to remove `test/vet/build`. It is to identify cache/environment regimes and benchmark a two-stage policy concept: fast repair feedback during convergence, then unchanged authoritative verification for candidate acceptance. Any V1.2 proposal must demonstrate that final correctness/freshness evidence remains equivalent.
