---
kind: incident
status: open
---

# Active workspace tabs can remain outside the visible strip

## What

In actual background Electron on macOS arm64, open eight conversations using
New chat in a 1488-pixel-wide window. The diagnostic snapshot selects the eighth
Conversation, but the tab strip remains at its initial horizontal position.
Only the earlier tabs are visible, with no visible active-tab indicator. The
conversation body is current; its corresponding navigation control is offscreen.

Observed on 2026-10-29 against the architecture source committed through
`24fc93a`, in the assembled development build with parallel App changes. The
probe used a disposable profile, sent no Agent prompt and reported no page errors.

Evidence: `test-results/workspace-comparison/eight-tabs-new-active.png` and
`result.json` (`count = 8`, active item index `7`, viewport width `1488`).

## Why

`workspace_view.rs` gives every tab a fixed width of 200 pixels and enables
horizontal scrolling, but it has no tab-strip ScrollHandle or reveal step when
the route changes. The model owns the right active identity; the view does not
bring that identity into view. Buttons also use list indices for their element
IDs, so a follow-up should keep their identity stable as earlier tabs close.

Zed's `Pane::update_active_tab` explicitly reveals the activated tab using its
scroll handle. That behavior is useful independently of a full Pane/Dock system.

## Bounded follow-up

Reveal the active tab after opening, cycling and selecting a retained source.
Keep element IDs based on WorkspaceItem identity. Verify overflow in wide and
narrow windows, then close an earlier tab and confirm both selection and keyboard
focus remain attached to the same source. Preserve the current open order and
Host execution/lifecycle boundaries. This record does not implement the fix.
