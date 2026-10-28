# Tree-sitter source comparison

A local development tool for comparing official DSH behavior references with
Pero's Go implementation. It parses source but never imports or runs that source,
contacts a model, fetches a repository, or publishes a report.

**Primary mode:** explicitly mapped official TypeScript → Pero Go functions.
**Secondary mode:** Go → Go declaration comparison, useful for Point provenance
and before/after changes. Neither mode proves semantic equivalence.

## Setup and tests

Python 3.10+ and the pinned official Tree-sitter bindings/grammars:

```sh
python3 -m venv scripts/ast-compare/.venv
scripts/ast-compare/.venv/bin/pip install -r scripts/ast-compare/requirements.txt
scripts/ast-compare/.venv/bin/python -m unittest discover -s scripts/ast-compare -v
```

Use the corresponding `.venv/Scripts/python.exe` on Windows. This is an optional
analysis dependency, not a dependency of the Go runtime or the root application.
The tested interpreter was Python 3.12. Source projects need no dependencies
installed. Parser versions are included in every report.

Official references: [Python bindings](https://github.com/tree-sitter/py-tree-sitter),
[Go grammar](https://github.com/tree-sitter/tree-sitter-go), and
[TypeScript grammar](https://github.com/tree-sitter/tree-sitter-typescript).

## Official DSH → Pero reference mode

Acquire the official source separately and pin it to the mapping's `left_ref`.
Use a fixed Pero checkout, not a branch being concurrently edited.

```sh
scripts/ast-compare/.venv/bin/python scripts/ast-compare/references.py \
  --mapping scripts/ast-compare/mappings/official-dsh.json \
  --left-root /path/to/deepseek-harness \
  --right-root /path/to/pero \
  --json /tmp/dsh-pero-reference.json \
  --markdown /tmp/dsh-pero-reference.md
```

A mapping explicitly names both source files and callable symbols:

```json
{
  "left_ref": "official Git commit",
  "right_ref": "Pero Git commit or documented working snapshot",
  "pairs": [{
    "id": "acp-prompt",
    "left": {"path": "packages/acp/acp/src/session.ts", "language": "typescript", "symbol": "AcpSession.prompt"},
    "right": {"path": "harness/go/acp/server.go", "language": "go", "symbol": "Server.prompt"},
    "notes": "Intentional composition differences stay visible.",
    "landmarks": [{"id": "cancellation", "left_terms": ["abort"], "right_terms": ["promptCancel"]}]
  }]
}
```

Symbols use exact names: top-level `functionName`, TypeScript `Class.method`,
Go `Receiver.method` (pointer receiver does not change the lookup name).
Named arrow-function variables are supported. Optional `kind` and 1-based `line`
selectors can disambiguate. An optional `sha256` on either selector enforces the
exact file bytes. No fuzzy name matching or automatic pairing is performed.

For each pair, the tool records:

- source labels, actual SHA256, exact locations and signatures;
- branch/loop guards, call targets and arguments, write targets,
  return/throw paths and string/error landmarks;
- surrounding control-node locations and nested callable ownership for every fact;
- assignment target category (local declaration, member/index, or unknown),
  not a claim that every assignment is persistent state;
- exact raw snippets and byte spans separately from concise display text;
- exact lexical fact differences and side-by-side counts;
- optional manually specified landmark observations with source evidence.

The Markdown report is a small per-pair review summary; JSON retains detailed
facts and locations. Long snippets are explicitly marked `truncated`. The
extractor is syntactic: it does not build an interprocedural control-flow graph,
resolve types/aliases, follow a called helper, infer a callback's execution, or
prove that a guard dominates an effect. Language adaptation often changes all
call names; lexical differences are **not automatically bugs**. Mapped
landmarks are substring observations in AST-derived facts, **not behavioral
assertions**. A term can occur in a diagnostic string, dead code, or nested
callback; inspect its recorded location and run the associated regression.

Every successfully extracted pair is `cross_language_review_required`, even if
all landmarks were observed. Missing/ambiguous symbols, parser ERROR/MISSING
nodes, missing files, unsupported languages and hash mismatches are explicit
`unavailable` cases. Official Python is not supported by this version. Do not
substitute a same-language report for official cross-language behavior review.

Exit 0 means all selected references were extracted, **not parity passed**.
Exit 2 means an input/reference was unavailable. Empty mappings are rejected.
The ref labels are supplied by the mapping, not inferred Git attestations; verify
checkout identity separately. Per-file hashes capture the actual parsed bytes.

## Go → Go declaration mode

```sh
scripts/ast-compare/.venv/bin/python scripts/ast-compare/compare.py \
  --left /path/to/point-projected-go --left-ref POINT_SHA \
  --right /path/to/pero/harness/go --right-ref PERO_SHA \
  --import-map github.com/phodal/point/go/harness=github.com/phodal/pero/harness/go \
  --json /tmp/point-pero-ast.json --markdown /tmp/point-pero-ast.md
```

Use additional exact/prefix `--import-map OLD=NEW` rules only for intended
relocations. They apply solely to import paths, never arbitrary string literals
or identifier names. `--package-map OLD=NEW` explicitly normalizes a package
clause name. Mappings are recorded. Test files are excluded unless
`--include-tests` is supplied.

Declarations match by logical package directory, package name, declaration kind,
and name (methods include receiver type). Type-group wrappers and variable-group
wrappers are removed for matching. Const groups stay intact because iota and
omitted expressions make their order meaningful. A declaration moving to another
file is identified independently of its AST/signature/body differences.

The canonical tree preserves anonymous operators, literals, receivers, generics
and field labels. Ordinary comments, whitespace and explicit separator semicolons
are ignored; compiler/build/line directives retain positions and following declaration identity,
and cgo comments remain file context. This deliberately flags some harmless
directive movement rather than claiming a false clean comparison.
Import bindings are compared separately. The tool conservatively treats a file's
underscore suffix as build context, even when it is not a GOOS/GOARCH suffix.
This can flag harmless file moves for review; it cannot establish build equivalence.
Different per-file import sets can likewise produce context-only differences
when a declaration is split into a smaller file. `ast_equal` remains separate
from those context changes.

Variable and init declaration sequence/placement changes produce explicit
initialization-review entries. This is not Go dependency analysis. Multiple
init functions, build-tag alternatives, blank-identifier declarations or other
repeated keys are `ambiguous`; the tool does not guess which pair is equivalent.
Use an explicit target-specific inventory to avoid mixing mutually exclusive
platform implementations. No Go type checking or build constraint evaluation
is performed.

For projected/multi-pin source inventories use `--left-manifest` or
`--right-manifest` instead of a directory:

```json
{
  "root": "/path/to/pinned-files",
  "ref": "composite provenance; see per-file ref",
  "files": [{"path": "SHA/go/harness/agent.go", "logical_path": "harness/agent.go", "ref": "SHA", "sha256": "actual file hash"}]
}
```

`logical_path` locates the original file in the comparison's package layout.
It does not rewrite its bytes. Duplicate logical paths and mismatched hashes
fail at admission. Relative manifest roots are relative to the manifest file.
Directory discovery ignores symlink files; a manifest is an explicit local
read list supplied by the operator. Reports preserve original source paths.

Exit 0 means no structural/context differences in the selected valid input;
exit 1 means differences/review items; exit 2 means parser errors or ambiguity.
Even exit 0 does not prove equivalent behavior. Source compilation, tests,
side effects, configuration and runtime wiring remain separate checks.

## Current grammar coverage

The 17-pair official mapping at DSH `f3803c9` extracts 15 pairs with the pinned
grammars. Two Todo pairs share a source file containing TypeScript 5
`export type *`, which the official published TypeScript grammar 0.23.2 does not
parse. Its current official generated grammar also lacks that form. The complete
source is left unchanged, those pairs report `parse_error`, and the run exits 2.
This is a parser-coverage gap, not a Todo behavior failure or a successful check.
A compatible official grammar release can be adopted with explicit version and
regression updates; do not strip unsupported syntax to obtain a green report.

See [validated snapshot and source receipts](VALIDATION.md) for the actual selected-pair run and reproduction details.
