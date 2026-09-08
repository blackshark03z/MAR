# MAR V1 Remote Web MCP Bridge

## Purpose

Allow a cloud Tech Lead client such as Claude Web to reach the same MAR MCP task control surface without changing MAR execution authority.

The bridge is transport only. It does not own tasks, workers, verification, integration, project policy, or durable state. MAR SQLite and the existing TaskService/runtime remain authoritative.

## V1 flow

```text
Claude Web / compatible remote MCP client
  -> HTTPS temporary tunnel URL
  -> outbound tunnel process
  -> loopback Streamable HTTP endpoint
  -> mcpedge public task tools
  -> same TaskService / SQLite authority
  -> worker / verification / integration
```

Local stdio remains supported. Claude Desktop is optional and is not required for the Web path.

## Security boundary

- Local HTTP listener binds only to `127.0.0.1` on an ephemeral port.
- Every Start generates a fresh 256-bit random token embedded in the MCP URL path.
- Unknown token/path returns 404 before MCP handling.
- Browser-style `Origin` is rejected unless it is HTTPS from the configured Claude origin allowlist; server-to-server requests may omit Origin.
- Request bodies are bounded to 1 MiB.
- Streamable HTTP uses JSON responses; MAR does not require standalone SSE for the public task-control flow.
- The SDK localhost Host protection is disabled only behind the token/Origin wrapper because the tunnel forwards the external Host to the loopback listener.
- The full capability URL is secret and is never written to MAR SQLite/task history.
- Stop or Console shutdown terminates the tunnel/listener and revokes the URL. Restart creates a different token/link.

## Truthful readiness

The tunnel may print a public hostname before DNS/edge propagation completes. MAR therefore performs a token-bound `/health/<token>` round trip through the public URL before returning `LINK_READY`. V1 allows a bounded 90-second readiness window because observed Quick Tunnel DNS propagation can exceed 30 seconds after a cold start. Network diagnostics retain the useful DNS/connect cause but redact the capability token and never persist the secret health/MCP URL as an error string.

`LINK_READY` means only that the public route reaches the local bridge.

`CONNECTED` is not inferred from configuration. MAR promotes to connected only after the bridge observes an actual MCP `initialize`; it separately records whether `tools/list` was observed.

## Tunnel provider

The zero-configuration V1/UAT path uses `cloudflared` Quick Tunnel as an external process. It holds no MAR authority and no MAR database. The executable may live at `<data-root>/runtime/cloudflared.exe` or on PATH.

Quick Tunnel is temporary by design. A persistent named tunnel/domain is an optional deployment concern and must remain an adapter around this HTTP edge rather than becoming a second MAR coordinator.

## Acceptance evidence

The remote bridge slice is not accepted by unit tests alone. Required evidence includes:

1. secure handler tests: secret path, foreign Origin rejection, exact public tool surface;
2. lifecycle tests: Start -> initialize/tools-list telemetry -> Stop -> endpoint revoked;
3. public internet probe through a real tunnel URL;
4. clean-candidate full Goal through the public remote MCP URL;
5. final Owner UAT in the actual Web client.
