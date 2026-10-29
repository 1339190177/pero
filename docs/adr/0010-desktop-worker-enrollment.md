# Desktop Worker enrollment

Status: implemented locally, 2026-10-29. Extends ADRs 0002 and 0006.

## Decision

Desktop sign-in connects a user Client. Enabling local execution is a separate,
explicit delegation to a device; it does not give a new GitHub identity project
rights. The device uses the existing independent `pero-worker` and Cloud Run
lease protocol rather than a second Agent loop in the UI.

- `POST /api/client/v1/workers` enrolls a device only within the authenticated
  Client's current project grants. `POST /api/v1/workers/enroll` provides the same
  setup for an operator. The existing operator provisioning endpoint and legacy
  Worker behavior remain available.
- Enrollment names a stable device ID, request ID, projects and exact
  `(project_id, workspace_key, agent_id)` capabilities. Paths and executables
  stay in the local Host configuration. Each requested project must have an
  advertised capability; capabilities cannot introduce additional project scope.
- The device generates 32 random secret bytes and saves the pending enrollment
  through the platform credential store before making its request. The server
  derives the separate Worker bearer token from the Worker ID and that secret,
  stores only its hash and returns the token. User credentials never become
  Worker credentials and never enter the Agent process.
- A device belongs to its Client identity. A receipt fingerprints the canonical
  request, including the secret. Retrying the same enrollment retrieves the same
  Worker and credential after a lost reply. Changed requests with the same ID,
  disabled receipts and receipts superseded by a later configuration return 409.
- A new request may reconfigure the same owned device and rotate its credential.
  Reconfiguration and credential disable reject active or unresolved Runs;
  they cannot silently detach an executing process from its lease authority.
  Local stop need not revoke the saved credential. An explicit owned-device
  disable endpoint revokes it once execution has stopped.
- Cloud intersects enrolled-device scope with its owner's current Client grants
  and matches capabilities before granting a Run lease. A
  missing Agent or workspace does not consume an incompatible queued Run.
  Legacy administrator-provisioned Workers retain their existing project-scoped
  claim semantics. Neither type implies heartbeat-based online discovery.
- Host supervision, local workspace admission, provider authentication and
  approval behavior remain execution-side responsibilities. Enrollment alone
  does not start or dispatch a task. The initial UI configuration is one project,
  one workspace and one Agent; enrollment does not create isolated checkouts.

## Verification and limits

The full server race suite passed against an isolated local PostgreSQL cluster,
including actual HTTP ownership checks and migration application. Enrollment
receipts, credential rotation/disable, project grants and capability filtering
before lease grant were exercised. Existing legacy Worker tests also passed.

Desktop/native wiring and packaged delivery require their own evidence. No live
Cloud enrollment, service deployment or production execution was performed by
these server tests.
