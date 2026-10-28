---
kind: verification_report
date: 2026-10-28
---

# GitHub and ROADMAP connector acceptance

Implementation: [ADR 0005](../adr/0005-github-roadmap-connector.md).
The Go control plane, Desktop connector and official project-local Codex ACP
dependency were merged into `master` and pushed through `0aa1064` before the
final public-source acceptance run from the main checkout. Subsequent report
changes do not alter the ROADMAP content or task intake behavior.

## Public repository result

`npm run test:connector:live` passed against read-only public `phodal/pero`
using real Electron, its native Host, Go HTTP handlers and isolated PostgreSQL.
The service ran locally; this did not deploy openpero.com.

| Observed source | Result |
| --- | --- |
| Default-branch commit | `0aa1064ec7281b2eb4954d5050753f1408d12b03` |
| Selected path | `docs/ROADMAP.md`, using `auto` fallback |
| ROADMAP checkbox Tasks | 411: 400 unchecked and 11 checked |
| GitHub Issues, all states, excluding PRs | 2 |
| Import executions | 0 |
| Desktop page errors | 0 |

An independent pinned-content line ledger matched all 411 imported checkbox
positions. The Desktop loaded every source page, checked document order, and
matched the scoped API denominator. Two successive source syncs preserved Task
IDs and produced no duplicate imports or execution. The test did not use a
GitHub token, a local GitHub login or model credentials, and made no GitHub writes.

The actual Desktop form rejected missing fields, masked the access token, and
saved a project-scoped client connection. After closing and relaunching the app,
with no cloud/token environment variables, it automatically opened Remote tasks
and loaded the saved connection. Private-project Task details returned 404 and
their goal never entered the client snapshot. Checked ROADMAP items displayed
their closed source and their disabled Execute control created no Run.

Generated local evidence is under `test-results/connector/live/`: `result.json`
contains counts, the pinned commit, full line ledger, observed Task IDs and
assertions; screenshots include `masked-connection-form.png`,
`roadmap-first-page.png`, `roadmap-all.png`, `checked-disabled.png`, `dark.png`,
`narrow.png` and `narrow-list.png`. These regenerable artifacts are ignored by Git.

## Additional acceptance

| Check | Result |
| --- | --- |
| `npm run test:connector` | Passed: one Issue plus five checkbox Tasks, heading groups, explicit IDs, nested parent navigation, actual tag-chip filtering and checked-source controls |
| `npm run test:remote-settings` | Passed: actual OS encryption, Host-owned 0600 writes, restart, URL/mode binding, no client-to-operator fallback, storage-error recovery, retryable failed disconnect and persistent disconnect |
| Desktop launched from `desktop/` | Project-local Codex remains available and Host workspace resolves to the repository root |
| `npm run test:github` | Passed: two clients, claim conflict/release/handoff, Worker execution, automatic dispatch once, changed/closed source gates |
| `npm run test:remote` | Passed: 51 Tasks, pagination, execute/cancel, server restart, connection recovery, English/dark/narrow views |
| `npm test` | 35 Rust tests passed, including six Worker integration cases |
| `npm run test:contracts` | Passed: 24/31 declared Host event kinds observed; unexercised kinds reported by the existing harness |
| Full Go suite with real PostgreSQL | `go test -race ./...` and `go vet ./...` passed |
| Bazel CI-frozen checks and server build | Seven checks passed; server linked successfully |
| Bazel strict PostgreSQL suite | All four package targets executed and passed |
| Full build and `npm run check` | Passed; 259 bilingual messages and current generated contracts |

The legacy remote test initially clicked the new connector button using its old
Refresh coordinate. The screenshot showed the connection form rather than the
task list; updating that target restored the existing 51-Task pagination and
recovery assertions without changing them.

Visual review also found long titles overflowing the narrow list. Explicit
constrained title/metadata lines fixed both widths; the fixture suite and the
post-push public run passed with screenshots inspected. See the resolved
[Task-row incident](../issues/2026-10-28-roadmap-task-row-overflow.md).

Store regressions also cover partial snapshots, ambiguous identities, frozen
claimed/executed goals, current source tags, scoped cursors, parent/child order
across page boundaries, queued cancellation and complete path replacement.
Missing sources retain their last known URL/commit/blob, recording the absence
observation separately. A partial replacement cannot retire prior-path Tasks.

The official `@agentclientprotocol/codex-acp` dependency is pinned to 2.3.0.
The native Host completed its real ACP initialization with this adapter; the
empty test Codex home required explicit authentication. No real model request
was used to establish connector acceptance.

## Boundaries

The public run establishes GitHub reads and Desktop source browsing through a
local Go/PostgreSQL service. Fixture runs establish nested/tag interactions and
the Worker lifecycle. Neither establishes signed Desktop packaging, Kubernetes,
production hosting, real-model execution, PR publication or GitHub assignee
writeback. ROADMAP intake always creates drafts; Issue automatic dispatch remains
an explicit server-owned label policy.

Existing worktree feature integration is recorded in
[worktree-integration.md](worktree-integration.md). Rechecking source branch
snapshots found no newer feature commits to merge. Independent chats committed
AGENTS/README/reference updates to `master` during completion; these commits were
retained. Remaining local `AGENTS.md` edits are outside the connector commits.
