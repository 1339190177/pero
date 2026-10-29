//! One completion surface for context sources, references, commands and skills.
use crate::app::Workbench;
use gpui_kit::{
    component::{button::*, *},
    prelude::*,
    *,
};
use pero_contracts::context::ContextKind;

pub(crate) struct PickerItem {
    pub icon: &'static str,
    pub label: String,
    pub detail: String,
    pub group: String,
}

impl Workbench {
    pub(crate) fn composer_picker_view(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        let items = self.composer_picker_items();
        let selected = self.composer_state.selected;
        div()
            .id("composer-completions")
            .absolute()
            .bottom_full()
            .left(px(16.))
            .right(px(16.))
            .max_w(px(560.))
            .mb(px(4.))
            .border_1()
            .border_color(p.color("border"))
            .rounded(px(8.))
            .bg(p.color("surface"))
            .text_color(p.color("text"))
            .text_size(px(13.))
            .occlude()
            .on_mouse_down_out(cx.listener(|this, _, _, cx| {
                this.close_composer_picker(cx);
            }))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(8.))
                    .px(px(10.))
                    .py(px(5.))
                    .border_b_1()
                    .border_color(p.color("border"))
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .truncate()
                            .font_weight(FontWeight::SEMIBOLD)
                            .child(self.composer_state.title.clone()),
                    )
                    .child(
                        Button::new("close-completions")
                            .ghost()
                            .small()
                            .label("Esc")
                            .tooltip(self.t("composer.close"))
                            .on_click(cx.listener(|this, _, _, cx| this.close_composer_picker(cx))),
                    ),
            )
            .when(self.composer_state.loading, |el| {
                el.child(
                    div()
                        .px(px(12.))
                        .py(px(12.))
                        .text_color(p.color("text-muted"))
                        .child(self.t("composer.loading")),
                )
            })
            .when_some(self.composer_state.error.as_ref(), |el, error| {
                el.child(
                    div()
                        .px(px(12.))
                        .py(px(10.))
                        .text_color(p.color("error"))
                        .child(error.clone()),
                )
            })
            .when(
                !self.composer_state.loading
                    && self.composer_state.error.is_none()
                    && items.is_empty(),
                |el| {
                    el.child(
                        div()
                            .px(px(12.))
                            .py(px(12.))
                            .text_color(p.color("text-muted"))
                            .child(self.t("composer.empty")),
                    )
                },
            )
            .child(
                div()
                    .id("composer-candidate-list")
                    .role(Role::ListBox)
                    .aria_label(self.composer_state.title.clone())
                    .track_scroll(&self.composer_picker_scroll)
                    .max_h(px(250.))
                    .overflow_y_scroll()
                    .p(px(4.))
                    .children(items.iter().enumerate().map(|(index, item)| {
                        let first_group = index == 0 || items[index - 1].group != item.group;
                        div()
                            .when(first_group && !item.group.is_empty(), |el| {
                                el.child(
                                    div()
                                        .px(px(8.))
                                        .pt(px(7.))
                                        .pb(px(3.))
                                        .text_size(px(11.))
                                        .text_color(p.color("text-muted"))
                                        .child(item.group.clone()),
                                )
                            })
                            .child(
                                div()
                                    .id(("composer-candidate", index))
                                    .role(Role::ListBoxOption)
                                    .aria_selected(index == selected)
                                    .aria_label(item.label.clone())
                                    .flex()
                                    .items_center()
                                    .gap(px(8.))
                                    .px(px(8.))
                                    .py(px(6.))
                                    .rounded(px(4.))
                                    .cursor_pointer()
                                    .when(index == selected, |el| el.bg(p.color("accent-soft")))
                                    .hover(|el| el.bg(p.color("surface-muted")))
                                    .on_mouse_down(MouseButton::Left, |_, window, _| {
                                        window.prevent_default()
                                    })
                                    .on_click(cx.listener(move |this, _, window, cx| {
                                        this.select_composer_candidate(index, window, cx);
                                    }))
                                    .child(
                                        Icon::default()
                                            .path(item.icon)
                                            .small()
                                            .text_color(p.color("text-muted")),
                                    )
                                    .child(
                                        div()
                                            .flex_1()
                                            .min_w_0()
                                            .truncate()
                                            .child(item.label.clone()),
                                    )
                                    .when(!item.detail.is_empty(), |el| {
                                        el.child(
                                            div()
                                                .max_w(relative(0.5))
                                                .truncate()
                                                .text_size(px(11.))
                                                .text_color(p.color("text-muted"))
                                                .child(item.detail.clone()),
                                        )
                                    }),
                            )
                    })),
            )
            .child(
                div()
                    .px(px(12.))
                    .py(px(5.))
                    .border_t_1()
                    .border_color(p.color("border"))
                    .text_size(px(11.))
                    .text_color(p.color("text-muted"))
                    .child(self.t("composer.keyboard_hint")),
            )
    }

    pub(crate) fn context_kind_label(&self, kind: ContextKind) -> &str {
        self.t(match kind {
            ContextKind::File => "composer.files",
            ContextKind::Directory => "composer.directories",
            ContextKind::Symbol => "composer.symbols",
            ContextKind::Thread => "composer.threads",
            ContextKind::Skill => "composer.skills",
            ContextKind::Image => "composer.image",
            ContextKind::Selection => "composer.selection",
            ContextKind::BranchDiff => "composer.branch_diff",
            ContextKind::Fetch => "composer.fetch",
        })
    }

    pub(crate) fn context_kind_icon(kind: ContextKind) -> &'static str {
        match kind {
            ContextKind::File => "icons/file.svg",
            ContextKind::Directory => "icons/folder.svg",
            ContextKind::Symbol => "icons/code.svg",
            ContextKind::Thread => "icons/message-circle.svg",
            ContextKind::Skill => "icons/sparkles.svg",
            ContextKind::Image => "icons/image.svg",
            ContextKind::Selection => "icons/text-cursor.svg",
            ContextKind::BranchDiff => "icons/git-branch.svg",
            ContextKind::Fetch => "icons/globe.svg",
        }
    }
}
