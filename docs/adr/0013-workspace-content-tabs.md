# Workspace content tabs and shared Chat

Status: implementation design, 2026-10-29. Extends ADRs 0001, 0007 and 0012.
Runtime verification is recorded separately after the assembled UI is exercised.

## Context

The Workbench already renders navigation in GPUI and domain Apps in retained
HTML iframes. Switching full-page routes makes it difficult to move between a
conversation, its Project, a Task and an App without losing the visible work
context. The first improvement is one content area with named open items and
one shared Chat panel; it does not require another renderer or execution owner.

## Decision

`WorkspaceItem` identifies content by its source object: `Conversation` by
thread ID, `Project` by Project key, `Task` by Task ID, and `App` by App and panel
names. Titles and icons are presentation derived from those source objects.
An App item does not relax the current one-panel App manifest contract.

`WorkspaceTabs` owns the open-item list and active selection. Opening an
already-open item activates it without duplication. Selecting an absent item
does not open it. Closing the active item selects its right neighbor, or its
left neighbor when it was the last item. Closing the final item clears the
selection. Source reconciliation removes unavailable objects without opening
new items or changing the remaining order.

A tab is a view. Closing it does not delete a conversation, cancel a running
session or Task, uninstall an App, or stop an App backend. Those actions retain
their existing explicit controls and owners. App iframe documents remain in
the existing surface cache across view switches and tab closure; disabling or
uninstalling the App, or disconnecting the Host, performs the existing cleanup.
Open tabs and selection are current-window state in this slice, not a new
durable session store.

The Workbench has one center content area and a global shared Chat panel. The
existing `pero-chat-ui::ChatPanel` owns Composer and conversation presentation;
the Host continues to own ACP sessions, history, permissions and cancellation.
Conversation content uses the same shared panel rather than creating a second
conversation implementation. App-scoped conversation selection continues to
use `AppChatScope`; displaying an App does not grant its tools or file access.

`LayoutPreferences` belongs to `WorkbenchPreferences` in `pero-contracts`.
It carries the preferred Chat width and whether the panel is open. Width is an
integer from 320 to 640 pixels, initially 420; the panel is initially closed.
Dragging is bounded by this domain type. The current viewport can display a
smaller area or switch to a narrow-window layout without overwriting the
preferred width. Appearance and layout use the existing
`settings.workbench` / `settings.workbench.get` path and Host-owned runtime
settings file. An omitted layout in older settings or requests restores these
defaults; no renderer-local preference store is added.

Apps continue to render through the existing `pero-app://` iframe surface. GPUI
owns its visible bounds; the Web shell mounts and retains the document. The
iframe paints above the GPUI canvas, so overlays must explicitly coordinate
surface visibility. This change does not turn an iframe into a native GPUI
element or extend the native developer preview with a WebView.

An external Browser content type, arbitrary URL navigation, Electron
`WebContentsView`, freely split panes, tab dragging and persisted workspace
restoration are outside this implementation slice. The existing App lifecycle
and Host execution authorities remain the integration boundaries for future
work.

## Verification

Core tests cover tab transitions and saved-layout admission and resize rules.
The runtime settings test covers migration from settings without layout and
restoration of the saved layout while preserving other preferences. Generated
Host contracts describe the bounded width and optional legacy layout field.

Compilation and these tests do not establish Desktop acceptance. Runtime
verification must exercise content switching, closing and reopening views,
retained App state, shared Chat, resize and restart, keyboard focus and IME,
overlays, and narrow and light/dark layouts in the assembled Electron app.
Native preview behavior and signed-package acceptance remain separate evidence.
