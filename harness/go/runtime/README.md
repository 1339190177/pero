# Headless lifecycle runtime

This internal package is the lifecycle substrate used by the extracted Go
agent. It is not the agent loop, ACP server, or a second Pero task scheduler.
It uses only the Go standard library.

Retained: typed service keys and scopes, definition/instance activation,
service readiness and generation tracking, provider drain barriers, owned
effects and goroutines, signal/pipeline listeners, and in-memory application
composition. Activation ownership and fresh-value event delivery remain intact.

Excluded: process/config supervisors, file include loading and watchers,
debounce/throttle helpers, policy overlays, repair tooling, commands, examples
applications, and all Point product/Hub/Projects/web functionality. The
application JSON decoder rejects file includes rather than silently ignoring them.
Run-owned After/Every helpers remain part of the lifecycle kernel; these are not
task scheduling or orchestration APIs.

Source: `phodal/point` commit
`69a1f8692725078c4ce7feaf497f5b80799ca44d`, `go/runtime`.
See `SOURCE.json` for exact source blobs and adaptations. The upstream README
credits Cordis lifecycle semantics; this extraction makes no new license grant
and does not claim a license for the entire Point repository. No third-party
notice files exist in the selected upstream runtime directory.

From `harness/go`: `go test ./runtime`, `go test -race ./runtime`, and
`go vet ./runtime`. Tests are local and require no model, credentials, service,
or deployment.
