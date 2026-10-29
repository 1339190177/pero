---
kind: verification_report
date: 2026-10-29
---

# Composer context and completion

Scope: [Zed composer alignment](../design-docs/zed-composer-parity.md).

## Implementation

The composer uses the Kit ordinary multiline Textarea and its atomic reference
tokens instead of the code Editor. The input owns token ranges, edits and undo;
Pero owns frozen content, completion queries and selected context. Source keywords
live on `ContextKind`, and one shared picker presents `+`, `@` and slash results.

Host searches files/directories, actual Tree-sitter definitions for Rust, Go,
Python, JavaScript and TypeScript/TSX, saved threads, project/global skill files,
images, explicit-base Git diff and text/JSON/XML URLs. Directory scans honor ignore
rules and stop at documented entry/file/byte limits. Definitions bind their source
version and range. Every resolved selection gets a distinct snapshot ID, so
selecting an edited file cannot overwrite an earlier reference or undo binding.

Selected content travels separately from user text as ACP embedded resources,
images, or individual text blocks when embedded resources are unsupported. Agent
image capability gates selection and sending. Message history records reference
labels/content, and session snapshots retain the actual prompt capabilities for
renderer reloads. Picker requests are correlated; results from a dismissed query
or another chat are discarded. Draft text and references survive chat switching.

Slash completion groups actual Agent-advertised commands separately from locally
discovered skills. Choosing a command inserts it; choosing a skill freezes its
SKILL.md as context. Neither action submits a turn. Native image/file paste and
Electron browser image paste use the same typed reference path. Native and
Electron file/image/directory dialogs also resolve through Host.

The toolbar places context and permissions on the left, with ACP Agent identity,
model and thinking effort on the right. Modes and additional permissions share
the shield menu, which shows the current mode. The connected ACP identity is fixed
to its existing session; draft sessions allow Agent selection.
Source menus and completion rows use the same icon mapping; icon-only controls
have accessible names.

Thinking effort, mode and permission choices come from actual ACP declarations.
The last confirmed selection remains active while a change is pending. Pero does
not invent Write/Ask/Minimal profiles for Agents that do not expose them. Tool-call
approval remains an Agent/Host request, separate from session configuration.
Thinking can be disabled only when the Agent advertises an Off/None choice;
the UI does not invent unsupported model settings. The bilingual placeholder
explains `@` for context and `/` for skills/commands.

## Verification

- Full five-crate Rust regression: 65 tests pass, including Unicode cursor-range
  queries. `npm run check` (TypeScript, theme, locale and generated contracts) passes.
- Full runtime suite passes, including ACP commands/settings/native resource and
  image transmission, actual loopback HTTP and temporary Git snapshot cases.
- Host JSONL schema conformance exercises search, resolve, error and structured
  prompt events; generated TypeScript/schema checked.
- Native UI compiles and GPUI Wasm/Desktop build.
- Real Electron with the ACP fixture verifies acknowledged settings, context-type
  menus, keyboard file selection, atomic deletion/undo, frozen file content after
  edits, slash command/skill selection, native ACP images, ClipboardEvent/FileReader
  image paste, tool approval, Escape, macOS editing shortcuts, dark and narrow UI.
  All 17 checks pass with no browser errors, including an advertised `Off`
  thinking choice followed by restoring `High`. Evidence: `test-results/composer/`
  (ignored by Git), including `toolbar.png`, `thinking-off.png`,
  `context-types.png` and `narrow.png`.

## Limits

This is local development/fixture evidence (L0/L1), not real-provider or signed
release acceptance. Selection is disabled because the workbench has no active
source editor exposing selected ranges. Symbol lookup covers the listed languages,
not a language-server index. Global/project skill discovery does not prove that
an external Agent installed or registered that skill; the selected text is supplied
as context. Switching chats restores draft content and references but resets the
input's undo stack and caret. A physical IME and native clipboard on each target
platform require separate verification.
