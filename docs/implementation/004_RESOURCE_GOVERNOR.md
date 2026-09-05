# Slice 004 — Resource Governor / Admission Backpressure

**Architecture:** FROZEN
**Status:** COMPLETE / VERIFIED

## Goal

Apply resource backpressure before a personal workstation reaches CPU/RAM/disk/process exhaustion.

## Design

The slice deliberately separates:

- **policy/admission** (`resourcegov.Governor`), which is deterministic and unit-testable;
- **host observation** (`WindowsSensor`), which reads live Windows CPU/RAM/disk/user-idle state;
- **OS process containment**, already provided by Slice 003 Job Objects.

A resource `Claim` reserves estimated RAM/disk headroom and declares whether the workload is heavy. `TryAcquire` returns an idempotently releasable lease only if all hard/soft gates pass.

### Hard gates

- minimum host free-RAM reserve;
- minimum host free-disk reserve;
- maximum total MAR disk budget;
- sensor failure fails closed.

### Heavy-work gates

- live CPU pressure;
- live memory-load pressure;
- optional I/O pressure when a sensor is available;
- global heavy-job capacity;
- per-project heavy-job capacity;
- lower heavy capacity while the owner is actively using the machine.

Lightweight/search/model-wait work may continue under CPU/memory pressure as long as hard physical reserves remain safe.

## Windows sensor

- CPU: `GetSystemTimes` deltas; first sample is explicitly unknown rather than fabricated.
- RAM: `GlobalMemoryStatusEx`.
- Disk: `GetDiskFreeSpaceEx`.
- User activity: `GetLastInputInfo` against a configured idle threshold.
- MAR disk consumption: bounded root scan with optional short cache TTL.
- I/O pressure remains `unknown` in this slice; the policy supports it when a future/available sensor supplies it.

## Important boundary

This slice is **admission control**, not the scheduler queue. It returns explicit denial reasons. A later scheduler decides retry/priority/aging, preserving the frozen rule that one project must not starve others.

Numeric thresholds are configuration/benchmark inputs, not architecture constants.

## Acceptance

1. Disk-reserve pressure denies work before host reserve is consumed.
2. MAR aggregate disk budget includes active reservations.
3. High memory/CPU/I/O pressure blocks heavy work but permits lightweight work when hard reserves are safe.
4. Global and per-project heavy capacity are enforced under concurrency.
5. Interactive-owner mode reduces heavy capacity.
6. Lease release is idempotent and returns capacity.
7. Sensor failure fails closed.
8. Windows sensor returns real CPU/RAM/disk/MAR-disk measurements.
9. Existing Slice 001–003 tests remain green.
10. `go test ./...`, `go vet ./...`, and Windows build pass.

## Evidence

- Hard host disk-reserve denial: PASS.
- MAR aggregate disk budget + active reservation accounting: PASS.
- High memory pressure blocks heavy / allows lightweight: PASS.
- CPU + optional I/O pressure heavy throttling: PASS.
- Global/per-project/interactive heavy capacity: PASS.
- Concurrent heavy admission never exceeds configured capacity: PASS.
- Lease release is idempotent and returns capacity: PASS.
- Sensor failure fails closed: PASS.
- Real Windows CPU/RAM/disk/user-idle/MAR-disk sensor test: PASS.
- `go test -count=1 ./...`: PASS.
- `go vet ./...`: PASS.
- Windows `cmd/mar` build: PASS.
- `git diff --check`: PASS.
- Go race detector was attempted but is unavailable on this host because the installed C compiler cannot build 64-bit race/cgo runtime (`cc1.exe: 64-bit mode not compiled in`). This is recorded as a toolchain limitation, not a passing test.

Final implementation revision is recorded by the Git commit containing this document.
