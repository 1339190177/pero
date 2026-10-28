# Incremental Bazel build

Status: accepted for a Go and code-generation pilot, 2026-10-28.

## Context

Pero builds a Go control plane, native Rust Host/Worker, GPUI Wasm renderer and
Electron shell. Generated themes and protocol types connect these parts. The
existing npm and Cargo entry points remain useful for development; replacing
the complete Desktop pipeline would also require modeling the external GPUI
checkout and separate native/Wasm Rust toolchains.

## Decision

Introduce Bazel for the Go control plane and deterministic theme, locale and
protocol checks. Pin the Bazel, Go and Node toolchains and resolve dependencies
from the existing Go/npm manifests and their locks. Keep npm as the owner of
JavaScript dependency declarations; the translated pnpm lock supports Bazel.

| Target | Scope |
| --- | --- |
| `//:checks` | Six checks: theme parity, four protocol output comparisons and locale validation |
| `//:generate_theme` | Generate the theme under `bazel-bin/generated/theme/` |
| `//:generate_contracts` | Generate protocol JSON Schema and TypeScript under `bazel-bin/generated/contracts/` |
| `//server/cmd/server` | Build the Go control-plane binary |
| `//server:postgres_integration` | Explicit real-PostgreSQL integration suite |
| `//:gazelle` | Maintain Go BUILD declarations |

Generators accept explicit source roots and output directories. Bazel generation
does not overwrite committed artifacts; comparison checks detect drift. npm
generation keeps its existing source-tree destinations. TypeSpec compilation
uses the Node API and retains intermediate emitter output in memory.

PostgreSQL tests are `manual`, local, unavailable to remote execution and marked
`no-cache`. An `external` tag additionally prevents reuse of local test status.
They require an explicitly supplied `PERO_TEST_DATABASE_URL`; the
Bazel targets enable `PERO_REQUIRE_TEST_DATABASE=1`, so missing configuration
fails instead of skipping. Default checks do not establish database acceptance.

## Boundaries

Keep the npm/Cargo development and test commands. Native Rust, GPUI Wasm,
Vite/Electron builds, UI/provider tests and signed packaging are outside this
pilot. The six Bazel checks do not replace TypeScript checking or the existing
runtime and Desktop verification gates.

This decision establishes a bounded build path. Build-time improvements and
broader migration require separate measurements and verification evidence.

Setup, commands, lock maintenance and database configuration are documented in
[Bazel quality gates](../fitness/bazel.md).
