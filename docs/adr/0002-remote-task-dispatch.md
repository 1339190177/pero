# Remote task dispatch and task sources

Status: accepted, 2026-10-28. Implementation starts with remote execution.

## Context

The Desktop already launches a Rust Host and ACP sessions. The Go server only
provides health discovery. OpenPero needs durable tasks that an authorized remote
machine can execute while the Desktop observes progress and requests cancellation.
GitHub Issues and ROADMAP.md will supply tasks through the same task model.

## Decision

- Extend `server/` as one Go control-plane process backed by PostgreSQL. Keep ACP
  execution in `pero-runtime`; add an independent headless `pero-worker` binary.
- Cloud is Go-first: future Kubernetes Provider/Controller, scheduling and resource
  lifecycle coordination belong to Go. The Worker wire protocol is language-neutral;
  reusing Rust ACP now does not require future Go or Kubernetes Workers to use Rust.
- Workers establish outbound authenticated HTTPS requests. User and Worker tokens
  have separate scopes. A configured operator provisions project-scoped Workers;
  anonymous registration and execution are disabled.
- Task describes the goal and acceptance criteria. Each explicit dispatch creates
  a Run with an immutable input snapshot. A PostgreSQL transaction claims a Run,
  binds Worker and process-instance identities, and grants an expiring lease.
- Start with one slot per Worker. Claim requests, event batches and completion are
  idempotent. Renewals and new writes require the current Run, generation, Worker
  instance and unexpired lease. A completed request can retrieve its original
  receipt after its lease ends. Old executions cannot replace current results.
- Worker state is persisted before execution. Restart reconciles the original Run;
  it never blindly replays an interrupted prompt. Lease loss stops execution.
  Unknown cleanup or effects require reconciliation; they are not automatic retries.
- ACP cancellation is followed by process supervision and bounded cleanup. Pending
  ACP permission requests are denied in the first unattended Worker slice. Agent
  configurations and allowed workspaces are controlled on the Worker machine.
- Workspace keys map to explicitly allowed local directories. Hold an exclusive
  workspace lock for a Run. This initial trusted shared-checkout mode does not
  provide sandbox isolation; container/VM and isolated Git preparation are later
  capabilities. Task payloads cannot select arbitrary paths or executable commands.
- Execution success records an Agent outcome. Independent verification, acceptance,
  GitHub publication and external deployment remain separate operations.
- Desktop uses a bounded main-process HTTP adapter; credentials stay outside the
  renderer. It lists remote tasks, explicitly dispatches them, reads persisted
  progress and requests cancellation. Initial polling can later become resumable SSE.

## Task sources and authority

GitHub Issues and local/repository ROADMAP.md feed SourceItem revisions into the
same Task inbox. Source identity is stable across edits; Markdown headings retain
context, checklist parents can be groups, and ambiguous identity changes require
review. Importing content does not grant execution permission. Source completion
does not manufacture a Verification or acceptance Decision.

A cloud-owned Task is dispatched through Cloud even when its Worker is the local
machine. Pure local tasks retain local authority. Moving a local task to Cloud
requires an idempotent handoff and reconciliation of active local execution.

## Delivery order

1. Remote first: PostgreSQL Task/Run/Worker, authenticated claim/renew/events/result,
   Rust Worker with local journal and supervision, and actual Desktop integration.
2. GitHub Issue and ROADMAP adapters, stable source bindings and task inbox sync.
3. Independent candidate verification, authorized source writeback and Publisher.
4. Additional environment providers and production operations.

The first slice uses explicitly configured operator credentials and manually
created tasks. It does not claim GitHub login, public multi-tenant hosting,
untrusted-code isolation, automatic PR creation or production acceptance.

## Hosting

`openpero.com` is the public site; `app.openpero.com` can serve Web and same-origin
API routes. The existing HTTPS proxy may share a machine with `dsh.phodal.com`,
with separate service identities, volumes, database roles and resource limits.
Worker code execution should initially run on other machines. Bind address,
database URL and credentials are configuration, not hard-coded domain names.

## Verification

Use real PostgreSQL to test concurrent claims, lost responses, restart durability,
authorization, event replay, expired lease rejection and cancellation races. Run
the independent Rust Worker against that server and an ACP fixture, then launch
real Electron for create/dispatch/progress/cancel/error and narrow/dark checks.
Fixture execution is protocol acceptance; real provider and deployed Linux
acceptance must be recorded separately.
