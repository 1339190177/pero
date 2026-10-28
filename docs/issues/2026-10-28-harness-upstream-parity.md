---
kind: incident
status: resolved
---

# Restore upstream response, read-window and cancellation behavior

Comparison baseline: official DeepSeek Harness
[`f3803c96974ad4a94195fb97e5c637e9c2a3afe3`](https://github.com/deepseek-ai/deepseek-harness/tree/f3803c96974ad4a94195fb97e5c637e9c2a3afe3),
release 0.2.2-alpha.2. Pero's Go implementation was extracted through Point;
this is a behavioral comparison, not a claim that the languages or product
surfaces are identical.

## Findings and fixes

- **Output limit:** the OpenAI adapter tried to validate truncated tool-call
  arguments and classified `finish_reason=length` as a malformed response. It
  now discards every tool call from that response, preserves text/reasoning and
  reports the existing max-tokens stop. This prevents retrying or executing a
  partial tool call. Upstream: `packages/llm/llm/src/assembler.ts` and
  `packages/llm/llm-deepseek/src/translate.ts`.
- **Context recovery:** explicit context overflow in HTTP 413 was classified as
  an invalid request, bypassing existing compaction recovery. Explicit overflow
  now receives the context-window classification independently of the generic
  request-size status. Generic 400/413, authentication and rate-limit outcomes
  retain their classifications. Upstream:
  `packages/llm/llm-deepseek/src/transport.ts` and its `tests/runtime.spec.ts`.
- **File windows:** a terminal newline created a phantom line and an empty file
  counted as one line; byte truncation could cut a line with no next offset.
  Read windows now use physical lines, strip CRLF carriage returns, return
  `total_lines` and `next_offset`, and stop at whole-line boundaries under the
  existing 16 KiB budget. Lines over 2000 Unicode characters carry an explicit
  truncation marker. This mirrors upstream's bounded line presentation, without
  adopting its larger byte budget. Upstream:
  `packages/fs/tool-fs/src/read-render.ts`. Path, digest and existing JSON fields
  remain; out-of-range phantom-line requests now fail.
- **Cancellation admission:** `session/cancel` could arrive after ACP accepted
  a prompt but before the Agent installed its active-turn canceler. The provider
  still ran although ACP eventually reported cancelled. A per-prompt context
  now spans admission and execution; cancellation and close cancel that context.
  ACP keeps its prompt slot until cancellation settles. Upstream:
  `packages/acp/acp/src/session.ts` and `tests/turns.spec.ts`.

## Regression evidence

The provider table fails before restoration on partial/complete tool calls at
output limit and explicit 413 overflow. The read-window table fails on phantom
lines, CRLF and continuation. The admission regression cancels on the observed
inbox insertion event, before dispatch: the old source executes the provider
once, while the restored source executes it zero times. It also checks close
at admission, the next normal prompt, and cancellation cleanup ownership.

The existing CLI process tests also prove an actual HTTP 413 triggers summary
then main-request retry without pressure compaction masking the cause; an
output-limited complete tool call returns ACP `max_tokens` without a second
durable todo write. Both fail with the old provider source and pass after repair.

Tests use deterministic events and loopback adapters, not paid inference.
No injected BOM fixture defect was treated as a product bug. Memory remains
separate in issue #9; approval, workspace restrictions, provider budgets and
one-level subagent limits are unchanged. This does not add OS confinement,
crash-exactly-once execution, UI, or autonomous real-model acceptance.
