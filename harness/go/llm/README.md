# Provider-neutral LLM contracts

Second package in the ACP-only extraction, following `model` (issue #3).
It owns model metadata/capability validation, structured provider failures,
image-pricing interfaces and retry policy values/lifecycle. No network calls,
model routes, credentials, provider SDK, UI or Project state are installed.

The six production files and three existing tests come from Point commit
`69a1f8692725078c4ce7feaf497f5b80799ca44d`; `SOURCE.json` records originals
and adaptations. The Point runtime Service Key is intentionally not carried
over: future composition owns registration. `Override` returns the same
cleanup function directly; cancellation generations and idempotent cleanup
are retained and tested. This is not a provider implementation or Agent loop.

From `harness/go`, run `GOWORK=off go test -race ./llm`, `go vet ./llm` and
`go build ./llm`. Dependencies are only the sibling `model` package and the
standard library. Additional tests cover generation replacement, concurrent
cleanup, error-cause preservation and context-overflow classification.

Publication was authorized. These source files contain no identified license
header; no new whole-module license is assigned. The owner still needs to
decide the applicable license. See the source manifest rather than assuming
the separate model package's partial DeepSeek notices license these files.
