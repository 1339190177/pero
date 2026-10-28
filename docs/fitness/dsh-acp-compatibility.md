# Headless DSH ACP compatibility candidate

Issue: https://github.com/phodal/pero/issues/3

Base: `codex/workbench-components`, commit
`926e9587b73522305ed2a585684878414c345000`.

## Existing integration boundary

Reuse `pero-worker` (Rust) → `pero-runtime` ACP client → explicitly configured
runtime process. The Go `server/` remains the Task/Run dispatch authority. Do not
add another scheduler or couple runtime completion to task acceptance.

The public branch already has a headless Worker; this does **not** mean a
standalone, Point-independent DSH executable exists. Extracting that executable
and its dependencies is a separate prerequisite. No runtime binary, credentials,
provider request, deployment, or user-computer access is used by these tests.

## DSH compatibility constraints

- ACP protocol version 1, JSON-RPC over stdio; diagnostic output must stay off stdout.
- Use `session/new` with the allowed absolute workspace and no extra directories.
- DSH currently returns an empty `configOptions` array and has no
  `session/set_model` implementation. Leave `AgentConfig.model` absent/null;
  choose the provider/model through the standalone runtime's documented startup
  configuration once that entrypoint is available. Do not guess its CLI flags.
- Text, tool-call, tool-result updates and the minimal permission request shape
  are covered by deterministic synthetic protocol fixtures, not copied runtime
  code. Unattended permission decisions remain denied by the existing Worker.
- No automatic resume is added: the current Worker journal reconciles interrupted
  Runs rather than replaying prompts. DSH advertising session load does not grant
  authority to restart a cancelled or unknown execution.
- The runtime rejects an unsupported negotiated ACP version before session setup.

## Verification boundaries

The three changed code/test files are `session.rs`, `acp_lifecycle.rs`, and
`lifecycle-agent.mjs`. The fixtures use no credentials, tools, network requests,
or real models. These checks do not prove independent DSH buildability, actual
DSH-to-Worker execution, persistence/resume, or production acceptance.

The public snapshot does not include issue #2's local Client/community commits
`7a5a03f` or `87c0045` (GitHub commit lookups returned 422). It is therefore an
earlier dispatch base, not the latest unpublished interface integration.

Verified in a private cloud workspace using Rust 1.99.1 and Node 24.22.1:
`cargo test -p pero-runtime -p pero-worker` passed 16 runtime tests, 6 Worker unit
tests, and 6 Worker subprocess/HTTP fixture tests. Only the verification workspace
removed the unused `pero-host`/`pero-ui` workspace members because its GPUI source
dependency is not present. Cargo pruned unused lock entries; all retained package
names, versions, and sources matched the public lock (no upgrades). This is a
focused headless test result, not a full repository build. The deliverable does
not modify the upstream workspace manifest or lockfile.

`node --check` and `git diff --check` passed. Clippy completed with four existing
`collapsible_if` warnings in runtime model selection and Worker code; strict
`-D warnings` is therefore not a pass. No unrelated lint refactor is included.

Official protocol references:
- https://agentclientprotocol.com/protocol/v1/initialization
- https://agentclientprotocol.com/protocol/v1/session-setup
