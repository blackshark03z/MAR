# MAR Verification Speed V1

**Status:** implementation candidate  
**Scope:** Go verification profile optimization only

## Goal

Reduce routine MAR verification latency without weakening candidate identity, acceptance evidence, authority fencing, integration rules, or release qualification.

The live pre-optimization read-only smoke on MAR HEAD `5a0f2965753253a39ef2d2ab0fda1352ff082822` took roughly **223 seconds** through the old `go-standard` full uncached path. The dominant cost was repeatedly executing the same full Go test suite even when the candidate did not change Go code.

## Research basis

### Go-native cache

The Go tool caches successful package test results in package-list mode such as `go test ./...`. Cache identity depends on the same test binary plus cacheable flags, and Go also tracks relevant files and environment inputs. The documented idiomatic way to disable this cache is `-count=1`.

Source: https://go.dev/src/cmd/go/internal/test/test.go

Speed V1 therefore removes `-count=1` from the daily gate instead of building a second cache system inside MAR.

### Speed V1.1 benchmark correction — shared GOCACHE

Live benchmarking after Speed V1 activation showed that enabling the Go cache flag path alone was insufficient:

- pre-optimization full uncached baseline: about **223 s**;
- first cache-enabled `go-standard` run on the same live HEAD: about **195.7 s**;
- second same-HEAD `go-standard` run: about **207.3 s**.

The expected warm-cache speedup did not appear because MAR still set a **task-local GOCACHE** under each disposable workspace (`workspace/.mar/go/build`). Workspace cleanup therefore discarded the build/test cache after every task.

Speed V1.1 moves only `GOCACHE` to MAR-owned durable storage under the resolved DataRoot (`runtime/go-build-cache`). The Windows LPAC receives an explicit writable grant only for this cache directory. `GOMODCACHE` and `GOTMPDIR` remain task-local, the shared module proxy remains read-only, and offline `GOPROXY`, `GOSUMDB=off`, `GOENV=off`, and `GOTOOLCHAIN=local` invariants remain unchanged.

The Go documentation states that the build cache is safe for concurrent Go command invocations and that cached build/test actions are keyed by the relevant build inputs. This makes a MAR-owned shared `GOCACHE` preferable to a custom cache protocol while preserving fail-closed filesystem isolation.

Source: https://pkg.go.dev/cmd/go#hdr-Build_and_test_caching

### Affected work before full work

Nx documents the same general optimization principle at project-graph scale: determine the minimum affected set from Git changes and dependency relationships, then run tasks only on that set. It also recommends pairing affected execution with caching.

Source: https://nx.dev/docs/features/ci-features/affected

MAR does **not** implement a custom Go affected-package graph in Speed V1 because the Go build/test cache already performs package/dependency invalidation at the toolchain level. A MAR-owned dependency engine would add correctness and maintenance risk before measurements justify it.

### Content-addressed reuse

Bazel models actions by declared inputs, command line, environment, and outputs, and reuses cached results when that identity matches.

Source: https://bazel.build/remote/caching

MAR keeps the same principle at a higher level: candidate revision, profile hash, environment identity, acceptance evidence, and integration rules remain authoritative. Speed V1 only allows the Go toolchain to reuse package test/build work that Go itself considers unchanged.

## Profiles

### go-standard — daily development default

Commands:

- `go test -p 1 -timeout 180s ./...`
- `go vet -p 1 ./...`
- `go build -p 1 ./...`

Properties:

- still covers the entire module package set;
- keeps the current conservative `-p 1` resource envelope;
- permits Go test cache reuse;
- removes verbose success output to reduce I/O and evidence/context noise;
- remains the recommended Go profile in project capability.

### go-docs — documentation-only fast path

Admission remains `documentation-only`.

Command:

- `go test -p 1 -run ^$ -timeout 180s ./...`

Separate full `go vet` and `go build` passes are intentionally removed. The Go test command still builds packages/tests and runs Go's default high-confidence vet subset while executing no tests because of `-run ^$`. Since the profile rejects source changes, repeating full source verification is unnecessary for this gate.

### go-release — explicit escalation

Commands preserve the former canonical gate:

- `go test -v -p 1 -count=1 -timeout 180s ./...`
- `go vet -p 1 ./...`
- `go build -p 1 ./...`

Use `go-release` for:

- Release Qualified decisions;
- runtime promotion/activation candidates;
- high-risk core verification when the Tech Lead intentionally requires an uncached rerun;
- independent release audits.

## Escalation policy

Default daily work uses `go-standard`.

Documentation-only changes use `go-docs`.

Release qualification and explicitly high-risk validation use `go-release`.

If a cache-related correctness concern appears, escalate the specific candidate to `go-release`; do not globally disable caching again without evidence.

## Deferred optimizations

Speed V1 deliberately defers:

- MAR-owned affected-package graph calculation;
- remote/distributed cache;
- parallelism changes beyond `-p 1`;
- cross-task verification-result reuse;
- Python verification optimization.

Those require measurements from real task telemetry before implementation.
