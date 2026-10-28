# Remote Worker

The independent Rust Worker pulls one authorized Run at a time from the Go
control plane and executes it through `pero-runtime`'s official ACP integration.
It does not require Electron to remain open.

Build with `node scripts/cargo.mjs build -p pero-worker`. Configure these local
environment variables, then run `target/debug/pero-worker` (`--once` checks the
queue once and exits after at most one execution):

- `PERO_CLOUD_URL`: HTTPS origin. HTTP is accepted only for loopback development.
- `PERO_WORKER_TOKEN`: project-scoped token returned when the operator provisions
  a Worker. It is separate from the operator token.
- `PERO_WORKER_CONFIG`: path to the local configuration below.
- `PERO_WORKER_STATE`: path to a persistent local journal, on durable local storage.

```json
{
  "workspaces": { "default": "/srv/pero/workspaces/example" },
  "agents_file": "agents.json"
}
```

`agents_file` is an array of local `AgentConfig` entries, for example:

```json
[
  { "id": "codex", "name": "Codex", "command": "/usr/local/bin/codex-acp", "args": [], "env": {} }
]
```

Relative configuration paths resolve beside the Worker configuration file. If
`agents_file` is omitted, the existing local ACP registry supplies the allowed
Agents. Cloud tasks supply only `workspace_key` and `agent_id`, never executable
commands or directory paths. Protect local configuration and provider credentials
with the Worker OS account's file permissions.

The Worker persists the claim identity before contacting Cloud and the Run before
launching any Agent. Events are journaled before upload and assigned monotonic
sequence numbers. Completion receipts are retried with the same payload. Each
batch stays below 480 KiB of encoded JSON, including its envelope. Individual
event fields are checked against the Go API's byte limits before journaling.
The supervised execution input accepts up to 512 KiB, including full-length
Unicode prompts and acceptance criteria.
A restart reconciles interrupted execution without replaying its prompt, then
stops so the operator can review effects. The journal has an exclusive process
lock, and the selected checkout has an exclusive `.pero-worker.lock` write lock.

Renewals continue even while the Agent produces no output. An independent
monotonic watchdog stops execution when its lease expires. The Worker first
requests ACP cancellation, then enforces bounded cleanup of its executor and
the Agent's separate SDK process group. The executor also detects supervisor
death. Failure to confirm cleanup produces `reconcile` and stops task intake.
Unattended permission requests are denied. All `PERO_*` environment variables are
removed from the executor and Agent configuration; provider credentials should
use the provider's own variable names.

The versioned Worker HTTP contract is owned by the Go control plane and is
independent of Kubernetes or another resource provider.

This slice supports Unix hosts with `/bin/ps`; macOS has been exercised, while
deployed Linux acceptance remains pending. It uses an explicitly allowed,
trusted shared checkout. Process groups and workspace locks do not isolate
untrusted code, and deliberately detached processes require a stronger runtime
such as a container/cgroup or VM. Agent success records only the completed turn;
independent verification, acceptance and publication remain separate.

Run `node scripts/cargo.mjs test -p pero-worker` for journal, protocol, subprocess,
permission, cancellation, lease-loss and interrupted-claim checks. The HTTP/ACP
fixtures do not establish production or real model-provider acceptance.
