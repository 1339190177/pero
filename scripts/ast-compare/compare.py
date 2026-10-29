#!/usr/bin/env python3
"""Conservative Go declaration comparison using Tree-sitter, never an equivalence proof."""
from __future__ import annotations

import argparse
from collections import Counter, defaultdict
from dataclasses import asdict, dataclass
import hashlib
from importlib.metadata import version
import json
from pathlib import Path
import sys

from tree_sitter import Language, Node, Parser
import tree_sitter_go


@dataclass(frozen=True)
class Source:
    path: str
    logical_path: str
    content: bytes
    ref: str


@dataclass(frozen=True)
class Declaration:
    key: str
    kind: str
    name: str
    package: str
    file: str
    line: int
    end_line: int
    signature: str
    body: str
    ast: str
    imports: tuple
    constraints: tuple
    source_ref: str


@dataclass(frozen=True)
class Snapshot:
    ref: str
    declarations: tuple[Declaration, ...]
    files: tuple[dict, ...]
    initialization: tuple[tuple[str, tuple], ...]
    diagnostics: tuple[dict, ...]


def digest(value: object) -> str:
    return hashlib.sha256(json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode()).hexdigest()


def walk(node: Node):
    yield node
    for child in node.children:
        yield from walk(child)


def canonical(node: Node, omit_body: bool = False):
    """Include anonymous tokens/operators and field labels, not whitespace/comments."""
    if node.type == "comment" or node.type == ";":
        return None
    children = []
    for index, child in enumerate(node.children):
        field = node.field_name_for_child(index)
        if omit_body and field == "body":
            continue
        value = canonical(child)
        if value is not None:
            children.append((field, value))
    if children:
        return (node.type, children)
    return (node.type, node.text.decode("utf-8"))


def import_path(node: Node) -> str:
    raw = node.text.decode("utf-8")
    if raw.startswith("`"):
        return raw[1:-1]
    # Go imports normally use JSON-compatible quoting. Unrecognized escapes are
    # kept verbatim; they cannot accidentally match a normalization rule.
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return raw


def mapped(value: str, mappings: tuple[tuple[str, str], ...]) -> str:
    for old, new in sorted(mappings, key=lambda pair: -len(pair[0])):
        if value == old or value.startswith(old + "/"):
            return new + value[len(old):]
    return value


def receiver_type(node: Node) -> str:
    receiver = node.child_by_field_name("receiver")
    if receiver is None:
        return ""
    types = [n for n in walk(receiver) if n.type == "type_identifier"]
    return types[0].text.decode() if types else receiver.text.decode()


def declarations(node: Node):
    for child in node.named_children:
        if child.type in ("function_declaration", "method_declaration", "const_declaration"):
            yield child
        elif child.type == "type_declaration":
            yield from (n for n in child.named_children if n.type in ("type_spec", "type_alias"))
        elif child.type == "var_declaration":
            for entry in child.named_children:
                if entry.type == "var_spec":
                    yield entry
                elif entry.type == "var_spec_list":
                    yield from (n for n in entry.named_children if n.type == "var_spec")


def declaration_name(node: Node) -> str:
    if node.type == "const_declaration":
        return ",".join(n.text.decode() for spec in node.named_children
                        if spec.type == "const_spec" for n in spec.children_by_field_name("name"))
    names = node.children_by_field_name("name")
    return ",".join(n.text.decode() for n in names)


def parse_sources(sources: list[Source], ref: str, imports=(), packages=()) -> Snapshot:
    parser = Parser(Language(tree_sitter_go.language()))
    items, files, diagnostics = [], [], []
    initialization = defaultdict(list)
    for source in sorted(sources, key=lambda s: s.logical_path):
        tree = parser.parse(source.content)
        root = tree.root_node
        failures = [n for n in walk(root) if n.type == "ERROR" or n.is_missing]
        for node in failures:
            diagnostics.append({"file": source.logical_path, "line": node.start_point.row + 1,
                                "column": node.start_point.column + 1,
                                "kind": "missing" if node.is_missing else "syntax_error", "node": node.type})
        package_nodes = [n for n in root.named_children if n.type == "package_clause"]
        if failures or len(package_nodes) != 1:
            if not failures:
                diagnostics.append({"file": source.logical_path, "kind": "missing_package"})
            continue
        package_name = package_nodes[0].named_children[0].text.decode()
        package_name = dict(packages).get(package_name, package_name)
        package = str(Path(source.logical_path).parent) + ":" + package_name
        bindings = []
        for node in walk(root):
            if node.type == "import_spec":
                alias = node.child_by_field_name("name")
                path = node.child_by_field_name("path")
                bindings.append((alias.text.decode() if alias else None, mapped(import_path(path), imports)))
        bindings = tuple(sorted(bindings, key=repr))
        comment_nodes = [n for n in walk(root) if n.type == "comment"]
        comments = [n.text.decode() for n in comment_nodes]
        # Compiler/build/line directives and cgo preambles must not be erased.
        decl_nodes = list(declarations(root))
        def directive_context(comment):
            following = next((d for d in decl_nodes if d.start_byte >= comment.end_byte), None)
            owner = (following.type, receiver_type(following), declaration_name(following)) if following else None
            return (comment.text.decode(), comment.start_point.row + 1, comment.start_point.column + 1, owner)
        constraints = tuple(directive_context(n) for n in comment_nodes
                            if n.text.decode().startswith(("//go:", "// +build", "//line ", "/*line ")))
        # Locations deliberately remain: identical directive text reassigned to
        # a different declaration/statement must not become a false clean result.
        if any(path == "C" for _, path in bindings):
            constraints += tuple(comments)
        # File names can select GOOS/GOARCH/test build contexts. Keep the suffix
        # conservatively, without guessing a target or running go list.
        stem_parts = Path(source.logical_path).stem.split("_")
        suffix = "_".join(stem_parts[1:]) if len(stem_parts) > 1 else ""
        constraints += (("filename_suffix", suffix),)
        file_entry = {"path": source.logical_path, "source_path": source.path,
                      "ref": source.ref, "sha256": hashlib.sha256(source.content).hexdigest(),
                      "package": package, "imports": bindings, "constraints": constraints}
        files.append(file_entry)
        for node in declarations(root):
            name = declaration_name(node)
            kind = node.type
            receiver = receiver_type(node)
            identity = (receiver + "." if receiver else "") + name
            key = f"{package}/{kind}/{identity}"
            body = node.child_by_field_name("body")
            item = Declaration(key, kind, identity, package, source.logical_path,
                               node.start_point.row + 1, node.end_point.row + 1,
                               digest(canonical(node, True)), digest(canonical(body)) if body else "",
                               digest(canonical(node)), bindings, constraints, source.ref)
            items.append(item)
            if kind == "var_spec" or (kind == "function_declaration" and name == "init"):
                initialization[package].append((item.key, item.ast, item.file))
    return Snapshot(ref, tuple(items), tuple(files),
                    tuple((p, tuple(v)) for p, v in sorted(initialization.items())), tuple(diagnostics))


def compare(left: Snapshot, right: Snapshot) -> dict:
    groups = [defaultdict(list), defaultdict(list)]
    for side, snapshot in enumerate((left, right)):
        for item in snapshot.declarations:
            groups[side][item.key].append(item)
    results = []
    for key in sorted(set(groups[0]) | set(groups[1])):
        old, new = groups[0][key], groups[1][key]
        entry = {"key": key, "left": [asdict(d) for d in old], "right": [asdict(d) for d in new]}
        if len(old) > 1 or len(new) > 1:
            entry["status"] = "ambiguous"
        elif not old:
            entry["status"] = "added"
        elif not new:
            entry["status"] = "removed"
        else:
            a, b = old[0], new[0]
            changes = []
            if a.signature != b.signature:
                changes.append("signature")
            if a.body != b.body:
                changes.append("body")
            if a.ast != b.ast and not changes:
                changes.append("declaration")
            if a.imports != b.imports:
                changes.append("import_context")
            if a.constraints != b.constraints:
                changes.append("build_or_compiler_context")
            entry["changes"] = changes
            entry["moved"] = a.file != b.file
            entry["ast_equal"] = a.ast == b.ast
            entry["status"] = "modified" if changes else "moved" if a.file != b.file else "unchanged"
        results.append(entry)
    order = []
    old_order, new_order = dict(left.initialization), dict(right.initialization)
    for package in sorted(set(old_order) | set(new_order)):
        a, b = old_order.get(package, ()), new_order.get(package, ())
        # Even a move that preserves lexical order warrants review: Go package
        # initialization depends on dependencies/build-file selection as well.
        if a != b:
            order.append({"package": package, "status": "initialization_review_required", "left": a, "right": b})
    context_changes = []
    old_files = {f["path"]: f for f in left.files}
    new_files = {f["path"]: f for f in right.files}
    for path in sorted(set(old_files) | set(new_files)):
        a, b = old_files.get(path), new_files.get(path)
        fields = ("package", "imports", "constraints")
        if a is None or b is None or any(a[f] != b[f] for f in fields):
            context_changes.append({"file": path, "left": a, "right": b})
    diagnostics = [{"side": side, **d} for side, s in (("left", left), ("right", right)) for d in s.diagnostics]
    return {"schema_version": 1, "mode": "go_declarations", "parser": {"tree_sitter": version("tree-sitter"),
            "tree_sitter_go": version("tree-sitter-go")}, "semantic_equivalence": "not_established",
            "valid": not diagnostics, "left": {"ref": left.ref, "files": left.files},
            "right": {"ref": right.ref, "files": right.files}, "diagnostics": diagnostics,
            "summary": dict(sorted(Counter(r["status"] for r in results).items())),
            "initialization": order, "file_context": context_changes, "declarations": results}


def load_sources(root: Path | None, manifest: Path | None, ref: str, include_tests: bool) -> tuple[list[Source], str]:
    if manifest:
        data = json.loads(manifest.read_text())
        root = Path(data["root"])
        if not root.is_absolute():
            root = manifest.parent / root
        ref = data.get("ref", ref)
        entries = data["files"]
    else:
        entries = [{"path": str(p.relative_to(root)), "logical_path": str(p.relative_to(root))}
                   for p in sorted(root.rglob("*.go")) if not p.is_symlink() and ".git" not in p.parts]
    sources, seen = [], set()
    for entry in entries:
        logical = entry["logical_path"]
        if not include_tests and logical.endswith("_test.go"):
            continue
        if logical in seen:
            raise ValueError(f"duplicate logical path: {logical}")
        seen.add(logical)
        path = root / entry["path"]
        content = path.read_bytes()
        content.decode("utf-8")
        if "sha256" in entry and hashlib.sha256(content).hexdigest() != entry["sha256"]:
            raise ValueError(f"source hash mismatch: {path}")
        sources.append(Source(str(path), logical, content, entry.get("ref", ref)))
    if not sources:
        raise ValueError("no Go source files selected")
    return sources, ref


def markdown(report: dict) -> str:
    lines = ["# Tree-sitter Go declaration comparison", "", "AST evidence only; semantic equivalence is not established.",
             "", f"- Left: `{report['left']['ref']}`", f"- Right: `{report['right']['ref']}`",
             f"- Parse valid: {report['valid']}", f"- Counts: {report['summary']}",
             f"- Initialization reviews: {len(report['initialization'])}", "",
             "| Declaration | Status | Changes | Left | Right |", "| --- | --- | --- | --- | --- |"]
    for row in report["declarations"]:
        if row["status"] == "unchanged":
            continue
        locations = [", ".join(f"{d['file']}:{d['line']}" for d in row[s]) for s in ("left", "right")]
        lines.append("| " + " | ".join(str(v).replace("|", "\\|").replace("\n", " ") for v in
                                      (row["key"], row["status"], ", ".join(row.get("changes", [])), *locations)) + " |")
    if report["diagnostics"]:
        lines += ["", "## Parse failures", "", "```json", json.dumps(report["diagnostics"], indent=2), "```"]
    if report["initialization"]:
        lines += ["", "## Initialization review", "", "Changed variable/init sequence or placement; inspect JSON for exact order."]
        lines += [f"- `{r['package']}`" for r in report["initialization"]]
    return "\n".join(lines) + "\n"


def mapping(value: str) -> tuple[str, str]:
    parts = value.split("=", 1)
    if len(parts) != 2 or not all(parts):
        raise argparse.ArgumentTypeError("mapping must be nonempty OLD=NEW")
    return tuple(parts)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    for side in ("left", "right"):
        group = parser.add_mutually_exclusive_group(required=True)
        group.add_argument(f"--{side}", type=Path, help="root of Go files")
        group.add_argument(f"--{side}-manifest", type=Path, help="explicit source inventory")
        parser.add_argument(f"--{side}-ref", default="unspecified")
    parser.add_argument("--import-map", type=mapping, action="append", default=[])
    parser.add_argument("--package-map", type=mapping, action="append", default=[])
    parser.add_argument("--include-tests", action="store_true")
    parser.add_argument("--json", type=Path, required=True)
    parser.add_argument("--markdown", type=Path)
    args = parser.parse_args(argv)
    snapshots = []
    try:
        for side in ("left", "right"):
            sources, ref = load_sources(getattr(args, side), getattr(args, side + "_manifest"),
                                        getattr(args, side + "_ref"), args.include_tests)
            # Mapping applies to both sides so an already-normalized target is stable.
            snapshots.append(parse_sources(sources, ref, tuple(args.import_map), tuple(args.package_map)))
    except (ValueError, OSError, KeyError) as error:
        parser.error(str(error))
    report = compare(*snapshots)
    report["normalization"] = {"import_map": args.import_map, "package_map": args.package_map,
                               "include_tests": args.include_tests, "comments": "ignore ordinary; retain directives/cgo"}
    args.json.parent.mkdir(parents=True, exist_ok=True)
    args.json.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    if args.markdown:
        args.markdown.parent.mkdir(parents=True, exist_ok=True)
        args.markdown.write_text(markdown(report))
    print(json.dumps({"valid": report["valid"], "summary": report["summary"],
                      "initialization_reviews": len(report["initialization"])}))
    if not report["valid"] or report["summary"].get("ambiguous"):
        return 2
    return int(any(r["status"] != "unchanged" for r in report["declarations"]) or bool(report["initialization"]) or bool(report["file_context"]))


if __name__ == "__main__":
    sys.exit(main())
