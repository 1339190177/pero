use crate::remote_view::phase;
use crate::{
    app::Workbench,
    components::{
        ActionHandler, Ui,
        sidebar::{self, DeleteAffordance, SidebarAction, SidebarProps},
        task_detail::{self, SessionHeader, TaskDetailAction, TaskDetailProps},
        workspace::{self, AgentRow, WorkspaceAction},
    },
};
use gpui_kit::{prelude::*, *};
use serde_json::json;
use std::rc::Rc;

impl Workbench {
    fn ui(&self) -> Ui<'_> {
        Ui {
            palette: self.palettes.get(self.dark),
            locale: &self.locale,
        }
    }

    pub fn icon(&self, name: &str) -> Svg {
        self.ui().icon(name)
    }

    fn sidebar(&self, width: f32, cx: &mut Context<Self>) -> Stateful<Div> {
        let on_action = Rc::new(cx.listener(
            |this: &mut Self, action: &SidebarAction, window, cx| {
                match action {
                    SidebarAction::RequestDelete(index) => {
                        this.chat.update(cx, |panel, _| {
                            panel.deleting_chat =
                                panel.chats.get(*index).map(|chat| chat.id.clone());
                        });
                    }
                    SidebarAction::ConfirmDelete(index) => {
                        this.chat.update(cx, |panel, cx| {
                            if panel.deleting_chat.as_deref()
                                == panel.chats.get(*index).map(|chat| chat.id.as_str())
                            {
                                panel.delete_chat(*index, cx);
                            }
                        });
                    }
                    SidebarAction::CancelDelete => {
                        this.chat.update(cx, |panel, _| panel.deleting_chat = None);
                    }
                    SidebarAction::NewChat => {
                        this.chat.update(cx, |panel, _| panel.deleting_chat = None);
                        this.new_chat(cx);
                    }
                    SidebarAction::Navigate(index) => {
                        this.chat.update(cx, |panel, _| panel.deleting_chat = None);
                        this.navigation = *index;
                        this.sync_follow(cx);
                        if *index == 7 {
                            this.refresh_apps(cx);
                        }
                    }
                    SidebarAction::OpenProject(key) => {
                        this.chat.update(cx, |panel, _| panel.deleting_chat = None);
                        this.open_project(key.clone(), window, cx);
                    }
                    SidebarAction::ToggleProject(key) => {
                        this.chat.update(cx, |panel, _| panel.deleting_chat = None);
                        if !this.collapsed_projects.remove(key) {
                            this.collapsed_projects.insert(key.clone());
                        }
                    }
                    SidebarAction::SelectSession(index) => {
                        this.chat.update(cx, |panel, _| panel.deleting_chat = None);
                        this.navigation = 0;
                        this.focus_chat(Some(*index), 4, cx);
                    }
                    SidebarAction::SelectTask(id) => {
                        this.chat.update(cx, |panel, _| panel.deleting_chat = None);
                        this.open_task(id.clone(), cx);
                    }
                }
                cx.notify();
            },
        ));
        let panel = self.chat.read(cx);
        let query = self.search.read(cx).value().to_lowercase();
        let matches = |title: &str| query.is_empty() || title.to_lowercase().contains(&query);
        let session_row =
            |i: usize, chat: &pero_chat_ui::conversation::Conversation| sidebar::RecentRow {
                label: chat.title.clone(),
                icon: "terminal",
                active: self.navigation == 0 && panel.active_chat == Some(i),
                busy: chat.busy(),
                attention: chat.permission.is_some() || chat.error.is_some(),
                action: SidebarAction::SelectSession(i),
                delete: Some(DeleteAffordance {
                    confirming: panel.deleting_chat.as_deref() == Some(chat.id.as_str()),
                    request: SidebarAction::RequestDelete(i),
                    confirm: SidebarAction::ConfirmDelete(i),
                    cancel: SidebarAction::CancelDelete,
                }),
            };
        let groups = self
            .project_catalog(cx)
            .into_iter()
            .filter_map(|project| {
                let mut rows = panel
                    .chats
                    .iter()
                    .enumerate()
                    .rev()
                    .filter(|(_, chat)| {
                        project.contains_workspace(&chat.workspace) && matches(&chat.title)
                    })
                    .take(8)
                    .map(|(i, chat)| session_row(i, chat))
                    .collect::<Vec<_>>();
                rows.extend(
                    self.remote
                        .tasks()
                        .iter()
                        .filter(|task| {
                            project.contains_task(crate::remote::string(task, "project_id"))
                                && matches(crate::remote::string(task, "title"))
                                && (matches!(
                                    phase(task),
                                    crate::cloud::Phase::Running
                                        | crate::cloud::Phase::Stopping
                                        | crate::cloud::Phase::Failed
                                        | crate::cloud::Phase::Reconcile
                                ) || self
                                    .recent_tasks
                                    .iter()
                                    .any(|id| Some(id.as_str()) == task["id"].as_str())
                                    || self.remote.data["selected_task_id"].as_str()
                                        == task["id"].as_str())
                        })
                        .take(4)
                        .map(|task| sidebar::RecentRow {
                            label: crate::remote::string(task, "title").to_string(),
                            icon: "layout-list",
                            active: self.navigation == 3
                                && self.remote.data["selected_task_id"].as_str()
                                    == task["id"].as_str(),
                            busy: matches!(
                                phase(task),
                                crate::cloud::Phase::Running | crate::cloud::Phase::Stopping
                            ),
                            attention: matches!(
                                phase(task),
                                crate::cloud::Phase::Failed | crate::cloud::Phase::Reconcile
                            ),
                            action: SidebarAction::SelectTask(
                                crate::remote::string(task, "id").to_string(),
                            ),
                            delete: None,
                        }),
                );
                if !query.is_empty() && rows.is_empty() && !matches(&project.name) {
                    return None;
                }
                Some(sidebar::ProjectGroup {
                    active: self.navigation == 3
                        && self.selected_project.as_ref() == Some(&project.key),
                    collapsed: self.collapsed_projects.contains(&project.key),
                    path: project
                        .workspace
                        .unwrap_or_else(|| project.shared_id.unwrap_or_default()),
                    key: project.key,
                    name: project.name,
                    rows,
                })
            })
            .collect();
        let ungrouped = panel
            .chats
            .iter()
            .enumerate()
            .rev()
            .filter(|(_, chat)| chat.workspace.is_empty() && matches(&chat.title))
            .map(|(i, chat)| session_row(i, chat))
            .collect();
        sidebar::sidebar(
            self.ui(),
            width,
            SidebarProps {
                navigation: self.navigation,
                local_connected: self.connected,
                groups,
                ungrouped,
                search: &self.search,
            },
            self.settings_menu(cx).into_any_element(),
            on_action,
        )
    }

    fn detail(&self, _window: &mut Window, cx: &mut Context<Self>) -> Div {
        let panel = self.chat.read(cx);
        let session = panel
            .active_chat
            .and_then(|i| panel.chats.get(i))
            .map(|chat| SessionHeader {
                title: chat.title.clone(),
                agent: chat.agent.clone(),
                closed: chat.state == "closed",
            });
        let content = if self.tab == 4 {
            self.chat.clone().into_any_element()
        } else {
            let ui = self.ui();
            div()
                .id("detail-content")
                .flex_1()
                .min_h_0()
                .overflow_y_scroll()
                .p(px(20.))
                .when(self.tab == 3, |el| {
                    el.children(self.events.iter().map(|event| {
                        div()
                            .py(px(8.))
                            .border_b_1()
                            .border_color(ui.palette.color("border"))
                            .child(self.event_text(event))
                    }))
                })
                .into_any_element()
        };
        let on_action = Rc::new(cx.listener(
            |this, action: &TaskDetailAction, _, cx| match action {
                TaskDetailAction::NewSession => this.new_chat(cx),
                TaskDetailAction::CloseSession => {
                    let panel = this.chat.read(cx);
                    if let Some(id) = panel
                        .active_chat
                        .and_then(|i| panel.chats.get(i))
                        .map(|chat| chat.id.clone())
                    {
                        this.command(json!({"action":"close","thread_id":id}), cx);
                    }
                }
                TaskDetailAction::SelectTab(tab) => {
                    let active = this.chat.read(cx).active_chat;
                    this.focus_chat(active, *tab, cx);
                }
            },
        ));
        task_detail::task_detail(
            self.ui(),
            TaskDetailProps {
                session,
                tab: self.tab,
            },
            content,
            on_action,
        )
    }

    fn workspace_actions(&self, cx: &mut Context<Self>) -> ActionHandler<WorkspaceAction> {
        Rc::new(
            cx.listener(|this, action: &WorkspaceAction, _, cx| match action {
                WorkspaceAction::NewSession(index) => {
                    this.chat.update(cx, |chat, _| chat.agent_index = *index);
                    this.new_chat(cx);
                }
            }),
        )
    }

    fn environments(&self, cx: &mut Context<Self>) -> Div {
        let agents: Vec<_> = self
            .chat
            .read(cx)
            .agents
            .iter()
            .map(|agent| AgentRow {
                name: agent.name.clone(),
                available: agent.available,
            })
            .collect();
        workspace::environments(self.ui(), &agents, self.workspace_actions(cx))
    }
}

impl Render for Workbench {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let scope = self
            .apps
            .active
            .as_ref()
            .filter(|_| self.navigation == 7)
            .map(|active| pero_contracts::AppChatScope {
                app: active.name.clone(),
                panel: active.panel.clone(),
            });
        self.chat.update(cx, |chat, cx| chat.set_scope(scope, cx));
        self.sync_follow(cx);
        let show_return = self
            .chat
            .read(cx)
            .active_chat
            .and_then(|i| self.chat.read(cx).chats.get(i))
            .is_some_and(|chat| {
                self.contribution.data["local"]["thread_id"].as_str() == Some(chat.id.as_str())
            });
        self.chat
            .update(cx, |chat, _| chat.show_return_to_review = show_return);
        if self.input_locale != self.locale {
            self.input_locale = self.locale.clone();
            let placeholder = self.t("sessions.search");
            self.search.update(cx, |input, cx| {
                input.set_placeholder(placeholder, window, cx)
            });
            let placeholder = self.t("project.search_tasks");
            self.remote.search.update(cx, |input, cx| {
                input.set_placeholder(placeholder, window, cx)
            });
            let placeholder = self.t("input.placeholder");
            self.input.update(cx, |input, cx| {
                input.set_placeholder(placeholder, window, cx)
            });
        }
        self.apply_local_execution(window, cx);
        let width: f32 = window.viewport_size().width.into();
        let compact = width < 1150.;
        let sidebar_width = if compact { 240. } else { 260. };
        self.sync_content_tabs(window, cx);
        self.sync_follow(cx);
        let chat_width = self.chat_dock_width(width, width - sidebar_width);
        let dock = self.chat_dock_visible();
        let narrow = width < 1050.;
        let has_tabs = !self.contents.items().is_empty();
        self.follow_task_pages(window, cx);
        if self.navigation != 7
            || self.apps.active.is_none()
            || self.settings_menu_open
            || self.resizing_chat
            || (dock && narrow)
        {
            self.sync_app_surface(None);
        }
        let p = self.palettes.get(self.dark);
        div()
            .id("workbench")
            .on_key_down(
                cx.listener(|this, event, window, cx| this.workspace_keys(event, window, cx)),
            )
            .on_mouse_move(cx.listener(|this, event: &MouseMoveEvent, window, cx| {
                if this.resizing_chat {
                    let width: f32 = window.viewport_size().width.into();
                    let x: f32 = event.position.x.into();
                    this.preferences.layout.resize_chat(width - x);
                    cx.notify();
                }
            }))
            .on_mouse_up(
                MouseButton::Left,
                cx.listener(|this, _, _, cx| {
                    if this.resizing_chat {
                        this.resizing_chat = false;
                        this.save_preferences(this.preferences.clone(), cx);
                        cx.notify();
                    }
                }),
            )
            .size_full()
            .flex()
            .bg(p.color("surface"))
            .text_color(p.color("text"))
            .font_family("Noto Sans CJK SC")
            .text_size(px(14.))
            .child(self.sidebar(sidebar_width, cx))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .h_full()
                    .flex()
                    .flex_col()
                    .when(has_tabs, |el| el.child(self.content_tabs(cx)))
                    .when(self.error.is_some(), |el| {
                        el.child(
                            div()
                                .id("workbench-error")
                                .role(Role::Alert)
                                .px(px(20.))
                                .py(px(10.))
                                .bg(p.color("error-bg"))
                                .text_color(p.color("error"))
                                .child(
                                    self.error
                                        .as_deref()
                                        .map(|error| {
                                            if error.starts_with("error.") {
                                                self.t(error)
                                            } else {
                                                error
                                            }
                                        })
                                        .unwrap_or_default()
                                        .to_string(),
                                ),
                        )
                    })
                    .child(
                        div()
                            .flex_1()
                            .min_h_0()
                            .flex()
                            .when(!dock || !narrow, |el| {
                                el.child(
                                    div()
                                        .flex_1()
                                        .min_w_0()
                                        .min_h_0()
                                        .h_full()
                                        .flex()
                                        .flex_col()
                                        .when(self.navigation == 3, |el| {
                                            el.child(self.project_workspace(width < 1050., cx))
                                        })
                                        .when(self.navigation == 0, |el| {
                                            el.child(self.detail(window, cx))
                                        })
                                        .when(self.navigation == 5, |el| {
                                            el.child(self.workers_view(cx))
                                        })
                                        .when(self.show_local_execution_onboarding(), |el| {
                                            el.child(
                                                div()
                                                    .id("first-run-setup")
                                                    .flex_1()
                                                    .min_h_0()
                                                    .overflow_y_scroll()
                                                    .child(
                                                        self.local_execution_setup_view(true, cx),
                                                    ),
                                            )
                                        })
                                        .when(
                                            self.navigation == 1
                                                && !self.show_local_execution_onboarding(),
                                            |el| el.child(self.projects_view(cx)),
                                        )
                                        .when(self.navigation == 2, |el| {
                                            el.child(self.environments(cx))
                                        })
                                        .when(self.navigation == 7, |el| {
                                            el.child(self.apps_view(cx))
                                        })
                                        .when(self.navigation == 8, |el| {
                                            el.child(self.insights_view(cx))
                                        })
                                        .when(self.navigation == 9, |el| {
                                            el.child(self.remote_connection_view(cx))
                                        })
                                        .when(self.navigation == 10, |el| {
                                            el.child(self.general_settings(cx))
                                        }),
                                )
                            })
                            .when(dock, |el| {
                                el.child(self.chat_dock(chat_width, narrow, window, cx))
                            }),
                    ),
            )
    }
}
