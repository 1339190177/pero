---
kind: verification_report
date: 2026-10-28
---

# Bazel pilot verification

Verified on macOS arm64 with Bazel 9.2.0, Bazel-managed Go 1.26.4 and Node.js
24.14.0, and a disposable local PostgreSQL 17 cluster. The implementation is on
`codex/bazel-pilot`, based on local commit `aa9e8ff`. The original checkout had
unrelated merge conflicts, so all changes and checks used an isolated worktree.

## Build and input boundaries

Removed the worktree's `node_modules` temporarily and restricted the client PATH
to `/usr/bin:/bin`, excluding the installed Node/Go/Bazel directories. Invoked
Bazel by absolute path. After `bazel clean`, the server and both generators built,
and all six checks passed. Source files were restored byte-for-byte after each
dependency probe.

| Probe | Observed executed work |
| --- | --- |
| No changes | No compiler or generator execution |
| README comment | No compiler or generator execution |
| DESIGN.md comment | Theme input copy and theme generator only |
| Host TypeSpec comment | Protocol input copy and protocol generator only |
| SQL migration comment | Migration package compile and server link; no JavaScript generation |

CI-frozen mode rejected a modified npm source lock with exit 48 and did not
rewrite the derived pnpm lock. After restoration, all checks passed. Gazelle's
`update -mode=diff server` also reported no differences after regeneration,
preserving the explicit database environment and execution tags.

## Timing observations

One local sample per row, measured as command wall time. The clean build had warm
repository/download caches and an initially empty disk action cache. These are
pilot measurements, not a comparison with the full npm/Desktop build.

| Operation | Seconds |
| --- | ---: |
| Clean server + theme + protocol build | 24.42 |
| Six checks after that build | 0.63 |
| Repeated unchanged build | 0.19 |
| Repeated six checks | 0.37 |
| Build from separate clean source archive using shared disk cache | 6.55 |
| Checks from that source archive | 0.43 |

The separate source archive came from implementation commit `c407c57`, had no
`node_modules` or previous Bazel outputs, and used the same restricted PATH. Its
build reported 209 disk-cache hits and no compiler/generator execution; its six
checks reused cached results. This demonstrates reuse between source directories
on the same machine and platform, not between machines or operating systems.

## PostgreSQL and generator checks

- Native `go test -count=1 -race -v ./...` against the disposable database:
  7 cases passed, no skips.
- After adding the `external` tag, two consecutive Bazel invocations each
  executed both package targets: 7 cases passed, no skips.
- Stopping the database caused the unchanged Bazel command to execute and fail
  both targets. Omitting the URL also failed; a previous pass did not conceal it.
- Original npm theme, locale and protocol checks passed. Explicit generator
  outputs matched all five committed files byte-for-byte. Missing outputs,
  unknown translation keys and invalid TypeSpec failed as expected. `--check`
  passed with filesystem writes blocked and did not create missing directories.

The disposable database was stopped after verification. The local test-status
cache issue and fix are recorded in the [resolved incident](../issues/2026-10-28-bazel-postgres-test-cache.md).

## Combined GitHub/community integration

On `codex/integrate-master`, the pilot was adapted to the shared Client/Issue
sources and community service. Gazelle declarations now include all current Go
sources/tests, the GitHub package, community templates/assets, and SQL migrations
001, 002, 003 and 010. The strict suite includes four database-bearing packages;
community and GitHub helpers enforce the same missing-database failure policy.

| Check | Result |
| --- | --- |
| Gazelle regeneration | Passed; existing local/manual/external/no-cache/no-remote database tags retained |
| Go server plus protocol and JSON/CSS theme generation | Passed using pinned Bazel toolchains |
| `bazel test --config=ci //:checks //server:postgres_integration` | Passed: seven freshness/locale checks and all four PostgreSQL package targets |
| Strict suite without `PERO_TEST_DATABASE_URL` | All four package targets failed as required; no cached pass hid missing configuration |
| Nested locale probe | Valid nested remote key passed; unknown nested remote key failed |

The theme target declares both JSON and CSS outputs and checks the community CSS
against `DESIGN.md`. Locale inputs include recursive Rust components and remote
keys. The normal generation/check commands retain their existing source-tree
paths. The derived npm repository input-hash record was refreshed for the merged
package metadata; dependency declarations and lock contents were unchanged.

These checks reused the original pilot's warm Bazel output/download cache to
avoid duplicating toolchains on the same machine. No unrelated caches were
removed. This is not a new cold-build or cross-machine performance measurement.
Desktop, real-provider and community-browser acceptance are owned by their
separate suites; a Bazel package pass does not establish those paths.

## Remaining limits

The original clean pilot worktree could not run the full `npm run check` before
building the `web/src/wasm/pero_ui` TypeScript declarations. No substitute
declarations were added. The integrated Bazel targets still do not build those
artifacts. Rust/GPUI Wasm, Vite/Electron, provider, signed-package, Linux and
remote-execution verification remain separate gates; current remote/source
results are recorded in [remote dispatch verification](remote-dispatch-verification.md).

One Go tool-builder rebuild exited with no compiler diagnostic. A retry and a
subsequent clean rebuild passed without source changes; the cause remains
unknown and is tracked [locally](../issues/2026-10-28-bazel-go-builder-rebuild.md).
