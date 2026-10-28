---
kind: verification_report
date: 2026-10-28
---

# Electron / GPUI bootstrap verification

Historical initial slice. Current conversation acceptance is documented in [conversation-verification.md](conversation-verification.md).

Environment: macOS Apple Silicon, Electron 45.1.0, Node 26.8.1,
GPUI Kit local ec89cfc (0.8.1), GPUI pre snapshots 0.3.10,
nightly-2026-10-28 for WASM, wasm-bindgen 0.2.127, ACP SDK 3.5.0.

Passed:

- Full development build of WASM, Rust Host, web assets and Electron.
- TypeScript checks and DESIGN.md generated theme consistency.
- Two native tests: exact Unicode/request identity; empty and oversized rejection.
- Real Electron loaded the WASM workbench through a secure custom scheme.
- Chromium IME composition (`nihao` → `你好`) and committed Chinese text entered
  the GPUI input; Enter sent it through preload, Electron and the Rust Host.
- The exact text returned with the same request ID and updated the visible UI.
- A desktop menu action independently sent a notice to GPUI.
- Oversized IPC requests were rejected; renderer Node access was absent.
- Light, dark, 960px window and Host disconnect states were captured and inspected.
- Go server packages compiled with `go test ./...`.

Artifacts: `test-results/desktop/` contains light/dark/narrow, Chinese round-trip
and disconnected screenshots, plus result.json. They are generated, ignored files.

Limits: this is Chromium IME event verification, not a physical macOS Pinyin input
method test. Agent sessions, distributed scheduling, real tasks, GitHub publication,
persistent Host service and signed/packaged delivery have not been implemented or
accepted. The current WASM is an unoptimized development build; production size and
startup performance are not acceptance claims. Browser/mobile accessibility and
other operating systems remain unverified.
