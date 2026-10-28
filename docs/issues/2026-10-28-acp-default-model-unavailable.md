---
kind: incident
status: resolved
---

# Configured Codex model unavailable for the account

Real codex-acp 1.8.0 initializes and creates a session, but the local default
`gpt-6.1-sol` produces model_not_found for the current ChatGPT authentication.
The Agent returns that error as message content and finishes the prompt turn.
An ACP end_turn therefore does not establish a successful model response.

The workbench exposes the advertised model options and keeps Agent error content
visible. Selecting advertised `gpt-5.6-sol` through session/set_config_option
produced the exact real response PERO_ACP_OK. User-global Codex configuration was
not changed. Provider-pinned models must be selected successfully before the Host
emits session_ready, so a Grok session cannot silently run the Agent's default model.
