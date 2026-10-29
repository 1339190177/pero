---
kind: verification_report
date: 2026-10-29
---

# Local execution setup and automatic restore

Scope: [canonical local-execution tracker](../issues/2026-10-29-local-worker-onboarding.md)
and [device enrollment](../adr/0010-desktop-worker-enrollment.md).

A fresh profile shows Cloud connection in first-run setup. It does not open a
browser or authorize execution automatically. Users may skip and start local
chats. After sign-in, enabling this device requires an explicit granted Project,
workspace key/directory and installed ACP Agent. Existing configured profiles keep
normal navigation and can enable local execution from execution resources.

The shell persists Cloud/device credentials in OS storage. Local execution uses
a separate Worker identity. A pending enrollment receipt and secret are durable
before the HTTP mutation, so interrupted replies can be reconciled. Returning to
the same account restores only explicitly enabled configuration. Logout or a
changed account suspends intake; returning to the original verified account
restores the saved enabled intent. Explicit Stop or an attention state persists
disabled intent and requires Resume. The Host owns the
managed Worker, progress and bounded shutdown. It does not create another Agent
runtime or replay unknown execution.

The server checks current Client project grants and exact advertised capability
triples before leasing a Run. Local ACP turns and Worker execution share the same
workspace lease. Worker network retries preserve the original claim identity.
Provider authentication/reconciliation requires attention instead of repeated
execution. Unattended permission requests remain denied by the existing Worker.

## Evidence — passed 2026-10-29

- Server tests use isolated PostgreSQL and `go test -race ./...`, including
  lost-reply enrollment, conflicting receipts, credential rotation, owner/scope
  isolation, revoked grants, capability matching and legacy Worker compatibility.
- Host/Worker shell checks use temporary data directories and loopback Cloud;
  actual Worker startup/restart/stop, cold restore, Host EOF/crash cleanup and
  credential exclusions passed in `tests/runtime/local-worker-host.mjs`.
- Electron evidence is in `test-results/local-execution/`: actual scoped enrollment,
  ACP fixture execution, uploaded events and result, OS-encrypted stored record,
  restart without re-enrollment, explicit Stop/Resume/account recovery and
  dark/narrow setup. All 14 checks passed: one enrollment, one completed Run,
  12 uploaded events, zero reported errors.
- Native macOS uses the same public UI with keychain credentials and a separate
  private controller. All 11 native Cloud/device tests passed using an in-memory
  vault and loopback fixtures; they do not read or modify the user's keychain.
- `npm test` passed 73 Rust tests. `npm run check` passed TypeScript, 491 bilingual
  messages, theme and generated-contract checks. Five packaging tests passed;
  native, Host, Worker, Wasm and Desktop development builds succeeded.

## Acceptance boundary

These are development L0/L1 checks using isolated fixture data. No Worker was
registered on app.openpero.com, no deployed database was migrated, and no real
provider/user task was executed. Cloud rollout requires migration 014 and the
matching server endpoint. Native secure storage is implemented for macOS; other
native platforms report unavailable, while Electron uses the platform safeStorage
backend. Worker execution remains Unix-only. Signed packages and physical OS
keychain restart acceptance remain separate from compilation/fixture evidence.
