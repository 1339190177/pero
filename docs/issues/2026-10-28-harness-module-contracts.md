---
kind: incident
status: resolved
---

# Existing module contract restoration after source comparison

This follow-up inspects the existing 25 Go packages after the first restoration.
The [module matrix](../fitness/harness-module-comparison.md) records active wiring,
retained libraries, provenance and remaining gaps. Official reference is DeepSeek
Harness `f3803c96974ad4a94195fb97e5c637e9c2a3afe3`; direct Go provenance remains
Point `69a1f8692725078c4ce7feaf497f5b80799ca44d` / `edff76d7dd39b2900cff707af1896fe8ff9fd2dc`.

## Confirmed and restored

1. The common Agent loop now enforces max-tokens before tool execution, not just
   the OpenAI adapter. Such calls are absent from committed history. A later
   injected successful step cannot downgrade the turn's max-tokens result.
2. Native tool JSON rendering preserves the shell collector's trailing-output
   contract, retaining final diagnostics under its existing 4096-byte budget.
3. MCP discovery honors advertised tools capability. Resources-only connections
   no longer fail on an unsupported tools/list request; resources remain readable.
4. Provider usage validates nonnegative safe integer counts before conversion.
   The SDK's numeric coercions do not silently accept fractions/strings. Inclusive
   Chat Completions prompt counts must cover cached counts before subtraction;
   this is a local format-conversion invariant, not an official Messages field rule.
5. JSONL Load and Attach reject an unterminated final record before mutation.
   Existing writer-lease revalidation covers stale loaded sessions. Original bytes
   remain unchanged. This intentionally fails closed; it does not implement the
   official automatic torn-tail repair/discard flow or claim exactly-once recovery.
6. An empty declared per-record table directory remains initialized storage.
   Deleting the last migrated record no longer resurrects legacy data on reopen.
   Unknown directories still allow bootstrap; malformed recognized data is preserved.
7. File settings equality reuses the existing exact-number decoder, so adjacent
   large int64 updates no longer silently become no-ops. This is a retained Go
   library fix; FileSettingsBackend is not supplied by the current CLI.

8. The DSH-compatible CLI explicitly selects the existing DSH todo normalization,
   diagnostics and open-root schema options. The generic library's Legacy default
   remains unchanged. An actual CLI process rejects BOM-only content, preserves
   NEL, accepts the upstream root extension and persists the normalization tag.
   This is a deliberate composition choice; old DSH-mode unit tests and the
   injected BOM repair exercise did not prove what mode the CLI mounted.

Pinned Point source confirms the common-loop, JSONL, migration and settings
issues predated Pero extraction. They are inherited gaps, not defects introduced
by import rewriting. Storage migration and file settings are library-only here;
no production user-data loss is asserted.

## Verification contracts

Each finding has a deterministic red reproduction and a regression in its
existing concept test file. Coverage includes no tool side effect or dangling
committed call at max-tokens, sticky final turn status, ASCII/Unicode/control
shell tails, actual MCP resource reads with no tools/list, optional valid usage,
unterminated header/event bytes unchanged on rejection, migrated last-record
reopen, unknown directories and exact returned/persisted int64 endpoints.

The sessionview header's inherited claim of a present JavaScript oracle was also
corrected: that oracle/test is not in this extraction. The unused package is not
deleted or presented as verified UI parity. MCP structuredContent/outputSchema
canonical integration remains a documented gap, not silently expanded scope.
