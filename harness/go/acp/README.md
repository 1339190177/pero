# ACP session admission and MCP restoration

Native composition can supply a working-directory validator; new/load/resume
run it before persistence loading or MCP/agent activation. Protocol-only users
without a validator retain the absolute-cwd contract. Tool execution still
rechecks authority after admission.

Resume and load inspect the latest persisted `session/acp-controller` receipt.
For the current version-1 receipt, `requiresClientMCP: true` requires the client
to supply a nonempty `mcpServers` list again. Missing dependencies fail before
agent resumption, interrupted-turn repair or a new controller receipt is written.
Malformed, incomplete or unknown-version controller receipts fail explicitly;
they are never silently treated as `false`. A session without a receipt, or whose
latest supported receipt says `false`, retains its existing behavior.

This is only a dependency-presence check. It does not establish equality of
server names, endpoints, executable paths, versions or permissions. Current MCP
configuration is still explicitly supplied by the trusted ACP client and passes
normal validation. No MCP credentials or endpoint configuration are added to the
receipt, and this change introduces no automatic permission grant or reconnect
configuration policy.

## Incremental output

Live ACP output projects the existing Point-derived `assistant/chunk` events,
without another model runtime or provider dispatch. Text becomes
`agent_message_chunk`; visible reasoning text becomes `agent_thought_chunk`.
Opaque reasoning metadata, usage and partial tool arguments are not exposed as
assistant text. Tool calls/results keep their existing separate updates.

The completion supplies only text/thought suffixes not already streamed (plus
non-text committed content). Non-streaming adapters still project the full
completion. If a provider contradicts its streamed prefix, ACP cannot retract
those bytes: the conflicting final lane is suppressed live, and the committed
completion remains canonical on load. Failed/retried attempts may already be
visible provisionally; attempts reset deduplication, never merge their prefixes.

`session/resume` does not replay old notifications. `session/load` projects only
committed messages and tool events, not provisional chunks; failed uncommitted
prefixes therefore disappear on load, while interrupted committed prefixes
appear once. Cancellation/close seals live projection. The shared harness fences
provider emit callbacks to the exact Generate invocation and cancellation context,
so retained callbacks cannot write into later attempts or reused sessions.
