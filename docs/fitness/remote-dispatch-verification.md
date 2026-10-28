# Remote dispatch verification

Date: 2026-10-28. Design: [ADR 0002](../adr/0002-remote-task-dispatch.md).

## Implemented and exercised

The control plane is Go with real PostgreSQL. An independent Rust Worker reuses
the existing ACP runtime; neither task scheduling nor future Kubernetes provider
ownership moves into Rust. This slice starts with manual Tasks and configured
operator credentials. Source import is the next slice.

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

## Reproduce

Build with `npm run build` and `npm run build:worker`. Set
`PERO_TEST_DATABASE_URL` to a disposable PostgreSQL database, then run
`npm run test:remote`. Run Electron suites sequentially. Database credentials are
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

GitHub/ROADMAP ingestion, device revocation and explicit reconciliation UI,
independent verification, PR publication, Kubernetes providers and deployment to
openpero.com remain unimplemented. The Go service has configurable listen,
database and token settings for a future separate deployment.

The independent conversation regression is tracked in
[conversation scroll follow](../issues/2026-10-28-conversation-scroll-follow.md).
The complete pre-existing Desktop suite is therefore not reported as passing.
