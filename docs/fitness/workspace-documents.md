# Workspace document views

This is the first document-item slice of the [Zed view research](../design-docs/zed-content-view-data-agent-research.md).
It extends the existing content lifecycle rather than creating another navigation owner.

## Source implementation

- `ContentDocument` now belongs to `pero-contracts`. Source identity, revision,
  format and immutable text are independent of Chat rendering.
- `WorkspaceItem::Document { owner, source }` identifies a document by its source
  and originating conversation. Equal source IDs under different owners do not
  merge. Opening an existing item activates it without duplicating its view.
- The existing `WorkspaceNavigation` selects, cycles, closes and reconciles
  documents together with conversations, projects, tasks and Apps.
- `ContentReader` supplies both inline and workspace presentation. The workspace
  uses the available content height; each view retains its own query, selection
  and focus while sharing the same immutable source snapshot.
- Existing document views receive source revision updates at the Chat event
  boundary. Deleting their source conversation removes unavailable document
  items. Closing a document removes its view, preserving its conversation and
  original result. App iframe visibility continues to use `WorkspaceLayout`.
- `OpenDocument` is a local typed UI intent. It starts no Agent turn and grants
  no additional App or filesystem authority. Standalone Chat shells do not
  advertise a workspace-opening control.

## Actual acceptance

Run `node scripts/verify-content-reader.mjs --functional --workspace-documents`.
The verifier snapshots the sources, builds release Wasm and opens actual hidden
Electron. The fixture supplies Rust code with Unicode and 80 functions; it does
not call a model or execute that Rust code.

The probe verifies:

1. An ordinary code result opens from Chat into a document tab and fills the
   main workspace instead of remaining in the inline result's fixed height.
2. The code language, complete source bytes and source-derived title survive.
3. Command-F and actual typing find a function in the document.
4. Switching to Chat and reopening keeps the same document and reading state.
   Inline and workspace searches remain independent.
5. Closing the tab returns to its retained conversation without removing the
   source or starting another Agent turn.

Screenshots and `result.json` are under ignored `test-results/workspace-documents/`.
Navigation and layout behavior are rows in the existing pure-core test tables.
TypeScript/contract checks and native compilation are separate checks.

## Remaining scope

Documents currently originate from source-backed Chat results and remain
readonly. Independent Markdown preview, project-file editing, split panes,
workspace restoration, external browser items and MCP Apps resources remain
separate implementations. This acceptance does not establish those capabilities
or certify Windows/Linux packages. The existing large-source performance incident
also remains open; sharing immutable text does not yet share every editor parse.
