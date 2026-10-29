---
kind: feature
status: resolved
---

# First-run Cloud connection and local execution

## Deployed-service mismatch (2026-10-29)

The user reached the local execution form against app.openpero.com and received
an English generic setup failure in the Chinese UI. Read-only inspection found
the active release and Docker revision still at
`ed086f6f40bbf0b7e41880230da897f4ed0b44f8`: this source has only the Client
Worker-list GET route, no enrollment POST and no migration 014. A healthy
service therefore does not establish support for Desktop device enrollment.
No authenticated registration request was sent during this inspection.

The old GET-only route is expected to return HTTP 405 with a plain-text body
for enrollment. Desktop decoded JSON before checking status, losing that reason;
the setup controller then replaced every remote failure with the same retry
message. Native enrollment also discarded HTTP status. Fix both boundaries to
retain a safe typed failure, translate it in the UI, and distinguish a required
Cloud update from authentication, scope, conflict and temporary network errors.
Do not reflect response bodies or credentials. Verify plaintext 405 and recovery
with the existing fixture, then roll out the matching server API and migration
before accepting the deployed flow.

The diagnostic fix passed 15 Native tests and nine actual Electron enrollment
failure/recovery cases, including plain-text 405 in Chinese. The saved device,
receipt and secret survive retries; the recovered registration completed an ACP
fixture Run. The Cloud build for committed revision
`8b55dd835587cc8c9fdbadba2eae3548f45fd032` is prepared and its server tree matches
the previously race-tested implementation. After explicit user approval, release
`20261029T075057Z-8b55dd83` was deployed. Active revision and binary checksum
matched the prepared build; HTTPS health returned 200, migration 014 was present,
both new tables were empty and a pre-deployment backup existed. The user can now
retry the saved enrollment. No real device was registered or task dispatched by
the deployment. Evidence is in the verification report below and the
[deployment receipt](../fitness/task-center-deployment.md).

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
The original checks below were local. The approved Cloud rollout above now
verifies the deployed API revision and migration; real device enrollment, real
user task execution and signed-release acceptance remain separate.

Cloud rollout applied migration `014_worker_devices.sql` and the matching
Client/operator enrollment API from ADR 0010.
