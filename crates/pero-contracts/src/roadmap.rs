//! A local checklist is source context, not a Cloud Task or accepted Run.
use serde::{Deserialize, Serialize};

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct RoadmapItem {
    pub line: usize,
    pub parent_line: Option<usize>,
    pub title: String,
    pub text: String,
    pub checked: bool,
    pub headings: Vec<String>,
}

impl RoadmapItem {
    pub fn prompt(&self, path: &str) -> String {
        format!(
            "{}\n\nImplement this local ROADMAP item.\nSource: {path}:{}\nContext: {}\n\n{}\n\nFollow the workspace instructions, preserve unrelated changes, and verify the result. Do not commit, push, publish, or mark the source complete without explicit user authorization.",
            self.title,
            self.line,
            self.headings.join(" / "),
            self.text
        )
    }
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum RoadmapError {
    NotFound,
    Read,
    Invalid,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct LocalRoadmap {
    pub workspace: String,
    pub path: String,
    pub items: Vec<RoadmapItem>,
    pub error: Option<RoadmapError>,
}
