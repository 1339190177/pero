# Explicit native tool composition

`--workspace /absolute/root` enables `read_file`, `write_file`, `edit_file`,
`list_directory`, and literal content `search_files`. Session cwd must resolve to
that exact workspace. ACP new/load/resume reject a mismatch before session or
MCP activation; tools still recheck at execution in case the root changes.
Reads/list/search rely on this explicit root grant.
Every write/edit call requires a fresh approval; refusal or unavailable client
fails closed. `--approval never` denies these operations even inside workspace.
No persistent allow-all grant is introduced.

`--enable-shell` additionally registers `shell`; every invocation requires ACP
approval. It starts fresh noninteractive Bash, default `/bin/bash`, with only a
small explicit environment (no inherited credentials), 30 s default / 120 s max
timeout, 16 KiB captured per output stream (further limited to 4 KiB per stream for
worst-case JSON escaping) and process-group cancellation.
**Shell is not sandboxed**: approved commands can access outside workspace and
network, and deliberately detached processes can escape process groups. Use
external OS/container isolation when running untrusted commands. The workspace
is a launch cwd, not a shell filesystem security boundary. No background job,
persistent shell, package installation automation or default broad approval.

Encoded output is capped at 64 KiB. File effects retain structured receipts on
failure; `unknown` means inspect/reconcile, not automatically retry. Shell errors
retain output/exit/timeout flags. The runtime may additionally redact a result
when the owning session is cancelled or retired, as its existing safety contract.

The CLI default approval is `ask`; existing unattended Rust Worker rejection
policy is unchanged. Without `--workspace`, the old ACP-only/no-builtins mode
remains available. Tools use the existing real agent registry and lifecycle.

## Explicit sandbox selections

`--sandbox=strict-read-only` and `--sandbox=strict-workspace-write` retain the
Linux/macOS read-and-network isolation requirement with explicit runtime mounts.
They never fall back to Windows ACL confinement.

On Windows, select `--sandbox=windows-acl-read-only` or
`--sandbox=windows-acl-workspace-write`, provide `--workspace`, `--enable-shell`,
`--approval=ask` and an absolute `--shell` path to `pwsh.exe`. Do not provide
`--sandbox-runtime`: Windows ACL does **not** isolate reads or network. The tool
description and result report this partial boundary. Every invocation still
requires approval; no strict native auto-approval descriptor is emitted.

Windows `--workspace` independently enables bounded `read_file`,
`list_directory`, `search_files` and approved **create-new-only** `write_file`.
The create-only description is exposed to the model; an existing target is always
rejected and `edit_file` is omitted. No shell or ACL changes are required for file
tools. Explicit root AGENTS.md loading also uses this read capability.

The Windows partial sandbox adds `shell` to those file tools. Its read-only mode
omits `write_file` as well as `edit_file`; shell remains approved and partial.
File-tool reads/creates remain workspace-handle-confined regardless of the shell's
unconfined-read/network limitations. Session cwd must match the configured
workspace and its original identity.
No Windows shell is enabled without the explicit partial sandbox selection.
