/// A stable identity for content that can remain open in the workbench.
/// Titles and other presentation metadata come from the current source object.
#[derive(Clone, Debug, PartialEq, Eq, serde::Serialize)]
#[serde(tag = "kind", content = "value", rename_all = "snake_case")]
pub(crate) enum WorkspaceItem {
    Conversation(String),
    Project(String),
    Task(String),
    App { name: String, panel: String },
}

/// Owns both the open items and their selection. Closing a tab only removes it
/// from this value; it never deletes a conversation, project, or installed App.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub(crate) struct WorkspaceTabs {
    items: Vec<WorkspaceItem>,
    active: Option<WorkspaceItem>,
}

impl WorkspaceTabs {
    pub(crate) fn items(&self) -> &[WorkspaceItem] {
        &self.items
    }

    pub(crate) fn active(&self) -> Option<&WorkspaceItem> {
        self.active.as_ref()
    }

    /// Opens an item once and activates it. Returns whether the tabs changed.
    pub(crate) fn open(&mut self, item: WorkspaceItem) -> bool {
        let already_open = self.items.contains(&item);
        let changed = !already_open || self.active.as_ref() != Some(&item);
        if !already_open {
            self.items.push(item.clone());
        }
        self.active = Some(item);
        changed
    }

    /// Selects an existing tab. Missing items and the current selection do not
    /// change the workspace.
    pub(crate) fn select(&mut self, item: &WorkspaceItem) -> bool {
        if self.active.as_ref() == Some(item) || !self.items.contains(item) {
            return false;
        }
        self.active = Some(item.clone());
        true
    }

    /// The right neighbor becomes active when the selected tab closes. At the
    /// end of the list, the left neighbor is used instead.
    pub(crate) fn close(&mut self, item: &WorkspaceItem) -> bool {
        let Some(index) = self.items.iter().position(|open| open == item) else {
            return false;
        };
        self.items.remove(index);
        if self.active.as_ref() == Some(item) {
            self.active = self.items.get(index).or_else(|| self.items.last()).cloned();
        }
        true
    }

    /// Removes tabs whose source objects no longer exist, without opening any
    /// newly discovered objects or reordering the remaining tabs.
    pub(crate) fn reconcile(&mut self, available: &[WorkspaceItem]) -> bool {
        let removed: Vec<_> = self
            .items
            .iter()
            .filter(|item| !available.contains(item))
            .cloned()
            .collect();
        let changed = !removed.is_empty();
        for item in removed {
            self.close(&item);
        }
        changed
    }

    pub(crate) fn select_next(&mut self) -> bool {
        let Some(active) = self.active.as_ref() else {
            return false;
        };
        let index = self
            .items
            .iter()
            .position(|item| item == active)
            .expect("the active item is an open tab");
        let next = self.items[(index + 1) % self.items.len()].clone();
        self.select(&next)
    }

    pub(crate) fn select_previous(&mut self) -> bool {
        let Some(active) = self.active.as_ref() else {
            return false;
        };
        let index = self
            .items
            .iter()
            .position(|item| item == active)
            .expect("the active item is an open tab");
        let previous = self.items[(index + self.items.len() - 1) % self.items.len()].clone();
        self.select(&previous)
    }
}

#[cfg(test)]
mod tests {
    use super::{WorkspaceItem, WorkspaceTabs};

    #[derive(Clone)]
    enum Action {
        Open(WorkspaceItem),
        Select(WorkspaceItem),
        Close(WorkspaceItem),
        Reconcile(Vec<WorkspaceItem>),
        Next,
        Previous,
    }

    #[test]
    fn workspace_tab_transitions() {
        use Action::*;
        let conversation = WorkspaceItem::Conversation("conversation".into());
        let project = WorkspaceItem::Project("project".into());
        let task = WorkspaceItem::Task("project".into());
        let app = WorkspaceItem::App {
            name: "paper-agent".into(),
            panel: "papers".into(),
        };
        let app_report = WorkspaceItem::App {
            name: "paper-agent".into(),
            panel: "report".into(),
        };

        struct Case {
            name: &'static str,
            actions: Vec<Action>,
            items: Vec<WorkspaceItem>,
            active: Option<WorkspaceItem>,
        }

        let cases = [
            Case {
                name: "missing selection and empty navigation do not open tabs",
                actions: vec![
                    Select(project.clone()),
                    Close(project.clone()),
                    Next,
                    Previous,
                ],
                items: vec![],
                active: None,
            },
            Case {
                name: "opening an existing item selects it without duplication",
                actions: vec![
                    Open(conversation.clone()),
                    Open(project.clone()),
                    Open(conversation.clone()),
                ],
                items: vec![conversation.clone(), project.clone()],
                active: Some(conversation.clone()),
            },
            Case {
                name: "selecting an existing item retains open order",
                actions: vec![
                    Open(conversation.clone()),
                    Open(project.clone()),
                    Select(conversation.clone()),
                    Select(app.clone()),
                ],
                items: vec![conversation.clone(), project.clone()],
                active: Some(conversation.clone()),
            },
            Case {
                name: "closing an earlier inactive tab retains the active identity",
                actions: vec![
                    Open(conversation.clone()),
                    Open(project.clone()),
                    Close(conversation.clone()),
                ],
                items: vec![project.clone()],
                active: Some(project.clone()),
            },
            Case {
                name: "closing a later inactive tab retains the active identity",
                actions: vec![
                    Open(conversation.clone()),
                    Open(project.clone()),
                    Select(conversation.clone()),
                    Close(project.clone()),
                ],
                items: vec![conversation.clone()],
                active: Some(conversation.clone()),
            },
            Case {
                name: "closing the selected middle tab selects its right neighbor",
                actions: vec![
                    Open(conversation.clone()),
                    Open(project.clone()),
                    Open(app.clone()),
                    Select(project.clone()),
                    Close(project.clone()),
                ],
                items: vec![conversation.clone(), app.clone()],
                active: Some(app.clone()),
            },
            Case {
                name: "closing the selected last tab selects its left neighbor",
                actions: vec![
                    Open(conversation.clone()),
                    Open(project.clone()),
                    Close(project.clone()),
                ],
                items: vec![conversation.clone()],
                active: Some(conversation.clone()),
            },
            Case {
                name: "closing the last open tab clears selection",
                actions: vec![Open(conversation.clone()), Close(conversation.clone())],
                items: vec![],
                active: None,
            },
            Case {
                name: "app panels have independent persistent identities",
                actions: vec![
                    Open(app.clone()),
                    Open(app_report.clone()),
                    Open(app.clone()),
                ],
                items: vec![app.clone(), app_report.clone()],
                active: Some(app.clone()),
            },
            Case {
                name: "a task and project with the same source id open separately",
                actions: vec![
                    Open(project.clone()),
                    Open(task.clone()),
                    Open(project.clone()),
                ],
                items: vec![project.clone(), task.clone()],
                active: Some(project.clone()),
            },
            Case {
                name: "reconciliation removes deleted objects and selects a surviving neighbor",
                actions: vec![
                    Open(conversation.clone()),
                    Open(project.clone()),
                    Open(app.clone()),
                    Select(project.clone()),
                    Reconcile(vec![app.clone(), conversation.clone(), app_report.clone()]),
                ],
                items: vec![conversation.clone(), app.clone()],
                active: Some(app.clone()),
            },
            Case {
                name: "uninstalled apps close all their panels without changing a surviving selection",
                actions: vec![
                    Open(app.clone()),
                    Open(app_report.clone()),
                    Open(conversation.clone()),
                    Reconcile(vec![conversation.clone()]),
                ],
                items: vec![conversation.clone()],
                active: Some(conversation.clone()),
            },
            Case {
                name: "reconciliation can remove every source object",
                actions: vec![
                    Open(conversation.clone()),
                    Open(project.clone()),
                    Reconcile(vec![]),
                ],
                items: vec![],
                active: None,
            },
            Case {
                name: "next wraps from the last tab to the first",
                actions: vec![Open(conversation.clone()), Open(project.clone()), Next],
                items: vec![conversation.clone(), project.clone()],
                active: Some(conversation.clone()),
            },
            Case {
                name: "previous wraps from the first tab to the last",
                actions: vec![
                    Open(conversation.clone()),
                    Open(project.clone()),
                    Select(conversation.clone()),
                    Previous,
                ],
                items: vec![conversation.clone(), project.clone()],
                active: Some(project.clone()),
            },
            Case {
                name: "cycling a single tab retains its selection",
                actions: vec![Open(conversation.clone()), Next, Previous],
                items: vec![conversation.clone()],
                active: Some(conversation.clone()),
            },
        ];

        for case in cases {
            let mut tabs = WorkspaceTabs::default();
            for action in case.actions {
                match action {
                    Open(item) => tabs.open(item),
                    Select(item) => tabs.select(&item),
                    Close(item) => tabs.close(&item),
                    Reconcile(available) => tabs.reconcile(&available),
                    Next => tabs.select_next(),
                    Previous => tabs.select_previous(),
                };
            }
            assert_eq!(tabs.items(), case.items, "{}", case.name);
            assert_eq!(tabs.active(), case.active.as_ref(), "{}", case.name);
        }
    }
}
