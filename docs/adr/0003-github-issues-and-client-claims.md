# GitHub issue intake and client ownership

Status: accepted, 2026-10-28. Extends ADR 0002.

## Context

Remote execution needs online task sources and a way for different clients to
coordinate who will do the work. A Worker lease identifies an executor; it does
not identify the person or client claiming an Issue.

Point's GitHub Connector supplies useful references: its connection catalog keeps
credentials out of Agent input, taskinbox normalizes provider facts, and Factory
Issue intake freezes a source digest. Reference implementations are
`go/harness/connectors/registry.go`, `go/harness/taskinbox/github.go`,
`go/harness/entrypoints/web/project_factory_github_issue.go`, and
`packages/code-hosting/github/client.mjs` in Point. Pero retains its Go Task/Run
owner and ports these boundaries rather than adding Point's Host/MCP runtime.

## Decision

- The Go control plane reads explicitly configured GitHub repositories through
  bounded REST requests. Provider origin is fixed to GitHub in production.
  Tokens stay in server configuration. Sync runs at startup and every 60 seconds;
  an operator can also trigger it manually. Signed webhook wakeups can later call
  the same synchronizer.
- Stable source identity is project + immutable repository ID + immutable Issue
  ID. Issue numbers and names are display references. Pull requests returned by
  the Issues API are excluded. Pagination/invalid records/rate errors explicitly
  report incomplete coverage instead of inventing a complete inventory.
- Repeated sync updates the same Task. Goal-bound `source` records the precise
  input revision; `latest_source` separately records changed provider facts.
  A claimed or previously dispatched goal is not silently replaced. Stale goal
  dispatch is rejected; an unexecuted released draft can refresh on the next sync.
  Every Run retains its immutable goal and source snapshot.
- Operator-provisioned Client identities are scoped to projects and have separate
  hashed bearer credentials. They are independent of operator and Worker tokens.
  Client task lists, cursors, details and Workers enforce the stored project scope.
- Human task claim is atomic and independent of Worker lease. Another client
  cannot steal a claim or dispatch/cancel its Run. Release requires ownership and
  no active or unresolved execution. Idempotency receipts are scoped to the actor.
- Automatic dispatch is opt-in per repository, requires an explicit label and
  fixed project/workspace/Agent binding, and applies only to unclaimed Tasks never
  dispatched before. Claim and auto-dispatch use the same Task transaction lock.
  Issue content does not select commands, credentials or arbitrary filesystem paths.
- A synced closed Issue blocks new claims and dispatch; a still queued Run is
  cancelled atomically. Running work continues against its frozen input. Reopen,
  editing or relabeling does not automatically retry completed or uncertain work.
- GitHub remains read-only. Pero claim ownership is not a GitHub assignee change.
  Execution completion does not close an Issue or establish acceptance.

## Client experience

Desktop uses the existing remote-task view. Operator can sync sources; ordinary
clients use `PERO_CLIENT_TOKEN`, see available/mine/all loaded Tasks, claim or
release their idle Tasks, and explicitly execute them. Source links open only a
server-returned, validated canonical GitHub Issue URL. Client credentials never
fall back to operator authority when both are configured.

The initial identities are provisioned clients, not GitHub OAuth accounts. Public
signup, revocation, GitHub assignment writeback, verifier acceptance and source
refresh for a previously executed goal remain separate capabilities.

## Verification

Use real PostgreSQL for concurrent ownership, private-project/cursor isolation,
actor-scoped idempotency, source deduplication, source-change provenance, closure,
and auto-vs-human races. Run actual Electron clients against Go/PostgreSQL with a
fixture GitHub upstream and independent ACP Worker. A public read-only live-source
check is separate from fixture and production acceptance.

Setup and endpoint reference: [Go service](../../server/README.md#client-identities-and-shared-tasks)
and [Desktop](../../desktop/README.md#remote-tasks). Results and limits are tracked
in [remote dispatch verification](../fitness/remote-dispatch-verification.md).
