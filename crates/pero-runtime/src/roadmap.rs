//! Local Markdown intake. No Cloud identity, claim, or dispatch is created here.
use markdown::mdast::Node;
use pero_contracts::roadmap::{LocalRoadmap, RoadmapError, RoadmapItem};
use std::{io::Read, path::Path};

const MAX_BYTES: u64 = 512 * 1024;

pub fn load(workspace: &Path) -> LocalRoadmap {
    let mut result = LocalRoadmap {
        workspace: workspace.to_string_lossy().into_owned(),
        path: String::new(),
        items: vec![],
        error: None,
    };
    let read = || -> Result<(String, Vec<RoadmapItem>), RoadmapError> {
        for relative in ["ROADMAP.md", "docs/ROADMAP.md"] {
            let path = workspace.join(relative);
            let file = match std::fs::File::open(&path) {
                Ok(file) => file,
                Err(error) if error.kind() == std::io::ErrorKind::NotFound => continue,
                Err(_) => return Err(RoadmapError::Read),
            };
            if !file.metadata().is_ok_and(|meta| meta.is_file()) {
                return Err(RoadmapError::Read);
            }
            let mut bytes = vec![];
            file.take(MAX_BYTES + 1)
                .read_to_end(&mut bytes)
                .map_err(|_| RoadmapError::Read)?;
            let text = std::str::from_utf8(&bytes).map_err(|_| RoadmapError::Invalid)?;
            return Ok((relative.into(), parse(text)?));
        }
        Err(RoadmapError::NotFound)
    };
    match read() {
        Ok((path, items)) => {
            result.path = path;
            result.items = items;
        }
        Err(error) => result.error = Some(error),
    }
    result
}

pub fn parse(source: &str) -> Result<Vec<RoadmapItem>, RoadmapError> {
    if source.len() as u64 > MAX_BYTES || source.contains('\0') {
        return Err(RoadmapError::Invalid);
    }
    let document = markdown::to_mdast(source, &markdown::ParseOptions::gfm())
        .map_err(|_| RoadmapError::Invalid)?;
    let mut items = vec![];
    visit(&document, &mut vec![], None, &mut items)?;
    Ok(items)
}

fn visit(
    node: &Node,
    headings: &mut Vec<(u8, String)>,
    parent_line: Option<usize>,
    items: &mut Vec<RoadmapItem>,
) -> Result<(), RoadmapError> {
    // Quoted examples and code blocks are not actionable checklist entries.
    if matches!(node, Node::Blockquote(_) | Node::Code(_)) {
        return Ok(());
    }
    if let Node::Heading(heading) = node {
        headings.retain(|(depth, _)| *depth < heading.depth);
        headings.push((heading.depth, node.to_string()));
    }
    let mut parent = parent_line;
    if let Node::ListItem(item) = node {
        if let Some(checked) = item.checked {
            let text = item
                .children
                .iter()
                .map(goal_text)
                .filter(|text| !text.is_empty())
                .collect::<Vec<_>>()
                .join("\n\n");
            if items.len() >= 2000 || text.trim().is_empty() || text.chars().count() > 60000 {
                return Err(RoadmapError::Invalid);
            }
            let first = text.lines().next().unwrap_or_default();
            let title = first.split_once(". ").map_or(first, |(title, _)| title);
            let line = item.position.as_ref().unwrap().start.line;
            items.push(RoadmapItem {
                line,
                parent_line,
                title: title.into(),
                text,
                checked,
                headings: headings.iter().map(|(_, text)| text.clone()).collect(),
            });
            parent = Some(line);
        }
    }
    if let Some(children) = node.children() {
        for child in children {
            visit(child, headings, parent, items)?;
        }
    }
    Ok(())
}

fn goal_text(node: &Node) -> String {
    match node {
        Node::ListItem(item) if item.checked.is_some() => String::new(),
        Node::Html(_) => String::new(),
        _ => match node.children() {
            Some(children) => children
                .iter()
                .map(goal_text)
                .filter(|text| !text.is_empty())
                .collect::<Vec<_>>()
                .join(if matches!(node, Node::List(_) | Node::ListItem(_)) {
                    "\n"
                } else {
                    ""
                }),
            None => node.to_string(),
        },
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn checklist_goals() {
        let cases = [
            (
                "# Roadmap\n## Local\n- [ ] Parent. Full goal\n  continuation\n  - [ ] Child\n- [X] Done",
                3,
            ),
            ("```md\n- [ ] Example\n```\n> - [ ] Quoted\n\n- [ ] Real", 1),
            (
                "Tasks\n=====\n\n1. [ ] **Bold**. [Details](https://example.com)\n\n   Acceptance: passes.",
                1,
            ),
            ("No checklist", 0),
        ];
        for (source, count) in cases {
            let items = parse(source).unwrap();
            assert_eq!(items.len(), count, "{source}");
            if source.contains("Parent") {
                assert_eq!(items[0].title, "Parent");
                assert!(items[0].text.contains("continuation"));
                assert!(!items[0].text.contains("Child"));
                assert_eq!(items[1].parent_line, Some(3));
                assert_eq!(items[1].headings, ["Roadmap", "Local"]);
                assert!(items[2].checked);
            }
            if source.contains("Bold") {
                assert_eq!(items[0].title, "Bold");
                assert!(items[0].text.contains("Acceptance: passes."));
            }
        }
        let ordinary =
            parse("- [ ] Goal\n  - Acceptance: run tests\n  - [ ] Separate task").unwrap();
        assert!(ordinary[0].text.contains("Acceptance: run tests"));
        assert!(!ordinary[0].text.contains("Separate task"));
        for source in [
            "nul\0",
            &"x".repeat(MAX_BYTES as usize + 1),
            &"- [ ] Item\n".repeat(2001),
        ] {
            assert!(parse(source).is_err());
        }
        let items = parse(include_str!("../../../ROADMAP.md")).unwrap();
        assert!(items.len() > 100);
    }
}
