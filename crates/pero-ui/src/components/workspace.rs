use super::{ActionHandler, Ui, click};
use gpui_kit::{
    component::{Disableable, button::*},
    prelude::*,
    *,
};

pub(crate) struct AgentRow {
    pub name: String,
    pub available: bool,
}
pub(crate) enum WorkspaceAction {
    ChooseWorkspace,
    NewSession(usize),
}
pub(crate) fn projects(
    ui: Ui<'_>,
    workspace: &str,
    on_action: ActionHandler<WorkspaceAction>,
) -> Div {
    let p = ui.palette;
    div()
        .p(px(32.))
        .flex_1()
        .child(
            div()
                .text_size(px(28.))
                .font_weight(FontWeight::BOLD)
                .child(ui.t("nav.projects")),
        )
        .child(
            div()
                .mt(px(28.))
                .text_color(p.color("text-muted"))
                .child(workspace.to_string()),
        )
        .child(
            Button::new("open-workspace")
                .outline()
                .label(ui.t("chat.workspace"))
                .on_click(click(&on_action, WorkspaceAction::ChooseWorkspace)),
        )
}
pub(crate) fn environments(
    ui: Ui<'_>,
    agents: &[AgentRow],
    on_action: ActionHandler<WorkspaceAction>,
) -> Div {
    let p = ui.palette;
    div()
        .p(px(32.))
        .flex_1()
        .child(
            div()
                .text_size(px(28.))
                .font_weight(FontWeight::BOLD)
                .child(ui.t("nav.environments")),
        )
        .child(
            div()
                .mt(px(12.))
                .text_color(p.color("text-muted"))
                .child(ui.t("agents.description")),
        )
        .children(agents.iter().enumerate().map(|(i, a)| {
            div()
                .py(px(24.))
                .border_b_1()
                .border_color(p.color("border"))
                .flex()
                .items_center()
                .gap(px(16.))
                .child(ui.icon("terminal"))
                .child(a.name.clone())
                .child(div().flex_1())
                .child(ui.badge(
                    ui.t(if a.available {
                        "agents.installed"
                    } else {
                        "agents.missing"
                    }),
                    if a.available { "success" } else { "warning" },
                ))
                .child(
                    Button::new(("connect-agent", i))
                        .outline()
                        .label(ui.t("chat.new"))
                        .disabled(!a.available)
                        .on_click(click(&on_action, WorkspaceAction::NewSession(i))),
                )
        }))
}
