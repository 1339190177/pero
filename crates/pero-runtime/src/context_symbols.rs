//! Parser-backed definitions. Matches carry exact definition ranges, not grep hits.
use pero_contracts::context::{ContextCandidate, ContextKind};
use std::{
    collections::hash_map::DefaultHasher,
    hash::{Hash, Hasher},
    path::Path,
};

pub fn supported(path: &Path) -> bool {
    matches!(
        path.extension().and_then(|e| e.to_str()),
        Some("rs" | "go" | "py" | "js" | "jsx" | "mjs" | "cjs" | "ts" | "tsx")
    )
}

pub fn definitions(path: &Path, text: &str, query: &str) -> Vec<ContextCandidate> {
    let extension = path
        .extension()
        .and_then(|e| e.to_str())
        .unwrap_or_default();
    let language = match extension {
        "rs" => tree_sitter_rust::LANGUAGE.into(),
        "go" => tree_sitter_go::LANGUAGE.into(),
        "py" => tree_sitter_python::LANGUAGE.into(),
        "js" | "jsx" | "mjs" | "cjs" => tree_sitter_javascript::LANGUAGE.into(),
        "ts" => tree_sitter_typescript::LANGUAGE_TYPESCRIPT.into(),
        "tsx" => tree_sitter_typescript::LANGUAGE_TSX.into(),
        _ => return vec![],
    };
    let mut version = DefaultHasher::new();
    text.hash(&mut version);
    let version = version.finish();
    let mut parser = tree_sitter::Parser::new();
    if parser.set_language(&language).is_err() {
        return vec![];
    }
    let Some(tree) = parser.parse(text, None) else {
        return vec![];
    };
    let mut nodes = vec![tree.root_node()];
    let mut found = vec![];
    let query = query.to_lowercase();
    while let Some(node) = nodes.pop() {
        if matches!(
            node.kind(),
            "function_item"
                | "struct_item"
                | "enum_item"
                | "trait_item"
                | "type_item"
                | "const_item"
                | "mod_item"
                | "function_declaration"
                | "method_declaration"
                | "type_spec"
                | "function_definition"
                | "class_definition"
                | "class_declaration"
                | "interface_declaration"
                | "type_alias_declaration"
                | "method_definition"
        ) {
            if let Some(name) = node
                .child_by_field_name("name")
                .and_then(|n| n.utf8_text(text.as_bytes()).ok())
            {
                if name.to_lowercase().contains(&query) {
                    let start = node.start_position().row + 1;
                    let end = node.end_position().row + 1;
                    found.push(ContextCandidate {
                        kind: ContextKind::Symbol,
                        label: name.into(),
                        source: format!("{}#L{start}-L{end}@{version:016x}", path.display()),
                        detail: format!("{}:{start} · {}", path.display(), node.kind()),
                    });
                }
            }
        }
        let mut cursor = node.walk();
        nodes.extend(node.named_children(&mut cursor));
    }
    found
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn definitions_ignore_comments_and_preserve_ranges() {
        for (file, text, query, count) in [
            ("a.rs", "// fn fake() {}\nfn real() {\n}\n", "real", 1),
            ("a.rs", "// fn fake() {}\nfn real() {}", "fake", 0),
            (
                "a.py",
                "class Model:\n    def apply(self):\n        return 1\n",
                "apply",
                1,
            ),
            ("a.ts", "interface Context { id: string }\n", "Context", 1),
        ] {
            let result = definitions(Path::new(file), text, query);
            assert_eq!(result.len(), count);
            if let Some(result) = result.first() {
                assert!(result.source.contains("#L"));
            }
        }
    }
}
