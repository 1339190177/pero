#!/usr/bin/env python3
"""Build the explicit local TS5 grammar extension from a verified official archive.

No project source is rewritten. Only export_statement gains a production for
export type (* | namespace_export) _from_clause _semicolon.
"""
from __future__ import annotations
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tarfile
import urllib.request

VERSION = "0.23.2+pero.export-type-star.1"
URL = "https://registry.npmjs.org/tree-sitter-typescript/-/tree-sitter-typescript-0.23.2.tgz"
ARCHIVE_SHA256 = "0fdf63c35930a75885145d75ee63cb879da2d49db9bfe454bd8e60b08ba778a1"
GRAMMAR_SHA256 = "99dc4b95424dff47c89acb50d5d3f25b081b07eaff43994b7881dccd4186b214"
CLI_VERSION = "0.25.10"
BUILD = Path(__file__).resolve().parent / ".grammar"


def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def patched_grammar(raw: bytes) -> bytes:
    if digest(raw) != GRAMMAR_SHA256:
        raise ValueError("official grammar hash differs; review the extension before upgrading")
    grammar = json.loads(raw)
    symbol = lambda name: {"type": "SYMBOL", "name": name}
    token = lambda value: {"type": "STRING", "value": value}
    # Require a module source for both star forms; do not broaden named exports.
    grammar["rules"]["export_statement"]["members"].append({"type": "SEQ", "members": [
        token("export"), token("type"),
        {"type": "CHOICE", "members": [token("*"), symbol("namespace_export")]},
        symbol("_from_clause"), symbol("_semicolon")]})
    return (json.dumps(grammar, indent=2) + "\n").encode()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cli", default="tree-sitter")
    parser.add_argument("--cc", default="cc")
    parser.add_argument("--archive", type=Path, help="use a previously downloaded official tarball")
    args = parser.parse_args()
    if sys.platform not in ("linux", "darwin"):
        parser.error("local shared-library build supports Linux/macOS; use WSL on Windows")
    cli_version = subprocess.check_output([args.cli, "--version"], text=True).strip()
    if not cli_version.startswith(f"tree-sitter {CLI_VERSION} "):
        parser.error(f"requires official tree-sitter CLI {CLI_VERSION}; found {cli_version}")
    BUILD.mkdir(exist_ok=True)
    archive = args.archive or BUILD / "upstream.tgz"
    if not args.archive:
        archive.write_bytes(urllib.request.urlopen(URL, timeout=60).read())
    if digest(archive.read_bytes()) != ARCHIVE_SHA256:
        parser.error("official archive SHA256 mismatch")
    # Extract only required regular files under fixed destinations, not tar paths.
    names = ["LICENSE", "common/scanner.h", "typescript/src/scanner.c", "typescript/src/grammar.json"]
    with tarfile.open(archive) as bundle:
        for name in names:
            member = bundle.getmember("package/" + name)
            if not member.isfile():
                parser.error(f"not a regular source file: {name}")
            destination = BUILD / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(bundle.extractfile(member).read())
    grammar_path = BUILD / "typescript/src/grammar.json"
    grammar_path.write_bytes(patched_grammar(grammar_path.read_bytes()))
    subprocess.run([str(Path(args.cli).resolve()) if "/" in args.cli else args.cli,
                    "generate", "--abi", "14", "src/grammar.json"], cwd=BUILD / "typescript", check=True)
    library = BUILD / "typescript.so"
    subprocess.run([args.cc, "-O2", "-fPIC", "-shared", "-I", str(BUILD / "typescript/src"),
                    str(BUILD / "typescript/src/parser.c"), str(BUILD / "typescript/src/scanner.c"),
                    "-o", str(library)], check=True)
    receipt = {"version": VERSION, "upstream_url": URL, "archive_sha256": ARCHIVE_SHA256,
               "upstream_grammar_sha256": GRAMMAR_SHA256, "patched_grammar_sha256": digest(grammar_path.read_bytes()),
               "generator": cli_version, "abi": 14, "library_sha256": digest(library.read_bytes()),
               "compiler": subprocess.check_output([args.cc, "--version"], text=True).splitlines()[0]}
    (BUILD / "receipt.json").write_text(json.dumps(receipt, indent=2) + "\n")
    print(json.dumps(receipt, indent=2))


if __name__ == "__main__":
    main()
