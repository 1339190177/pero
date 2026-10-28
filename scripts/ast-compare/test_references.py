import json
from pathlib import Path
import tempfile
import unittest

from references import compare_references, inspect_selector, markdown


class OfficialReferenceContracts(unittest.TestCase):
    def test_function_mapping_extracts_control_calls_writes_and_errors(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "left.ts").write_text('''export class Agent {
  async run(value: number) {
    this.stopped = false;
    if (value < 0) { throw new Error("negative"); }
    while (value > 0) { value--; await this.step(value); }
    return "done";
  }
}
''')
            (root / "right.go").write_text('''package p
func (a *Agent) run(value int) string {
 a.stopped = false
 if value < 0 { return "negative" }
 for value > 0 { value--; a.step(value) }
 return "done"
}
''')
            mapping = {"left_ref": "official-pin", "right_ref": "candidate-pin", "pairs": [{
                "id": "run", "left": {"path": "left.ts", "language": "typescript", "symbol": "Agent.run"},
                "right": {"path": "right.go", "language": "go", "symbol": "Agent.run"},
                "notes": "throw versus returned error is intentional language adaptation",
                "landmarks": [{"id": "negative path", "left_terms": ["negative"], "right_terms": ["negative"]},
                              {"id": "unknown path", "left_terms": ["not-present"], "right_terms": ["not-present"]}]}]}
            report = compare_references(mapping, root, root)
            self.assertEqual(report["semantic_equivalence"], "not_established")
            self.assertEqual(report["summary"], {"cross_language_review_required": 1})
            pair = report["pairs"][0]
            self.assertEqual(pair["left"]["counts"]["throw"], 1)
            self.assertNotIn("throw", pair["right"]["counts"])
            for side in ("left", "right"):
                for kind in ("guard", "loop", "call", "write", "return", "string"):
                    self.assertGreater(pair[side]["counts"][kind], 0)
                calls = [f for f in pair[side]["facts"] if f["kind"] == "call" and "step" in f["target"]]
                self.assertEqual(calls[0]["arguments"], "(value)")
                self.assertTrue(calls[0]["controls"])
                self.assertEqual(pair["landmarks"][0][side]["status"], "observed")
                self.assertEqual(pair["landmarks"][1][side]["status"], "not_observed")
                self.assertEqual(len(pair[side]["sha256"]), 64)
            self.assertIn("not semantic equivalence", markdown(report))
            self.assertIn("throw versus", markdown(report))
            mapping["pairs"][0]["right"]["symbol"] = "missing"
            missing = compare_references(mapping, root, root)
            self.assertEqual(missing["summary"], {"unavailable": 1})
            self.assertEqual(missing["pairs"][0]["right"]["status"], "missing_symbol")
            self.assertEqual(missing["pairs"][0]["landmarks"][0]["left"]["status"], "observed")

    def test_parser_failure_ambiguity_and_arrow_selection(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            selector = {"path": "f.ts", "language": "typescript", "symbol": "work"}
            for source, expected in [
                ("export const work = (n: number) => n + 1", "inspected"),
                ("function work( {", "parse_error"),
                ("function work(){}\nfunction work(){}", "ambiguous_symbol"),
            ]:
                with self.subTest(expected=expected):
                    (root / "f.ts").write_text(source)
                    self.assertEqual(inspect_selector(root, selector, "pin")["status"], expected)
            selector["line"] = 2
            self.assertEqual(inspect_selector(root, selector, "pin")["status"], "inspected")
            selector["sha256"] = "bad"
            self.assertEqual(inspect_selector(root, selector, "pin")["status"], "source_hash_mismatch")

    def test_raw_literals_arguments_assignments_and_nested_owners(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "f.ts").write_text('function f(){ publish(user); state.enabled = yes; const fn = () => { return "a  b"; }; return "a  b"; }')
            (root / "f.go").write_text('package p\nfunc f(){ publish(admin); state.enabled = no; fn := func() string { return "a b" }; _ = fn; return }')
            mapping = {"left_ref": "a", "right_ref": "b", "pairs": [{"id": "f", "left": {"path": "f.ts", "language": "typescript", "symbol": "f"}, "right": {"path": "f.go", "language": "go", "symbol": "f"}}]}
            pair = compare_references(mapping, root, root)["pairs"][0]
            deltas = {d["kind"]: d for d in pair["lexical_fact_deltas"]}
            for kind in ("call", "write", "string"):
                self.assertTrue(deltas[kind]["left_only"], kind)
                self.assertTrue(deltas[kind]["right_only"], kind)
            self.assertTrue(any('a  b' in f["raw_text"] for f in pair["left"]["facts"]))
            self.assertTrue(any(f["nested_callable"] for f in pair["left"]["facts"] if f["kind"] == "return"))
            self.assertTrue(any(f["nested_callable"] for f in pair["right"]["facts"] if f["kind"] == "return"))
            writes = [f for f in pair["left"]["facts"] if f["kind"] == "write"]
            self.assertIn("member_or_index_target", [f["write_scope"] for f in writes])
            self.assertIn("local_declaration", [f["write_scope"] for f in writes])

    def test_no_pairs_cannot_be_a_successful_review(self):
        with self.assertRaisesRegex(ValueError, "explicit pair"):
            compare_references({"pairs": []}, Path("."), Path("."))


if __name__ == "__main__":
    unittest.main()
