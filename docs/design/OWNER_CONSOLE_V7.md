# MAR Owner Console v7 — Release UI Contract

Status: implementation candidate; owner real-use acceptance pending.

## Objective
Converge the accumulated v3-v6 visual layers into one bright, low-density owner operations experience matching the approved reference direction without changing MAR authority, transport, task lifecycle, or telemetry truth.

## App shell
The console keeps a fixed left navigation for Overview, Live Operations, Tasks, Workspaces, Connections, Usage, and Diagnostics. The compact top bar keeps registered-workspace selection plus truthful telemetry/runtime/model/worker badges. Light surfaces, restrained borders/shadows, consistent spacing, and clear semantic badges are shared across all pages.

## Live Operations
The first row answers exactly four owner questions: system health, AI connection readiness, active execution flows, and durable token usage today. Realtime input/output trends use only bounded observations already exposed by MAR; no synthetic history is generated. Active flows remain task_id + run_epoch. OpenAI and Claude remain separate provider zones; unknown sessions/provider attribution remain unavailable rather than inferred.

## Tasks
Desktop uses three columns: filtered/searchable task list, selected task detail, and create-task panel. The detail pane remains the only source for technical state, verification, integration, revision, evidence, controls, and feedback. The create panel accepts normal owner language and internally binds to the selected registered project's current HEAD and persisted local authority policy. Worker choice is `Tự động · MAR scheduler` only. Network, remote Git write, and deploy are never enabled by this form.

## Responsive and accessibility
At wide desktop widths all three task columns are visible. Below the desktop breakpoint the layout stacks without page-level horizontal overflow. Existing focus, status, keyboard and WCAG-oriented semantics are preserved. Secondary pages reuse the same shell/tokens without adding new product scope.

## Release gate
Targeted Owner UI/JS and cmd/mar tests plus the full go-standard profile must pass before integration. Engineering verification does not imply PRODUCT_ACCEPTED; the owner will perform real-use UAT after the integrated runtime is launched.