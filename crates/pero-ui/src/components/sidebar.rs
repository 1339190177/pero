use super::{ActionHandler, Ui, click};
use gpui_kit::{
    component::{
        button::*,
        input::{Input, InputState},
        *,
    },
    prelude::*,
    *,
};

pub(crate) struct RecentRow {
    pub label: String,
    pub icon: &'static str,
    pub active: bool,
    pub busy: bool,
    pub attention: bool,
    pub action: SidebarAction,
}
pub(crate) struct ProjectGroup {
    pub key: String,
    pub name: String,
    pub path: String,
    pub active: bool,
    pub collapsed: bool,
    pub rows: Vec<RecentRow>,
}
pub(crate) struct SidebarProps<'a> {
    pub navigation: usize,
    pub local_connected: bool,
    pub groups: Vec<ProjectGroup>,
    pub ungrouped: Vec<RecentRow>,
    pub search: &'a Entity<InputState>,
}
#[derive(Clone)]
pub(crate) enum SidebarAction {
    NewChat,
    Navigate(usize),
    OpenProject(String),
    ToggleProject(String),
    SelectSession(usize),
    SelectTask(String),
}

fn recent_row(
    ui: Ui<'_>,
    row: RecentRow,
    index: usize,
    on_action: &ActionHandler<SidebarAction>,
) -> impl IntoElement {
    let label = if row.attention {
        format!("{} · {}", row.label, ui.t("insights.attention"))
    } else {
        row.label.clone()
    };
    Button::new(("recent-work", index))
        .ghost()
        .small()
        .w_full()
        .h(px(34.))
        .justify_start()
        .child(div().flex_1())
        .toggled(row.active)
        .icon(Icon::default().path(format!(
            "icons/{}.svg",
            if row.attention {
                "alert-circle"
            } else if row.busy {
                "clock"
            } else {
                row.icon
            }
        )))
        .label(row.label)
        .accessibility_label(label)
        .on_click(click(on_action, row.action))
}

pub(crate) fn sidebar(
    ui: Ui<'_>,
    width: f32,
    props: SidebarProps<'_>,
    settings: AnyElement,
    on_action: ActionHandler<SidebarAction>,
) -> Stateful<Div> {
    let p = ui.palette;
    let mut index = 0;
    div()
        .id("sidebar")
        .w(px(width))
        .h_full()
        .flex_shrink_0()
        .flex()
        .flex_col()
        .bg(p.color("bg"))
        .border_r_1()
        .border_color(p.color("border"))
        .p(px(10.))
        .gap(px(4.))
        .child(
            Button::new("sidebar-new-chat")
                .primary()
                .w_full()
                .h(px(38.))
                .justify_start()
                .child(div().flex_1())
                .icon(IconName::Plus)
                .label(ui.t("chat.new"))
                .on_click(click(&on_action, SidebarAction::NewChat)),
        )
        .child(
            Button::new("sidebar-projects")
                .ghost()
                .w_full()
                .h(px(36.))
                .justify_start()
                .child(div().flex_1())
                .icon(Icon::default().path("icons/folder.svg"))
                .label(ui.t("nav.projects"))
                .toggled(matches!(props.navigation, 1 | 3))
                .on_click(click(&on_action, SidebarAction::Navigate(1))),
        )
        .child(
            div()
                .py(px(6.))
                .child(Input::new(props.search).prefix(ui.icon("search")).w_full()),
        )
        .child(
            div()
                .id("sidebar-project-scroll")
                .flex_1()
                .min_h_0()
                .overflow_y_scroll()
                .children(
                    props
                        .groups
                        .into_iter()
                        .enumerate()
                        .map(|(group_index, group)| {
                            let count = group.rows.len();
                            let rows = group
                                .rows
                                .into_iter()
                                .map(|row| {
                                    index += 1;
                                    recent_row(ui, row, index, &on_action)
                                })
                                .collect::<Vec<_>>();
                            div()
                                .mb(px(8.))
                                .child(
                                    div()
                                        .flex()
                                        .items_center()
                                        .gap(px(2.))
                                        .child(
                                            Button::new(("project-disclosure", group_index))
                                                .ghost()
                                                .small()
                                                .w(px(26.))
                                                .h(px(32.))
                                                .icon(Icon::default().path(if group.collapsed {
                                                    "icons/chevron-right.svg"
                                                } else {
                                                    "icons/chevron-down.svg"
                                                }))
                                                .accessibility_label(format!(
                                                    "{} {}",
                                                    ui.t(if group.collapsed {
                                                        "project.expand"
                                                    } else {
                                                        "project.collapse"
                                                    }),
                                                    group.name
                                                ))
                                                .on_click(click(
                                                    &on_action,
                                                    SidebarAction::ToggleProject(group.key.clone()),
                                                )),
                                        )
                                        .child(
                                            Button::new(("sidebar-project", group_index))
                                                .ghost()
                                                .small()
                                                .flex_1()
                                                .min_w_0()
                                                .h(px(32.))
                                                .justify_start()
                                                .child(div().flex_1())
                                                .label(group.name)
                                                .tooltip(group.path)
                                                .toggled(group.active)
                                                .on_click(click(
                                                    &on_action,
                                                    SidebarAction::OpenProject(group.key),
                                                )),
                                        ),
                                )
                                .when(!group.collapsed, |el| {
                                    el.child(
                                        div().pl(px(22.)).flex().flex_col().children(rows).when(
                                            count == 0,
                                            |el| {
                                                el.child(
                                                    div()
                                                        .px(px(8.))
                                                        .py(px(8.))
                                                        .text_size(px(12.))
                                                        .text_color(p.color("text-muted"))
                                                        .child(ui.t("project.no_recent")),
                                                )
                                            },
                                        ),
                                    )
                                })
                        }),
                )
                .when(!props.ungrouped.is_empty(), |el| {
                    el.child(
                        div()
                            .pt(px(8.))
                            .child(
                                div()
                                    .px(px(8.))
                                    .py(px(5.))
                                    .text_size(px(12.))
                                    .text_color(p.color("text-muted"))
                                    .child(ui.t("project.unassigned")),
                            )
                            .children(
                                props
                                    .ungrouped
                                    .into_iter()
                                    .enumerate()
                                    .map(|(i, row)| recent_row(ui, row, 100000 + i, &on_action)),
                            ),
                    )
                }),
        )
        .child(
            div()
                .pt(px(8.))
                .border_t_1()
                .border_color(p.color("border"))
                .flex()
                .flex_col()
                .gap(px(2.))
                .child(
                    Button::new("sidebar-insights")
                        .ghost()
                        .w_full()
                        .h(px(36.))
                        .justify_start()
                        .child(div().flex_1())
                        .icon(Icon::default().path("icons/layout-list.svg"))
                        .label(ui.t("nav.insights"))
                        .toggled(props.navigation == 8)
                        .on_click(click(&on_action, SidebarAction::Navigate(8))),
                )
                .child(
                    Button::new("sidebar-apps")
                        .ghost()
                        .w_full()
                        .h(px(36.))
                        .justify_start()
                        .child(div().flex_1())
                        .icon(Icon::default().path("icons/file-code.svg"))
                        .label(ui.t("nav.apps"))
                        .toggled(props.navigation == 7)
                        .on_click(click(&on_action, SidebarAction::Navigate(7))),
                ),
        )
        .when(!props.local_connected, |el| {
            el.child(
                div()
                    .px(px(8.))
                    .py(px(6.))
                    .text_size(px(12.))
                    .text_color(p.color("warning"))
                    .child(ui.t("status.disconnected")),
            )
        })
        .child(
            div()
                .pt(px(8.))
                .border_t_1()
                .border_color(p.color("border"))
                .child(settings),
        )
}
