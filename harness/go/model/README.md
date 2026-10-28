# Agent model values

First independent package of the ACP-only Go runtime extraction (issue #3).
Import `github.com/phodal/pero/harness/go/model` with package name `model`.

This is a **value-model library, not an executable Agent or ACP server**.
It provides messages, content blocks, attachment references, tool updates,
session event envelopes, cloning and token-estimate helpers. It has no external
dependencies, service registry, provider, tool execution, session storage,
Project/Hub integration, UI, telemetry, or model/network calls.

## Source and boundary

Extracted from `phodal/point` commit
`69a1f8692725078c4ce7feaf497f5b80799ca44d`, `go/harness/model`.
`SOURCE.json` records every original file and Git blob identity and the bounded
changes. Core value types and serialization are retained. The Point-only
`SessionHeader.ProjectBinding` field and `ProjectBinding` type are removed; 32
Point product event names are no longer part of the native known vocabulary.
Unknown explicitly ignorable events retain the existing opaque round-trip
contract; this does not authorize their execution or import Point state.
DSH event names describe wire vocabulary only; they do not activate features.

The dependency direction for later extractions is model first, then separately
reviewed session/LLM/tool modules, followed by Agent and ACP composition.
No private Point dependency, placeholder module requirement or local `replace`
is necessary. This package alone does not fulfill the runnable ACP worker goal.

## Verification

With Go 1.25 or newer, from this directory:

```sh
GOWORK=off go test ./...
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build ./...
```

Tests are deterministic and require no API keys or model invocation. The two
upstream source-oracle tests requiring external JavaScript sources are not
included; the eight self-contained upstream test files are retained, with a
product-boundary regression added. The dependency guard rejects non-standard
library imports in production source.

## License status

Publication was authorized by the repository owner. No general license was
found for the inspected source module or Point repository; this extraction
does not assign MIT or any other license. Existing source notices are retained.
`file_content.go` and `tool_updates.go` identify algorithms ported from
DeepSeek DSH under MIT; their applicable notice is included verbatim in
`DEEPSEEK_FILES_LICENSE.txt`. That notice does not license the entire module.
The repository owner still needs to decide the license for the other source.
