use crate::app::Workbench;
use gpui_kit::{
    component::{button::*, input::Input, *},
    prelude::*,
    *,
};
use serde_json::json;

const TASKS: [(&str, &str, &str, &str, &str); 5] = [
    (
        "task.order",
        "order-service",
        "status.review",
        "time.eight",
        "",
    ),
    (
        "task.export",
        "api-gateway",
        "status.testing",
        "time.twelve",
        "task.testing",
    ),
    (
        "task.search",
        "search-service",
        "status.coding",
        "time.twenty_eight",
        "task.coding",
    ),
    (
        "task.errors",
        "core-service",
        "status.blocked",
        "time.hour",
        "task.needs_input",
    ),
    (
        "task.dependencies",
        "web-console",
        "status.queued",
        "time.two_hours",
        "task.queued",
    ),
];
impl Workbench {
    pub fn icon(&self, name: &str) -> Svg {
        svg()
            .path(format!("icons/{name}.svg"))
            .size(px(19.))
            .flex_shrink_0()
            .text_color(self.palettes.get(self.dark).color("text-muted"))
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
    fn sidebar(&self, width: f32, cx: &mut Context<Self>) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        div()
            .w(px(width))
            .h_full()
            .flex_shrink_0()
            .flex()
            .flex_col()
            .bg(p.color("bg"))
            .border_r_1()
            .border_color(p.color("border"))
            .px(px(12.))
            .py(px(16.))
            .gap(px(4.))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(10.))
                    .px(px(6.))
                    .mb(px(8.))
                    .child(self.icon("circle-filled").text_color(p.color("action")))
                    .child(
                        div()
                            .text_size(px(18.))
                            .font_weight(FontWeight::BOLD)
                            .child(self.t("workbench.title")),
                    ),
            )
            .child(
                Button::new("workspace-label")
                    .outline()
                    .label(self.t("workspace.local"))
                    .w_full()
                    .h(px(32.))
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.command(json!({"action":"choose_workspace"}), cx)
                    })),
            )
            .children(
                [
                    ("layout-list", "nav.tasks"),
                    ("folder", "nav.projects"),
                    ("devices", "nav.environments"),
                ]
                .into_iter()
                .enumerate()
                .map(|(i, (icon, label))| {
                    div()
                        .id(("nav", i))
                        .flex()
                        .items_center()
                        .gap(px(4.))
                        .px(px(12.))
                        .h(px(32.))
                        .rounded(px(6.))
                        .cursor_pointer()
                        .bg(p.color(if self.navigation == i {
                            "accent-soft"
                        } else {
                            "bg"
                        }))
                        .text_color(p.color(if self.navigation == i {
                            "accent-text"
                        } else {
                            "text-muted"
                        }))
                        .child(self.icon(icon).text_color(p.color(if self.navigation == i {
                            "accent-text"
                        } else {
                            "text-muted"
                        })))
                        .child(
                            div()
                                .font_weight(if self.navigation == i {
                                    FontWeight::BOLD
                                } else {
                                    FontWeight::NORMAL
                                })
                                .child(self.t(label)),
                        )
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.navigation = i;
                            this.sync_follow(cx);
                            cx.notify();
                        }))
                }),
            )
            .child(
                Button::new("nav-remote")
                    .ghost()
                    .label(self.t("nav.remote"))
                    .w_full()
                    .h(px(32.))
                    .bg(p.color(if self.navigation == 3 {
                        "accent-soft"
                    } else {
                        "bg"
                    }))
                    .on_click(cx.listener(|this, _, _, cx| this.open_remote(cx))),
            )
            .child(div().flex_1())
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(4.))
                    .child(
                        Button::new("language")
                            .ghost()
                            .small()
                            .w(px(32.))
                            .h(px(32.))
                            .icon(Icon::default().path("icons/languages.svg"))
                            .tooltip(format!("{} · 中文 / English", self.t("menu.language")))
                            .accessibility_label(self.t("menu.language"))
                            .on_click(cx.listener(|this, _, _, cx| this.toggle_locale(cx))),
                    )
                    .child(
                        Button::new("theme")
                            .ghost()
                            .small()
                            .w(px(32.))
                            .h(px(32.))
                            .icon(Icon::default().path(if self.dark {
                                "icons/sun.svg"
                            } else {
                                "icons/moon.svg"
                            }))
                            .tooltip(self.t(if self.dark {
                                "appearance.light"
                            } else {
                                "appearance.dark"
                            }))
                            .accessibility_label(self.t(if self.dark {
                                "appearance.light"
                            } else {
                                "appearance.dark"
                            }))
                            .on_click(cx.listener(|this, _, _, cx| {
                                this.dark = !this.dark;
                                this.palettes.apply(this.dark, cx);
                                cx.notify();
                            })),
                    ),
            )
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(9.))
                    .px(px(8.))
                    .text_size(px(12.))
                    .text_color(p.color("text-muted"))
                    .child(self.icon("brand-github"))
                    .child(self.t("github.unconnected")),
            )
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(9.))
                    .px(px(8.))
                    .text_size(px(12.))
                    .text_color(p.color("text-muted"))
                    .child(self.icon("devices"))
                    .child(self.t(if self.connected {
                        "status.local_connected"
                    } else {
                        "status.disconnected"
                    })),
            )
    }
    fn task_list(&self, width: f32, cx: &mut Context<Self>) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        let query = self.search.read(cx).value().to_lowercase();
        div()
            .w(px(width))
            .h_full()
            .flex_shrink_0()
            .flex()
            .flex_col()
            .border_r_1()
            .border_color(p.color("border"))
            .child(
                div()
                    .px(px(14.))
                    .pt(px(18.))
                    .child(
                        div()
                            .text_size(px(22.))
                            .font_weight(FontWeight::BOLD)
                            .child(self.t("nav.tasks")),
                    )
                    .child(
                        div()
                            .mt(px(5.))
                            .text_size(px(14.))
                            .text_color(p.color("text-muted"))
                            .child(self.t("tasks.subtitle")),
                    )
                    .child(
                        div()
                            .mt(px(16.))
                            .flex()
                            .items_center()
                            .gap(px(12.))
                            .child(
                                Button::new("all-projects")
                                    .outline()
                                    .label(self.t("tasks.projects"))
                                    .on_click(cx.listener(|this, _, _, cx| {
                                        this.filter = 0;
                                        cx.notify();
                                    })),
                            )
                            .child(
                                Input::new(&self.search)
                                    .prefix(self.icon("search"))
                                    .w_full(),
                            ),
                    )
                    .child(
                        div().mt(px(12.)).flex().justify_between().children(
                            [
                                "filter.all",
                                "filter.running",
                                "filter.blocked",
                                "filter.review",
                            ]
                            .into_iter()
                            .enumerate()
                            .map(|(i, key)| {
                                Button::new(("filter", i))
                                    .ghost()
                                    .label(self.t(key))
                                    .text_color(p.color(if self.filter == i {
                                        "accent-text"
                                    } else {
                                        "text-muted"
                                    }))
                                    .on_click(cx.listener(move |this, _, _, cx| {
                                        this.filter = i;
                                        cx.notify();
                                    }))
                            }),
                        ),
                    ),
            )
            .child(
                div()
                    .id("task-list")
                    .flex_1()
                    .min_h_0()
                    .overflow_y_scroll()
                    .border_t_1()
                    .border_color(p.color("border"))
                    .children(
                        self.chats
                            .iter()
                            .enumerate()
                            .filter(|(_, c)| {
                                query.is_empty() || c.title.to_lowercase().contains(&query)
                            })
                            .map(|(i, chat)| {
                                div()
                                    .id(("chat-row", i))
                                    .px(px(14.))
                                    .py(px(10.))
                                    .border_b_1()
                                    .border_color(p.color("border"))
                                    .cursor_pointer()
                                    .bg(p.color(if self.active_chat == Some(i) {
                                        "accent-soft"
                                    } else {
                                        "surface"
                                    }))
                                    .child(
                                        div()
                                            .font_weight(FontWeight::BOLD)
                                            .child(chat.title.clone()),
                                    )
                                    .child(
                                        div()
                                            .mt(px(6.))
                                            .flex()
                                            .items_center()
                                            .gap(px(10.))
                                            .text_size(px(12.))
                                            .text_color(p.color("text-muted"))
                                            .child(self.icon("terminal"))
                                            .child(chat.agent.clone())
                                            .child(self.badge(
                                                self.t(if chat.busy() {
                                                    "filter.running"
                                                } else {
                                                    "chat.session"
                                                }),
                                                "info",
                                            )),
                                    )
                                    .on_click(cx.listener(move |this, _, _, cx| {
                                        this.focus_chat(Some(i), 4, cx);
                                    }))
                            }),
                    )
                    .children(
                        TASKS
                            .into_iter()
                            .enumerate()
                            .filter(|(i, t)| {
                                let state_match = match self.filter {
                                    1 => *i == 1 || *i == 2,
                                    2 => *i == 3,
                                    3 => *i == 0,
                                    _ => true,
                                };
                                state_match
                                    && (query.is_empty()
                                        || self.t(t.0).to_lowercase().contains(&query)
                                        || t.1.contains(&query))
                            })
                            .map(|(i, (title, repo, status, time, note))| {
                                div()
                                    .id(("task", i))
                                    .px(px(14.))
                                    .py(px(10.))
                                    .border_b_1()
                                    .border_color(p.color("border"))
                                    .cursor_pointer()
                                    .bg(p.color(
                                        if self.active_chat.is_none() && self.selected == i {
                                            "accent-soft"
                                        } else {
                                            "surface"
                                        },
                                    ))
                                    .hover(|s| s.bg(p.color("surface-muted")))
                                    .child(
                                        div()
                                            .flex()
                                            .justify_between()
                                            .items_center()
                                            .gap(px(8.))
                                            .child(
                                                div()
                                                    .flex_1()
                                                    .min_w_0()
                                                    .truncate()
                                                    .text_size(px(14.))
                                                    .font_weight(FontWeight::BOLD)
                                                    .child(self.t(title)),
                                            )
                                            .child(
                                                div()
                                                    .flex_shrink_0()
                                                    .text_size(px(11.))
                                                    .text_color(p.color("text-muted"))
                                                    .child(self.t(time)),
                                            ),
                                    )
                                    .child(
                                        div()
                                            .mt(px(6.))
                                            .flex()
                                            .items_center()
                                            .gap(px(10.))
                                            .text_size(px(13.))
                                            .text_color(p.color("text-muted"))
                                            .child(self.icon("brand-github"))
                                            .child(repo)
                                            .child(self.badge(
                                                self.t(status),
                                                if i == 0 {
                                                    "success"
                                                } else if i == 3 {
                                                    "warning"
                                                } else if i == 4 {
                                                    "muted"
                                                } else {
                                                    "info"
                                                },
                                            ))
                                            .when(i == 0, |el| el.child("PR #128")),
                                    )
                                    .when(!note.is_empty(), |el| {
                                        el.child(
                                            div()
                                                .mt(px(6.))
                                                .text_size(px(13.))
                                                .text_color(p.color("text-muted"))
                                                .child(self.t(note)),
                                        )
                                    })
                                    .on_click(cx.listener(move |this, _, _, cx| {
                                        this.selected = i;
                                        this.focus_chat(None, 0, cx);
                                    }))
                            }),
                    ),
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
                                    TASKS[self.selected].1,
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
                                        .child(self.t(TASKS[self.selected].0)),
                                )
                                .child(self.badge(
                                    self.t(TASKS[self.selected].2),
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
            .when(self.navigation == 1, |el| {
                el.child(
                    div()
                        .p(px(32.))
                        .flex_1()
                        .child(
                            div()
                                .text_size(px(28.))
                                .font_weight(FontWeight::BOLD)
                                .child(self.t("nav.projects")),
                        )
                        .child(
                            div()
                                .mt(px(28.))
                                .text_color(p.color("text-muted"))
                                .child(self.workspace.clone()),
                        )
                        .child(
                            Button::new("open-workspace")
                                .outline()
                                .label(self.t("chat.workspace"))
                                .on_click(cx.listener(|this, _, _, cx| {
                                    this.command(json!({"action":"choose_workspace"}), cx)
                                })),
                        ),
                )
            })
            .when(self.navigation == 2, |el| {
                el.child(
                    div()
                        .p(px(32.))
                        .flex_1()
                        .child(
                            div()
                                .text_size(px(28.))
                                .font_weight(FontWeight::BOLD)
                                .child(self.t("nav.environments")),
                        )
                        .child(
                            div()
                                .mt(px(12.))
                                .text_color(p.color("text-muted"))
                                .child(self.t("agents.description")),
                        )
                        .children(self.agents.iter().enumerate().map(|(i, a)| {
                            div()
                                .py(px(24.))
                                .border_b_1()
                                .border_color(p.color("border"))
                                .flex()
                                .items_center()
                                .gap(px(16.))
                                .child(self.icon("terminal"))
                                .child(a.name.clone())
                                .child(div().flex_1())
                                .child(self.badge(
                                    self.t(if a.available {
                                        "agents.installed"
                                    } else {
                                        "agents.missing"
                                    }),
                                    if a.available { "success" } else { "warning" },
                                ))
                                .child(
                                    Button::new(("connect-agent", i))
                                        .outline()
                                        .label(self.t("chat.new"))
                                        .disabled(!a.available)
                                        .on_click(cx.listener(move |this, _, _, cx| {
                                            this.agent_index = i;
                                            this.new_chat(cx);
                                        })),
                                )
                        })),
                )
            })
    }
}
