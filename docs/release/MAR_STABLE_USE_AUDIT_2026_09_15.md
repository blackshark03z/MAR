# MAR stable-use audit — 2026-09-15

## Scope and status

Requalify current MAR use against the frozen V1 architecture. Story Audio was an accidental request and is excluded. The accepted V1.2 release record is historical acceptance, not proof that today's executable, environment and checkout still match it.

Inspected canonical HEAD: `c2d2929a57fddff2868c2927a944e045819ba467`. Canonical checkout has pre-existing MCP profile-validation and Console reconnect changes. The isolated candidate now includes copies of both owner changes, together with the small verification fixes below. The original checkout and its pending edits remain untouched.

Current production verdict: **REQUALIFICATION_REQUIRED**. No production restart, deployment, task steering, database repair, provider call, or worktree deletion was performed.

## Architecture conclusion

**Keep the frozen architecture. No rewrite or architecture reopening is justified by this audit.**

The authoritative path remains MCP → TaskService/SQLite → preflight → scheduler/resource governor → isolated workspace → contained worker/ACI → sealed revision verification → serialized integration → durable result/Owner feedback. Web reasoning and the Console are clients of that path. CADS remains development guidance.

The observed self-hosting failure is a test/environment boundary problem. A host E2E test constructs a new daemon, grants ACLs and starts contained children. Running that test inside the verifier's deliberately restricted LPAC does not make those host operations legitimate worker capabilities. Reuse the existing OS-token-based host-test guard; do not broaden the worker PATH, filesystem authority, or production path-normalization rules to satisfy the nested test.

## Findings and unfinished work

| Priority | Finding and evidence | Disposition |
|---|---|---|
| P1 | Live `/api/runtime` reports `MANIFEST_MISMATCH`, `BINARY_SHA256_MISMATCH`, `trusted_for_release=false`, health `DEGRADED`, despite sandbox/execution readiness being true. Binary SHA-256 is `F3B9D3990C5EEDA58D5077D81D7047D6E6E4D28F31989E1529EBE7CD3FFDA1A8`. | Rebuild and qualify an exact clean source candidate, generate its manifest with the existing command, then explicitly promote it. Do not relabel an unqualified executable by editing the old manifest. |
| P1 | Canonical full host suite recorded 7 failing tests across ACI, orchestrator and process control. Failures identify an inaccessible/missing sandbox GOROOT or denied ACL grant on `.mar/runtime/go-portable/go`. That root is owned by `BUILTIN\\Administrators`; the current ordinary process has Modify, not DACL-management rights. | Qualify a user-owned isolated toolchain copy. Production provisioning must support the launcher identity that grants and revokes task-specific ACLs. Preserve per-task grants and network denial. |
| P1 | `TestRuntimeE2EWebWaitCapacityAllowsThirdReadyTask` fails inside a real LPAC with `CreateFile C:\\Users: Access is denied`, while the host probe passes. Existing `RequireOutsideAppContainer` detects the actual Windows token; an overlay using it records SKIP inside LPAC and PASS at host. | **FIX** test classification with the existing guard. A skipped host test is not host acceptance; both execution environments are separate required evidence. |
| P1 | Two native-Go ACI tests prepend `ProgramFiles/Go/bin` and omit an explicit Go-root read grant. This host has portable Go and no `C:/Program Files/Go`. | **FIX** those fixtures to discover the existing portable toolchain and grant its root read access, as production already does. |
| P1 | Source, embedded assets, live binary and release record currently refer to different states. Two owner changes are uncommitted: public verification-profile validation and reconnect CTA handling. | Included in the isolated consolidated candidate; rerun full host tests and the exact LPAC profile. The original checkout is preserved. |
| P1 | A fresh Windows checkout/build changed the CSS and dependent bundle filenames because UI source/assets did not have LF attributes. JavaScript content was identical; CSS differences were line endings. | Pin frontend source and generated JS/CSS to LF, normalize those files, rebuild assets. Preserve BOMs and all functional source content except the owner's reconnect predicate. Repeated normalized builds produce the same filenames. |
| P2 | An unclean daemon restart can leave an attempt logically fenced without a recoverable physical-termination proof. `reconcileUnprovenAttempts` deliberately blocks; real T9 tests assert no fabricated completion. Snapshot contains six logically fenced attempts. | **KEEP** fail-closed behavior. A later bounded recovery improvement must prove physical absence/revocation before replacement. Do not mark these terminated merely because an old PID is absent. |
| P2 | `Manager.RemoveTerminal` has safety checks and tests, but no production caller was found. Snapshot retains 57 READY workspaces, including 8 COMPLETE and 12 CANCELLED. A bounded, incomplete disk scan already found about 4.62 GB under runtime and 2.88 GB under managed workspaces. | **REWIRE** the existing removal operation through reviewed retention policy in a separate slice. First enumerate eligibility and preserve result/evidence and owner work. No blanket cleanup is authorized or performed here. |
| P2 | `CURRENT.md` and `TASK.md` accumulate superseded completion/pending statements. | Keep the release record and current evidence authoritative; reduce the current handoff to current status and links when the candidate is promoted. Do not create another lifecycle database. |

The 38 BLOCKED tasks in the snapshot are historical task outcomes, not 38 independently verified unfinished product requirements. The most recent INPUT_REQUIRED task concerns self-hosting path/toolchain fixes; it was not resumed or cancelled by this audit.

## Small implementation slice

The initial verification correction changes three existing test files:

1. `internal/orchestrator/e2e_windows_test.go`: classify the Web-wait capacity E2E as host-only using the existing token guard.
2. `internal/aci/sandbox_executor_windows_test.go`: use portable-Go discovery plus explicit read grant.
3. `internal/aci/sandbox_failed_go_windows_test.go`: apply the same fixture correction to the intentionally failing Go-test case.

The consolidated candidate also carries the owner's MCP profile-validation and reconnect changes, their existing tests, and LF-normalized frontend source/assets. New production behavior in this pass is limited to those pre-existing owner fixes. Execution fencing, Go command environment, Git broker, SQLite schema, integration and provider behavior are unchanged.

## Qualification

Evidence is stored outside tracked source, under the parent audit directory:

| Gate | Result |
|---|---|
| Full host repository suite, `go test -json -p 1 -count=1 -timeout 420s ./...` | PASS: 20 packages, 439 passing test events, zero failures, three skips. T1–T4 is separately opt-in; helper process and unavailable symlink creation account for the other skips. |
| Full repository through production ACI sanitized LPAC boundary, diagnostic 420s run | PASS: 20 packages, 346 passing test events, zero failures, 96 explicit environment/host-capability skips. |
| Exact built-in `go-standard` command sequence through ACI: `test -v -p 1 -count=1 -timeout 180s ./...`, `vet -p 1 ./...`, `build -p 1 ./...`; five-minute outer command bound | PASS for all three commands, no truncated output. This follows the diagnostic run with a warm task-local build cache. |
| Host `go vet -p 1 ./...` and `go build -p 1 ./...` | PASS. |
| Web-wait capacity E2E | PASS at host; SKIP under real LPAC based on the OS token. |
| Explicit T1–T4 self-hosting task-class benchmark on the initial verification slice | PASS: all four scenarios, 233.47 seconds. A final T1 run is recorded separately after consolidation. |
| Consolidated candidate full host suite, including both owner fixes | PASS: 20 packages, 441 passing test events, zero failures, three documented skips. |
| Final asset rebuild and affected Console/MCP packages | TypeScript check and Vite build PASS; affected Go packages PASS. Full exact LPAC profile repeated after consolidation; final results in the receipt. |
| Exact committed candidate binary/manifest identity | Recorded by the post-commit `final-candidate-receipt.json`; no canonical runtime activation is implied. |

Host-only skips inside LPAC are not acceptance of those host scenarios; the separate host run supplies that evidence. Fake/local provider fixtures exercise orchestration and physical execution without paid model calls. They do not establish live Web client discovery or Owner real-use acceptance.

Result files: `candidate-host-tests.jsonl`, `candidate-check-summary.json`, `candidate-lpac-test.log`, `candidate-lpac-profile-test.log`, `candidate-lpac-profile-vet.log`, `candidate-lpac-profile-build.log`, and `candidate-t1-t4.jsonl`. The scratch reproducer is `candidate/.mar/verify_candidate.go` and invokes the existing `aci.Runtime.RunCommand`, not a replacement verifier.

Consolidated evidence: `consolidated-host-tests.jsonl`, `consolidated-lpac-profile-{test,vet,build}.log`, `console-reconnect-actions.json`, and `final-candidate-receipt.json` in the parent audit directory.

## Reconnect UI review

Narrow UX contract: the owner opens Connections, starts GPT, stops it, and can start it again in the same card. The primary action is `Kết nối`; details remain secondary. GPT's state must not change Claude's presentation. No layout redesign or new product choice is involved.

Actual compiled Console assets were rendered against an isolated local HTTP state fixture. Two start/stop cycles showed the reconnect action returning and the ready state following the next start. Screenshots at 1280×720 and 1024×768 showed a visible primary action and readable card reflow; the narrower DOM had width 1009 within a 1024 viewport and one reconnect button. This validates the UI contract against fixture responses, not real provider connectivity.

`technical_validation=PASS`, `candidate_preview=PASS (fixture)`. Levels 1–4 pass for this narrow action: implemented, discoverable, understandable, and visible in the primary hierarchy. Level 5 remains Owner acceptance, not inferred. Information architecture, navigation, workspace layout, task flow, execution state and resource management retain the existing design. `owner_ux_gate=NOT_REQUIRED` for additional design decisions; no new Owner acceptance is claimed.

Implementation-review fields: primary-surface discoverability, scope clarity, apply/reapply explicitness, advanced-control balance, visible hierarchy, control density, coherent composition, existing-workflow preservation, information architecture, navigation, workspace layout, viewport budget, contextual controls, layout fit, responsive behavior, task-flow structure and execution-state separation are PASS within this scenario. Bulk/destructive safety, destructive differentiation, disabled-state explanation, sprawl reduction, multistep creation, review-before-commit, resource-library separation and post-completion destination are NOT_APPLICABLE to the reconnect change. No blocking UI finding was observed in this fixture.

Existing before-change evidence: `test-summary.json`, `runtime-redacted.json`, `durable-snapshot.json`, `lpac-probe.json`, `lpac-guard-probe.json`, `host-guard-probe.log`, and `storage-snapshot.json` in `D:/MAR/.mar/runtime/audit-20260915`.

## Path to stable use

1. Finish the isolated qualification and retain a reviewable patch and exact source identity.
2. Use the consolidated candidate containing the reviewed owner changes. Reconcile any later canonical edits before integration; do not overwrite owner work.
3. Provision Go for the actual launcher identity and use the existing release-manifest path for the exact binary. No new packaging authority.
4. After explicit activation authorization, promote/restart the canonical runtime and verify identity, health, sandbox/worker readiness, one real bounded Web Goal through verification/integration, then reopen result and begin the next Goal. Preserve existing tasks and SQLite state.
5. Record the engineering outcome separately from explicit Owner real-use acceptance. Address recovery/retention next as bounded operational slices; optional transports, broad UI changes and context-engine expansion are deferred.

No push, deployment, release seal, or claim of new product acceptance is made by this candidate.
