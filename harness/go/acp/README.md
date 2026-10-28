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
