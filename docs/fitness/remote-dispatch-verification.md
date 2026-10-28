# Remote dispatch verification

Date: 2026-10-28. Design: [ADR 0002](../adr/0002-remote-task-dispatch.md)
and [ADR 0003](../adr/0003-github-issues-and-client-claims.md).

## Implemented and exercised

The control plane is Go with real PostgreSQL. An independent Rust Worker reuses
the existing ACP runtime; neither task scheduling nor future Kubernetes provider
ownership moves into Rust. The original baseline starts with manual Tasks and
operator credentials. The current extension adds read-only GitHub intake and
project-scoped client claims; its checks are listed separately below.

The following baseline results were recorded before the shared-client extension.
They do not stand in for its final combined checks.

| Check | Result |
| --- | --- |
| Go `go test -race ./...` with an isolated PostgreSQL database | Passed |
| Go `go vet ./...` | Passed |
| Worker unit and HTTP/ACP subprocess tests | 12 passed |
| `npm run check` | Passed: TypeScript, themes, 219 bilingual keys, generated contracts |
| `npm test` | Passed before the final Worker size-bound additions; the updated Worker suite then passed separately |
| `npm run test:contracts` | Passed; every observed Host line conforms |
| `npm run test:remote` with real Electron, Go, PostgreSQL and independent Worker | Passed |
| Existing `npm run test:e2e` conversation suite | Failed at the existing single-wheel scroll assertion; see incident below |

The database cases exercise concurrent claims, durable restart, frozen input,
operator/Worker/project/instance scope separation, expiry during a row-lock wait,
duplicate claims/events/results, cancellation races, Worker quarantine, Unicode
limits and response byte bounds. They use isolated schemas and remove them.

The actual Desktop flow creates two drafts through the GPUI form, explicitly
dispatches them, observes persisted ACP output, cancels a silent execution after
confirmation, restarts the Go service, recovers the same Tasks, checks connection
errors and loads 51 Tasks across pages. English, dark and 1000px-wide views and
the unconfigured state were captured. A native Workbench menu/shortcut opens
remote tasks. Cloud credentials are absent from renderer state and local Agent
environment. Test configuration and artifacts stay outside user repositories.

Worker fault injection also exercised supervisor SIGKILL and restart: descendant
processes stopped, the original Run entered `reconcile`, and its prompt ran once.
The ACP SDK's separate process group required explicit descendant-group cleanup.
See [cleanup incident](../issues/2026-10-28-remote-worker-descendant-cleanup.md).

## GitHub intake and shared clients

The extension imports stable GitHub Issue identities, preserves frozen goals and
source revisions, and separates latest provider metadata from the goal accepted
for execution. Closed or stale sources block new execution. Operator-provisioned
clients can claim, release and execute project-scoped Tasks; these claims do not
write GitHub assignees. Label-gated automatic dispatch is opt-in and cannot take
an existing human claim or silently rerun previously dispatched work.

| Current extension check | Result |
| --- | --- |
| Go `go test -race ./...` with isolated PostgreSQL | Passed, including source/client ownership and provenance cases |
| Go `go vet ./...` | Passed |
| `npm run check` | Passed: TypeScript, themes, 239 bilingual messages and generated contracts |
| `npm run build` | Passed: generated fonts, Host, GPUI WASM, Web and Desktop |
| Local complete-font reuse without Python/downloads; cached-font build with network disabled | Passed; generated binaries/stamp ignored by Git |
| Public GitHub read against `api.github.com` | Passed; details below |
| `npm run test:github`: actual Electron clients + Go/PostgreSQL + independent ACP Worker + fixture GitHub | Passed, including changed/closed sources, dark and 1000px-wide client views |
| `npm run test:remote` against the combined implementation | Passed: 51 Tasks, no page errors; create/execute/cancel/reconnect/pagination and English/dark/narrow views |

The public-source check read `phodal/pero` without a provider token. Startup
imported Issue #2 as one draft with automatic dispatch disabled and no Run. A
subsequent manual sync returned HTTP 200 with zero new imports, zero dispatches,
`partial:false` and no errors. An overlapping manual request during startup
returned the expected 409 while the single sync owner was running. This was
read-only provider access; no GitHub label, assignment or Issue-state write was
performed. Evidence: `test-results/github/live-source.json`.

The Desktop/GitHub harness uses deterministic provider and ACP fixtures with real
Electron windows, the actual Go API, PostgreSQL transactions and an independent
Rust Worker. The completed flow passed Desktop source sync, automatic dispatch
once, project scope, claim conflict, release/claim handoff, Worker execution, both
clients observing the same Run, and credential exclusion. After an upstream edit
and closure, frozen goal/source and the entire Run remained unchanged. Clicking
the disabled Execute button added zero dispatch requests in either state; visible
warnings explained recovery. Dark and 1000px-wide client layouts also passed,
with no page errors. Evidence is
`test-results/github/result.json` and the `operator-before-sync`,
`automatic-policy`, `available`, `alice-claimed`, `bob-executed`, `source-changed`,
`source-closed`, `client-dark` and `client-narrow` screenshots in that directory. This workflow is
protocol/interaction evidence, not a real-model or production acceptance claim.

## Reproduce

Build with `npm run build` and `npm run build:worker`. Set
`PERO_TEST_DATABASE_URL` to a disposable PostgreSQL database, then run
`npm run test:remote`. For the shared source/client flow, use the same disposable
URL with `npm run test:github`. Run Electron suites sequentially. Database credentials are
configuration; no production database or remote deployment was touched.

Generated evidence: `test-results/remote/result.json`,
`test-results/remote/worker-faults.json`, and the `empty`, `create-form`, `draft`,
`completed`, `running`, `cancel-confirmation`, `cancelled`, `connection-error`,
`english`, `dark`, `narrow`, `unconfigured` PNGs in the same directory.

## Limits and remaining work

This is macOS local multi-process protocol acceptance with an ACP fixture, not
real-model, deployed Linux, Kubernetes, packaged Desktop or production acceptance.
The Worker uses approved trusted checkouts and Unix process supervision; it is
not an adversarial sandbox. Remote unattended permission requests are denied.
Task execution success does not establish candidate verification or acceptance.

ROADMAP ingestion, GitHub OAuth/webhooks/writeback, credential revocation and
explicit reconciliation/source-refresh UI for previously executed goals,
independent verification, PR publication, Kubernetes providers and deployment to
openpero.com remain unimplemented. The Go service has configurable listen,
database and token settings for a future separate deployment.

The independent conversation regression is tracked in
[conversation scroll follow](../issues/2026-10-28-conversation-scroll-follow.md).
The complete pre-existing Desktop suite is therefore not reported as passing.
