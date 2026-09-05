# Slice 009 — Coding ACI / Tool Runtime

**Architecture:** FROZEN
**Bootstrap milestone:** `MAR_BOOTSTRAP_STABLE -> SELF_HOSTING_READY`
**Status:** COMPLETE / VERIFIED

## Goal

Provide the bounded coding-tool surface required by the autonomous model loop while keeping workspace mutation explicit, revision-bound, and physically contained.

## Tool surface

- workspace-relative bounded file reads with SHA-256 revision identity;
- bounded lexical search;
- hash-bound whole-file create/replace;
- hash-bound exact search/replace patching;
- Git status/diff through typed command policy;
- bounded contained verification commands for Go bootstrap development.

## Security boundary

`Windows Job Object` provides process-tree containment, not a filesystem/network sandbox.

Therefore:

- `ContainedHostExecutor` reports `TRUSTED_HOST`, not `ENFORCED_SANDBOX`;
- command execution defaults to denied when only a trusted-host executor is present;
- direct-development tests may explicitly allow trusted command execution;
- `Runtime.SelfHostingSafe()` is false until a future OS-enforced sandbox executor is wired.

This is intentional. MAR must not claim `SELF_HOSTING_READY` while model-controlled commands still have ordinary host-user authority.

## Mutation rules

- model-facing paths are workspace-relative;
- direct `.git` access is forbidden;
- write/patch requires either `ABSENT` or the exact expected SHA-256;
- write paths reject symlink components;
- writes use same-directory temp + atomic rename;
- command output is memory-bounded;
- generic command execution is allow-listed to bootstrap-safe Git/Go shapes.

## Scope boundary

This slice does not implement:
- autonomous model loop;
- semantic context retrieval;
- OS security sandbox;
- MCP control surface;
- integration transaction.

## Acceptance

1. path traversal and direct `.git` access are rejected;
2. read output is bounded and returns file hash;
3. search results are bounded;
4. stale hash mutation is rejected;
5. exact patch count mismatch is rejected;
6. symlink mutation escape is rejected;
7. command output is bounded in memory;
8. unapproved command/subcommand shapes are rejected;
9. trusted-host executor is not self-hosting safe by default;
10. full existing suite remains green.

## Evidence

- Targeted `./internal/aci ./internal/processctl`: PASS.
- Tool definition schemas validated as JSON and tool dispatcher round-trip tests PASS.
- Path traversal, direct `.git`, stale hash, exact-count mismatch, and symlink write escape tests PASS.
- Contained command output flood is truncated within configured memory bound.
- Windows integration `ACI -> ContainedHostExecutor -> Job Object -> git status/diff`: PASS.
- `ContainedHostExecutor` remains explicitly `TRUSTED_HOST`; `SelfHostingSafe()` returns false.
- Full `go test -count=1 -timeout 120s ./...`: PASS.
- `go vet ./...`: PASS.
- Windows build `./cmd/mar`: PASS.
- `git diff --check`: PASS.
- Final implementation revision is the Git commit containing this document.
