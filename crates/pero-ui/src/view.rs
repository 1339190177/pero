use crate::app::Workbench;
use crate::components::{
    ActionHandler, Ui,
    sidebar::{self, SidebarAction, SidebarProps},
    workspace::{self, AgentRow, WorkspaceAction},
};
use crate::components::{
    examples::TASKS,
    task_list::{self, SessionRow, TaskListAction, TaskListProps},
};
use gpui_kit::{
    component::{button::*, *},
    prelude::*,
    *,
};
use serde_json::json;
use std::rc::Rc;

impl Workbench {
    pub fn icon(&self, name: &str) -> Svg {
        self.ui().icon(name)
    }
    fn badge(&self, label: impl Into<SharedString>, kind: &str) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        let (bg, fg) = match kind {
            "success" => ("success-bg", "success"),
            "warning" => ("warning-bg", "warning"),
            "info" => ("info-bg", "info"),
            _ => ("surface-muted", "text-muted"),
        };
        div()
            .px(px(10.))
            .py(px(3.))
            .rounded(px(6.))
            .text_size(px(12.))
            .font_weight(FontWeight::BOLD)
            .bg(p.color(bg))
            .text_color(p.color(fg))
            .child(label.into())
    }
    fn sidebar(&self, width: f32, cx: &mut Context<Self>) -> Div {
        let on_action = Rc::new(cx.listener(|this, action: &SidebarAction, _, cx| {
            match action {
                SidebarAction::Navigate(index) => {
                    this.navigation = *index;
                    this.sync_follow(cx);
                }
                SidebarAction::OpenRemote => this.open_remote(cx),
                SidebarAction::ChooseWorkspace => {
                    this.command(json!({"action":"choose_workspace"}), cx)
                }
                SidebarAction::ToggleLocale => this.toggle_locale(cx),
                SidebarAction::ToggleTheme => {
                    this.dark = !this.dark;
                    this.palettes.apply(this.dark, cx);
                }
            }
            cx.notify();
        }));
        sidebar::sidebar(
            self.ui(),
            width,
            SidebarProps {
                navigation: self.navigation,
                connected: self.connected,
                dark: self.dark,
            },
            on_action,
        )
    }
    fn task_list(&self, width: f32, cx: &mut Context<Self>) -> Div {
        let sessions: Vec<_> = self
            .chats
            .iter()
            .map(|chat| SessionRow {
                title: chat.title.clone(),
                agent: chat.agent.clone(),
                busy: chat.busy(),
            })
            .collect();
        let on_action = Rc::new(cx.listener(|this, action: &TaskListAction, _, cx| {
            match action {
                TaskListAction::Filter(filter) => this.filter = *filter,
                TaskListAction::SelectSession(index) => this.focus_chat(Some(*index), 4, cx),
                TaskListAction::SelectExample(index) => {
                    this.selected = *index;
                    this.focus_chat(None, 0, cx);
                }
            }
            cx.notify();
        }));
        task_list::task_list(
            self.ui(),
            width,
            TaskListProps {
                search: &self.search,
                query: self.search.read(cx).value().to_lowercase(),
                sessions: &sessions,
                examples: &TASKS,
                active_chat: self.active_chat,
                selected: self.selected,
                filter: self.filter,
            },
            on_action,
        )
    }
    fn overview(&self) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        let ready = self.selected == 0;
        div()
            .flex()
            .flex_col()
            .gap(px(12.))
            .child(
                div()
                    .p(px(16.))
                    .rounded(px(7.))
                    .bg(p.color(if ready { "success-bg" } else { "info-bg" }))
                    .flex()
                    .items_center()
                    .gap(px(12.))
                    .child(
                        self.icon(if ready {
                            "circle-check-filled"
                        } else {
                            "clock"
                        })
                        .size(px(46.))
                        .text_color(p.color(if ready {
                            "success"
                        } else {
                            "info"
                        })),
                    )
                    .child(
                        div()
                            .flex()
                            .flex_col()
                            .gap(px(8.))
                            .child(
                                div()
                                    .text_size(px(18.))
                                    .font_weight(FontWeight::BOLD)
                                    .child(self.t(if ready {
                                        "overview.ready"
                                    } else {
                                        "overview.sample"
                                    })),
                            )
                            .child(div().text_color(p.color("text-muted")).child(self.t(
                                if ready {
                                    "overview.summary"
                                } else {
                                    "task.example.description"
                                },
                            ))),
                    ),
            )
            .child(
                div()
                    .border_1()
                    .border_color(p.color("border"))
                    .rounded(px(6.))
                    .px(px(22.))
                    .py(px(12.))
                    .children(
                        [
                            ("overview.typecheck", "status.passed"),
                            ("overview.tests", "overview.tests_passed"),
                            ("overview.build", "status.passed"),
                        ]
                        .into_iter()
                        .enumerate()
                        .map(|(i, (label, value))| {
                            div()
                                .flex()
                                .items_center()
                                .gap(px(14.))
                                .py(px(12.))
                                .when(i < 2, |el| el.border_b_1().border_color(p.color("border")))
                                .child(
                                    self.icon("circle-check-filled")
                                        .size(px(24.))
                                        .text_color(p.color("success")),
                                )
                                .child(self.t(label))
                                .child(div().flex_1())
                                .child(div().text_color(p.color("success")).child(self.t(value)))
                        }),
                    ),
            )
            .child(
                div()
                    .border_1()
                    .border_color(p.color("border"))
                    .rounded(px(6.))
                    .p(px(16.))
                    .child(
                        div()
                            .font_weight(FontWeight::BOLD)
                            .mb(px(8.))
                            .child(self.t("overview.changes")),
                    )
                    .children(
                        [
                            ("src/orders/cancel.ts", "+24", "−8"),
                            ("src/orders/cancel.test.ts", "+21", "−0"),
                            ("src/orders/types.ts", "+2", "−4"),
                        ]
                        .into_iter()
                        .enumerate()
                        .map(|(i, (file, plus, minus))| {
                            div()
                                .py(px(10.))
                                .flex()
                                .items_center()
                                .gap(px(12.))
                                .when(i < 2, |el| el.border_b_1().border_color(p.color("border")))
                                .child(self.icon("file-code"))
                                .child(div().text_size(px(13.)).child(file))
                                .child(div().flex_1())
                                .child(div().text_color(p.color("success")).child(plus))
                                .child(div().ml(px(12.)).text_color(p.color("error")).child(minus))
                        }),
                    ),
            )
            .child(
                div()
                    .border_1()
                    .border_color(p.color("border"))
                    .rounded(px(6.))
                    .p(px(16.))
                    .child(
                        div()
                            .font_weight(FontWeight::BOLD)
                            .mb(px(24.))
                            .child(self.t("overview.process")),
                    )
                    .child(
                        div().flex().justify_between().children(
                            [
                                ("overview.prepare", "3", "10:12"),
                                ("overview.code", "6", "10:15"),
                                ("overview.verify", "4", "10:21"),
                                ("overview.pr", "2", "10:23"),
                            ]
                            .into_iter()
                            .map(|(label, minutes, time)| {
                                div()
                                    .flex()
                                    .flex_col()
                                    .gap(px(9.))
                                    .child(
                                        self.icon("circle-check-filled")
                                            .size(px(28.))
                                            .text_color(p.color("success")),
                                    )
                                    .child(
                                        div()
                                            .text_size(px(12.))
                                            .font_weight(FontWeight::BOLD)
                                            .child(self.t(label)),
                                    )
                                    .child(
                                        div()
                                            .text_size(px(11.))
                                            .text_color(p.color("text-muted"))
                                            .child(format!(
                                                "{} {}",
                                                minutes,
                                                self.t("time.minutes")
                                            )),
                                    )
                                    .child(
                                        div()
                                            .text_size(px(11.))
                                            .text_color(p.color("text-muted"))
                                            .child(time),
                                    )
                            }),
                        ),
                    )
                    .child(
                        div()
                            .mt(px(24.))
                            .pt(px(14.))
                            .border_t_1()
                            .border_color(p.color("border"))
                            .text_size(px(11.))
                            .text_color(p.color("text-muted"))
                            .child(self.t("overview.sample_footer")),
                    ),
            )
    }
    fn detail(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        let chat = self.active_chat.and_then(|i| self.chats.get(i));
        div()
            .flex_1()
            .min_w_0()
            .h_full()
            .flex()
            .flex_col()
            .when(chat.is_some(), |el| {
                el.child(
                    div()
                        .h(px(44.))
                        .px(px(16.))
                        .flex_shrink_0()
                        .flex()
                        .items_center()
                        .gap(px(10.))
                        .border_b_1()
                        .border_color(p.color("border"))
                        .child(self.icon("terminal"))
                        .child(
                            div()
                                .flex_1()
                                .truncate()
                                .font_weight(FontWeight::BOLD)
                                .child(chat.map(|c| c.title.clone()).unwrap_or_default()),
                        )
                        .child(
                            self.badge(chat.map(|c| c.agent.clone()).unwrap_or_default(), "muted"),
                        )
                        .child(
                            Button::new("close-chat")
                                .ghost()
                                .small()
                                .label(self.t("chat.close"))
                                .disabled(chat.is_none_or(|c| c.state == "closed"))
                                .on_click(cx.listener(|this, _, _, cx| {
                                    if let Some(i) = this.active_chat {
                                        let id = this.chats[i].id.clone();
                                        this.command(json!({"action":"close","thread_id":id}), cx);
                                    }
                                })),
                        )
                        .child(
                            Button::new("new-task")
                                .ghost()
                                .small()
                                .icon(IconName::Plus)
                                .label(self.t("chat.new"))
                                .on_click(cx.listener(|this, _, _, cx| this.new_chat(cx))),
                        ),
                )
            })
            .when(chat.is_none(), |el| {
                el.child(
                    div()
                        .h(px(54.))
                        .flex_shrink_0()
                        .px(px(20.))
                        .pt(px(14.))
                        .flex()
                        .justify_end()
                        .child(
                            Button::new("new-task")
                                .primary()
                                .small()
                                .icon(IconName::Plus)
                                .label(self.t("chat.new_task"))
                                .on_click(cx.listener(|this, _, _, cx| this.new_chat(cx))),
                        ),
                )
                .child(
                    div()
                        .px(px(24.))
                        .pt(px(8.))
                        .pb(px(12.))
                        .child(
                            div()
                                .flex()
                                .items_center()
                                .gap(px(8.))
                                .text_size(px(11.))
                                .text_color(p.color("text-muted"))
                                .child(format!(
                                    "{} / T-{}",
                                    TASKS[self.selected].repository,
                                    1042 + self.selected
                                ))
                                .child(self.badge(self.t("task.example"), "muted")),
                        )
                        .child(
                            div()
                                .mt(px(8.))
                                .flex()
                                .items_center()
                                .gap(px(12.))
                                .child(
                                    div()
                                        .text_size(px(20.))
                                        .font_weight(FontWeight::BOLD)
                                        .child(self.t(TASKS[self.selected].title_key)),
                                )
                                .child(self.badge(
                                    self.t(TASKS[self.selected].status_key),
                                    if self.selected == 0 {
                                        "success"
                                    } else {
                                        "info"
                                    },
                                )),
                        )
                        .child(
                            div()
                                .mt(px(6.))
                                .text_size(px(12.))
                                .text_color(p.color("text-muted"))
                                .child(self.t("overview.description")),
                        ),
                )
            })
            .child(
                div()
                    .mx(px(16.))
                    .flex()
                    .gap(px(14.))
                    .border_b_1()
                    .border_color(p.color("border"))
                    .children(
                        [
                            "tab.overview",
                            "tab.changes",
                            "tab.verification",
                            "tab.logs",
                            "chat.session",
                        ]
                        .into_iter()
                        .enumerate()
                        .map(|(i, key)| {
                            div()
                                .id(("detail-tab", i))
                                .h(px(34.))
                                .flex()
                                .items_center()
                                .px(px(6.))
                                .cursor_pointer()
                                .border_b_2()
                                .border_color(p.color(if self.tab == i {
                                    "action"
                                } else {
                                    "surface"
                                }))
                                .text_color(p.color(if self.tab == i {
                                    "accent-text"
                                } else {
                                    "text-muted"
                                }))
                                .child(self.t(key))
                                .on_click(cx.listener(move |this, _, _, cx| {
                                    this.focus_chat(this.active_chat, i, cx);
                                }))
                        }),
                    ),
            )
            .when(self.tab == 4, |el| el.child(self.chat_view(cx)))
            .when(self.tab != 4, |el| {
                el.child(
                    div()
                        .id("detail-content")
                        .flex_1()
                        .min_h_0()
                        .overflow_y_scroll()
                        .p(px(20.))
                        .when(self.tab == 0 && chat.is_none(), |el| {
                            el.child(self.overview())
                        })
                        .when(self.tab == 0 && chat.is_some(), |el| {
                            el.child(self.t("chat.open_tab"))
                        })
                        .when(self.tab == 1, |el| el.child(self.t("chat.no_changes")))
                        .when(self.tab == 2, |el| el.child(self.t("chat.no_verification")))
                        .when(self.tab == 3, |el| {
                            el.children(self.events.iter().map(|e| {
                                div()
                                    .py(px(8.))
                                    .border_b_1()
                                    .border_color(p.color("border"))
                                    .child(self.event_text(e))
                            }))
                        }),
                )
            })
    }
    fn ui(&self) -> Ui<'_> {
        Ui {
            palette: self.palettes.get(self.dark),
            locale: &self.locale,
        }
    }
    fn workspace_actions(&self, cx: &mut Context<Self>) -> ActionHandler<WorkspaceAction> {
        Rc::new(
            cx.listener(|this, action: &WorkspaceAction, _, cx| match action {
                WorkspaceAction::ChooseWorkspace => {
                    this.command(json!({"action":"choose_workspace"}), cx)
                }
                WorkspaceAction::NewSession(index) => {
                    this.agent_index = *index;
                    this.new_chat(cx);
                }
            }),
        )
    }
    fn projects(&self, cx: &mut Context<Self>) -> Div {
        workspace::projects(self.ui(), &self.workspace, self.workspace_actions(cx))
    }
    fn environments(&self, cx: &mut Context<Self>) -> Div {
        let agents: Vec<_> = self
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
        if self.input_locale != self.locale {
            self.input_locale = self.locale.clone();
            let placeholder = self.t("chat.placeholder");
            self.composer.update(cx, |input, cx| {
                input.set_placeholder(placeholder, window, cx)
            });
            let placeholder = self.t("tasks.search");
            self.search.update(cx, |input, cx| {
                input.set_placeholder(placeholder, window, cx)
            });
            let placeholder = self.t("input.placeholder");
            self.input.update(cx, |input, cx| {
                input.set_placeholder(placeholder, window, cx)
            });
        }
        let p = self.palettes.get(self.dark);
        let width: f32 = window.viewport_size().width.into();
        let compact = width < 1150.;
        div()
            .size_full()
            .flex()
            .bg(p.color("surface"))
            .text_color(p.color("text"))
            .font_family("Noto Sans CJK SC")
            .text_size(px(13.))
            .child(self.sidebar(if compact { 168. } else { 192. }, cx))
            .when(self.navigation == 3, |el| {
                el.child(self.remote_view(width < 1050., cx))
            })
            .when(self.navigation == 0, |el| {
                el.child(self.task_list(if compact { 300. } else { 360. }, cx))
                    .child(self.detail(cx))
            })
            .when(self.navigation == 1, |el| el.child(self.projects(cx)))
            .when(self.navigation == 2, |el| el.child(self.environments(cx)))
    }
}
