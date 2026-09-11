# MAR V1.1 — Release Candidate Closeout

**Status:** `ENGINEERING_STABLE_PENDING_OWNER_UAT`  
**Authoritative base HEAD:** `224de951c82f2e5d8381e461f90fcc491bc1b2b8`  
**Scope:** Documentation-only closeout

## Release boundary

This record freezes the MAR V1.1 engineering release-candidate truth without changing runtime behavior, execution authority, transport configuration, database state, credentials, generated production assets, or deployment state. It does **not** claim `PRODUCT_ACCEPTED` or `SELF_HOSTING_READY`; those labels remain reserved for explicit Owner real-use acceptance.

The engineering line includes the bounded Decision Projection/context architecture, bounded Web episodes and task-wide convergence budgets, the contracted public MCP surface, the React 19 + TypeScript + Vite Owner Console with Lucide icons, workspace/task interaction corrections, and the Claude Web connection UX closeout. The authoritative base for this closeout is the HEAD above.

## Live MCP discovery and cached-client compatibility

A live MCP client against the running MAR bridge observed exactly six canonical tools in `tools/list`:

`brain_respond, brain_turn, control, project, submit, task`

The legacy aliases `project_context`, `project_read`, `status`, `result`, `inspect`, `steer`, `input`, and `cancel` remain callable-but-unlisted for this release so already-open ChatGPT/plugin clients with cached schemas do not break. Canonical and legacy compatibility calls share the same MAR backend/service authority path; the public discovery list must not be re-expanded.

## Owner Console and Claude Web truth

The Owner Console is the integrated React presentation served from the Go MAR binary. Owner UAT corrections cover task-list overflow, workspace selection and folder browsing, shell alignment, responsive layout, and the Claude Web ready-state connection card.

Claude Web currently uses a **temporary Quick Tunnel** capability route. When the route is ready, the Console shows the active capability URL with copy, diagnose, and detail actions. A temporary Quick Tunnel hostname may change after MAR restarts. A stable named-tunnel/custom-host URL remains optional external hardening and is not required by this release-candidate closeout.

## Real-use probe and host resource recovery

The immediately preceding real-use release probe correctly failed closed during verification when the configured **host disk-reserve pressure** guard was crossed. This was an environmental resource-admission event, not evidence of a source/candidate failure: the exact sealed candidate subsequently passed the `go-docs` commands when run after diagnosis, and an isolated verifier reproduction persisted a VERIFIED result successfully.

Recovery did not alter runtime/source truth. Only regenerable MAR task-local Go caches under `.mar/go` for COMPLETE/CANCELLED tasks, the blocked release probe cache, and temporary diagnostic files were removed. No repository source, sealed candidate, durable TaskResult/evidence, SQLite authority, credentials, or user project data was deleted. The cleanup reclaimed about 3.69 GiB and restored observed D: free space from below the configured 2 GiB reserve to about 5.94 GiB before this fresh probe. This fresh task, not the blocked predecessor, is the authoritative release verification attempt.

## Engineering verification boundary

Repository handoff evidence already records successful React TypeScript/Vite production build, Owner UI regressions and browser interaction smoke, full sequential repository tests, `internal/verification` recheck, `go vet -p 1 ./...`, `go build -p 1 ./...`, and `git diff --check` on the implementation line. This documentation candidate must additionally pass MAR's `go-docs` profile, criterion-specific acceptance oracles, revision/environment freshness, and authoritative integration before the release candidate is considered engineering-closed.

## Remaining product gate

After successful MAR verification and integration, the remaining product gate is explicit Owner real-use acceptance of the integrated experience. Engineering PASS, clean Git, hashes, MCP tool discovery, successful connection probes, or this document alone cannot establish `PRODUCT_ACCEPTED`.

No push, deploy, remote Git write, stable Cloudflare named-tunnel setup, credential change, or source/runtime modification belongs to this closeout.
