use super::{ActionHandler, Ui, click};
use gpui_kit::{
    component::{button::*, *},
    prelude::*,
    *,
};

use super::examples::ExampleTask;
pub(crate) struct SessionHeader {
    pub title: String,
    pub agent: String,
    pub closed: bool,
}
pub(crate) struct TaskDetailProps<'a> {
    pub session: Option<SessionHeader>,
    pub example: &'a ExampleTask,
    pub example_id: usize,
    pub tab: usize,
}
pub(crate) enum TaskDetailAction {
    NewSession,
    CloseSession,
    SelectTab(usize),
}
pub(crate) fn task_detail(
    ui: Ui<'_>,
    props: TaskDetailProps<'_>,
    content: AnyElement,
    on_action: ActionHandler<TaskDetailAction>,
) -> Div {
    let p = ui.palette;
    let chat = props.session.as_ref();
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
                    .child(ui.icon("terminal"))
                    .child(
                        div()
                            .flex_1()
                            .truncate()
                            .font_weight(FontWeight::BOLD)
                            .child(chat.map(|c| c.title.clone()).unwrap_or_default()),
                    )
                    .child(ui.badge(chat.map(|c| c.agent.clone()).unwrap_or_default(), "muted"))
                    .child(
                        Button::new("close-chat")
                            .ghost()
                            .small()
                            .label(ui.t("chat.close"))
                            .disabled(chat.is_none_or(|c| c.closed))
                            .on_click(click(&on_action, TaskDetailAction::CloseSession)),
                    )
                    .child(
                        Button::new("new-task")
                            .ghost()
                            .small()
                            .icon(IconName::Plus)
                            .label(ui.t("chat.new"))
                            .on_click(click(&on_action, TaskDetailAction::NewSession)),
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
                            .label(ui.t("chat.new_task"))
                            .on_click(click(&on_action, TaskDetailAction::NewSession)),
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
                                props.example.repository, props.example_id
                            ))
                            .child(ui.badge(ui.t("task.example"), "muted")),
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
                                    .child(ui.t(props.example.title_key)),
                            )
                            .child(ui.badge(
                                ui.t(props.example.status_key),
                                if props.example.ready {
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
                            .child(ui.t("overview.description")),
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
                            .border_color(p.color(if props.tab == i {
                                "action"
                            } else {
                                "surface"
                            }))
                            .text_color(p.color(if props.tab == i {
                                "accent-text"
                            } else {
                                "text-muted"
                            }))
                            .child(ui.t(key))
                            .on_click(click(&on_action, TaskDetailAction::SelectTab(i)))
                    }),
                ),
        )
        .child(content)
}
