# R-025 — Connection self-healing and health semantics

**Status:** `RESEARCH_ONLY`  
**Baseline:** MAR v1.1.0  
**Code changes authorized:** none  
**Date:** 2026-09-12

## Research question

When a GPT Secure Tunnel, Quick Tunnel, local bridge, or remote request fails after an initially healthy connection, does MAR recover automatically and does its telemetry distinguish route reachability from end-to-end application success?

## OpenAI Secure Tunnel — current lifecycle

### Startup

When the Owner UI starts, persisted `DesiredRunning=true` causes one asynchronous `Start()`.

`Start()` performs bounded preparation before launching the tunnel process:

- tunnel-client `init`: up to 30 s;
- tunnel-client `doctor`: up to 45 s;
- then launches `tunnel-client run`;
- readiness is established from admin health/readiness probes or observed MCP traffic.

This is safe/fail-closed but can have a long worst-case startup envelope before an error is returned.

### Health

When a tunnel-client admin endpoint is known, MAR probes `/healthz` and `/readyz` on a one-second health loop. Each individual probe has a two-second timeout.

The loop is synchronous, so a degraded admin endpoint can lengthen the effective probe cycle beyond one second rather than creating unbounded overlapping probe goroutines.

### Process crash

`watchProcess()` detects owned tunnel-client exit, clears running/readiness state and records the error. It does **not** call `Start()` again.

Existing regression coverage explicitly expects a simulated crash with `DesiredRunning=true` to result in:

```text
Status = ERROR
Running = false
Connected = false
Healthy = false
Ready = false
```

The test then states that a **new MAR process** may observe `desired_running` and start fresh. Therefore V1.1 desired state currently provides boot-time restoration, not same-process crash self-healing.

## Remote MCP bridge / Quick Tunnel — current lifecycle

### Startup robustness

Temporary MCP Link startup is defensive during initial establishment:

- up to three bounded start attempts;
- each attempt can create a fresh Quick Tunnel;
- public token-bound health is verified before the link is accepted;
- a failed attempt stops the tunnel before retrying.

This protects startup from a dead/unpropagated temporary hostname.

The configured timeout envelope is still material for startup-speed research. `startCloudflaredQuickTunnel` may wait up to 30 seconds for a published URL, then each accepted attempt is given up to 15 seconds for the token-bound public readiness check. With three bounded attempts, the theoretical configured upper envelope before final failure is therefore roughly 135 seconds, excluding small cleanup/scheduling overhead. Normal startup should be much faster; benchmark actual median/p95 rather than treating the timeout envelope as typical latency.

### Runtime tunnel exit

After a Quick Tunnel is established, `watchTunnel()` observes the tunnel process. If it exits, the error is recorded and `StopTemporary()` clears temporary route state. No new `StartTemporary()` is scheduled.

Therefore mid-session Quick Tunnel loss also requires an external/manual restart or MAR process restart to recreate the temporary link.

A further constraint is structural: Cloudflare Quick Tunnels generate a random `trycloudflare.com` hostname for a new tunnel process and are explicitly a development/testing path rather than a stable production endpoint. Therefore even a future automatic `cloudflared` restart would not by itself guarantee transparent client recovery: ChatGPT/Claude may still hold the old public URL. Reliable unattended recovery for a client configured with a fixed endpoint requires a stable/named route or another mechanism that preserves endpoint identity.

This means self-healing policy must be transport-specific:

- OpenAI Secure Tunnel can potentially restart while preserving its configured tunnel identity, subject to fresh readiness;
- stable/named remote routes can recover behind a fixed public endpoint when their underlying tunnel infrastructure supports it;
- Quick Tunnel restart is primarily route regeneration and may still require client reconfiguration.

### Stable route

Stable connector URLs are health-probed every five seconds. A failed probe marks the route offline, but the manager has no active repair action because MAR does not own the external stable route lifecycle in the same way it owns a temporary cloudflared process.

That distinction is appropriate: detection and repair authority are separate.

## Health semantics — request arrival versus successful tool outcome

`internal/mcpedge/http.go` calls the configured `Observe(RemoteHTTPEvent)` **before** delegating to `streamable.ServeHTTP`.

The event contains method/session/host/origin but no response status, duration or backend outcome.

### OpenAI tunnel

`observeMCP()` currently treats any observed request arrival as:

```text
lastActivityAt = now
lastSuccessAt = now
healthy = true
ready = true
```

Thus a request that successfully reaches the MAR MCP handler can establish connection truth even if the subsequent MCP/backend operation returns an application error.

### Stateful remote bridge

Connector telemetry similarly increments request/session activity and marks `initialize` / `tools/list` observations based on request method arrival, not successful response completion.

This is useful evidence of **route reachability and real client activity**, but it is not end-to-end application-success telemetry.

The main risk is semantic naming/interpretation, not necessarily routing correctness.

## Stale-connected windows

### Secure Tunnel without an admin health endpoint

The Secure Tunnel manager deliberately allows real MCP activity to establish connection truth even when an admin endpoint was not discovered. This is useful because a healthy connection must not depend on parsing one optional process log line.

However, once `observeMCP()` sets `healthy=true` and `ready=true`, `stateLocked()` considers the tunnel connected while the owned process remains alive. If no admin base URL is available, `refreshHealth()` has no active probe and simply returns. There is currently no age/TTL check against `lastActivityAt` for this fallback mode.

Therefore a process that remains alive after a downstream/control-plane path failure can theoretically retain stale `CONNECTED` truth indefinitely until another lifecycle event changes the flags.

This is a research risk, not proof of a real outage mode on the current tunnel-client. Benchmark H3/H4 should include a live-process / broken-route case.

### Temporary Quick Tunnel

Temporary route readiness is continuously inferred from `quickBaseURL != ""` after startup; the public token-bound health round trip is not repeated periodically for the temporary URL.

If MCP session IDs have been observed, session telemetry prunes them only after the 30-minute MCP session timeout. A dead public route with a still-live cloudflared process can therefore retain `CONNECTED` semantics for part of that session window unless another signal resets the tunnel/telemetry.

If session identity is unavailable, fallback connection status uses a much shorter recent-activity window (45 seconds), so stale connected state is naturally more bounded.

Stable configured routes are different: they are public-health-probed every five seconds and can become `ROUTE_OFFLINE` independently of process/session activity.

This creates a useful benchmark target: deliberately break route reachability while keeping the local process alive and measure how long each transport mode continues to report connected/ready.

## Why this matters

The Owner's desired behavior is closer to:

> once configured, GPT/Claude connectivity should recover from ordinary transient process/tunnel failures without requiring repeated manual intervention, while never hiding prolonged or unsafe failure.

V1.1 currently prioritizes truthful failure state and fail-closed lifecycle over same-process automatic repair. That is a reasonable stable baseline, but continuous-use reliability research should test whether bounded self-healing would materially improve availability.

## Benchmark matrix

### H0 — healthy idle

Measure connection uptime and health noise with no injected fault.

### H1 — owned tunnel-client crash

Kill only the owned tunnel-client in a controlled fixture and measure detection latency, resulting MAR state, durable-task impact, manual actions required and time-to-restored connection.

### H2 — Quick Tunnel cloudflared exit

Inject controlled temporary tunnel exit. Measure the same fields and whether public URL identity changes after recovery.

### H3 — admin health failure while process remains alive

Make `/healthz` or `/readyz` fail/timeout without killing the tunnel process. Measure detection time, probe overhead, transition flapping and recovery after the health endpoint restores.

### H4 — remote request reaches MAR but tool returns application error

Verify connection telemetry remains distinguishable from tool success/failure.

### H5 — transient repeated failure

Use bounded repeated faults to evaluate whether any future self-heal policy would create a restart storm.

## Candidate future policy to test, not implement

A bounded same-process supervisor could conceptually use:

```text
desired_running
+ owned-process confirmed exit
+ safe configuration/auth still valid
+ exponential/jittered backoff
+ finite restart budget/window
+ fresh readiness required
```

Properties required before considering it:

- never start a second process while old termination is unconfirmed;
- never reuse stale connected/readiness state;
- stop immediately when Owner sets desired_running=false;
- no infinite restart storm;
- preserve the same tunnel identity for Secure Tunnel where appropriate;
- Quick Tunnel may receive a new temporary hostname and the UI/client implications must be explicit;
- failures remain visible during recovery rather than being hidden.

## Telemetry semantics candidate

Research should distinguish at least:

```text
route_reachable
request_observed
mcp_response_success
application_tool_success
```

Do not call request arrival `last_success` if the product/UI interprets that as a successful tool operation.

This may require no new durable schema if external benchmark evidence shows route-level telemetry is sufficient; the distinction must first be tested in failure scenarios.

## Phase-0 verdict

`MATERIAL_RELIABILITY_HYPOTHESIS`

V1.1 detects connection-process failure truthfully and restores desired GPT tunnel state on MAR restart, but it does not currently self-heal owned tunnel/Quick Tunnel crashes inside the same running Owner Console process. Connection telemetry also reflects request arrival more strongly than end-to-end operation success.

Benchmark H0-H5 before deciding whether V1.2 should add bounded connection supervision or richer transport-outcome telemetry.
