# Go control plane

The service persists manual Tasks, immutable Run snapshots, project-scoped
Workers, claims, leases, events and completion receipts in PostgreSQL. The Desktop
can create and dispatch a task, inspect progress and request cancellation. Workers
make outbound requests; ACP execution remains on the execution machine.

## Run

Set `PERO_DATABASE_URL` to a PostgreSQL database and `PERO_OPERATOR_TOKEN` to a
private random operator credential, then run `go run ./cmd/server` from `server/`.
The database role needs schema creation rights for the first startup. Versioned
embedded migrations are applied under a database advisory lock.

| Variable | Meaning |
| --- | --- |
| `PERO_DATABASE_URL` | Required PostgreSQL connection URL |
| `PERO_OPERATOR_TOKEN` | Required operator bearer credential |
| `PERO_LISTEN` | Bind address; defaults to `127.0.0.1:8787` |
| `PERO_LEASE_SECONDS` | Lease duration, 1–3600 seconds; defaults to 60 |

`GET /healthz` checks database connectivity without authentication. All other
routes require a bearer credential. Protect any non-loopback deployment with
HTTPS at the existing reverse proxy; the service does not terminate TLS itself.
Never put operator or Worker credentials in a renderer or source control.

## API

Operator routes use `Authorization: Bearer <PERO_OPERATOR_TOKEN>`:

```text
GET  /api/v1/tasks
POST /api/v1/tasks
GET  /api/v1/tasks/{id}
POST /api/v1/tasks/{id}/dispatch
POST /api/v1/tasks/{id}/cancel
GET  /api/v1/workers
POST /api/v1/workers
```

Task creation accepts `title`, `prompt`, `acceptance`, `project_id`,
`workspace_key`, and `agent_id`. It creates a draft, with no automatic execution.
Dispatch accepts an optional `worker_id`. The Run freezes the Task input. Repeating
dispatch while the same target Run is active returns that Run. Create and dispatch
also accept an `Idempotency-Key` header (maximum 128 bytes); reusing a key for a
different operation or payload returns 409.

Worker provisioning accepts `name`, nonempty `projects`, and optional `labels`.
The response returns a Worker and its credential once; only a SHA-256 digest is
stored. Labels are descriptive in this slice; project scope is enforced by the
server and workspace/Agent capability checks are enforced on the Worker. There is
one slot per Worker, across all its process instances.

Worker routes use their separate provisioned bearer credential:

```text
POST /api/worker/v1/claims
GET  /api/worker/v1/runs/{id}?instance_id=...
POST /api/worker/v1/runs/{id}/renew
POST /api/worker/v1/runs/{id}/events
POST /api/worker/v1/runs/{id}/complete
```

Claims accept `request_id` and `instance_id` and return `{run: Run|null}`. Persist
the request before sending it: repeating it retrieves the original assignment.
An empty claim also has a receipt, so a later poll uses a new request ID. A claim
receipt returns the Run's current state; a recovered terminal or expired Run must
not start another Agent prompt.

Renew, events and complete require `generation` and `instance_id` matching the
assigned Worker and an unexpired lease. Events contain a contiguous `seq` starting
at one, `kind`, `text`, and optional JSON `data`. A duplicate identical sequence is
acknowledged; conflicting payloads or gaps return 409. Complete accepts `outcome`
(`succeeded`, `failed`, `cancelled`, `reconcile`) and `summary`. An identical saved
completion receipt remains retrievable after lease expiry; a different result
cannot replace it.

Queued cancellation is immediate. Active cancellation remains `cancel_requested`
until the Worker confirms stopping, or becomes `reconcile` when its lease expires.
Success cannot overwrite a pending cancellation. Expired runs enter `reconcile`
and never requeue automatically; their Tasks cannot redispatch until a future
explicit reconciliation workflow resolves the unknown execution effects.

Task lists default to 50 rows, bounded to 512 KiB, and accept `cursor` and `limit`
(1–50). Responses contain `tasks`, `has_more` and nullable `next_cursor`. The cursor
is the last returned Task ID, ordered by creation time and ID descending. The list
omits the optional nested Run; detail contains its full snapshot.

Detail returns the newest 200 events, bounded to 256 KiB and ordered by sequence.
`events_truncated` explicitly reports omitted earlier evidence, and nullable
`events_first_seq` / `events_last_seq` identify the returned range. All events remain
in PostgreSQL; older-event pagination/export is a later slice. Source ingestion, artifact storage, independent
verification, user/team login, device revocation, PR publication and public
multi-tenant operations remain later slices. Any unresolved `reconcile` Run also
quarantines its assigned Worker's slot, preventing another task from overlapping
unknown cleanup. A future explicit resolution workflow must clear this quarantine.
`succeeded` records execution outcome,
not independent acceptance.

## Verify

```sh
go test ./...
PERO_TEST_DATABASE_URL='postgres://.../pero_test?sslmode=disable' go test -race ./...
```

The integration suite creates and drops isolated schemas in the supplied test
database. It verifies real concurrent claims, restart durability, frozen inputs,
lease fencing, token/project/instance isolation, event conflicts, completion
receipts, idempotency and cancellation. Without the test URL, integration cases
explicitly skip; ordinary compilation alone does not establish dispatch behavior.

## Deployment and Kubernetes direction

`openpero.com` can serve the public site and `app.openpero.com` the same-origin Web
and API. The HTTPS proxy can share a machine with `dsh.phodal.com`, with separate
service identities, data volumes, database roles and resource limits. Start actual
Worker execution on other machines.

Go owns coordination, scheduling and future environment/provider adapters. A future
Go Kubernetes provider can create approved execution Pods or Jobs and reconcile
their lifecycle through the same Task/Run protocol. It does not require moving
control-plane state or scheduling into Rust. Kubernetes provisioning, sandbox
isolation and production deployment are not implemented by this slice.
