---
kind: incident
status: open
---

# Conversation auto-follow can override a scroll toward earlier output

The existing Workbench Electron suite failed twice at
`tests/e2e/workbench.mjs:53`: after one wheel gesture of -4000 during the long ACP
fixture stream, `scroll.y` remained -1583 instead of zero. The declared scroll
extent was smaller than the gesture. No page errors were reported.

The failure was observed while verifying the remote-dispatch slice and again
after the other Desktop changes finished. Remote task create/dispatch/cancel,
pagination, recovery and theme tests pass separately. No conversation/stream
source was changed by the remote implementation.

The suspected cause is an already queued `scroll_to_bottom` from a parsed message
or session event being applied on layout after the wheel event. This is a
diagnosis candidate, not an established fix. Explicit user scroll intent and
pending-follow cancellation need a focused repro. Repeating the wheel in the
test alone would not prove that the single-gesture race was fixed.

Reproduce with `npm run test:e2e` after building Desktop. The latest output and
failure screenshot are in `test-results/workbench/` (generated artifacts).
