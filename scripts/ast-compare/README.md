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
npm ci --prefix scripts/ast-compare
scripts/ast-compare/.venv/bin/python scripts/ast-compare/build_typescript.py \
  --cli scripts/ast-compare/node_modules/.bin/tree-sitter
scripts/ast-compare/.venv/bin/python -m unittest discover -s scripts/ast-compare -v
```

Use WSL for the extended grammar on Windows; native Windows can use the stock
parser via `.venv/Scripts/python.exe` with its documented coverage gap. This is an optional
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

## TypeScript 5 grammar extension

The official published TypeScript grammar 0.23.2 cannot parse `export type *`.
This tool has a deliberately small **local grammar extension**, version
`0.23.2+pero.export-type-star.1`; it is not an upstream release. It adds one
`export_statement` alternative for `export type * from ...` and
`export type * as Ns from ...`, with a required module source. Every other rule
is unchanged. The actual C parser is regenerated; project source is never
stripped, masked, rewritten or parsed in isolated fragments.

`build_typescript.py` downloads the official npm 0.23.2 archive (the PyPI sdist
omits structured grammar), checks its pinned SHA256, extracts only the required
source/license files, checks the original grammar hash, extends the structured
rule, runs official Tree-sitter CLI 0.25.10 with ABI 14, and builds a shared
library using `cc`. The npm lockfile pins generator package integrity. The CLI
package installs its matching official platform binary. `--archive PATH` allows
an already downloaded, hash-verified tarball; `--cc PATH` selects the compiler.

First use needs Python, Node/npm, a C compiler, and network access to the npm
registry and official CLI release download. Generation/compilation is a one-time
local setup step (seconds on the validated Linux machine). The build supports
Linux/macOS; Linux was tested, macOS was not. Windows users should use WSL.
Generated parser C, binaries, source archive, upstream MIT license, and build
receipt stay in ignored `.grammar/`; no bulky vendoring is required. Removing
that directory reverts to stock grammar, explicitly reported as version 0.23.2;
then the two Todo mappings again fail and extension tests are explicitly skipped.
Run the build step before claiming full coverage.

Reports include extension identity, archive/original/patched grammar hashes,
generator/compiler versions, ABI and actual library SHA256. The loader checks
the local library against its receipt before use. This is local reproducibility
metadata, not an external attestation. Rebuild after moving machines or changing
the parser version. A source/hash change fails closed and requires reviewing
the extension. Remove the extension when a compatible upstream release is
adopted, retaining its valid/invalid syntax regressions.

All 17 selected mappings now parse with the extension, including the complete
unmodified official Todo file. Malformed exports and malformed surrounding
functions still produce ERROR/MISSING failures. Extraction success is still
cross-language review, not semantic equivalence or a passing behavior test.

Primary specifications: [TypeScript 5 type-only star exports](https://www.typescriptlang.org/docs/handbook/release-notes/typescript-5-0.html#support-for-export-type-),
[Tree-sitter structured grammar generation](https://tree-sitter.github.io/tree-sitter/cli/generate.html).
The official repository's [pending support proposal](https://github.com/tree-sitter/tree-sitter-typescript/pull/360)
is context only; no unmerged fork code is installed.

See [validated snapshot and source receipts](VALIDATION.md) for the actual selected-pair run and reproduction details.
