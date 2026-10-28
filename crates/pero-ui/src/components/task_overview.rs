use super::{
    Ui,
    examples::{CHANGED_FILES, CHECKS, EXECUTION_STEPS},
};
use gpui_kit::{prelude::*, *};
// Prototype presentation only; it does not produce verification receipts.
pub(crate) fn example_overview(ui: Ui<'_>, ready: bool) -> Div {
    let p = ui.palette;

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
                    ui.icon(if ready {
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
                                .child(ui.t(if ready {
                                    "overview.ready"
                                } else {
                                    "overview.sample"
                                })),
                        )
                        .child(
                            div()
                                .text_color(p.color("text-muted"))
                                .child(ui.t(if ready {
                                    "overview.summary"
                                } else {
                                    "task.example.description"
                                })),
                        ),
                ),
        )
        .child(
            div()
                .border_1()
                .border_color(p.color("border"))
                .rounded(px(6.))
                .px(px(22.))
                .py(px(12.))
                .children(CHECKS.into_iter().enumerate().map(|(i, (label, value))| {
                    div()
                        .flex()
                        .items_center()
                        .gap(px(14.))
                        .py(px(12.))
                        .when(i < 2, |el| el.border_b_1().border_color(p.color("border")))
                        .child(
                            ui.icon("circle-check-filled")
                                .size(px(24.))
                                .text_color(p.color("success")),
                        )
                        .child(ui.t(label))
                        .child(div().flex_1())
                        .child(div().text_color(p.color("success")).child(ui.t(value)))
                })),
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
                        .child(ui.t("overview.changes")),
                )
                .children(
                    CHANGED_FILES
                        .into_iter()
                        .enumerate()
                        .map(|(i, (file, plus, minus))| {
                            div()
                                .py(px(10.))
                                .flex()
                                .items_center()
                                .gap(px(12.))
                                .when(i < 2, |el| el.border_b_1().border_color(p.color("border")))
                                .child(ui.icon("file-code"))
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
                        .child(ui.t("overview.process")),
                )
                .child(
                    div()
                        .flex()
                        .justify_between()
                        .children(EXECUTION_STEPS.into_iter().map(|(label, minutes, time)| {
                            div()
                                .flex()
                                .flex_col()
                                .gap(px(9.))
                                .child(
                                    ui.icon("circle-check-filled")
                                        .size(px(28.))
                                        .text_color(p.color("success")),
                                )
                                .child(
                                    div()
                                        .text_size(px(12.))
                                        .font_weight(FontWeight::BOLD)
                                        .child(ui.t(label)),
                                )
                                .child(
                                    div()
                                        .text_size(px(11.))
                                        .text_color(p.color("text-muted"))
                                        .child(format!("{} {}", minutes, ui.t("time.minutes"))),
                                )
                                .child(
                                    div()
                                        .text_size(px(11.))
                                        .text_color(p.color("text-muted"))
                                        .child(time),
                                )
                        })),
                )
                .child(
                    div()
                        .mt(px(24.))
                        .pt(px(14.))
                        .border_t_1()
                        .border_color(p.color("border"))
                        .text_size(px(11.))
                        .text_color(p.color("text-muted"))
                        .child(ui.t("overview.sample_footer")),
                ),
        )
}
