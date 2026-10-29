---
kind: incident
status: resolved
---

# Complete actionable harness comparison gaps

## Accepted code snapshot

[CI run 40807109189](https://github.com/phodal/pero/actions/runs/40807109189) completed successfully:
**15/15 jobs passed** for code head `78a402d4be2ce89695d40bbd44797d4a174d70c8`,
tree `addf3222ddecce723d4c47cda1e85a34341f13a3`. This includes native harness Go on
Linux/macOS/Windows, standalone harness Bazel, all three build/check/Rust
aggregates, the positive Linux sandbox fixture, and actual extraction of all
17 selected official AST pairs. The macOS remote suite passed all eight tests,
including both the new socket regression and the previously failing worker case.

Windows portable coding workflows and explicit unsupported-native admission are
verified; a Windows implementation of Unix-only native workspace/shell features
was not added. AST extraction is structural evidence, not semantic equivalence.
Memory remains issue #9; no merge or deployment occurred. Earlier pending/failing
receipts below are historical checkpoints, superseded by this final acceptance.

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
Bazel was not installed locally; its isolated CI gate subsequently passed.
At this local checkpoint the Windows/macOS/Linux and positive sandbox jobs still
required remote validation; final acceptance above supplies that evidence. No real model, credentials, desktop/UI or deployment was used.

## Cross-platform CI follow-up

Run `40799866918` at public merge `c9fd173b68dff1f7b5165831c42db68458d28cb4`
proved the positive Linux sandbox fixture (not skipped), AST 18-test suite and all
17 selected official mappings. Windows font recovery tests (8), desktop build and
typecheck passed. The isolated Bazel generation check exposed an alphabetical
source-list ordering error; the source list is corrected rather than bypassing
Gazelle's diff gate.

macOS skills fixtures used a temporary path containing the OS `/var` alias,
violating the existing canonical-root precondition before their intended checks.
An aliased TMPDIR reproduces both the catalog and ABA failures on Linux. Trusted
test-directory creation now resolves that alias; full skills race tests pass with
the aliased TMPDIR. Production symlink/descriptor admission is unchanged.

Windows Rust ACP MCP fixtures used `/fixture/not-executed`, which is not a native
absolute path there; tests hit command admission instead of the intended protocol
check. The follow-up uses a native absolute fixture path and asserts the specific
expected rejection for invalid inputs. Production validation is not relaxed.
Current platform CI still needs to rerun after these fixture corrections.

The Windows harness matrix also exposed assumptions in Unix-only native-tool
fixtures. Positive descriptor-rooted workspace/catalog and process-group shell
cases now run only on their supported Linux/Darwin platforms. Windows runs explicit
fail-closed admission contracts instead; pure read-window, rendering, tail and
frontmatter parsing tests remain cross-platform. No production capability guard is
removed and the whole harness job is not skipped. The four affected packages pass
Linux race tests and Windows cross-compilation; Windows execution was still a CI gate at this checkpoint; it passed in final acceptance.

Three actual platform bugs are corrected separately from fixture scope:

- The pure sandbox profile builder walks POSIX mount destinations with `path.Dir`,
  not host-dependent `filepath.Dir`, avoiding a Windows non-progressing loop.
- Storage namespace proofs capture Windows file identity from an open handle
  immediately. Deferred path-based identity lookup no longer samples a replacement
  during later comparison. Unix permission behavior remains unchanged.
- Media uses the existing session-style Windows directory-sync limitation; it does
  not pretend Windows can sync a directory opened with `os.Open`. Writable staging
  links are removed before their shared published inode becomes read-only, and
  same-store file publication is serialized. Corrupt derived-cache removal clears
  its readonly attribute first. Cleanup assertions cover successful concurrent file
  publication and image staging. No cross-process transaction or Windows directory
  entry power-loss guarantee is asserted.

Focused three-package race tests (three repetitions), vet and Windows
cross-compilation pass. Actual Windows execution was still required at this checkpoint and subsequently
passed in final acceptance. AST source extraction remains 17/17: these platform fixes do not modify
any of the selected and hash-verified official mapping source files.

One aggregate Linux race run additionally observed
`TestReviewParentChildShareHardHTTPBudget/3` returning an activation-disposal-pending
diagnostic instead of the child's output. Thirty isolated race repetitions passed.
This observation is retained for a separate deterministic lifecycle investigation;
the platform corrections do not claim to fix it, and a later green rerun does not
erase the failed receipt. Vet and build passed on the platform-correction snapshot.

## Observed subagent result race resolved

A deterministic isolated scheduler-gap reproduction subsequently proved the
result-loss path: activation publishes its result after teardown/callbacks, then
closes its final receipt after retirement bookkeeping. Calling Dispose again in
that gap returns disposal-pending; the tool registry treated it as failure and
lost successful child output. The caller now joins `Wait` after receiving a result.
The existing selection/output mapping is named `awaitSubagentResult` and tested
through the existing typed activation receipt, without a new interface or
production test hook. Closed-result and pre-result cancellation behavior is retained.

The shipping contract table proves blocked-receipt joining, preserved child output,
real cleanup-error propagation, canceled receipt wait, pre-result authority
revocation and closed-without-result failure. Old Dispose behavior fails the three
receipt rows deterministically; the repaired table and real parent/child HTTP
budget integration pass 30 race repetitions. Failed pre-fix aggregate evidence is
retained rather than relabeled as a platform failure.

## Portable coding workflow verification

The next run (`40803485815`, `cb972a6`) passed the standalone harness Bazel gate,
macOS harness tests and the actual Windows media/storage/sandbox/subagent packages.
Its complete Windows harness failure index contained only native instruction
fixtures and the combined CLI fixture enabling Unix-only workspace tools.

The combined CLI workflow now runs a portable variant on every platform, without
workspace/native-instruction flags. MCP, Todo (including Unicode behavior), model
selection, compaction, streaming and resume assertions remain active; every model
request must omit the unmounted AGENTS marker. Linux/Darwin additionally run the
full native-instructions variant. Windows explicitly tests rejection when native
flags are requested, before any durable session is created. Instruction framing
and bounded Unicode rendering are separately tested on every platform. Production
capability guards remain unchanged; this is not a Windows native-files backend.

That run completed with 13 passing jobs and two Windows fixture failures only.
After the MCP fixture correction, Windows desktop tests reached the worker
approval fixture: its trusted root still used a noncanonical drive spelling.
The fixture now canonicalizes its freshly created directory before deriving the
inert executable path, matching `protected_path`'s existing canonical contract.
Production binary pinning, ownership, workspace exclusion and script rejection
are unchanged. The positive assertion reports the actual validation error on any
future failure. No local Rust execution is claimed for that fixture; final Windows CI subsequently passed.

## Final macOS fixture transport correction

Run `40805233021` at `e3b36ad` completed with 14 of 15 jobs passing, including
all three native harness jobs, standalone harness Bazel and the full Windows
aggregate. The sole failure was the macOS remote-worker test fixture accepting a
connection from a nonblocking listener and reading it before headers arrived.
BSD accepts inherit that nonblocking state; the per-connection reader now explicitly
returns its socket to blocking mode while retaining the existing two-second read
timeout and bearer validation. No worker/client transport or security policy changed.

A Unix regression forces that inherited state on a real socket, observes the
kernel flag transition before supplying headers, then checks the authenticated
request and timeout. Removing only the reset reproduces early return; restoring
it passes. The exact fixture source compiled in an isolated Rust 1.99.1 crate;
only the new regression ran there (seven worker integration tests filtered out).
The isolated run was not a local full-worker or macOS pass. Final CI acceptance
above subsequently verified the complete macOS remote suite and all other jobs.
