# Real tools through the headless Worker

Run the production Go ACP executable and production Rust Worker against local
scripted provider/control-plane endpoints:

```sh
node tests/runtime/go-tools-worker.mjs \
  /absolute/path/to/pero-worker /absolute/path/to/pero-agent-acp
```

This suite uses no test-only tool assembly, real model request, account, remote
service, user file or provider credential. Each case creates fresh private
fixtures and starts the real runtime with `--workspace <fixture>` and
`--approval ask`. Only the shell-denial case adds `--enable-shell`.

Checks cover:

- Actual `read_file`, `list_directory`, and `search_files` success, without
  per-call permission prompts under the explicitly granted read workspace.
- Actual `write_file`, `edit_file`, and `shell` permission requests refused by
  the unattended Worker. Existing content stays unchanged and the shell marker
  is never created.
- File tools reject absolute/outside paths, `..`, and both internal and escaping
  symlinks. Search skips an escaping symlink and exposes no outside sentinel.
- Files larger than 1 MiB fail explicitly. A supported read whose encoded result
  exceeds 64 KiB returns an explicit truncation marker, not an unbounded result.
- A real read followed by an in-flight provider request is cancelled by Cloud;
  the HTTP request closes and no late successful result replaces cancellation.
- Shell is not advertised without explicit opt-in. Fixture credentials, outside
  content and omitted-output sentinels do not enter model history or uploaded
  Worker events.

The report separates the actual tool status from the Run outcome. A Run can
finish successfully after the model receives and acknowledges a denied/failed
tool call; that does not mean the tool ran or the task was accepted.

The Worker deliberately cannot approve calls, so this suite does not establish
successful writes, edits, shell execution, or cancellation of an already running
shell. Those require the separate real ACP-client allow-once tests. File-tool
workspace containment is not an OS sandbox: an approved shell may access outside
the workspace or the network. Database authorization/concurrency and production
deployment are not tested by the loopback Cloud fixture.

All executable hashes are checked before and after the suite. A rebuilt runtime
must be retested; an earlier passing binary is not evidence for a changed one.
After a short natural-exit grace period, any owned runtime left alive is forcibly
cleaned up and makes the case fail; cleanup cannot silently turn a leak into a pass.
