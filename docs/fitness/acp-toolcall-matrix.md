---
kind: verification_report
date: 2026-10-28
---

# Compact ACP tool-call fixture matrix

Send `toolmatrix` to `tests/fixtures/acp-agent.mjs` through the test ACP Agent.
It emits nine distinct calls and eighteen partial updates, with 350 ms phases
(about 6.3 seconds total). It does not run commands, read or change project files,
create terminals, or fetch URLs. Existing normal, permission, wait and longstream
fixture branches are preserved.

| Variant | Expected compact presentation | Fixture content |
| --- | --- | --- |
| Adjacent read / search / fetch | One expandable activity group; active operation visible; completed summaries remain compact | Locations, structured input and short text outputs |
| Execute | One short command row; output expands without a second nested card | Text plus a synthetic `terminalId` reference |
| Edit | File/action row; diff available on expansion | ACP `diff` content with absolute `path`, `oldText`, `newText` |
| Think | Small review activity, without exposing a separate large panel | A short synthetic coverage note |
| Future kind | Generic icon/action fallback; retain title and output | `vendor_future_tool`, normalized to `other` by SDK 3.5 |
| Failed execute | Error state and useful error text remain visible even with details collapsed | Expected 200 / received 503, exit code 1 |
| Canceled empty execute | No leftover empty operation row after cancellation is recognized | Valid `failed` status plus `_meta.cancelled` and `rawOutput.cancelled` |

Every call goes through `pending` → `in_progress` → `completed` or `failed`.
Updates omit unchanged title/kind fields, so they must update the existing row
without duplication or timeline reordering. ACP v1 has no `cancelled` status;
cancellation metadata intentionally distinguishes that case from an ordinary
visible failure.

The native Host/SDK probe retained all nine calls, all eighteen updates, the diff
fields, terminal reference, and cancellation metadata, and finished the turn.
Diagnostics were captured in the temporary directory
`/var/folders/l8/7nm25zns5qjds0jc7z8f1gpm0000gn/T/pero-toolmatrix-pumvcler`.
This is protocol/fixture coverage; rendered screenshots and interaction checks
are separate. The terminal reference is a shape-only fixture, not proof of
terminal creation, output polling or release support. These simulated outputs
are not evidence that a real provider completed repository work.
