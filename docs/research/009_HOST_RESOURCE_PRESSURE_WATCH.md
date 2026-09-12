# R-009 — Host Resource Pressure Watch

**Status:** `RESEARCH_ONLY / ACTIVE`  
**Baseline:** MAR v1.1.0  
**MAR production-code changes:** none  
**Date:** 2026-09-12

## Purpose

Measure host RAM/Windows commit pressure and browser/MAR process memory independently from MAR so future Chrome OOM, pagefile and long-session failures can be correlated with actual host conditions instead of attributed to MAR by assumption.

## Why this is separate from MAR telemetry

A browser crash or Windows commit-pressure event can occur while MAR is not running. Conversely, MAR may be active while the host remains healthy. The research observer therefore needs a host baseline that exists independently of MAR task/runtime state.

## Hourly resource snapshot

`D:\MAR-Research\observer\resource_probe.py` is now part of the hourly external Research Observer snapshot.

It records only system/process metadata:

- Windows physical RAM load and available RAM;
- Windows commit used, commit limit and commit percentage via `GlobalMemoryStatusEx`;
- short CPU sample;
- process count and aggregate RSS for `mar`, `chrome`, `cloudflared`, `tunnel-client`;
- free-space/used percentage for C: and D:.

No process command lines, prompts, credentials or payload bodies are written into the resource report.

## Active resource watcher

External process:

```text
D:\MAR-Research\observer\resource_watch.py
```

Current-user Startup shortcut:

```text
MAR Research Resource Watch.lnk
```

The watcher samples:

- every 60 seconds while MAR local MCP is absent;
- every 10 seconds while MAR local MCP is observed on loopback 8788.

It writes:

```text
D:\MAR-Research\observer\resource_watch_state.json
D:\MAR-Research\observer\events\YYYY-MM-DD.ndjson
```

Only resource-pressure state changes are appended to the event log. The latest state keeps current/peak metadata since watcher start.

## Research-only pressure labels

Initial watcher labels are deliberately coarse and are **not product thresholds**:

- `NORMAL`: both Windows commit and RAM load below 80%;
- `ELEVATED`: commit or RAM load >=80%;
- `HIGH`: commit or RAM load >=90%.

These thresholds are for identifying research windows worth inspecting. They must not become MAR admission/resource-governor policy without independent evidence.

## Initial host baseline with MAR stopped

The first resource observations, while MAR itself was not running, were approximately:

```text
physical RAM total       15.34 GiB
RAM load                 62%
RAM available            5.8 GiB
Windows commit used      17.3 GiB
Windows commit limit     22.3 GiB
Windows commit pressure  ~78%
Chrome processes         28
Chrome aggregate RSS     ~4.5 GiB
MAR processes            0
MAR RSS                  0
C: free                  ~14.7 GiB
D: free                  ~8.3 GiB
```

This is a material research fact: the machine can already sit near 80% Windows commit pressure and several GiB of Chrome RSS with MAR absent. Therefore earlier Chrome OOM/pagefile observations cannot be causally assigned to MAR without before/during/after evidence.

## Research questions enabled

During real MAR use, correlate:

- MAR transition `NOT_RUNNING -> LOCAL_READY` with commit/RAM delta;
- MAR task start/end with host pressure delta;
- WebTurn/context growth with Chrome RSS/commit peaks;
- local disconnect transitions with host pressure peaks;
- Chrome crash/reload evidence with preceding commit/RAM state;
- worker/verification concurrency with MAR RSS and host commit.

The useful metric is **incremental pressure attributable during comparable MAR activity**, not absolute host use alone.

## Safety boundary

The resource observer does not:

- change pagefile configuration;
- kill/restart/throttle any process;
- change MAR resource limits;
- close Chrome tabs/processes;
- mutate MAR or Git;
- submit model/task work.

A previous attempt to replace a running research watcher by killing its process was blocked by the tool safety boundary. The research flow did not bypass that boundary; resource monitoring was instead added as a separate read-only sidecar.

## Next evidence gate

Collect at least several sessions containing both MAR-off and MAR-on periods. Then compute:

```text
baseline_commit_pct
MAR_active_commit_delta
baseline_chrome_rss
MAR_active_chrome_rss_delta
MAR_rss_peak
pressure-transition count/duration
```

Only if repeated comparable sessions show a material MAR-correlated increase should production resource/context changes be proposed.

## Current verdict

`HOST_BASELINE_COLLECTION_ACTIVE`

Automatic host/resource evidence is now being collected independently of MAR. No causal MAR OOM claim is established yet.
