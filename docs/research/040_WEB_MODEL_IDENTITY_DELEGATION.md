# Web Model Identity Delegation

Date: 2026-09-22  
Status: VERIFIED CANDIDATE  
Architecture authority: docs/architecture/MAR_ARCHITECTURE_STABILITY_POLICY.md  
Baseline: 79c70933444a9465b591e8e558cedfbe16f62a6a

## Goal

Remove model identity ownership from MAR's default Web-cognition path without changing provider compatibility or governed execution safety.

## Change

Before this slice, Web mode required an `agent.Profile.Model` and default runtime configuration injected `gpt-5.6-sol` into each durable WebTurn request.

After this slice:

- generic `model.TurnRequest` permits an absent model identity;
- empty model values are omitted from new JSON requests/responses;
- Web-mode task/worker validation requires bounded base instructions but not a model name;
- `mcp-stdio` and Owner UI Web paths no longer inject a GPT model name;
- any inherited `MAR_MODEL` value is cleared for Web mode;
- the OpenAI-compatible provider adapter still rejects an empty model;
- provider mode still requires explicit provider URL, API-key environment name, model, and base instructions.

Existing stored WebTurns that contain a model field remain readable.

## Evidence

Focused boundary tests:

- generic TurnRequest with `model=""`: PASS;
- OpenAI-compatible provider with `model=""`: rejected as required;
- Web TaskRunner config with empty model: PASS.

Real Web E2E:

`TestRuntimeE2EWebBrainMCPWorkerVerifyIntegrate`: PASS in ~35.24s.

That E2E proved:

```text
Web task
-> worker
-> durable WebTurn with model=""
-> brain_turn / brain_respond
-> bounded tool mutation
-> completed_candidate
-> verification
-> integration
```

Regression:

- `go test ./cmd/mar ./internal/worker ./internal/orchestrator`: PASS
  - cmd/mar ~13.4s
  - worker ~31.5s
  - orchestrator ~88.7s

## Architectural consequence

MAR Web mode no longer claims or persists a specific model identity as required execution truth.

Model selection belongs to the external ChatWeb surface. Provider compatibility remains explicit and provider-owned.

This is a responsibility reduction, not a new abstraction or lifecycle.
