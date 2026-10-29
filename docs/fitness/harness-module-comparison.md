# Existing Go harness module comparison

Status: scoped module comparison. Inventory and source review cover every existing
package; a whole-suite pass is not evidence of full behavioral parity. This report
distinguishes demonstrated defects, intentional composition limits, and verification
gaps. It is not an exhaustive proof of all source paths.

## Baselines

- Official DSH: `f3803c96974ad4a94195fb97e5c637e9c2a3afe3` (0.2.2-alpha.2).
- Original Pero PR #4: `d258dacfefe72534f74337f59a7948f0e68e064e`.
- Published first restoration: `236aaff64c7af516f48dbb033a45b51e0ff1c45f`
  (tested local commit `c074628b350f4f02b25ec1b732ea6243b41b7064`; identical
  tree `c1e4e51065e4846149e39eb737021bf4e782997a`).
- Point extraction pins: `69a1f8692725078c4ce7feaf497f5b80799ca44d`
  for the core; `edff76d7dd39b2900cff707af1896fe8ff9fd2dc` for later
  coding/delegation/strict-policy components. Package manifests take precedence.
- 25 Go packages, 406 Go files, 133 test files. Official DSH is TypeScript/Python;
  this Go extraction has different composition boundaries.

## Module inventory and comparison scope

Official paths below identify behavioral reference areas, not one-to-one copying
claims. Counts are Go files / test files.

| Go package | Count | Reference and responsibility | Verified state / intentional boundary |
| --- | ---: | --- | --- |
| `acp` | 10 / 4 | Protocol/session admission, streaming, model selection; `packages/acp/acp` | Cancellation admission restored; provisional stream cannot be retracted; schedule/workspace registry omitted. Focused comparison and regression complete. |
| `agentinstructions` | 5 / 1 | Bounded untrusted workspace context; `packages/context/agent-instructions` | Explicit root AGENTS.md only; no home/ancestor/nested discovery. Source inspected within declared subset; see findings and limits below. |
| `cmd/pero-agent-acp` | 14 / 8 | Headless CLI composition; `apps/ + profile composition` | Explicit endpoint/model/budgets, opt-in services; 413 recovery and length stop verified. Focused comparison and regression complete. |
| `config` | 15 / 1 | Typed configuration and credentials helpers; `packages/settings/settings; packages/credentials` | Point setting/account discovery not mounted by CLI. Source inspected within declared subset; see findings and limits below. |
| `fstools` | 11 / 5 | Descriptor-rooted bounded text files; `packages/fs/fs-local; packages/fs/tool-fs` | Physical read window; no selected reparse links, 1 MiB files. Windows native read/list/search/create-new implemented; replacement/edit still Unix-only. Native acceptance pending exact-head CI. |
| `harness` | 142 / 32 | Agent loop, tools, routing, lifecycle, compaction; `packages/core/agent; agent-loop; tools; packages/compaction` | Projects/jobs/workflows/schedules and product assembly excluded; delegation separately pinned. Source inspected within declared subset; see findings and limits below. |
| `internal/jsonwire` | 5 / 2 | JSON boundary parsing; `Local Go boundary utility` | Implementation utility, not an independently advertised DSH plugin. Source inspected within declared subset; see findings and limits below. |
| `internal/sessionlogroute` | 2 / 1 | Session log routing utility; `packages/session/session-log-deepseek (related seam)` | Remote log upload/product services not composed. Source inspected within declared subset; see findings and limits below. |
| `llm` | 10 / 4 | Provider metadata, request model and retry policy; `packages/llm/llm; llm-retry` | Point runtime service registration removed. Source inspected within declared subset; see findings and limits below. |
| `mcp` | 7 / 2 | MCP transport and resources; `packages/mcp/mcp-client; mcp-resources` | Trusted operator config; MCP grants are not per-call file approvals or OS isolation. Source inspected within declared subset; see findings and limits below. |
| `media` | 18 / 7 | Attachments and image metadata primitives; `packages/attachment; model content support` | Presence does not imply multimodal CLI/provider composition. Source inspected within declared subset; see findings and limits below. |
| `model` | 19 / 9 | Shared event/content/value types; `packages/core/session; packages/llm/llm` | Point product events/project binding omitted; extensions retained as opaque data. Source inspected within declared subset; see findings and limits below. |
| `nativetools` | 3 / 2 | Pero composition of six tools; `packages/fs/tool-fs; packages/shell/tool-bash` | Local glue rather than full upstream tool surface; writes/shell approval retained. Source inspected within declared subset; see findings and limits below. |
| `permissionpresets` | 2 / 1 | Immutable strict policy bundle; `packages/sandbox/sandbox-policy; authorization` | Full-access, mutable selectors and product services deliberately omitted. Source inspected within declared subset; see findings and limits below. |
| `providers/openai` | 6 / 1 | OpenAI-compatible transport and stream assembly; `packages/llm/llm-deepseek` | Output-limit and context-overflow restored; static text-only explicit credentials/config. Focused comparison and regression complete. |
| `runtime` | 26 / 12 | Stdlib lifecycle/service kernel; `Cordis lifecycle concepts` | Not an agent scheduler; supervisor/timer/product repair services excluded. Source inspected within declared subset; see findings and limits below. |
| `sandbox` | 7 / 2 | Strict native process policy; `packages/sandbox/sandbox-local` | Unsupported backend refuses launch; positive OS confinement not demonstrated here. Source inspected within declared subset; see findings and limits below. |
| `session` | 52 / 20 | Durable events, projection and context receipts; `packages/core/session; packages/session/session-format/projection` | Point project/context-plan receipt formats removed/replaced; no cross-product format claim. Source inspected within declared subset; see findings and limits below. |
| `sessionview` | 9 / 3 | Conversation/UI projection helpers; `packages/session/session-projection` | Direct projection/history/digest/cache contract tests added; immutable-checkpoint lifetime repaired. No production importer, UI acceptance or JavaScript oracle equivalence. |
| `shell` | 7 / 2 | Foreground command lifecycle; `packages/shell/bash-local; shell` | Background/persistent/PowerShell/spill/managed-env paths excluded. Source inspected within declared subset; see findings and limits below. |
| `skills` | 3 / 1 | Explicit read-only skill catalog; `packages/skill/skill-filesystem; tool-skill` | No implicit roots, bundle precedence, scripts or invocation hooks. Source inspected within declared subset; see findings and limits below. |
| `storage` | 11 / 4 | Storage and persistence helpers; `packages/storage/storage-json; session-persistence-jsonl` | No SQLite migration/product-storage parity claim. Source inspected within declared subset; see findings and limits below. |
| `subagents` | 7 / 4 | Synchronous one-level delegation; `packages/subagent/subagent-spawn-in-process; subagent-fork-in-process` | No external/async/resumable provider; caps shared, child required approvals rejected. Source inspected within declared subset; see findings and limits below. |
| `todos` | 7 / 5 | Session-backed task list; `packages/todo/tool-todo` | Whole-list replacement; next turn resets list; CLI selects DSH normalization while library default remains Legacy; plan/user-question service omitted. Source inspected within declared subset; see findings and limits below. |
| `tool` | 12 / 2 | Schemas/arguments/tool-facing primitives; `packages/core/tools; tool-executor-native` | Core contribution ownership retained; not all official tool plugins mounted. Source inspected within declared subset; see findings and limits below. |

## Completed first restoration

See [the canonical incident record](../issues/2026-10-28-harness-upstream-parity.md)
for the four defects and red/green evidence. Final native Go tests, race, vet and
build pass; repository repair/restart and child lifecycle scenarios pass with
scripted loopback model responses. No real-model autonomy claim is made. Bazel
was not rerun because this executor has no Bazel executable.

## Review criteria

For each existing module, compare the declared subset's error contracts, durable
state transitions, cancellation, resource ownership and observable results. A
missing optional upstream product feature is not automatically a regression.
Preserve Pero's explicit approvals, workspace limits and request budgets. Any
newly confirmed defect requires a deterministic red test, minimal local repair
and a repeat of affected integration gates.

Memory remains issue #9; Projects, UI, accounts, workflow/job/schedule product
assembly and full plugin profile parity are outside this restoration.

## Second-pass findings in existing modules

| Area | Reproduction and restoration | Attribution / runtime reach |
| --- | --- | --- |
| Core output-limit stop | A custom adapter returning max-tokens plus a tool call still executed the tool. Core now removes those calls before durable commit/execution. A later injected completion no longer downgrades max-tokens. | Both gaps were present in the pinned Point loop; provider-only filtering was insufficient. Active Agent path. |
| Native shell result | The collector retained a tail, but JSON rendering retained its prefix and hid final compiler/test diagnostics. Rendering now keeps a UTF-8-safe 4096-byte tail per stream. | Pero native-tool composition; no output-budget increase. |
| MCP capability discovery | A resources-only server received tools/list and failed method-not-found. Discovery now respects the advertised tools capability; the resource remains readable. | Active MCP mounting path; no new permission or server grant. |
| Provider token usage | Negative/unsafe/coerced fractional counts were accepted; cached tokens greater than inclusive prompt tokens yielded negative input. Validate at the provider boundary before arithmetic. | Nonnegative-safe-count contract follows official DSH; cache subset is specifically the Chat Completions conversion invariant, not an official Messages field rule. |
| JSONL tail | Load accepted a complete final object without its newline; subsequent append concatenated records and corrupted reload. Reject unterminated records before append under the existing lease; preserve original bytes. | Inherited from pinned Point. Active session persistence. Deliberately fail-closed rather than silently discard/repair a torn tail; not crash-exactly-once recovery. |
| Per-record migration | After migration and deletion of the last record, reopen restored the deleted legacy data. A declared table directory, even empty, now marks initialized storage. | Inherited from pinned Point. Retained storage library; current CLI does not mount this legacy migration unit. Unknown directories do not suppress bootstrap. |
| CLI todo composition | Existing DSH-mode code was present but CLI mounted zero-value Legacy options. CLI now explicitly selects DSH normalization/error/schema behavior; real process checks BOM rejection and NEL preservation. | Deliberate CLI compatibility choice; generic library defaults unchanged. Earlier injected BOM unit scenario did not validate CLI selection. |
| File settings equality | Updating 9007199254740992 to 9007199254740993 was a silent no-op after float64 comparison. Reuse exact JSON-number decoding for section equality. | Inherited Go-library issue, not identical JS semantics and not a Pero extraction regression. FileSettingsBackend is not mounted by CLI; in-memory settings are. |

Each repair has a deterministic pre-fix failure and targeted post-fix regression.
The [canonical second-pass record](../issues/2026-10-28-harness-module-contracts.md) carries final verification status. Historical
Point source pins were read for the core loop, session persistence, storage and
settings attribution; remaining Point mapping is provenance, not byte-equivalence.

## Explicit remaining gaps

- **MCP structured output:** canonical content plus structuredContent and supported
  outputSchema now flow through the existing registry boundary. Unsupported schemas
  fall back explicitly; text and image projection remain the existing Go behavior,
  not full official rich-attachment/PTC rendering parity.
- **Inactive retained seams:** sessionview now has direct native contract tests but
  still has no production importer in this extraction; session-log routing has no upload caller; file settings,
  credentials and optional media codecs are not implicitly enabled by CLI.
- **Media:** native fixture tests are not an official Sharp/codec oracle run or
  multimodal provider E2E. The current CLI/provider remains text-only.
- **Rust bridge:** this round inspected ACP/Worker sources statically. It did not
  repeat the historical Rust/native 31-scenario acceptance or rebuild Desktop.
- **Composition differences:** root-only instructions, explicit read-only skills,
  foreground Bash, one-level synchronous children and default-denied mutation
  approval are intentional. No full upstream plugin/profile parity is claimed.
- **Cross-language evidence:** native tests and source comparison do not replace
  the omitted official JavaScript source-oracle suites.

## Provenance verification boundary

Official DSH is the primary behavioral baseline; Point is extraction provenance
and helps identify inherited issues. Current destination hashes were checked,
not every upstream origin. A separate partial Point-source collection checked
80 of 343 mappings: 74 matched, while six test-file declared blob hashes differed
from the file at the declared commit/path (`approval_test.go`, `harness_test.go`,
`retry_test.go`, `tools_arguments_dsh_test.go`,
`tools_arguments_dsh_todo_numbers_test.go`, `tools_arguments_dsh_todo_test.go`).
On 2026-10-29, all six were re-fetched at the exact declared Point commit/path
through the GitHub contents connector. Decoded file bytes were independently
hashed with Git blob framing and matched the connector-returned SHA, confirming
the discrepancies. Each affected `SOURCE.json` entry now preserves its original
`source_blob_sha1` declaration and records `source_verification` with
`declared_blob_mismatch`, `import_origin_status: unresolved`, and a separately
verified pinned reference (Git blob SHA-1, content SHA-256 and byte count).
This reconciles the audit record; it does **not** recover the unknown original
import blob or silently substitute a new provenance claim. To reproduce, fetch
the six `source_path` files from that commit and compare `git hash-object <file>`
and `sha256sum <file>` with `source_verification.pinned_reference`. No other
Point origin is newly verified by this bounded recheck.
Collection stopped when the requested focus moved to official DSH. This does
not invalidate the individually inspected official behavior references, but
precludes a blanket claim that all source provenance is independently verified.

## Tree-sitter source evidence

The reproducible [AST comparison tool](../../scripts/ast-compare/README.md) uses
real pinned Go/TypeScript parsers. Official DSH-to-Pero callable mappings are the
primary report; Point-to-Go declaration comparison is a secondary provenance
aid. It records control-flow guards, calls, state writes, error landmarks and
source spans without claiming cross-language semantic equivalence.

The [validated snapshot](../../scripts/ast-compare/VALIDATION.md) selects 17
callable pairs. The follow-up grammar extension parses the official Todo type-star
export without stripping syntax; all 17 pairs are extracted against the verified
source snapshot. The tool still fails closed on parser errors. Its 18 unit tests pass;
this scoped evidence is not a whole-repository AST or behavioral parity claim.

## Retained sessionview contract verification (2026-10-29)

Three local test files cover seven concepts: history pagination, contiguous/safe
JSON admission, whole-log digest, streaming projection checkpoint continuation,
content-only tool-result replacement, disk snapshot continuation, and corrupted
index/blob/checkpoint rejection. Pagination message counts, exclusive `before`,
inclusive `through`, turn boundaries and source grouping were checked against
official DSH `packages/api/session-controller/src/history.ts::paginate` at the
pin above. UTF-16 byte limits, canvas receipts, native-to-projected mappings and
the disk format remain retained Point-specific contracts, not DSH feature parity.

The disk continuation regression failed before the fix: a second `Save` deleted
the immutable checkpoint required by an already opened history. Checkpoints now
remain available for those lazy continuations, with the same cache-directory
lifetime as immutable blob segments. The cache owner must reclaim the directory
when no history uses it; this change does not introduce background GC.

Validation: `GOWORK=off go -C harness/go test -race ./sessionview` and
`GOWORK=off go -C harness/go vet ./sessionview`. The original Point JavaScript
projection oracle was not restored or executed; no full cross-language or UI
acceptance claim follows from these native tests.


## Actionable gap completion

The [follow-up record](../issues/2026-10-29-harness-gap-completion.md) distinguishes
implemented capability from intentionally unmounted product services. Current
JSONL recovery is explicit opt-in and complete-record-only, with preserved backup;
default fail-closed safety remains. The standalone harness now has its own Go and
Bazel CI gates, separate from root server jobs. Positive strict sandbox execution
is required on the selected Linux CI runner; an unavailable local backend is not
reported as a pass. Actual Windows font-cache recovery and the complete platform
aggregates have now passed the final CI gate below.


## Final acceptance

[CI run 40807109189](https://github.com/phodal/pero/actions/runs/40807109189) passed **15/15 jobs** on
code head `78a402d4be2ce89695d40bbd44797d4a174d70c8` (tree
`addf3222ddecce723d4c47cda1e85a34341f13a3`). Native harness tests ran on Linux, macOS
and Windows; standalone harness Bazel, full build/check/Rust aggregates on all
three platforms, positive Linux sandbox execution and 17 selected official AST
extractions passed. This acceptance is tied to that exact code snapshot.

Windows runs the portable coding workflow and explicit refusal tests for the
unsupported Unix-native capabilities; it does not gain a Windows native workspace
or process-group shell backend. The 17 AST pairs establish successful structural
extraction, not semantic equivalence or whole-repository coverage. The intentional
product/media/oracle limits above remain. PR #4 remains unmerged; no deployment
was performed.


## Windows file subset — 2026-10-29 (native acceptance pending)

The earlier acceptance above predates Windows file support. The current bounded
increment adds real handle-rooted read/list/search and atomic exclusive creation,
including approved built-CLI coverage and reparse/path/collision negatives.
Existing-file replacement/edit remain deliberately unavailable: DSH's
`ReplaceFileW` metadata merge cannot be replaced with pathname reopening or a
metadata-dropping handle rename. Strict shell policy and partial backend limits
are unchanged. See [verification and exact remaining gap](../issues/2026-10-29-windows-file-tools-verification.md).
