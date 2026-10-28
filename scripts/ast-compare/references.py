#!/usr/bin/env python3
"""Explicit official TypeScript/Go AST references for human contract review."""
from __future__ import annotations

import argparse
from collections import Counter
from dataclasses import asdict, dataclass
import hashlib
from importlib.metadata import version
import json
from pathlib import Path
import sys

from tree_sitter import Language, Node, Parser
import tree_sitter_go
import tree_sitter_typescript

from compare import receiver_type, walk


@dataclass(frozen=True)
class Fact:
    kind: str
    text: str
    line: int
    end_line: int
    target: str
    arguments: str
    controls: tuple[str, ...]
    truncated: bool
    raw_text: str
    start_byte: int
    end_byte: int
    nested_callable: tuple[str, ...]
    write_scope: str


LANGUAGES = {"go": Language(tree_sitter_go.language()),
             "typescript": Language(tree_sitter_typescript.language_typescript())}
CALLABLES = {"function_declaration", "method_declaration", "method_definition", "generator_function_declaration"}
FUNCTION_VALUES = {"arrow_function", "function_expression", "generator_function"}
CONTROLS = {"if_statement", "switch_statement", "expression_switch_statement", "type_switch_statement",
            "switch_case", "switch_default", "expression_case", "type_case", "default_case", "select_statement",
            "communication_case", "catch_clause", "ternary_expression", "conditional_expression"}
LOOPS = {"for_statement", "for_in_statement", "while_statement", "do_statement"}
WRITES = {"assignment_statement", "short_var_declaration", "assignment_expression", "augmented_assignment_expression",
          "update_expression", "inc_statement", "dec_statement", "variable_declarator", "var_spec"}
STRINGS = {"string", "template_string", "interpreted_string_literal", "raw_string_literal"}


def text(node: Node | None) -> str:
    return node.text.decode("utf-8") if node else ""


def compact(value: str) -> str:
    # Presentation only. Raw source bytes and spans identify the actual evidence.
    return " ".join(value.split())


def callable_name(node: Node, language: str) -> str | None:
    if node.type in CALLABLES:
        name = node.child_by_field_name("name")
    elif node.type in FUNCTION_VALUES and node.parent and node.parent.type in ("variable_declarator", "public_field_definition", "pair"):
        parent = node.parent
        name = parent.child_by_field_name("name") or parent.child_by_field_name("key")
    else:
        return None
    if name is None:
        return None
    symbol = text(name)
    if language == "go":
        receiver = receiver_type(node)
        return f"{receiver}.{symbol}" if receiver else symbol
    parent = node.parent
    owners = []
    while parent:
        if parent.type in ("class_declaration", "abstract_class_declaration", "class", "function_declaration"):
            owner = parent.child_by_field_name("name")
            if owner:
                owners.append(text(owner))
        parent = parent.parent
    return ".".join([*reversed(owners), symbol])


def extract_facts(node: Node) -> tuple[Fact, ...]:
    result = []
    for current in walk(node):
        kind, target, arguments = "", "", ""
        display = current
        if current.type in CONTROLS:
            kind = "guard"
            display = current.child_by_field_name("condition") or current.child_by_field_name("value") or current
        elif current.type in LOOPS:
            kind = "loop"
            display = current.child_by_field_name("condition") or current.child_by_field_name("initializer") or current
        elif current.type == "return_statement":
            kind = "return"
        elif current.type == "throw_statement":
            kind = "throw"
        elif current.type in ("call_expression", "new_expression"):
            kind = "call"
            target = compact(text(current.child_by_field_name("function") or current.child_by_field_name("constructor")))
            arguments = compact(text(current.child_by_field_name("arguments")))
        elif current.type in WRITES:
            kind = "write"
            lhs = current.child_by_field_name("left") or current.child_by_field_name("name") or current.child_by_field_name("argument")
            target = compact(text(lhs))
        elif current.type in STRINGS:
            kind = "string"
        if not kind:
            continue
        controls, owners = [], []
        parent = current.parent
        while parent and parent != node:
            if parent.type in CALLABLES | FUNCTION_VALUES | {"func_literal"}:
                owners.append(f"{parent.type}:{parent.start_point.row + 1}")
            if parent.type in CONTROLS | LOOPS:
                controls.append(f"{parent.type}:{parent.start_point.row + 1}")
            parent = parent.parent
        value = compact(text(display))
        scope = ""
        if kind == "write":
            lhs = current.child_by_field_name("left") or current.child_by_field_name("name") or current.child_by_field_name("argument")
            member = lhs and any(n.type in ("selector_expression", "member_expression", "index_expression", "subscript_expression") for n in walk(lhs))
            scope = "member_or_index_target" if member else "local_declaration" if current.type in ("variable_declarator", "var_spec", "short_var_declaration") else "identifier_or_unknown_target"
        result.append(Fact(kind, value[:500], current.start_point.row + 1, current.end_point.row + 1,
                           target[:200], arguments[:500], tuple(reversed(controls)),
                           len(value) > 500 or len(target) > 200 or len(arguments) > 500,
                           text(current), current.start_byte, current.end_byte, tuple(reversed(owners)), scope))
    return tuple(result)


def inspect_selector(root: Path, selector: dict, ref: str) -> dict:
    language = selector["language"]
    if language not in LANGUAGES:
        return {"status": "unsupported_language", "language": language, "ref": ref, **selector}
    path = root / selector["path"]
    source = path.read_bytes()
    source.decode("utf-8")
    source_hash = hashlib.sha256(source).hexdigest()
    if selector.get("sha256") and selector["sha256"] != source_hash:
        return {"status": "source_hash_mismatch", "ref": ref, **selector, "sha256": source_hash}
    tree = Parser(LANGUAGES[language]).parse(source)
    failures = [{"node": n.type, "line": n.start_point.row + 1, "column": n.start_point.column + 1,
                 "missing": n.is_missing} for n in walk(tree.root_node) if n.type == "ERROR" or n.is_missing]
    base = {"path": selector["path"], "language": language, "symbol": selector["symbol"],
            "ref": ref, "sha256": source_hash, "hash_verified": bool(selector.get("sha256"))}
    if failures:
        return {**base, "status": "parse_error", "diagnostics": failures}
    candidates = [n for n in walk(tree.root_node) if callable_name(n, language) == selector["symbol"]
                  and ("line" not in selector or n.start_point.row + 1 == selector["line"])
                  and ("kind" not in selector or n.type == selector["kind"])]
    if len(candidates) != 1:
        return {**base, "status": "missing_symbol" if not candidates else "ambiguous_symbol",
                "candidates": [{"kind": n.type, "line": n.start_point.row + 1} for n in candidates]}
    node = candidates[0]
    body = node.child_by_field_name("body")
    signature = source[node.start_byte:body.start_byte if body else node.end_byte].decode()
    facts = [asdict(f) for f in extract_facts(node)]
    return {**base, "status": "inspected", "kind": node.type, "line": node.start_point.row + 1,
            "end_line": node.end_point.row + 1, "signature": compact(signature)[:1000],
            "signature_truncated": len(compact(signature)) > 1000,
            "counts": dict(sorted(Counter(f["kind"] for f in facts).items())), "facts": facts}


def compare_references(mapping: dict, left_root: Path, right_root: Path) -> dict:
    if not mapping["pairs"]:
        raise ValueError("at least one explicit pair is required")
    pairs = []
    seen = set()
    for pair in mapping["pairs"]:
        if pair["id"] in seen:
            raise ValueError(f"duplicate pair id: {pair['id']}")
        seen.add(pair["id"])
        sides = {}
        for side, root in (("left", left_root), ("right", right_root)):
            try:
                sides[side] = inspect_selector(root, pair[side], mapping[f"{side}_ref"])
            except (OSError, UnicodeError) as error:
                sides[side] = {"status": "source_unavailable", **pair[side], "error": str(error)}
        available = all(s["status"] == "inspected" for s in sides.values())
        landmarks = []
        for landmark in pair.get("landmarks", []):
            observations = {}
            for side in ("left", "right"):
                terms = landmark.get(side + "_terms", [])
                facts = sides[side].get("facts", [])
                matches = [{"term": term, "evidence": [f for f in facts if term in f["raw_text"]]}
                           for term in terms]
                observations[side] = {"status": "unavailable" if sides[side]["status"] != "inspected" else
                                      "observed" if terms and all(m["evidence"] for m in matches) else "not_observed",
                                      "matches": matches}
            landmarks.append({"id": landmark["id"], "note": landmark.get("note", ""), **observations})
        fact_deltas = []
        if available:
            # Exact lexical facts only, not translated control-flow equivalence.
            for kind in sorted(set(sides["left"]["counts"]) | set(sides["right"]["counts"])):
                lists = [[f for f in sides[s]["facts"] if f["kind"] == kind] for s in ("left", "right")]
                keys = [set(f["raw_text"] for f in fs) for fs in lists]
                fact_deltas.append({"kind": kind, "left_count": len(lists[0]), "right_count": len(lists[1]),
                                    "left_only": sorted(keys[0] - keys[1]), "right_only": sorted(keys[1] - keys[0])})
        pairs.append({"id": pair["id"], "label": pair.get("label", pair["id"]),
                      "status": "cross_language_review_required" if available else "unavailable",
                      "notes": pair.get("notes", ""), **sides, "landmarks": landmarks, "lexical_fact_deltas": fact_deltas})
    return {"schema_version": 1, "mode": "official_references", "semantic_equivalence": "not_established",
            "parser": {name: version(name) for name in ("tree-sitter", "tree-sitter-go", "tree-sitter-typescript")},
            "left_ref": mapping["left_ref"], "right_ref": mapping["right_ref"],
            "ref_basis": "declared_by_mapping; per-file hashes verify actual bytes, not Git checkout identity",
            "coverage": {"selected_pairs": len(pairs), "left_files": len({p["left"]["path"] for p in pairs}),
                         "right_files": len({p["right"]["path"] for p in pairs}), "scope": "selected callable mappings only"},
            "mapping_sha256": hashlib.sha256(json.dumps(mapping, sort_keys=True).encode()).hexdigest(),
            "summary": dict(sorted(Counter(p["status"] for p in pairs).items())), "pairs": pairs}


def markdown(report: dict) -> str:
    def esc(value):
        return str(value).replace("|", "\\|").replace("\n", " ")
    lines = ["# Official DSH → Pero AST reference review", "", "Tree-sitter structure evidence; not semantic equivalence or a passing behavior test.",
             "", f"- Official: `{report['left_ref']}`", f"- Pero: `{report['right_ref']}`", f"- Summary: {report['summary']}", f"- Coverage: {report['coverage']}", "",
             "Exact lexical differences reflect language, adapter and composition differences as well as possible defects. Unobserved landmarks require review, not automatic repair."]
    for pair in report["pairs"]:
        lines += ["", f"## {pair['label']}", "", f"Status: **{pair['status']}**", ""]
        if pair["notes"]:
            lines += [pair["notes"], ""]
        for side, label in (("left", "Official"), ("right", "Pero")):
            item = pair[side]
            lines += [f"- {label}: `{item.get('path')}:{item.get('line', '?')}` · `{item.get('symbol')}` · {item['status']}"]
            if item["status"] != "inspected" and item.get("diagnostics"):
                lines += [f"  - Parser diagnostics: {item['diagnostics']}"]
            if item["status"] == "inspected":
                lines += [f"  - Signature: `{esc(item['signature'])}`", f"  - AST facts: {item['counts']}"]
        if pair["landmarks"]:
            lines += ["", "| Contract landmark | Official observation | Pero observation |", "| --- | --- | --- |"]
            for mark in pair["landmarks"]:
                cells = [mark["id"]]
                for side in ("left", "right"):
                    observation = mark[side]
                    evidence = [f"{m['term']}@{','.join(str(f['line']) for f in m['evidence'][:3]) or 'missing'}" for m in observation["matches"]]
                    cells.append(observation["status"] + ": " + "; ".join(evidence))
                lines.append("| " + " | ".join(esc(c) for c in cells) + " |")
        if pair["lexical_fact_deltas"]:
            lines += ["", "| Structural facts | Official / Pero count | Official-only examples | Pero-only examples |", "| --- | --- | --- | --- |"]
            for delta in pair["lexical_fact_deltas"]:
                left = "; ".join(delta["left_only"][:3])[:300]
                right = "; ".join(delta["right_only"][:3])[:300]
                lines.append("| " + " | ".join(esc(c) for c in (delta["kind"], f"{delta['left_count']} / {delta['right_count']}", left, right)) + " |")
    return "\n".join(lines) + "\n"


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mapping", type=Path, required=True)
    parser.add_argument("--left-root", type=Path, required=True)
    parser.add_argument("--right-root", type=Path, required=True)
    parser.add_argument("--json", type=Path, required=True)
    parser.add_argument("--markdown", type=Path)
    args = parser.parse_args(argv)
    try:
        report = compare_references(json.loads(args.mapping.read_text()), args.left_root, args.right_root)
    except (ValueError, KeyError, OSError) as error:
        parser.error(str(error))
    args.json.parent.mkdir(parents=True, exist_ok=True)
    args.json.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    if args.markdown:
        args.markdown.parent.mkdir(parents=True, exist_ok=True)
        args.markdown.write_text(markdown(report))
    print(json.dumps(report["summary"]))
    # 0 means evidence extraction succeeded, never that the implementations match.
    return 2 if report["summary"].get("unavailable") else 0


if __name__ == "__main__":
    sys.exit(main())
