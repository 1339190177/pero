---
kind: incident
status: resolved
---

# GPUI WASM bootstrap toolchain

## What

Building the local gpui-kit 0.8.1 Web backend on stable Rust 1.95 fails in
`wasm_thread 0.3.3`: `stdarch_wasm_atomic_wait` requires nightly. The Kit Web
examples also select nightly. This prevents the Electron renderer from being built.

## Why

The pinned GPUI snapshot enables Web thread support even though Pero opens a
single-threaded Web platform. The dependency's compile-time requirement still applies.

## Resolution

Use nightly-2026-10-28 for WASM and stable for the native Host. `npm run build`
and the real Electron end-to-end verification passed on 2026-10-28.

The first run also exposed an asynchronous startup boundary: the Web GPU window
can appear after `run()` returns. The loader now waits for the GPUI entity before
delivering Host readiness. The initial thin CJK gallery font was replaced with a
regular-weight Noto font, and GPUI component background tokens are mapped together
with their foreground colors to DESIGN.md.
