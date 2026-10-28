---
kind: feature
status: implemented
---

# Complete the Desktop contributor workflow

## What and why

A fresh contributor can sign in with GitHub, but the prior Desktop required an
operator-assigned project scope to see work. The task Execute action dispatched
to a separately configured Worker. Local ACP sessions, Git preparation and PR
publication were separate, so downloading the App did not provide a complete
contribution workflow.

## Scope

Follow [ADR 0009](../adr/0009-local-contributions.md): public task discovery and
claim without private project access; dedicated local Git workspace; the existing
ACP session; actual diff review; explicit draft PR publication and evidence
registration. Keep remote Worker coordination and acceptance authorities intact.

## Acceptance

- A new GitHub identity with no project scope can discover and claim published
  work while private Task/Run data remains unavailable.
- The Host prepares an isolated task checkout and branch without changing an
  existing repository. Missing GitHub CLI or model authentication has a visible
  recovery path.
- The existing Agent session modifies that checkout. Review includes actual
  changes, and publication rejects a candidate changed since review.
- Publication creates or finds the draft PR and registers its exact commit and
  source revision. A registration error preserves the known PR for retry.
- Real PostgreSQL, Git and Electron prove the chain using explicit GitHub/model
  fixtures. No fixture result is represented as a live PR or real-model acceptance.
- The macOS development package is rebuilt and checked; public distribution,
  notarization and live service deployment are reported separately.

## Local delivery

Implemented the public contribution API, Host-owned Git workspace/publication,
existing ACP session integration and task PR registration. Default and real Codex
acceptance passed, along with the installed development App, complete Go race
suite using PostgreSQL, Rust regression suite and Host contracts. The final
download artifact is `target/package/Pero-macos-arm64.dmg` (98,992,233 bytes).

Full results and evidence boundaries: [contributor verification](../fitness/contributor-workflow.md).
Public deployment and release remain pending separate authorization. No live
GitHub writes or production update were made by the implementation run.
