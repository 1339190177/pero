---
kind: incident
status: resolved
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

The same unchanged test passed once and then reproduced the exact -1583 failure
on the merged component build. This established an intermittent product failure,
rather than a consistently incorrect test coordinate or an error in the new
component split.

The pinned GPUI 0.3.10 implementation stores `scroll_to_bottom` as a boolean
consumed by a later layout. A wheel event changes the offset without cancelling
that boolean, so the subsequent layout can override the reader's gesture. Pero
now requests a concrete bottom offset through window frame callbacks. Both
callback stages discard the request if the offset changed in the meantime;
the second stage also allows the new content to finish layout. Requests originate
from rendering, where the window is available, and refresh only after an actual
offset change. The observer retains its parsed-content notification without
leaving an uncancellable scroll flag.

The original single-wheel gesture and zero-offset assertions remain unchanged.
The test additionally verifies that the conversation follows the tail before
that gesture, so disabling follow entirely cannot satisfy the regression check.
Its failure reporter also preserves the original error when a Wasm panic makes
diagnostics unavailable.

Verification on 2026-10-28 after rebuilding Wasm and Web:

- `npm run test:e2e` passed Workbench and all nine tool outcomes with no page
  errors. The long stream retained all 1000 Chinese chunks in 32 document flushes;
  one -4000 wheel reached zero and stayed there while later chunks arrived.
- A second standalone `node tests/e2e/workbench.mjs` passed the same assertions,
  with 1000 chunks in 33 flushes and no page errors (final implementation 2/2).
- `npm run test:i18n`, `npm run check`, and the configured Cargo formatting check
  passed. Electron screenshots cover the retained earlier output, dark theme,
  and narrow window. This is local Desktop fixture verification, not an installed
  release or production-agent acceptance.

Reproduce with `npm run test:e2e` after building Desktop. The latest output and
failure screenshot are in `test-results/workbench/` (generated artifacts).
