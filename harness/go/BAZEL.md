# Headless agent Bazel build

This directory is an independent Bazel module for the existing single Go module
`github.com/phodal/pero/harness/go`. Run Bazel **from this directory**, not the
repository root. The root Bazel module continues to own the Go control plane and
its existing code-generation checks; its dependencies and Go minimum are unchanged.
The root `.bazelignore` excludes this nested module from root wildcard queries.

Use Bazel 9.2.0 or Bazelisk (which reads `.bazelversion`):

```sh
cd harness/go
bazel build //cmd/pero-agent-acp
bazel test //...
bazel test --@rules_go//go/config:race=true //...
```

These commands build and test the headless ACP agent without starting a UI,
contacting a model, or installing tools into the user's system. The executable is
`bazel-bin/cmd/pero-agent-acp/pero-agent-acp_/pero-agent-acp` on Unix systems.
Initial setup downloads pinned toolchains and dependencies and requires network
access with valid TLS trust. Never disable TLS verification to bootstrap a build.
For enterprise proxies, configure Bazel's JVM to use an already trusted local
truststore according to your organization's policy; no environment-specific trust
path is committed here.

## Dependency and source maintenance

The module pins rules_go **0.64.0**, Gazelle **0.54.0**, and Go SDK **1.26.4**.
The SDK is a build tool; the native module still supports Go **1.25**. The root's
rules_go 0.65.0 pulls x/sys 0.48.0 and x/text 0.42.0, both requiring Go 1.26.
The independent 0.64.0 pin preserves this agent's current go.mod/go.sum and avoids
raising the control plane's native Go requirement. No `replace`, relaxed version
conflict check, or dependency override is used.

`go.mod` and `go.sum` remain the application dependency authority. After changing
imports, source files, embeds, or dependencies:

```sh
bazel mod tidy
bazel run //:gazelle
bazel run //:gazelle -- -mode=diff
bazel build //...
bazel test //...
```

Commit reviewed package BUILD changes and MODULE.bazel.lock. Generation is scoped
to this module, not the whole Pero repository. Keep explicit source-boundary test
fixtures in the model/runtime BUILD declarations: those tests must inspect the
production Go files and model SOURCE.json in the test sandbox, and fail if no
production sources are available.

Native Go checks remain useful and are not replaced by a successful Bazel build:

```sh
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build ./cmd/pero-agent-acp
```

Real provider credentials, external MCP services, deployment, and root server
PostgreSQL acceptance remain separate opt-in work.
