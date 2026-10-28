---
kind: verification_report
date: 2026-10-28
---

# Real ACP provider verification

These checks distinguish a successful ACP connection from an actual model reply.
They do not establish tool execution, repository modification or task acceptance.

| Provider | Connection / session | Model reply | Evidence |
| --- | --- | --- | --- |
| Codex through codex-acp | Passed | Exact `PERO_ACP_OK` reply after selecting advertised `gpt-5.6-sol` | `/tmp/pero-codex2-events.jsonl`: `session_ready`, `model_changed`, streamed reply, `turn_finished` |
| Claude through claude-agent-acp | Passed | Exact `PERO_CLAUDE_OK` reply | `/tmp/pero-claude-events.jsonl`: `session_ready`, streamed reply, `turn_finished` |
| Grok through OpenCode ACP | Passed with the current provider configuration | Blocked by xAI authentication; no Grok reply | Current probe described below |

The Codex and Claude replies were reconstructed from their actual
`agent_message_chunk` events and checked for exact equality with the requested
strings. They were not inferred solely from `end_turn`.

## Grok current-configuration probe

The probe used the current registry entry, without a local agents override:

- OpenCode `1.18.30`, invoked as `opencode acp`.
- `OPENCODE_CONFIG_CONTENT` defines provider `xai`, package `@ai-sdk/xai`, and
  selected model `xai/grok-code-fast-1`.
- The registry's `AgentConfig.model` is unset, so it does not issue an extra
  startup model-change request against an unconfigured provider.
- Pero settings and history were redirected to a temporary directory. Credential
  files were not inspected or modified. `XAI_API_KEY` was absent from the probe's
  environment.

The first sandboxed attempt stopped before initialization because OpenCode could
not open its normal local log file. An automatically approved retry allowed its
normal cache/log access and completed `initialize` and `session/new`. The returned
model configuration reported `xai/grok-code-fast-1` as selected.

One prompt was submitted: “Reply exactly PERO_GROK_OK. Do not use tools or change
files.” The provider returned:

```text
Authentication required: provider authentication required
providerId: xai
```

No successful Grok model response was observed. The Agent advertised an Agent
authentication method named `opencode-login` (“Login with opencode”). Existing
credential-store contents were not read, so the missing environment variable is
not by itself proof of the complete stored authentication state; the provider's
authentication error is the decisive evidence.

Probe events are temporarily retained at
`/var/folders/l8/7nm25zns5qjds0jc7z8f1gpm0000gn/T/pero-grok-current-3qszuqcr/events.jsonl`.
The session was closed and the probe Host exited normally.

## Authentication integration

The workbench should present Grok as installed / connected but requiring xAI
authentication, rather than ready to generate a reply. OpenCode can report
`AuthRequired` at prompt time even after session creation succeeds. That path
now exposes the advertised login method while retaining the connection. Login is
explicit, and the user must submit a new prompt after authentication. The failed
prompt and any queued follow-ups are not replayed automatically.

Runtime event contract:

- `session_auth_required`: contains `data.authMethods`, for both session creation
  and prompt-time authentication failures. Show an authentication state, not a
  completed turn.
- `session_auth_error`: keep the authentication UI available. Unknown method IDs
  and terminal methods are rejected; terminal methods require a separate
  interactive-login implementation.
- `session_authenticated`: explicit Agent authentication succeeded on an existing
  session; `data.sessionId` preserves its identity. Return the composer to idle,
  allowing the user to send again. This event does not mean a model reply passed.
- `command_rejected`: queued messages were not sent because authentication became
  necessary; `data.count` reports the number removed from the queue.

The focused fixture regression creates a session, receives prompt-time
`AuthRequired`, rejects the queued follow-up, authenticates explicitly, confirms
that no prompt is replayed, and accepts a new user prompt on the same session.
All ten native lifecycle tests and the earlier native Host build passed. No real
`opencode-login` call was invoked, and xAI authentication remains unverified.

## Model protocol compatibility

SDK 3.5 no longer includes the earlier `models` response and
`current_model_update` notification in its typed schema. The runtime preserves
those known legacy fields while validating standard session fields through the
SDK. `session_ready` and `model_changed` contain the full session data, including
`models.currentModelId` and `models.availableModels` when advertised.

Advertised model configuration takes precedence and uses
`session/set_config_option`. Only an Agent without model configuration and with
an advertised legacy model list can use `session/set_model`. Unknown IDs are
rejected locally; there is no guessed model, automatic fallback after errors, or
prompt replay. Legacy `current_model_update` is forwarded as a session update.
Focused native fixtures cover legacy startup selection, subsequent selection and
notification, modern precedence, and rejection without breaking the session.
