---
kind: verification_report
date: 2026-10-28
---

# GPUI conversation verification

The current implementation supersedes the initial echo-only bootstrap acceptance.

- Development builds and `npm run check` passed.
- `npm test`: 22 native contracts, runtime lifecycle, streaming and presentation tests passed.
- `npm run test:e2e`: Chinese composition, multi-turn, cancel, permissions, 1000-chunk exact rendered text, reader scroll preservation, replay, locale, light/dark/narrow, native selected context files and nine-call tool matrix passed; zero console errors.
- Streaming sample: 1000 received chunks, 27 document updates. This is a development-window observation, not a production benchmark or parser invocation count.
- Tool details create no Markdown entities while collapsed. Expansion creates a body; collapse releases it; reopening recreates from retained raw data. Inactive conversations also release tool bodies. Full viewport virtualization is not implemented.
- `npm run test:e2e:real`: actual Codex keyboard model selection, ACP acknowledgement, exact PERO_DESKTOP_OK reply and selected model after renderer reload passed. Claude native reply passed separately. Grok requires xAI authentication; see acp-provider-verification.md.
- Syntax coloring uses GPUI Kit's syntect feature, preserving retained Markdown and cached code blocks. Rust string coloring and JetBrains Mono were inspected in the real Electron screenshot.

Artifacts: test-results/workbench, test-results/toolcalls, test-results/real-session.
Physical OS IME, signed packaging, a live terminal attachment, full transcript
virtualization, and an xAI-authenticated Grok reply are outside this record.

Zed references inspected locally: crates/acp_thread/src/acp_thread.rs
(StreamingTextBuffer, update_markdown_in_place), crates/markdown/src/markdown.rs
(append and queued parsing), and crates/agent_ui model selector/tool views.
Pero follows retained entities, incremental append, 16ms ticks and adaptive
200ms reveal, using Kit's Web-compatible document/parser rather than linking
Zed's distinct GPUI snapshot. Terminal completion immediately flushes all text.
