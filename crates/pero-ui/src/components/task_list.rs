use super::{ActionHandler, Ui, click};
use gpui_kit::{component::button::*, prelude::*, *};

use super::examples::ExampleTask;
use gpui_kit::component::input::{Input, InputState};
pub(crate) struct SessionRow {
    pub title: String,
    pub agent: String,
    pub busy: bool,
}
pub(crate) struct TaskListProps<'a> {
    pub search: &'a Entity<InputState>,
    pub query: String,
    pub sessions: &'a [SessionRow],
    pub examples: &'a [ExampleTask],
    pub active_chat: Option<usize>,
    pub selected: usize,
    pub filter: usize,
}
pub(crate) enum TaskListAction {
    Filter(usize),
    SelectSession(usize),
    SelectExample(usize),
}
pub(crate) fn task_list(
    ui: Ui<'_>,
    width: f32,
    props: TaskListProps<'_>,
    on_action: ActionHandler<TaskListAction>,
) -> Div {
    let p = ui.palette;
    let query = &props.query;
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
                        .child(ui.t("nav.tasks")),
                )
                .child(
                    div()
                        .mt(px(5.))
                        .text_size(px(14.))
                        .text_color(p.color("text-muted"))
                        .child(ui.t("tasks.subtitle")),
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
                                .label(ui.t("tasks.projects"))
                                .on_click(click(&on_action, TaskListAction::Filter(0))),
                        )
                        .child(Input::new(props.search).prefix(ui.icon("search")).w_full()),
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
                                .label(ui.t(key))
                                .text_color(p.color(if props.filter == i {
                                    "accent-text"
                                } else {
                                    "text-muted"
                                }))
                                .on_click(click(&on_action, TaskListAction::Filter(i)))
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
                    props
                        .sessions
                        .iter()
                        .enumerate()
                        .filter(|(_, c)| {
                            query.is_empty() || c.title.to_lowercase().contains(query.as_str())
                        })
                        .map(|(i, chat)| {
                            div()
                                .id(("chat-row", i))
                                .px(px(14.))
                                .py(px(10.))
                                .border_b_1()
                                .border_color(p.color("border"))
                                .cursor_pointer()
                                .bg(p.color(if props.active_chat == Some(i) {
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
                                        .child(ui.icon("terminal"))
                                        .child(chat.agent.clone())
                                        .child(ui.badge(
                                            ui.t(if chat.busy {
                                                "filter.running"
                                            } else {
                                                "chat.session"
                                            }),
                                            "info",
                                        )),
                                )
                                .on_click(click(&on_action, TaskListAction::SelectSession(i)))
                        }),
                )
                .children(
                    props
                        .examples
                        .iter()
                        .enumerate()
                        .filter(|(i, t)| {
                            let state_match = match props.filter {
                                1 => *i == 1 || *i == 2,
                                2 => *i == 3,
                                3 => *i == 0,
                                _ => true,
                            };
                            state_match
                                && (query.is_empty()
                                    || ui.t(t.title_key).to_lowercase().contains(query.as_str())
                                    || t.repository.contains(query.as_str()))
                        })
                        .map(|(i, task)| {
                            div()
                                .id(("task", i))
                                .px(px(14.))
                                .py(px(10.))
                                .border_b_1()
                                .border_color(p.color("border"))
                                .cursor_pointer()
                                .bg(p.color(
                                    if props.active_chat.is_none() && props.selected == i {
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
                                                .child(ui.t(task.title_key)),
                                        )
                                        .child(
                                            div()
                                                .flex_shrink_0()
                                                .text_size(px(11.))
                                                .text_color(p.color("text-muted"))
                                                .child(ui.t(task.age_key)),
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
                                        .child(ui.icon("brand-github"))
                                        .child(task.repository)
                                        .child(ui.badge(ui.t(task.status_key), task.badge_kind))
                                        .when(i == 0, |el| el.child("PR #128")),
                                )
                                .when(!task.note_key.is_empty(), |el| {
                                    el.child(
                                        div()
                                            .mt(px(6.))
                                            .text_size(px(13.))
                                            .text_color(p.color("text-muted"))
                                            .child(ui.t(task.note_key)),
                                    )
                                })
                                .on_click(click(&on_action, TaskListAction::SelectExample(i)))
                        }),
                ),
        )
}
