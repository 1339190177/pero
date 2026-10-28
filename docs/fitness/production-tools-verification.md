# Production file and shell tool verification

This suite executes the real Go ACP entrypoint and its production native tools.
All files are in fresh private temporary directories. Model and Cloud responses
come from loopback fixtures; no real model, credential or user file is involved.

## Reproduce

Build `harness/go/cmd/pero-agent-acp` and the existing Rust Worker with the
repository's supported build setup. Build the small test driver against the
**existing** `pero-runtime` (it does not implement another ACP wire client):

```sh
cargo build --locked --manifest-path tests/fixtures/runtime-tool-client/Cargo.toml
node tests/runtime/go-tools-worker.mjs /absolute/pero-worker /absolute/pero-agent-acp
node tests/runtime/go-tools-runtime.mjs \
  "$PWD/tests/fixtures/runtime-tool-client/target/debug/pero-runtime-tool-client-fixture" \
  /absolute/pero-agent-acp
```

The scripts supply an explicit fake credential variable via `--api-key-env`; they
do not rely on `PERO_*` variables, which the Worker removes. The production default
is `AGENT_PROVIDER_API_KEY`. Never commit a real provider key into Agent config.

## Two distinct authorization paths

- **Worker:** actual read/list/search, bounded output and cancellation succeed;
  writes, edits and shell calls are denied with no file effects. Absolute paths,
  traversal, symlinks and oversized files are rejected. Without shell opt-in,
  that tool is not advertised. Tool status is checked independently of Run status.
- **Explicit allow-once:** the real Rust runtime answers only named fixture call
  IDs, once each. Production write/edit/read and shell execute. Tests check the
  actual file contents, tool status, exit code, credential-free child environment,
  bounded output, timeout, and cancellation of an already-started shell plus its
  child process. An approval does not bypass file-path checks or `never` policy.

Every write/edit/shell call still needs approval in production. These approvals
apply only to this test run, not to future tasks or the unattended Worker. Shell
is explicitly enabled and **is not an OS or network sandbox**. Its cwd and minimal
environment are not proof of filesystem/network isolation.

The files API is descriptor-rooted, text-only and bounded: 1 MiB files, bounded
listing/search and a 64 KiB encoded-result ceiling. Shell capture is limited to
16 KiB per stream, further narrowed before JSON encoding while preserving exit,
timeout and truncation facts. A nonzero shell exit remains a failed tool result.

Both suites hash executables before and after execution, reject changes during
the run and require normal process cleanup; forced orphan cleanup is a failed
case, not a success. Exact hashes, source fingerprints and scenario results are
recorded in the verification receipt. Rebuilding requires rerunning the suite.

This does not certify real-provider compatibility, production authorization,
PostgreSQL concurrency, arbitrary MCP approval, deployment or untrusted-code
isolation. The Worker is not automatically authorized to modify files merely
because these tools are installed.

## Recorded run

The frozen production binary `850947243799f814c43a08df6ad397451db270b7a08da5bc6473140dd5dd1646`
passed 15 runtime scenarios and 14 Worker scenarios (29 total). These results
precede the optional live-test budget flags; a changed binary needs a new run.
