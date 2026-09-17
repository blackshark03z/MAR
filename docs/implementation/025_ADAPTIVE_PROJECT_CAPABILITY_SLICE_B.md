# MAR V1.3 — Adaptive Project Capability Slice B

**Status:** IMPLEMENTED CANDIDATE  
**Scope:** bounded project-context capability detection only

## Goal

Stop the Tech Lead from having to guess a verification profile for common Go and Python repositories. `project_context` now returns deterministic capability facts derived from a fixed set of project-root marker files while preserving the existing project ID, current HEAD, and policy fields.

This slice does not add `verification_profile=auto`. The Tech Lead still compiles one concrete immutable Goal Contract profile, but it must use MAR's returned capability guidance rather than model guesswork.

## Bounded marker detection

Detection performs only root-level existence checks for this fixed marker set:

- Go: `go.mod`
- Python: `pyproject.toml`, `setup.py`, `setup.cfg`, `requirements.txt`, `requirements-dev.txt`

There is no recursive repository scan, package-manager probe, language server, provider call, network access, or background index.

The projection exposes:

- capability state;
- detected ecosystems and languages;
- the exact evidence marker names found;
- supported verification profiles;
- a recommended verification profile only when one ecosystem family is unambiguous.

## Decision table

| Root capability | State | Supported profiles | Recommendation |
| --- | --- | --- | --- |
| Go only | `supported` | `go-standard`, `go-docs` | `go-standard` |
| Python only | `supported` | `python-standard` | `python-standard` |
| Go + Python | `mixed` | all applicable profiles | none |
| No supported marker | `unknown` | none | none |

Mixed and unknown repositories deliberately do not fabricate a single recommendation.

## Invariants

This slice does not change task authority, Goal Contract immutability, verification execution, SQLite schema semantics, task/attempt/workspace/run-epoch fencing, integration, runtime activation, tunnel/provider behavior, or Owner Console design. The existing verification profiles remain the only accepted concrete profiles.
