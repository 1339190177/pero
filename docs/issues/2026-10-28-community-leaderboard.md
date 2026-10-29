---
kind: feature
status: implemented
github_issue: 2
github_state: open
github_url: https://github.com/phodal/pero/issues/2
---

# Community task board and contribution leaderboard

## What and why

Cloud dispatch has durable Tasks, Runs and Workers, but contributors cannot browse
public work, claim it with their own Worker, or inspect accepted contributions.
Add a small server-rendered community surface to the existing Go service.

## Scope

- Public task board, weekly/all-time leaderboard and contribution history.
- GitHub OAuth identity, server-side sessions, explicit Worker ownership and claims.
- Operator-controlled publication: private tasks and execution inputs stay private.
- One point per accepted task, attributed to the contributor bound to its Run.
- Explicit operator acceptance and reversal with append-only scoring receipts.
- Go templates and embedded assets; no separate frontend service or second queue.

## Acceptance

- Claiming dispatches the existing Task to an owned, project-authorized Worker.
- Concurrent claims have one winner; success alone never awards points.
- Acceptance pins the actual succeeded Run and evidence; retries do not duplicate points.
- Reversal is recorded and removes the original award from weekly and lifetime totals.
- Public responses contain no private prompt, local path, token or Agent log.
- Verify PostgreSQL transactions, authentication/CSRF, browser interactions,
  loading/empty/error states, narrow windows and light/dark themes.

## Delivery boundary

GitHub OAuth needs deployment credentials. Local provider fixtures verify the
protocol without claiming production login or deployment to openpero.com.
Issue ingestion remains the separate remote-dispatch task's responsibility.

## Implementation and local verification

- Go embeds the task board, contributor rankings/history and operator management.
- GitHub OAuth binds numeric identity to the existing Client model. Approved
  Worker ownership and task dispatch use the shared Task transaction and Run model.
- Reviewed awards and reversals are append-only; duplicate/stale acceptance,
  cross-client claims and foreign Workers are rejected or reconciled idempotently.
- Real PostgreSQL tests cover concurrent claims and awards, private-task filtering,
  existing Desktop claims, session expiry, cross-origin/CSRF rejection, OAuth state,
  provider failure, HTML escaping and logout revocation.
- Chromium completed publish, OAuth fixture login, Worker binding, claim,
  running/success, manual acceptance, weekly/all-time ranking and reversal.
  Keyboard focus, submit feedback, disabled/empty/error states, light/dark themes
  and 390px layout were checked. Captures are in `test-results/community/`.
- `server/README.md` documents deployment variables, admin operations and test commands.

The feature is developed in the isolated `feat/community-leaderboard` worktree.
Production OAuth credentials, real provider execution and public deployment are
outside this local delivery. No binary captures are committed to Git.

Final local checks after integrating the committed Client/Issue interfaces:

- `PERO_TEST_DATABASE_URL=... go test -race ./...` passed all Go packages.
- `go vet ./...` and the generated-theme check passed.
- `PERO_TEST_DATABASE_URL=... npm run test:community` passed 19 browser scenarios.
- Built and started the real server against an existing version-1 database;
  migrations 2, 3 and 10 applied. Public pages/assets and health returned 200;
  unauthenticated task APIs remained 401.

All five feature commits are included in the local master integration. An
integration correction makes public list/detail and claim decisions use the
latest GitHub source state while preserving frozen execution inputs. The combined
Go race suite and 21 Chromium scenarios passed, including changed/closed sources.
GitHub issue remains open pending public delivery; no push or deployment was
performed in this task.

## Roadmap outline follow-up (2026-10-29)

The public projection omitted heading paths and parent references, so published
Roadmap items appeared as an unordered flat table even though intake retained
those relationships. Earlier imported titles also included the whole description.

The public board now uses the complete current published outline rather than
one 50-item page. Removed sources stay in history but leave the current browser.
ROADMAP now owns a functional domain → feature → task hierarchy rather than
milestone navigation. It has 12 domains, 48 feature groups and 252 tasks: 219
unfinished and the original 33 checked items. Eight broad entries were split
into 37 bounded tasks, with observable checks for every unfinished entry. Both
READMEs link to the new functional sections.

Tree and panorama share search, state/category filters and the selected task.
The default excludes accepted and source-closed tasks; feature folders start
collapsed. Domain selection and search reveal tasks and their ancestors. Scope
and acceptance appear inline with Desktop/source actions. Panorama presents
functional domains and features, with drill-down into the same tree. Counts
follow query and state scope; reset restores the unfinished default. No
inspection route change is required, and source closure is not acceptance.

Local `go test -race ./...` passed with isolated PostgreSQL. The existing community
browser suite passed contribution and source-state regressions. The dedicated
`tests/e2e/task-browser.mjs` passed against all 252 Roadmap tasks and 12 domains,
including defaults, domain/feature/task keyboard navigation, shared filters,
acceptance-text search, panorama feature drill-down, inline inspection, URL
restoration after filtering out a selection, and 390px dark/no-JS rendering.
Screenshots and verification JSON are under `test-results/task-browser/`.
Production deployment and acceptance are recorded separately in
[Task Center deployment](../fitness/task-center-deployment.md).
