# DR-0001: Apply CADS to MAR product work without changing runtime authority

Status: Accepted
Date: 2026-09-06
Scope: Process / UX

## Context

MAR's technical self-hosting pipeline reached verified/integrated execution, but engineering success was nearly mistaken for Owner Product Acceptance before the owner had personally used the product. The remaining work is user-facing: a real entry path, understandable task flow, rendered UI quality, and owner hands-on acceptance.

## Decision

Use the Convergent AI Development Standard (CADS) as MAR's development operating/routing standard. For user-facing work, follow Product Goal Framing -> User-Facing Workflow -> Frontend Design -> rendered UI Quality Review -> Product Acceptance. CADS remains advisory development process/guardrails and does not become a MAR runtime subsystem.

The owner-facing surface must remain a client of MAR's bounded task/MCP control plane. It must not introduce a second task truth, coding loop, repository mutation authority, or integration authority. MAR's frozen architecture remains canonical under `docs/architecture/MAR_V1_Architecture_FROZEN/`; no duplicate root architecture authority is created.

## Why

This preserves MAR's frozen runtime guarantees while adding the missing product discipline: real Critical User Journey, discoverability/status/recovery, rendered UI review, and explicit Owner acceptance. It also prevents engineering PASS or agent reports from being treated as user acceptance and limits polish to issues that materially affect the active Goal.

## Consequences

- `TASK.md` holds the current Product Goal/CUJ/acceptance.
- `AGENTS.md` routes product, UX, debugging, acceptance and hygiene work through CADS procedures when available.
- UI work is ordered by user journey before visual implementation.
- Brain/provider/MCP connectivity is surfaced as a product dependency instead of hidden behind test harnesses.
- V1 remains not PRODUCT_ACCEPTED / SELF_HOSTING_READY until the owner personally runs and accepts the real journey.

## Revisit When

Revisit only if repeated project evidence shows CADS routing itself blocks delivery, or if a frozen MAR invariant is proven insufficient by implementation/owner-use evidence. Do not reopen for cosmetic preference or process novelty.
