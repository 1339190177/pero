---
kind: feature
status: resolved
---

# First-run Cloud connection and local execution

## Task setup recovery (2026-10-29)

A queued task only explained that a Worker was required; the project task detail
offered no direct route to the newly implemented local execution setup. Its old
copy still said the local Host could not claim Cloud tasks. Link that state to
the existing device setup with the task's project, workspace key and Agent, and
the project's known local directory. Opening setup must not enroll the device,
dispatch another Run or execute the queued task before explicit enablement.

Verified in the actual Electron sidebar suite: a queued task opens the existing
device form with its project, workspace key, Agent and known local checkout;
the Worker remains disabled and no POST is sent. Re-ran the existing local
execution suite successfully: one enrollment, one completed Host/Worker/ACP
fixture Run, 12 events and no renderer errors. These are local fixture checks;
the deployed Cloud and the user's real queued task were not changed or executed.

## What and why

Desktop sign-in restored the Cloud client, while the local Host never enrolled or
started a Worker. The execution-resource page therefore offered administrator CLI
instructions even when the user was already signed in. Native Cloud credentials
were memory-only despite reporting persistence availability.

## Implemented scope

- First-run account connection, optional local-only Skip, and explicit local
  execution consent for a granted project, workspace key, directory and Agent.
- Separate owned-device Worker credentials; the recoverable enrollment secret is
  saved in OS-encrypted storage before the server mutation. Restart restores the
  same enrollment without creating another device. Client identity never becomes
  a Worker bearer token.
- Client-scoped enrollment with idempotent receipts and capability triples.
  Claims check project/workspace/Agent and the owner's current project grants
  before assigning a lease. Legacy manually provisioned Workers remain supported.
- Host supervises the existing Worker binary and its private journal/Agent
  allowlist. Local ACP turns and Worker execution share a workspace lease.
- Account-bound automatic restore, idle reconnect, explicit Stop/Resume, and
  durable pause on attention/auth/configuration/reconciliation failures.
- Native macOS uses the system keychain; Electron uses safeStorage without its
  plaintext fallback. UI states and Host/public settings exclude credentials.
- Managed local execution stops with Desktop; independent Workers retain their
  separate lifecycle. Packaging includes the Worker binary.

## Verification

Passed local verification on 2026-10-29:

- Full server race suite against an isolated PostgreSQL cluster, including
  migration application, enrollment recovery, ownership and capability checks.
- 73 Rust workspace tests and 11 native Cloud/device flow tests.
- 14 Electron end-to-end checks with actual Host/Worker/ACP fixture execution:
  one enrollment, one completed Run, 12 uploaded events, no reported errors.
- Host startup/restart/stop, cold restore, Host-crash cleanup, and private
  credential exclusions; five packaging tests and the TypeScript, locale,
  theme and generated-contract checks.

See [verification and acceptance boundaries](../fitness/local-execution.md).
No deployed Cloud was changed and no real user task was dispatched.
Signed-release and deployed migration acceptance remain separate.

Cloud rollout requires migration `014_worker_devices.sql` and the matching
Client/operator enrollment API from ADR 0010.
