---
kind: exec_plan
status: active
date: 2026-10-30
---

# Complete content-view coverage and end-to-end acceptance

The scope is the content workflow in the Zed research: sources enter a workspace,
render in the right view, retain identity and interaction state, and integrate
with Apps. JSON is one projection. Output budgeting is already implemented in
part and is not the focus of this work.

Each row requires source implementation plus observable acceptance. A fixture
proves local wiring; real-provider, platform and installed-package acceptance
remain distinct. Do not mark a row complete for an interface, placeholder or
simulated result alone.

| Capability | Current baseline | Required implementation | Acceptance gate |
| --- | --- | --- | --- |
| Source identity and admission | Conversation-owned immutable documents | Typed conversation, file, App and MCP origins; one admission boundary and revision owner | Equal content from different authorities stays distinct; no renderer guesses authority |
| Markdown | Chat rendering only | Independent rendered/source document, links and source ranges, stable updates | Open a real Markdown source, switch modes, follow a link and return with state |
| File documents | Result documents only | Explicit open, edit/save, dirty state and external-change conflict | Real temp file open/edit/save/reopen; changed disk source cannot be silently overwritten |
| Workspace lifecycle | Retained open result tabs | Saved descriptors and reading state; reopen/restart recovery, back/forward and pane layout | Close/reopen and restart recover the same source and position without re-running tools |
| Generic Web content | Installed App iframe cache | Typed HTML/URL content item with loading, retry, navigation, context and teardown | Actual isolated Web view opens, switches, restores, fails/retries and closes cleanly |
| MCP Apps | No host implementation | Host-owned server/resource binding, preserved UI metadata, official AppBridge, CSP/sandbox and teardown | Real loopback MCP resource discovery, initialization, tool input/result, interactive tool call and teardown; external-provider evidence separate |
| App SDK consumption | GPUI source reader only | Shared reference contract and Web reader; managed App can open its source in the workspace | One actual App result opens completely, retains provenance, returns to App and keeps its state |
| Icons | Fixed shared action catalog | One content/language/App icon mapping with themed fallback and extension admission | Same source has the same icon in tabs, GPUI and Web; unknown types remain readable |
| Editor reading | Basic search, selection and brace folds | Regex/whole-word search, structural JSON folds and stable source navigation | Actual typed interaction, nested folds and source location with exact text copying |
| Structured data | Basic source-backed records | Sorting, filtering, resize, cell selection/copy and mappings | Reordered/filtered values still locate the original source and preserve precision |
| Large-source work | Known main-thread stalls | Shared parse/index lifetime, actual worker work and bounded viewport/IME paths | Full functionality retained; all interaction long tasks recorded; large datasets measured |
| Data Agent | No dataset workflow | Dataset schema/grain/provenance and table/chart/selection context | Real dataset -> view -> selected context -> source verification |
| Cross-platform/accessibility | macOS background fixture proof | Keyboard/focus, light/dark, minimum width, accessibility and platform builds | Separate OS/package receipts; physical IME and screen-reader proof identified explicitly |

## Implementation order

1. Complete typed source admission and Markdown/file/document lifecycle across GPUI and Web.
2. Build Web and MCP resource hosts against the same workspace lifecycle; use the official MCP Apps SDK.
3. Migrate a real App and verify all supported source/view kinds in one workflow.
4. Complete reading/data interactions and shared parse performance, then Data Agent consumption.
5. Run restart/error/accessibility/platform acceptance and close remaining gaps with evidence.

## Decisions

- Existing WorkspaceNavigation owns selection. Source stores own buffers;
  views own selection, search and scrolling. Host owns files, MCP credentials,
  permissions and execution. App frames do not receive those credentials.
- A source reference never grants file or tool authority. MCP server metadata
  is not reconstructed from rendered text. The current ACP route must preserve
  metadata and provide a real resource transport before claiming MCP Apps.
- Browser/HTML/MCP views use distinct source identities from installed Apps.
  Reuse geometry and lifecycle primitives, not AppCatalog entries or grants.
- Closing a view does not execute, delete or cancel its source. Restoring a view
  does not rerun an Agent tool. Missing/expired sources have explicit recovery.
- Source snapshots remain immutable. Edits and saves use a named buffer model;
  sorting and filtering retain source positions rather than rewriting source.

## Progress

- Baseline inspected at `b2f2259`; parallel local changes excluded in an isolated worktree.
- Existing result document acceptance: docs/fitness/workspace-documents.md.
- Existing reader/output acceptance: docs/fitness/content-reader.md.
- Large-source incident remains open: docs/issues/2026-10-29-content-reader-main-thread.md.
- All additions above are pending until their corresponding acceptance is recorded.
