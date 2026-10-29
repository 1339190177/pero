---
kind: incident
status: in_progress
---

# Complete actionable harness comparison gaps

The requested follow-up implements feasible gaps in the existing module comparison,
using official DSH `f3803c96974ad4a94195fb97e5c637e9c2a3afe3` as behavioral
reference. Starting Pero commit is published PR #4 head
`56b12c7e7f9cd9cd3add27ecb0440788d70b43e3`, tree
`a54351bb143f238c2336ea27a5edb8711d958f96`.

## Bounded completion checklist

- Repair the demonstrated Windows cached font-tool environment failure, validating
  required imports and rebuilding only the managed virtual environment when needed.
- Remove the two known official Todo Tree-sitter parser limitations without
  stripping unsupported syntax or suppressing genuine parser failures.
- Connect existing MCP structured content/output schema to the canonical result
  seam, preserving text, errors and current authority boundaries.
- Add direct behavioral coverage for retained session projection helpers and
  reconcile the six reported Point test-source provenance mismatches.
- Re-run affected tests and aggregate Go/tool/build gates; retain explicit platform
  and environment verification limits rather than presenting them as passes.

Memory remains issue #9. Projects/UI, remote uploads, credential-store product
assembly, all upstream plugin profiles and optional codec services are not implied
by the existence of retained library interfaces. Current explicit workspace,
approval, request-budget and one-level delegation boundaries remain unchanged.
JSONL retains fail-closed behavior by default. An explicit operator option adds
lossless complete-record delimiter recovery under the existing writer lease,
after exclusive private backup. Incomplete fragments remain rejected; no official
full torn-tail repair or exactly-once crash guarantee is claimed.

## Evidence continuity

This execution environment was replaced between rounds. Published source and
committed historical reports were recovered through normal public Git clones;
private prior-round logs/toolchains were not available and are not recreated as
historical evidence. New verification receipts belong to this round. The starting
remote tree was checked against the recorded tree before any new edits.

## Local findings and implementation

The retained sessionview contract tests reproduced one additional defect: saving
an updated cache deleted a checkpoint still needed by a previously opened lazy
history. Checkpoints now share immutable blob lifetime until cache-owner cleanup.
This package remains unmounted by the current CLI; tests do not imply UI wiring.

The six source-blob mismatches are now explicit provenance metadata with separately
verified pinned content. Their historical declared origins are retained rather
than silently replaced. This reconciles the report, not an assertion of exact
upstream-copy equivalence.

The actual CLI recovery fixture closes a session, removes only its final newline,
resumes with the explicit option, checks the exact backup and continues normal
prompts. Session and CLI race tests pass. Required-positive strict sandbox testing
fails locally with SANDBOX_UNAVAILABLE; no kernel/network security setting was
changed. A dedicated supported Linux CI runner must prove the positive path.

CI now separately gates the standalone harness Go module, isolated harness Bazel,
AST parser/build contracts and font environment fixtures. Existing root Go/Bazel
jobs alone were never evidence that these harness contracts ran.

## Final local verification

Standalone Go 1.27.3: full `go test -count=1 ./...`, full race suite, vet and
CLI build pass. Focused recovery/CLI and MCP security/discovery race tests pass.
AST tests: 18 passed, no skips; selected official source pairs: 17 extracted,
zero unavailable, with actual raw bytes checked against official `f3803c9` and
public Pero code checkpoint `ad001471c794cb5d116729b6631c7f7f4a731260`.
Font environment tests: 8 passed with real offline Python fixtures on Linux.

Go source inventory is 406 files, 133 test files, maximum 956 lines. The native
Go toolchain above is distinct from standalone Bazel's pinned Go 1.26.4 SDK.
Bazel is not installed locally; the new isolated CI job must establish that gate.
The new Windows/macOS/Linux jobs and required-positive sandbox job remain pending
remote CI; local passes do not certify Windows cache repair or supported-host
sandbox execution. No real model, credentials, desktop/UI or deployment was used.
