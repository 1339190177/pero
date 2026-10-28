# Electron and GPUI bootstrap

Status: accepted for the first implementation slice, 2026-10-28.

The user selected Electron, local GPUI Kit/Zed references and the official ACP
Rust SDK. The slice uses desktop/ for the Electron shell, web/ for WASM loading,
crates/ for UI/contracts/native execution, server/ for the Go control plane,
protocol/ for platform contracts, and harness/ for a future custom Agent.

The three-column Workbench is rendered by GPUI Web, compiled for wasm32. Colors
are generated from DESIGN.md. One GPUI snapshot family is resolved through Kit;
the separate Zed checkout is a reference, not an independently linked UI engine.

The Rust Host uses JSONL over inherited stdio and supervises persistent,
multi-turn ACP sessions through the official SDK. The bootstrap echo remains a
transport diagnostic. Codex and Claude have returned real model replies;
Grok/OpenCode connects but requires xAI authentication. The independent daemon,
local network API and Worker protocol remain separate implementation slices.
Existing architecture plans describe those broader goals.

The conversation UI follows Zed's retained Markdown and expandable tool patterns.
GPUI Kit owns the editor and Markdown entities; high-frequency text deltas are
coalesced, tool updates preserve chronological entries, and only a view near the
bottom follows incoming output. Adjacent read/search/fetch summaries are a Pero
density adaptation, not a literal copy of a Zed aggregation algorithm.

Native runtime and WASM renderer compile separately. Rust nightly-2026-10-28 is
needed for the Web dependency's wasm atomic wait feature. Stable builds the Host.
