import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from compare import Source, compare, load_sources, parse_sources


def snapshot(files, **kwargs):
    return parse_sources([Source(path, path, text.encode(), "test") for path, text in files.items()], "test", **kwargs)


class DeclarationContracts(unittest.TestCase):
    def test_declaration_changes(self):
        cases = [
            ("format", "func F(x int) int{return x+1}", "// ordinary\nfunc F( x int ) int { return x + 1 }", "unchanged", []),
            ("operator", "func F(x int) int{return x+1}", "func F(x int) int{return x-1}", "modified", ["body"]),
            ("literal", 'func F() string{return "left"}', 'func F() string{return "right"}', "modified", ["body"]),
            ("signature", "func F(x int){}", "func F(x string){}", "modified", ["signature"]),
            ("receiver", "func (x T) F(){}", "func (x *T) F(){}", "modified", ["signature"]),
            ("generic", "func F[T any](x T){}", "func F[T comparable](x T){}", "modified", ["signature"]),
            ("interface", "type T interface { F() int }", "type T interface { F() string }", "modified", ["signature"]),
            ("directive", "//go:noinline\nfunc F(){}", "func F(){}", "modified", ["build_or_compiler_context"]),
            ("build", "//go:build linux\n\npackage p\nfunc F(){}", "//go:build windows\n\npackage p\nfunc F(){}", "modified", ["build_or_compiler_context"]),
        ]
        for name, old, new, status, changes in cases:
            with self.subTest(name=name):
                old = old if "package p" in old else "package p\n" + old + "\n"
                new = new if "package p" in new else "package p\n" + new + "\n"
                report = compare(snapshot({"a.go": old}), snapshot({"a.go": new}))
                self.assertTrue(report["valid"], report["diagnostics"])
                self.assertEqual(len(report["declarations"]), 1)
                row = report["declarations"][0]
                self.assertEqual(row["status"], status)
                self.assertEqual(row["changes"], changes)

    def test_file_split_and_type_group_are_location_only(self):
        a = {"a.go": "package p\ntype (\nA int\nB string\n)\nfunc F(){}\nfunc G(){}\n"}
        b = {"a.go": "package p\ntype A int\nfunc F(){}\n", "b.go": "package p\ntype B string\nfunc G(){}\n"}
        report = compare(snapshot(a), snapshot(b))
        self.assertEqual(report["summary"], {"moved": 2, "unchanged": 2})
        self.assertFalse(report["initialization"])

    def test_import_mapping_is_explicit_and_not_literal_rewriting(self):
        a = 'package p\nimport h "old/core"\nfunc F(){h.Go("old/core")}\n'
        b = 'package p\nimport h "new/core"\nfunc F(){h.Go("old/core")}\n'
        kwargs = {"imports": (("old", "new"),)}
        report = compare(snapshot({"a.go": a}, **kwargs), snapshot({"a.go": b}, **kwargs))
        self.assertEqual(report["summary"], {"unchanged": 1})
        literal = b.replace('Go("old/core")', 'Go("new/core")')
        report = compare(snapshot({"a.go": a}, **kwargs), snapshot({"a.go": literal}, **kwargs))
        self.assertEqual(report["declarations"][0]["changes"], ["body"])
        alias = b.replace('h "new/core"', 'other "new/core"')
        report = compare(snapshot({"a.go": b}), snapshot({"a.go": alias}))
        self.assertIn("import_context", report["declarations"][0]["changes"])

    def test_package_rename_is_explicit(self):
        a, b = "package before\nfunc F(){}\n", "package after\nfunc F(){}\n"
        self.assertEqual(compare(snapshot({"a.go": a}), snapshot({"a.go": b}))["summary"], {"added": 1, "removed": 1})
        kwargs = {"packages": (("before", "after"),)}
        self.assertEqual(compare(snapshot({"a.go": a}, **kwargs), snapshot({"a.go": b}, **kwargs))["summary"], {"unchanged": 1})

    def test_parse_error_and_missing_node_never_pass(self):
        for code in ("package p\nfunc F( {", "package p\nfunc F(){return (1+)}", "package p\nfunc F(){"):
            with self.subTest(code=code):
                report = compare(snapshot({"a.go": code}), snapshot({"a.go": code}))
                self.assertFalse(report["valid"])
                self.assertTrue(report["diagnostics"])

    def test_duplicate_symbols_are_ambiguous_not_arbitrarily_matched(self):
        files = {"a.go": "package p\nfunc F(){}\n", "b.go": "package p\nfunc F(){}\n"}
        report = compare(snapshot(files), snapshot(files))
        self.assertEqual(report["summary"], {"ambiguous": 1})

    def test_variable_and_init_order_need_review(self):
        for a, b in [
            ("var A=f()\nvar B=g()\n", "var B=g()\nvar A=f()\n"),
            ("func init(){f()}\nfunc init(){g()}\n", "func init(){g()}\nfunc init(){f()}\n"),
        ]:
            with self.subTest(code=a):
                report = compare(snapshot({"a.go": "package p\n" + a}), snapshot({"a.go": "package p\n" + b}))
                self.assertEqual(len(report["initialization"]), 1)
        moved = compare(snapshot({"a.go": "package p\nvar A=f()\n"}), snapshot({"b.go": "package p\nvar A=f()\n"}))
        self.assertEqual(moved["summary"], {"moved": 1})
        self.assertTrue(moved["initialization"])

    def test_cgo_and_build_constraints_are_not_erased(self):
        a = 'package p\n/* #define VALUE 1 */\nimport "C"\nfunc F(){}\n'
        b = a.replace("VALUE 1", "VALUE 2")
        report = compare(snapshot({"a.go": a}), snapshot({"a.go": b}))
        self.assertIn("build_or_compiler_context", report["declarations"][0]["changes"])
        report = compare(snapshot({"a_linux.go": "package p\nfunc F(){}\n"}), snapshot({"a_windows.go": "package p\nfunc F(){}\n"}))
        self.assertIn("build_or_compiler_context", report["declarations"][0]["changes"])

    def test_directive_reassociation_and_local_var_scope(self):
        a = "package p\n//go:noinline\nfunc F(){}\nfunc G(){}\n"
        b = "package p\nfunc F(){}\n//go:noinline\nfunc G(){}\n"
        report = compare(snapshot({"a.go": a}), snapshot({"a.go": b}))
        self.assertNotEqual(report["summary"], {"unchanged": 2})
        self.assertTrue(report["file_context"])
        reordered = "package p\n//go:noinline\nfunc G(){}\nfunc F(){}\n"
        report = compare(snapshot({"a.go": a}), snapshot({"a.go": reordered}))
        self.assertNotEqual(report["summary"], {"unchanged": 2})
        self.assertTrue(report["file_context"])
        source = "package p\nvar F = func(){ var local = 1; _ = local }\n"
        parsed = snapshot({"a.go": source})
        self.assertEqual([d.name for d in parsed.declarations], ["F"])
        self.assertEqual(len(dict(parsed.initialization)[".:p"]), 1)

    def test_const_order_is_not_discarded(self):
        a = "package p\nconst (\nA = iota\nB\n)\n"
        b = "package p\nconst (\nB = iota\nA\n)\n"
        report = compare(snapshot({"a.go": a}), snapshot({"a.go": b}))
        self.assertNotEqual(report["summary"], {"unchanged": 1})


class SourceBoundary(unittest.TestCase):
    def test_manifest_integrity_and_cli_exit_contract(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "old.go").write_text("package p\nfunc F(){}\n")
            manifest = root / "source.json"
            manifest.write_text(json.dumps({"root": ".", "ref": "pinned", "files": [{"path": "old.go", "logical_path": "pkg/new.go"}]}))
            sources, ref = load_sources(None, manifest, "", False)
            self.assertEqual(ref, "pinned")
            self.assertEqual(sources[0].logical_path, "pkg/new.go")
            report = root / "report.json"
            cmd = [sys.executable, str(Path(__file__).with_name("compare.py")), "--left-manifest", str(manifest),
                   "--right-manifest", str(manifest), "--json", str(report)]
            run = subprocess.run(cmd, capture_output=True, text=True)
            self.assertEqual(run.returncode, 0, run.stderr)
            self.assertEqual(json.loads(report.read_text())["summary"], {"unchanged": 1})
            data = json.loads(manifest.read_text())
            data["files"][0]["sha256"] = "incorrect"
            manifest.write_text(json.dumps(data))
            with self.assertRaisesRegex(ValueError, "hash mismatch"):
                load_sources(None, manifest, "", False)
            (root / "old.go").write_text("package p\nfunc F(")
            data["files"][0].pop("sha256")
            manifest.write_text(json.dumps(data))
            self.assertEqual(subprocess.run(cmd, capture_output=True).returncode, 2)


if __name__ == "__main__":
    unittest.main()
