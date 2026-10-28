# Bazel pilot

The pilot covers the Go control plane and generated protocol/theme freshness and
locale checks. It does not build Rust, GPUI Wasm, Vite, Electron or release packages.
The existing npm and Cargo commands remain the Desktop development path.

## Run

Use Bazel 9.2.0, or Bazelisk (which reads `.bazelversion`). Bazel downloads the
pinned Go 1.26.4 and Node.js 24.14.0 toolchains and declared dependencies. These
targets do not require `npm ci`, a system Node/Go installation or a GPUI checkout.
Initial dependency setup requires network access.

```sh
bazel build //server/cmd/server
bazel test //:checks
```

`//:checks` runs six tests: Host schema/types, remote schema/types, theme freshness
and locale key/placeholder/UI-reference checks. Generated outputs land under
`bazel-bin/generated/contracts/` and `bazel-bin/generated/theme/`; freshness tests
compare those outputs with the committed files. Build only the generated outputs:

```sh
bazel build //:generate_contracts //:generate_theme
```

To update committed generated files, continue using `npm run gen:contracts` and
`npm run gen:theme` after `npm ci`. Both generators also accept `--root` and
`--out-dir`. Their `--check` mode reads files without creating output directories.
The full `npm run check` additionally requires the existing Wasm build's TypeScript
declarations; passing the Bazel pilot does not establish that broader check.

## Dependencies

`server/go.mod` and `go.sum` remain authoritative for Go dependencies. Regenerate
package BUILD declarations after adding Go source files, imports or SQL embeds:

```sh
bazel run //:gazelle
bazel run //:gazelle -- update -mode=diff server
```

Database test targets retain their strict environment and manual/local/cache tags.
Review generated BUILD changes before committing them.

`package-lock.json` remains the npm source lock. Bazel uses pinned pnpm 10.28.2 to
translate it into `pnpm-lock.yaml`. Changes to npm manifests or its lock update the
derived lock and `.aspect/rules/external_repository_action_cache/` hash record on
the next normal Bazel invocation. Review and commit both with dependency changes.
`pnpm-workspace.yaml` describes the same workspaces and disables install hooks for
this JavaScript-only pilot; it does not replace the npm developer workflow.

Use `--config=ci` to reject stale derived lock state without rewriting it:

```sh
bazel test --config=ci //:checks
```

Commit `MODULE.bazel.lock` with Bazel module changes. No remote cache is configured
by default. A shared local disk cache can be enabled with
`--disk_cache=/absolute/path/to/cache`; reuse requires compatible platforms,
toolchains and declared inputs. Remote cache and remote execution rollout remain
separate work.

## PostgreSQL acceptance

The existing Go tests currently exercise PostgreSQL; there is no separate pure
unit suite to advertise as database acceptance. The Bazel suite is manual and
requires an explicitly supplied disposable database with schema creation rights:

```sh
PERO_TEST_DATABASE_URL='postgres://.../pero_test?sslmode=disable' bazel test //server:postgres_integration
```

The two package test targets inherit only that URL, set
`PERO_REQUIRE_TEST_DATABASE=1`, and fail when the URL is missing. The `external`
tag forces execution on every invocation; `no-cache` alone would still allow
Bazel to reuse local test status. Results are not remotely cached or executed.
Ordinary `go test ./...` keeps its existing
missing-database skip behavior; strict native verification uses
`PERO_REQUIRE_TEST_DATABASE=1 go test -count=1 -race ./...` with the URL supplied.

Electron suites continue to run sequentially through their existing npm commands.
Real provider tests remain opt-in. A cached build, passing fixture or absent
database does not certify these external-state acceptance paths.

Recorded pilot checks and remaining limits: [verification report](bazel-verification.md).
