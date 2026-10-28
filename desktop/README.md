# Pero Desktop

Electron loads the shared GPUI Web/WASM workbench. The isolated renderer has no
Node.js access. A bounded preload bridge connects it to `pero-host --stdio`.
The Rust Host uses the official ACP SDK to supervise one Agent process per live
conversation. Closing the app closes those processes.

## Run

Requirements: Node.js 22.12+, Rust stable and nightly-2026-10-28, and the local
gpui-kit checkout at snapshot `ec89cfc` (0.8.1).

```sh
npm ci
node node_modules/electron/install.js
rustup toolchain install nightly-2026-10-28 --profile minimal --target wasm32-unknown-unknown
export PERO_GPUI_KIT_PATH=/absolute/path/to/gpui-kit
npm run dev
```

The maintainer checkout detects `../../test/gpui-kit` automatically. The Cargo
wrapper patches only Kit packages; matching `gpui-pre-* =0.3.10` snapshots come
from the registry. `/Users/phodal/test/zed` is a source reference, not a second
GPUI dependency. No upstream source is modified.

## Agents and conversations

Choose **New task**, an installed Agent and a workspace. Enter sends;
Shift+Enter inserts a new line. Context adds user-selected text files through a
native file chooser. The Agent and workspace are fixed once connected.

| Agent | Local adapter | Setup |
| --- | --- | --- |
| Codex | `codex-acp` on PATH | Install the ACP adapter and authenticate Codex. Select a model offered by the Agent. |
| Claude | Bundled `@agentclientprotocol/claude-agent-acp` | Authenticate Claude using its supported local login. |
| Grok | `opencode acp` | Install OpenCode and configure xAI authentication. The registry selects `xai/grok-code-fast-1`. |

Installed means an adapter executable was found; it does not establish provider
authentication. Advertised Agent login methods are explicit actions. Terminal
login methods are displayed but require login in a terminal. Authentication
does not automatically resend a failed prompt.

Optional adapters live in ignored `agents.local.json`, or a path supplied with
`PERO_AGENTS_FILE`. Start with [agents.example.json](../agents.example.json).
An entry with an existing ID replaces that built-in entry. `model` is optional;
omit it to retain the Agent's default. Configuration values and credentials are
kept in the native process, never sent in the public Agent list.

Messages retain GPUI Markdown entities. Text deltas are coalesced before document
updates. Tools appear as compact, expandable rows; adjacent read/search/fetch
calls share one summary. Permission requests and failure reasons stay visible.

History is saved in the app's user-data directory (`sessions.jsonl`). Renderer
reload reconstructs live conversations; restarting the app reopens history as
read-only, without automatically resuming or replaying Agent work. Temporary
overrides: `PERO_HISTORY_PATH`, `PERO_SETTINGS_PATH`, `PERO_DESKTOP_DATA_DIR`.

## Verification

Suite map and screenshot paths: [tests/README.md](../tests/README.md). Commands: [fitness](../docs/fitness/README.md).

```sh
npm run check
npm test
npm run test:e2e
# Opt-in: uses the local Codex login and makes one real model request.
npm run test:e2e:real
```

Run desktop tests sequentially because they control native input focus.
See [provider verification](../docs/fitness/acp-provider-verification.md) for
actual Codex/Claude replies and the outstanding Grok authentication check.

The original task overview remains labeled example data. **Remote tasks** is a
separate real Task/Run view backed by the Go service described below. GitHub/PR
publishing, a persistent local Desktop daemon, signed packaging and physical
macOS IME testing remain separate slices. `npm run dev:web` renders the same UI
without the native Agent/remote bridge.

## Remote tasks

Start the [Go control plane](../server/README.md) with PostgreSQL. Launch Desktop
with `PERO_CLOUD_URL` set to its HTTPS origin (loopback HTTP is allowed for local
development) and `PERO_OPERATOR_TOKEN` set to the server's operator credential.
These are process configuration in this first slice; there is no separate
Desktop preference store. Credentials stay in Electron's main process and are
filtered out of the local Host environment.

Open **Remote tasks**, create a draft with its project, allowed workspace key,
Agent ID and completion criteria, then choose **Dispatch**. A provisioned
[independent Worker](../crates/pero-worker/README.md) for that project receives the
Run even if Desktop subsequently closes. Agent choices are identifiers to resolve
on the Worker; local installation does not establish remote availability.

The view polls durable task state and recent events, supports task pagination,
and marks a truncated event tail explicitly. Stop requires confirmation and stays
pending until the Worker acknowledges. `reconcile` prevents redispatch while
execution effects or cleanup remain unknown. Execution completion is distinct
from independent verification and acceptance.

Build and exercise the complete local protocol path:

```sh
npm run build
npm run build:worker
PERO_TEST_DATABASE_URL='postgres://.../pero_test?sslmode=disable' npm run test:remote
```

This launches real Electron, Go, PostgreSQL and a separate Rust Worker using an
ACP fixture. It checks UI create/dispatch/progress/cancel, server restart recovery,
connection errors, language, dark/narrow layouts and credential exclusion. It
does not deploy a service to openpero.com or certify a real model provider.
