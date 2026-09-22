# Web Reasoning Configuration Delegation

Date: 2026-09-22
Status: VERIFIED CANDIDATE
Architecture authority: docs/architecture/MAR_ARCHITECTURE_STABILITY_POLICY.md
Baseline: 7756e130e9c630fcd63533296a5bad1e70a31f70

## Goal

Stop MAR's default Web-cognition path from selecting a model-specific reasoning effort while preserving explicit provider compatibility.

## Change

- Web `mcp-stdio` runtime clears `ReasoningEffort`.
- Owner UI Web child launch no longer forwards `-reasoning`.
- Claude Desktop/Web stdio package no longer pins either `-model` or `-reasoning`.
- Web E2E profiles no longer configure a reasoning effort.
- The generic request schema remains backward-compatible and still permits an optional `reasoning_effort` field.
- OpenAI-compatible provider mode continues to forward an explicitly configured reasoning effort.

No durable schema or lifecycle was added.

## Evidence

Guard:
- `TestWebStdioArgsDoNotPinModelOrReasoning`: PASS.

Provider compatibility:
- `TestClientMapsToolConversationAndUsage`: PASS and observes `reasoning_effort=high` when explicitly configured.

Real Web E2E:
- `TestRuntimeE2EWebBrainMCPWorkerVerifyIntegrate`: PASS in ~41.42s.
- The pending Web turn is asserted to contain both `model=""` and `reasoning_effort=""`.
- Candidate mutation, verification and integration still complete.

Full repository:
- `go test ./...`: PASS.
- wall time ~98.5s because the changed orchestrator E2E reran.
- `cmd/mar` ~14.8s.
- `internal/orchestrator` ~91.6s.
- `internal/model/openaichat` ~4.9s.

## Architectural consequence

MAR's default Web path no longer chooses either:
- a model identity; or
- a model-specific reasoning effort.

Those choices belong to ChatWeb.

Provider mode remains an explicit compatibility path and may continue to carry provider-specific model/reasoning configuration.

This is another responsibility reduction on the frozen architecture, not a redesign.
