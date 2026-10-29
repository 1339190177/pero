//! Project identity and task selection, independent of rendering and I/O.
use pero_contracts::workbench::{ProjectBinding, RepositoryIdentity, WorkspaceRepository};
use serde::Serialize;

#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub(crate) struct Project {
    pub key: String,
    pub name: String,
    pub workspace: Option<String>,
    pub workspaces: Vec<String>,
    pub shared_id: Option<String>,
}

#[derive(Clone, Debug)]
pub(crate) struct ProjectRepository {
    pub project_id: String,
    pub repository: RepositoryIdentity,
}

impl Project {
    pub fn catalog(
        workspaces: &[String],
        shared_ids: &[String],
        bindings: &[ProjectBinding],
        workspaces_repositories: &[WorkspaceRepository],
        repositories: &[ProjectRepository],
    ) -> Vec<Self> {
        let mut projects = Vec::new();
        for workspace in workspaces.iter().filter(|path| !path.is_empty()) {
            if projects
                .iter()
                .any(|project: &Self| project.workspace.as_ref() == Some(workspace))
            {
                continue;
            }
            let shared_id = bindings
                .iter()
                .find(|binding| binding.workspace == *workspace)
                .map(|binding| binding.project_id.clone())
                .or_else(|| {
                    let repository = &workspaces_repositories
                        .iter()
                        .find(|entry| entry.workspace == *workspace)?
                        .repository;
                    let mut matching = repositories
                        .iter()
                        .filter(|entry| entry.repository == *repository);
                    let first = matching.next()?;
                    // A repository can feed several shared Projects. Only an
                    // unambiguous mapping can replace an explicit binding.
                    matching
                        .all(|entry| entry.project_id == first.project_id)
                        .then(|| first.project_id.clone())
                })
                .filter(|id| !id.is_empty());
            if let Some(project) = projects
                .iter_mut()
                .find(|project| shared_id.is_some() && project.shared_id == shared_id)
            {
                project.workspaces.push(workspace.clone());
                continue;
            }
            projects.push(Self {
                key: format!("workspace:{workspace}"),
                name: workspace
                    .trim_end_matches(['/', '\\'])
                    .rsplit(['/', '\\'])
                    .next()
                    .unwrap_or(workspace)
                    .to_string(),
                workspace: Some(workspace.clone()),
                workspaces: vec![workspace.clone()],
                shared_id,
            });
        }
        for id in shared_ids.iter().filter(|id| !id.is_empty()) {
            if projects
                .iter()
                .any(|project| project.shared_id.as_ref() == Some(id))
            {
                continue;
            }
            projects.push(Self {
                key: format!("shared:{id}"),
                name: id.clone(),
                workspace: None,
                workspaces: vec![],
                shared_id: Some(id.clone()),
            });
        }
        projects
    }

    pub fn matches_key(&self, key: &str) -> bool {
        self.key == key
            || key
                .strip_prefix("workspace:")
                .is_some_and(|workspace| self.contains_workspace(workspace))
            || key
                .strip_prefix("shared:")
                .is_some_and(|id| self.contains_task(id))
    }

    pub fn contains_workspace(&self, workspace: &str) -> bool {
        self.workspaces.iter().any(|path| path == workspace)
    }

    pub fn contains_task(&self, project_id: &str) -> bool {
        self.shared_id.as_deref() == Some(project_id)
    }
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub(crate) enum ProjectTab {
    #[default]
    Tasks,
    Sources,
    Execution,
    Settings,
}

impl ProjectTab {
    pub const ALL: [Self; 4] = [Self::Tasks, Self::Sources, Self::Execution, Self::Settings];
    pub fn label_key(self) -> &'static str {
        match self {
            Self::Tasks => "nav.tasks",
            Self::Sources => "project.sources",
            Self::Execution => "project.execution",
            Self::Settings => "settings.title",
        }
    }
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub(crate) enum TaskStatusFilter {
    #[default]
    All,
    Ready,
    Running,
    Attention,
    Ended,
}

impl TaskStatusFilter {
    pub const ALL: [Self; 5] = [
        Self::All,
        Self::Ready,
        Self::Running,
        Self::Attention,
        Self::Ended,
    ];
    pub fn label_key(self) -> &'static str {
        match self {
            Self::All => "filter.all",
            Self::Ready => "project.ready",
            Self::Running => "filter.running",
            Self::Attention => "cloud.group.attention",
            Self::Ended => "project.ended",
        }
    }
    pub fn matches(self, phase: crate::cloud::Phase) -> bool {
        use crate::cloud::Group;
        match self {
            Self::All => true,
            Self::Ready => phase.group() == Group::Ready,
            Self::Running => matches!(phase.group(), Group::Running | Group::Queued),
            Self::Attention => phase.group() == Group::Attention,
            Self::Ended => phase.group() == Group::Ended,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn project_identity_requires_an_explicit_shared_binding() {
        let workspaces = vec![
            "/work/pero".into(),
            "/other/pero".into(),
            "/work/pero".into(),
        ];
        let shared = vec!["pero".into(), "point".into()];
        for (bindings, expected) in [
            (vec![], 4),
            (
                vec![ProjectBinding {
                    workspace: "/work/pero".into(),
                    project_id: "pero".into(),
                }],
                3,
            ),
        ] {
            let projects = Project::catalog(&workspaces, &shared, &bindings, &[], &[]);
            assert_eq!(projects.len(), expected);
            assert_ne!(projects[0].key, projects[1].key);
            assert_eq!(projects[0].contains_task("pero"), !bindings.is_empty());
            assert!(!projects[1].contains_task("pero"));
            assert!(!projects[0].contains_task("point"));
        }
    }
    #[test]
    fn task_filters_distinguish_results_from_attention() {
        use crate::cloud::Phase;
        for (filter, phase, expected) in [
            (TaskStatusFilter::Ready, Phase::Draft, true),
            (TaskStatusFilter::Running, Phase::Queued, true),
            (TaskStatusFilter::Attention, Phase::Failed, true),
            (TaskStatusFilter::Attention, Phase::Succeeded, true),
            (TaskStatusFilter::Ended, Phase::Succeeded, false),
            (TaskStatusFilter::Ended, Phase::Running, false),
        ] {
            assert_eq!(filter.matches(phase), expected);
        }
    }
}

#[cfg(test)]
mod repository_tests {
    use super::*;
    fn repo(remote: &str) -> RepositoryIdentity {
        RepositoryIdentity::from_git_remote(remote).unwrap()
    }

    #[test]
    fn project_groups_reconcile_repository_evidence_without_merging_homonyms() {
        let workspaces: Vec<String> = vec!["/work/pero".into(), "/other/pero".into()];
        let shared = vec!["pero".into(), "research".into()];
        let local = vec![
            WorkspaceRepository {
                workspace: workspaces[0].clone(),
                repository: repo("git@github.com:phodal/pero.git"),
            },
            WorkspaceRepository {
                workspace: workspaces[1].clone(),
                repository: repo("https://github.com/another/pero"),
            },
        ];
        for (remote, bindings, expected, inferred) in [
            (
                vec![ProjectRepository {
                    project_id: "pero".into(),
                    repository: repo("https://github.com/Phodal/Pero.git"),
                }],
                vec![],
                3,
                true,
            ),
            (
                vec![ProjectRepository {
                    project_id: "pero".into(),
                    repository: repo("https://github.com/another-owner/pero"),
                }],
                vec![],
                4,
                false,
            ),
            (
                vec![
                    ProjectRepository {
                        project_id: "pero".into(),
                        repository: repo("https://github.com/phodal/pero"),
                    },
                    ProjectRepository {
                        project_id: "research".into(),
                        repository: repo("ssh://git@github.com/phodal/pero.git"),
                    },
                ],
                vec![],
                4,
                false,
            ),
            (
                vec![ProjectRepository {
                    project_id: "pero".into(),
                    repository: repo("https://github.com/phodal/pero"),
                }],
                vec![ProjectBinding {
                    workspace: "/work/pero".into(),
                    project_id: "research".into(),
                }],
                3,
                false,
            ),
        ] {
            let projects = Project::catalog(&workspaces, &shared, &bindings, &local, &remote);
            assert_eq!(projects.len(), expected);
            assert_eq!(projects[0].contains_task("pero"), inferred);
            assert_eq!(projects[0].matches_key("shared:pero"), inferred);
            assert!(projects[0].matches_key("workspace:/work/pero"));
            assert!(projects[0].contains_workspace("/work/pero"));
            assert!(!projects[0].contains_workspace("/other/pero"));
            assert!(!projects[1].contains_task("pero"));
        }
        let two_checkouts = vec![
            WorkspaceRepository {
                workspace: workspaces[0].clone(),
                repository: repo("git@github.com:phodal/pero.git"),
            },
            WorkspaceRepository {
                workspace: workspaces[1].clone(),
                repository: repo("https://github.com/phodal/pero"),
            },
        ];
        let projects = Project::catalog(
            &workspaces,
            &shared,
            &[],
            &two_checkouts,
            &[ProjectRepository {
                project_id: "pero".into(),
                repository: repo("https://github.com/phodal/pero"),
            }],
        );
        assert_eq!(projects.len(), 2);
        assert!(projects[0].contains_workspace("/other/pero"));
    }
}
