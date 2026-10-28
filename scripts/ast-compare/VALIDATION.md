# Validated reference snapshot

Run: 2026-10-28. Python 3.12; pinned requirements; 15 unit tests passed.

- Official DSH: `f3803c96974ad4a94195fb97e5c637e9c2a3afe3`
- Tested Pero: `73085a72ae6f32a32a3ab478c5aae98745869249`
- Published Pero: `75657810bf99932564e0b56634f4598e604a5b8c`. Independently fetched from the public GitHub repository; `git rev-parse REF^{tree}` equals local tested tree `a0ba010b7fc8391e2743b364b1a9d43272dcb269`. All mapped Go source bytes were also compared directly against `git show` at this published commit. The mapping uses this fetchable ref.
- Coverage: 17 explicitly selected callable pairs; 11 official files, 10 Go files. This is not whole-module coverage.
- Result: **15 extracted for cross-language review; 2 unavailable**. Exit 2 is expected.
- Both Todo selections fail whole-file parsing at official `packages/todo/tool-todo/src/index.ts:20` (`export type *` unsupported by TypeScript grammar 0.23.2). No source syntax was stripped.
- Extraction and observed landmarks do not establish semantic equivalence or passing behavioral tests.

## Selected pairs

| Pair | Result |
| --- | --- |
| `core-max-token-dispatch` | cross_language_review_required |
| `core-max-token-sticky` | cross_language_review_required |
| `stream-truncated-tools` | cross_language_review_required |
| `provider-error-classification` | cross_language_review_required |
| `provider-usage-validation` | cross_language_review_required |
| `read-physical-window` | cross_language_review_required |
| `read-continuation` | cross_language_review_required |
| `acp-prompt-admission` | cross_language_review_required |
| `acp-cancel-admission` | cross_language_review_required |
| `acp-settlement` | cross_language_review_required |
| `session-torn-tail` | cross_language_review_required |
| `session-tail-recovery` | cross_language_review_required |
| `storage-migration-marker` | cross_language_review_required |
| `todos-normalization` | unavailable |
| `todos-composition` | unavailable |
| `mcp-tool-capability` | cross_language_review_required |
| `mcp-structured-output-gap` | cross_language_review_required |

## Before/after evidence

Compared against initial Pero `d258dacfefe72534f74337f59a7948f0e68e064e`:
- Go declaration mode: 3 added, 35 modified, 3410 unchanged, 16 ambiguous identities; valid parsing, exit 2. Platform alternatives and repeated declarations are not arbitrarily paired.
- Official references: initial 14 extracted / 3 unavailable; final 15 / 2. The added JSONL scanner selector resolves in the final snapshot.
- New structural landmark observations cover output-limit dispatch/sticky state, safe usage counters, read continuation offsets, ACP admission/cancel/settlement, JSONL tail boundaries, storage marker gating and MCP capabilities.
- These are lexical AST observations, not regression-test outcomes. Intentional provider, persistence and capability differences remain annotated in the mapping.

## Reproduction

Install the pinned requirements and run tests as shown in README. With the exact source checkouts:

```sh
python scripts/ast-compare/references.py \
  --mapping scripts/ast-compare/mappings/official-dsh.json \
  --left-root /path/to/dsh-f3803c9 --right-root /path/to/pero-7565781 \
  --json /tmp/official-dsh.json --markdown /tmp/official-dsh.md
```

Full JSON/Markdown are deliberately generated outside the repository. JSON contains exact raw fact text, byte spans, nested callable context, actual hashes and parser diagnostics. Ref labels alone are not Git attestations; selectors enforce the following byte receipts.

## Verified source receipts

Each unique file below was independently compared byte-for-byte with `git show REF:path` in its source checkout and its mapping SHA256. Repeat this check when changing refs. No source content is vendored.

| Side | File | SHA256 |
| --- | --- | --- |
| left | `packages/acp/acp/src/session.ts` | `e4d624d1a4d305d924ae0cff23e0f005becf9a797b02c037e45a1e86c3c17d12` |
| left | `packages/core/agent-loop/src/agent.ts` | `ad9ee0778c0a93dec40363f88b53f442add9b033837b9ff049675a1ada3df61f` |
| left | `packages/fs/tool-fs/src/read-render.ts` | `72bcf4a70fd3e0908d4aa3869e0fbd6ee63b05ad790af65951af0c5db9edba84` |
| left | `packages/llm/llm-deepseek/src/translate.ts` | `254f4b8fe292dadc5c24df3375b01920f5e5b345be09eae4290e91e9e810c1ba` |
| left | `packages/llm/llm-deepseek/src/transport.ts` | `7d1f6ed8fe861ee71ba2d0c6a2a9e8512ddc1b6dc17d43a8795ab520a38746b6` |
| left | `packages/llm/llm/src/assembler.ts` | `a2d065645058a70195a504b101e9eed23df3eaced3def868d602f3a863b03887` |
| left | `packages/mcp/mcp-client/src/tools.ts` | `59203150bda479a990afcdaf60483a35a836887926892e1074e02d0294c01af9` |
| left | `packages/session/session-persistence-jsonl/src/format.ts` | `87d536ead041fb953f866e0191a79b4dbbbc510aee1461706f9cdc2e4c46a038` |
| left | `packages/session/session-persistence-jsonl/src/storage.ts` | `57d5fa1d7efd17789d6a223f6aa5e06b79092665bea13f8a23d3bab644d066c3` |
| left | `packages/storage/storage-json/src/per-record-unit.ts` | `9aa4df293c02c28aaf74515443a7c089c660c33e66c3ece2255ff8c53d47add1` |
| left | `packages/todo/tool-todo/src/index.ts` | `80559ed95b7fc9c1dc083312f187885254bf2c1498d0f7a079dad21427d24def` |
| right | `harness/go/acp/server.go` | `722f1f137bea925688b419a50f338766987a938b6375adc6f6fa6a0fd065dd74` |
| right | `harness/go/cmd/pero-agent-acp/coding.go` | `6898a4178c921c7683991e735c369554978162b610de0237b1728b07ab53c583` |
| right | `harness/go/fstools/workspace.go` | `d00c8f0b640e0670705025965fce957eb58c45c7f44872bad1f878dbb45d23be` |
| right | `harness/go/harness/loop.go` | `95113ee77a8856acf39b64319cb1db92cabc2799e8e1581796885e6daa64d798` |
| right | `harness/go/mcp/tools.go` | `943556672e4d40ce76f3131781945560050759a7ad6d1cd39a424a86d56b6fcc` |
| right | `harness/go/providers/openai/provider_errors.go` | `20ce09a75aa29b167c5e680dd14643ebd60593a5866451f22ba12f3b198ba8ab` |
| right | `harness/go/providers/openai/stream.go` | `2cecfaf622dcebbb5400207ddeadd95d2b24ed3c9ece466d82fa8d42288aae8f` |
| right | `harness/go/session/persistence.go` | `22c3a9c224a48fdd7d0f3ec2feee9e59da954436b7e3add4b4025c8ae3095702` |
| right | `harness/go/storage/json_units.go` | `f001054d8cf58146d48c7ed840f96866edbd0da5ea6488e79b2116ec73491f52` |
| right | `harness/go/todos/normalization.go` | `df8da35d86358b0d8e5a9be8226c1b17cef340d3c4a4da34b0337ed84ed26d09` |
