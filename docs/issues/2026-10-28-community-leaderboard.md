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
