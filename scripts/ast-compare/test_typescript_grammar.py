"""Full-source TS5 parsing: extension acceptance is not source rewriting."""
import unittest
from tree_sitter import Parser
from build_typescript import VERSION, patched_grammar
from references import LANGUAGES, TYPESCRIPT_PROVENANCE
from compare import walk


@unittest.skipUnless(TYPESCRIPT_PROVENANCE.get("version") == VERSION,
                     "build_typescript.py required for local TS5 grammar tests")
class TypeScriptGrammarContracts(unittest.TestCase):
    def test_export_forms_keep_original_tokens_and_spans(self):
        for statement in (
            'export type * from "./types.ts";',
            'export type * as Ns from "./types.ts";',
            'export /*a*/ type /*b*/ * /*c*/ from "./types.ts"',
            'export type { T } from "./types.ts";',
            'export * from "./types.ts";',
            'export * as Ns from "./types.ts";',
            'export type T = string;',
            'export const type = 1;',
        ):
            with self.subTest(statement=statement):
                source = (statement + '\nfunction after() { return "a  b"; }').encode()
                root = Parser(LANGUAGES["typescript"]).parse(source).root_node
                self.assertFalse(root.has_error, str(root))
                self.assertEqual(root.text, source)
                self.assertEqual(root.named_children[0].type, "export_statement")
                function = root.named_children[1]
                self.assertEqual(function.start_byte, len(statement.encode()) + 1)
                self.assertEqual(function.child_by_field_name("name").text, b"after")
                self.assertIn(b'"a  b"', function.text)

    def test_malformed_exports_and_surrounding_functions_stay_errors(self):
        for source in (
            'export type *;', 'export type * as from "m";',
            'export type * from;', 'export type * as Ns;',
            'export type * from "m"; function f( {',
            'function f() { const x = ; } export type * from "m";',
        ):
            with self.subTest(source=source):
                root = Parser(LANGUAGES["typescript"]).parse(source.encode()).root_node
                self.assertTrue(root.has_error)
                self.assertTrue(any(n.type == "ERROR" or n.is_missing for n in walk(root)))

    def test_changed_upstream_grammar_requires_review(self):
        with self.assertRaisesRegex(ValueError, "grammar hash differs"):
            patched_grammar(b'{"rules":{}}')


if __name__ == "__main__":
    unittest.main()
