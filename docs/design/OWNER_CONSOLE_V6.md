# MAR Owner Console v6 — Convergence Contract

Owner Console v6 is a bounded product/UX convergence over the frozen MAR V1 runtime. It does not change orchestration, sandbox, verification, connector, or integration authority. The interface is a light, compact operations console whose primary job is to answer: is MAR healthy, what is working now, are GPT/Claude routes usable, and does the Owner need to act?

## Information hierarchy

The global header keeps brand, selected workspace, telemetry freshness, runtime/model summary, and worker execution choice on one compact baseline. Overview remains concise. GPT/OpenAI and Claude appear as separate provider summaries with local identity marks and current evidence-backed status. Secondary route/session/activity/transport facts are collapsed under `Chi tiết bên dưới`; actionable failures stay visible in the summary.

Live Operations prioritizes system/connection health, active flows, and live model usage. Provider route/session/request detail is progressive disclosure rather than primary content. A flow is identified by `task_id + run_epoch`; route readiness, session count, request count, and active execution flow are distinct concepts. Stateless transports never fabricate a zero session count when identity is unavailable. Provider attribution is never guessed.

## Realtime chart truth

The realtime chart consumes only the existing non-overlapping MAR polling observations. Each observed sample carries measured/estimated active-turn input and output token counters already exposed by MAR; the browser keeps a bounded 40-sample window. There is no generated history, interpolation, or decorative random data. With fewer than two samples the chart explicitly reports insufficient realtime data. Live Web Brain token counters retain the `~` estimated semantics where required and remain separate from durable TaskResult-based Usage accounting.

## Tasks and execution choice

Tasks uses a compact master-detail journey. The list shows goal summary, workspace, owner-facing stage, update time, and available usage; search/status filtering acts on the list, while technical state, verification, integration, revision, evidence, changed areas, controls, and feedback live in the selected detail pane. `Waiting for AI` is not `Needs your input`; only current actionable conditions should demand Owner attention.

MAR V1 does not expose arbitrary named-worker binding. The execution selector therefore truthfully exposes `Tự động · MAR scheduler` only unless the runtime later provides authoritative supported profiles. The UI must never invent worker identities or add hidden Goal Contract fields. Workspace selection is based on registered projects and the native folder-picker path; the Owner is not asked to type project IDs, revisions, verification profiles, authority JSON, or worker IDs for normal use.

## Responsive/accessibility and release gate

Primary pages avoid horizontal page overflow, large dead areas, and unbounded task/flow lists. Controls retain keyboard/focus/status semantics with WCAG-2.2-AA-oriented contrast and readable type. Desktop and compact-width layouts must preserve hierarchy without clipped labels or overlapping controls.

Engineering completion requires JavaScript syntax/static UI regression, relevant Go tests, then the sealed revision-bound `go-standard` profile (sequential repository tests, `go vet`, and `go build`) and authoritative integration. Only then may this v6 revision be described as `ENGINEERING_STABLE`. `PRODUCT_ACCEPTED` is reserved for explicit Owner real-use acceptance of the integrated UI.
