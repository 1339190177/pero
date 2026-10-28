# Agent session

Package `github.com/phodal/pero/harness/go/session` owns the execution transcript,
model-visible surface, durable input queue and local JSONL persistence. It is
not a Project/Task database, Agent loop, tool runner or ACP server.

## Ownership and durability

- `Session` is the single event-log truth; projections and context plans are
  derived values. Restoring validates events without replaying live observers.
- `JSONLSessionPersistence.Attach` owns one live durability checkpoint and one
  cross-process writer lease per session. Call its returned detach exactly once
  at owner shutdown (repeated calls are safe). Borrowers use `Session.Flush`;
  they must not detach the owner's checkpoint.
- In-memory append success is not a durable acknowledgement. Call `Flush` at
  semantic durability boundaries and handle its error. A read-only load may
  inspect a live log; it is not permission to attach a second writer.
- Detached loads and snapshots do not alias the live event buffers. Stale
  snapshots cannot replace a newer persisted log. Interrupted tool execution
  repairs to explicit unknown/not-started outcomes, never blind re-execution.
- Local filesystem locks are not a distributed lease. The caller chooses and
  controls the storage directory and must supply its own authorization and
  data-isolation boundary. No filesystem sandbox is created by this package.

## Pero boundary

Extracted from `phodal/point` commit
`69a1f8692725078c4ce7feaf497f5b80799ca44d`, `go/harness/session`.
`SOURCE.json` pins retained original files and records removed policy code.
No Point imports, Hub, project binding, project cleanup operation, product
plugin composition or network/model calls are included.

`pero.context.plan.v1` is an independent receipt format: it preserves the
surface and image-offload projection and does not select content by plugin
identity, tool name, channel policy or source-rule metadata. The retained
`memoryTokens`/`memoryBudget` fields are zero, not an automatic memory policy.
Point v1–v4 context-plan receipts are rejected on append and restore, not
reinterpreted under the new algorithm. This is not a Point-log migration tool.
Ordinary opaque metadata still round-trips; retaining it grants no authority.
The previous Host-specific `dsh-session:` claimed-input rewrite exception is
also removed: durable message identity requires exact content equality.

Dependencies: sibling `model` and `runtime` packages in the same Go module,
and `golang.org/x/sys v0.47.0` for Unix/Windows file locking. No local replaces.

## Verification

From `harness/go`, with Go 1.25+:

```sh
GOWORK=off go test ./session
GOWORK=off go test -race ./session
GOWORK=off go vet ./session
```

Selected self-contained upstream tests cover log invariants, publication,
single writers across processes, stale resumes, repair, fork boundaries,
opaque values, input admission, image offload and projections. New tests cover
Pero receipt hashes, rejection of old Point receipts, no product-policy
interpretation, canceled loads and durable input recovery. All tests are local;
no API key or model invocation is needed. Windows lock code is compile-checked
only on this Linux validation host, not runtime-tested here.

## License status

Repository-owner publication authorization does not establish a blanket
license. No general Point LICENSE was found at the pinned commit. The source's
DeepSeek MIT notice is retained verbatim as `DEEPSEEK_LICENSE.txt`; it covers
applicable DeepSeek-derived algorithms, not automatically all Point-authored
source. The repository owner still needs to choose the remaining license.
