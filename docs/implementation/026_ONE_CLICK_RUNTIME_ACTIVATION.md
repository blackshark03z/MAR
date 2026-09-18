# MAR — One-click local runtime activation

**Status:** IMPLEMENTED CANDIDATE  
**Scope:** activation tooling only

MAR upgrades should not require ad-hoc build/copy/restart steps. The supported Windows-local entry point is:

`powershell -ExecutionPolicy Bypass -File .\scripts\activate-current-head.ps1`

The script resolves a clean Git HEAD, builds a provenance-bound runtime, generates the release manifest, backs up the current stable executable, release manifest, and SQLite `mar.db` / WAL / SHM files, then promotes through the existing `scripts/start-owner-console.ps1` launcher.

Activation passes only when the live loopback runtime reports the exact target `source_revision` and `trusted_for_release=true`.

After that trusted identity gate, activation performs bounded best-effort retention: it keeps the newest five marked `.mar/recovery/activation-*` backups and removes only older marked activation backups plus known activation candidate files in `.mar/runtime/staging`. Unknown recovery directories, cache directories, databases, active workspaces and active runtime files are outside retention scope. Retention failure emits a warning and does not roll back an otherwise successful activation.

If startup or identity verification fails, the script stops the attempted runtime, restores the previous runtime/database artifacts, restarts the prior runtime when it had been running, and verifies the prior revision when known. Recovery evidence remains under `.mar/recovery`.

This implementation task does not activate or restart the running MAR instance. It adds activation tooling only; product features, task semantics, remote Git, provider/tunnel configuration, and other projects remain unchanged.
