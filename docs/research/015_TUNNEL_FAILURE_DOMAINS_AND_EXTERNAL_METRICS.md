# R-015 — Tunnel failure domains and external metrics

**Status:** `RESEARCH_ONLY`  
**Date:** 2026-09-12  
**Verdict:** `EXTERNAL_OBSERVABILITY_AVAILABLE`

## Research question

Can MAR connection failures such as 502/504, reconnect delays and tunnel instability be attributed automatically without first modifying MAR production telemetry?

The answer is partially yes. `cloudflared` already exposes enough local metrics and diagnostics to separate several important failure domains.

This document does not authorize tunnel reconfiguration, a named-tunnel migration, Prometheus/Grafana installation, or MAR code changes.

## Failure path to measure

For a remote MCP request, do not treat "502" as one failure class. Model the path as distinct hops:

```text
client / Chat host
    -> Cloudflare edge
    -> cloudflared tunnel connection
    -> cloudflared -> local origin (MAR bridge)
    -> MAR HTTP/MCP handler
    -> durable MAR application operation
```

A failure at each boundary has different recovery semantics.

### F0 — Client / platform failure

Examples:

- Chat tool call times out before response;
- browser or host loses the request;
- platform safety/client layer refuses a retry;
- caller receives a generic upstream error while backend work may already exist.

The ambiguous-ACK evidence observed during R-011 belongs here until lower-hop evidence identifies a more specific source.

### F1 — Cloudflare edge / tunnel-network failure

Named Cloudflare Tunnel normally maintains four outbound connections across at least two Cloudflare data centers. Connection loss can therefore be masked by another healthy connection.

Relevant metrics include active HA connections and edge/QUIC connection health.

### F2 — cloudflared cannot reach MAR origin

Cloudflare documentation explicitly distinguishes a tunnel that is connected to Cloudflare but returns 502 because `cloudflared` cannot reach the configured local origin. This is materially different from a disconnected tunnel.

Examples:

- MAR local bridge is not listening;
- wrong local target/port;
- process stopped/restarted;
- localhost connection refused.

### F3 — MAR HTTP/MCP request failure

Examples:

- request body rejected;
- origin/path rejection;
- protocol/session mismatch;
- handler error;
- request context cancelled;
- application error returned through JSON-RPC.

### F4 — Durable application ambiguity

The HTTP request may fail while a durable `submit`, control, or brain response has already committed. Recovery here is governed by MAR idempotency and durable task/turn identity, not by tunnel health.

## Quick Tunnel versus named tunnel

Current Claude/GPT MCP Link work may use a temporary Quick Tunnel path.

Cloudflare explicitly documents Quick Tunnels as development/testing infrastructure:

- no SLA or uptime guarantee;
- currently limited to 200 concurrent in-flight requests;
- no Server-Sent Events support;
- random/temporary hostname behavior.

These facts make Quick Tunnel unsuitable as the sole evidence source for MAR transport reliability. A Quick Tunnel outage must not automatically be classified as a MAR regression.

A managed/named tunnel has stronger availability primitives. `cloudflared` normally establishes four outbound connections to at least two data centers, and additional replicas can add independent ingress points. This is a future comparison target, not an automatic V1.2 requirement.

## cloudflared metrics already available externally

Cloudflare documents a Prometheus metrics endpoint started by `cloudflared`, normally on loopback port range `20241`–`20245` unless configured otherwise.

Metrics useful to MAR research include:

- `cloudflared_tunnel_ha_connections` — active HA connections;
- `cloudflared_tunnel_active_streams` — active streams;
- `cloudflared_tunnel_concurrent_requests_per_tunnel` — in-flight request load;
- `cloudflared_tunnel_request_errors` — errors proxying to origin;
- `cloudflared_tunnel_server_locations` — connected edge locations;
- `cloudflared_tunnel_timer_retries` — unacknowledged heartbeat retries where available;
- `quic_client_smoothed_rtt` / `quic_client_min_rtt` — tunnel-network RTT;
- process/runtime metrics for cloudflared itself.

This creates an important research opportunity: scrape these metrics from the external MAR Research Observer and correlate them with MAR local-port health and caller-visible errors.

## Proposed external correlation model

For every observer sample or transport incident, capture three independent views:

```text
A. caller result
   status/error + wall time

B. cloudflared view
   HA connections + request errors + active streams + RTT

C. MAR-local view
   8788/health reachable + process identity + task durable state
```

Then classify the incident conservatively.

Example classifications:

### `EDGE_OR_TUNNEL_NETWORK_SIGNAL`

Caller fails while local MAR is healthy, cloudflared origin errors do not rise, and HA/QUIC connectivity degrades.

### `LOCAL_ORIGIN_UNREACHABLE`

Caller receives 502, tunnel is still connected, `cloudflared_tunnel_request_errors` rises, and MAR local health/port is unavailable.

### `MAR_APPLICATION_ERROR`

Tunnel metrics and local transport are healthy, request reaches MAR, and MAR returns a typed application error.

### `AMBIGUOUS_ACK`

Caller reports failure but MAR durable state proves the operation committed. Retry must use the same semantic identity.

### `INSUFFICIENT_EVIDENCE`

Required hop data is absent or sampling resolution is too coarse. Do not guess the owner.

## Observer cadence

Do not install a full Prometheus/Grafana stack merely for MAR research.

A lightweight observer can scrape the local `cloudflared` metrics endpoint directly:

- inactive/MAR-offline: every 30–60 seconds;
- MAR active or recent transport error: every 5 seconds;
- preserve state transitions and selected counters/gauges rather than the full metrics payload;
- 14-day research retention is sufficient initially.

The observer should discover the active local metrics endpoint from process/log evidence rather than assume a fixed port if the default range is exhausted.

## Benchmark use

Future fault injection should deliberately separate:

1. close/restart MAR local origin while cloudflared stays healthy;
2. interrupt cloudflared while MAR stays healthy;
3. interrupt host network / QUIC/TCP path;
4. break client HTTP request after durable MAR commit;
5. restart tunnel and reconnect with a changed temporary hostname where applicable.

For each case, the observer should identify the expected failure domain without inspecting private payload content.

## Disposable fault-injection evidence — 2026-09-12

A research-only Quick Tunnel was started with the same managed `cloudflared.exe` binary used by MAR, but pointed at a disposable local HTTP origin under `D:\\MAR-Research`. Production MAR ports, DB and tunnel processes were untouched. Only the SHA-256 of the temporary public URL was retained.

The healthy control produced local HTTP 200, public HTTP 200, `cloudflared_tunnel_ha_connections=1` and `request_errors=0`.

### Origin down while tunnel remains alive

The disposable origin was stopped while `cloudflared` remained connected:

```text
local origin                  unreachable
HA connections               1
request_errors               0 -> 1
public result                HTTP 502
classification               LOCAL_ORIGIN_UNREACHABLE
```

Restarting the same local origin restored public HTTP 200 through the existing Quick Tunnel without recreating the tunnel. This proves the external correlation model can distinguish an origin outage from a tunnel-network disconnect.

### Tunnel process down while origin remains alive

Next, the disposable `cloudflared` process was stopped while the local origin remained HTTP 200:

```text
local origin                  HTTP 200
cloudflared metrics           unavailable
public result                 HTTP 502
classification               TUNNEL_PROCESS_OR_METRICS_UNAVAILABLE
```

This is a distinct signature from origin-down: local service remains healthy but the owned transport process/metrics surface disappears.

These qualitative failure-domain signatures require only one controlled reproduction. They do not establish production incident frequency or Quick Tunnel SLA quality.

## Promotion gate

Before changing MAR transport architecture because of 502/504 incidents, collect enough correlated incidents to answer:

- what percentage originate before MAR versus inside MAR;
- whether Quick Tunnel instability materially dominates;
- whether local-origin downtime dominates;
- whether named-tunnel redundancy would address the measured failure class;
- whether stateless MCP reduces recovery calls independent of tunnel behavior.

Only then decide whether V1.2 needs transport changes, tunnel hardening, internal telemetry, or no MAR change at all.

## External references

- Cloudflare Quick Tunnels: https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/trycloudflare/
- Cloudflare Tunnel configuration / HA: https://developers.cloudflare.com/tunnel/configuration/
- Cloudflare Tunnel monitoring: https://developers.cloudflare.com/tunnel/monitoring/
- Cloudflare Tunnel troubleshooting: https://developers.cloudflare.com/tunnel/troubleshooting/
