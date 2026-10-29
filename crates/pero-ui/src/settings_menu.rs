use crate::app::{Workbench, request_locale};
use crate::workspace::WorkbenchPage;
use gpui_kit::{
    component::{
        button::*,
        menu::{DropdownMenu, PopupMenuItem},
        *,
    },
    prelude::*,
    *,
};
use pero_contracts::workbench::{Appearance, WorkbenchPreferences};
use serde_json::json;

#[derive(Clone, Copy, Default, PartialEq, Eq, serde::Serialize)]
#[serde(rename_all = "snake_case")]
pub(crate) enum PreferenceSaveState {
    #[default]
    Idle,
    Saving,
    Saved,
}

impl Workbench {
    pub(crate) fn save_preferences(
        &mut self,
        preferences: WorkbenchPreferences,
        cx: &mut Context<Self>,
    ) {
        self.error = None;
        self.preferences_status = PreferenceSaveState::Saving;
        self.command(
            json!({"method":"settings.workbench","preferences":preferences}),
            cx,
        );
    }

    pub(crate) fn settings_menu(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let workbench = cx.entity().downgrade();
        let menu_owner = workbench.clone();
        let locale = self.locale.clone();
        let preferences = self.preferences.clone();
        let t = |key| self.t(key).to_string();
        let identity = self.remote.data["identity"]["name"]
            .as_str()
            .unwrap_or("")
            .to_string();
        let host = self.remote.data["connection"]["url"]
            .as_str()
            .unwrap_or("")
            .trim_start_matches("https://")
            .trim_start_matches("http://")
            .trim_end_matches('/')
            .to_string();
        let connections = t("settings.connections");
        let resources = t("settings.resources");
        let agents = t("settings.agents");
        let all = t("settings.all");
        let appearance = format!(
            "{} · {}",
            t("settings.appearance"),
            t(preferences.appearance.label_key())
        );
        let language = format!(
            "{} · {}",
            t("menu.language"),
            if locale == "en" {
                "English"
            } else {
                "简体中文"
            }
        );
        let appearance_choices = [Appearance::Light, Appearance::Dark, Appearance::System]
            .map(|mode| (mode, t(mode.label_key())));
        Button::new("sidebar-settings")
            .ghost()
            .w_full()
            .h(px(36.))
            .justify_start()
            .icon(Icon::default().path("icons/settings.svg"))
            .label(if identity.is_empty() {
                t("settings.title")
            } else {
                identity.clone()
            })
            .child(div().flex_1())
            .accessibility_label(t("settings.title"))
            .dropdown_caret(true)
            .dropdown_menu_with_anchor(Anchor::BottomLeft, move |menu, window, cx| {
                let mut menu = menu.min_w(px(240.)).check_side(Side::Right);
                if !identity.is_empty() {
                    menu = menu.item(PopupMenuItem::label(identity.clone()));
                }
                if !host.is_empty() {
                    menu = menu.item(PopupMenuItem::label(host.clone()));
                }
                let target = workbench.clone();
                menu = menu
                    .separator()
                    .item(PopupMenuItem::new(connections.clone()).on_click(
                        move |_, window, cx| {
                            let _ = target.update(cx, |this, cx| {
                                this.navigate(WorkbenchPage::Connections, cx);
                                this.open_remote_connection(window, cx);
                                this.sync_follow(cx);
                            });
                        },
                    ));
                for (label, navigation) in [
                    (resources.clone(), WorkbenchPage::Workers),
                    (agents.clone(), WorkbenchPage::Agents),
                ] {
                    let target = workbench.clone();
                    menu = menu.item(PopupMenuItem::new(label).on_click(move |_, _, cx| {
                        let _ = target.update(cx, |this, cx| {
                            this.navigate(navigation, cx);
                            this.sync_follow(cx);
                            if navigation == WorkbenchPage::Workers {
                                this.command(json!({"action":"remote_refresh"}), cx);
                            }
                            cx.notify();
                        });
                    }));
                }
                let target = workbench.clone();
                let saved = preferences.clone();
                let appearance_choices = appearance_choices.clone();
                menu =
                    menu.separator()
                        .submenu(appearance.clone(), window, cx, move |menu, _, _| {
                            appearance_choices.iter().fold(
                                menu.min_w(px(170.)),
                                |menu, (mode, label)| {
                                    let target = target.clone();
                                    let mut next = saved.clone();
                                    next.appearance = *mode;
                                    menu.item(
                                        PopupMenuItem::new(label.clone())
                                            .checked(saved.appearance == *mode)
                                            .on_click(move |_, _, cx| {
                                                let _ = target.update(cx, |this, cx| {
                                                    this.save_preferences(next.clone(), cx)
                                                });
                                            }),
                                    )
                                },
                            )
                        });
                let target = workbench.clone();
                let selected_locale = locale.clone();
                menu = menu.submenu(language.clone(), window, cx, move |menu, _, _| {
                    [("zh-CN", "简体中文"), ("en", "English")].into_iter().fold(
                        menu.min_w(px(170.)),
                        |menu, (value, label)| {
                            let target = target.clone();
                            menu.item(
                                PopupMenuItem::new(label)
                                    .checked(selected_locale == value)
                                    .on_click(move |_, _, cx| {
                                        if request_locale(value).is_err() {
                                            let _ = target.update(cx, |this, cx| {
                                                this.error = Some("error.preferences".into());
                                                cx.notify();
                                            });
                                        }
                                    }),
                            )
                        },
                    )
                });
                let target = workbench.clone();
                menu.separator()
                    .item(PopupMenuItem::new(all.clone()).on_click(move |_, _, cx| {
                        let _ = target.update(cx, |this, cx| {
                            this.navigate(WorkbenchPage::Settings, cx);
                            this.sync_follow(cx);
                            cx.notify();
                        });
                    }))
            })
            .on_open_change(move |open, _, cx| {
                let _ = menu_owner.update(cx, |this, cx| {
                    this.settings_menu_open = *open;
                    cx.notify();
                });
            })
    }

    pub(crate) fn general_settings(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        div()
            .flex_1()
            .min_w_0()
            .p(px(24.))
            .flex()
            .flex_col()
            .gap(px(20.))
            .child(
                div()
                    .text_size(px(24.))
                    .font_weight(FontWeight::BOLD)
                    .child(self.t("settings.title")),
            )
            .child(
                div().flex().flex_wrap().gap(px(8.)).children(
                    [
                        ("settings.connections", WorkbenchPage::Connections),
                        ("settings.resources", WorkbenchPage::Workers),
                        ("settings.agents", WorkbenchPage::Agents),
                    ]
                    .into_iter()
                    .enumerate()
                    .map(|(i, (key, navigation))| {
                        Button::new(("general-settings", i))
                            .outline()
                            .label(self.t(key))
                            .on_click(cx.listener(move |this, _, window, cx| {
                                this.navigate(navigation, cx);
                                if navigation == WorkbenchPage::Connections {
                                    this.open_remote_connection(window, cx);
                                }
                                cx.notify();
                            }))
                    }),
                ),
            )
            .child(
                div()
                    .border_t_1()
                    .border_color(p.color("border"))
                    .pt(px(16.))
                    .child(self.t("settings.appearance"))
                    .child(
                        div().mt(px(10.)).flex().flex_wrap().gap(px(8.)).children(
                            [Appearance::Light, Appearance::Dark, Appearance::System]
                                .into_iter()
                                .enumerate()
                                .map(|(i, mode)| {
                                    Button::new(("appearance-choice", i))
                                        .outline()
                                        .label(self.t(mode.label_key()))
                                        .toggled(self.preferences.appearance == mode)
                                        .on_click(cx.listener(move |this, _, _, cx| {
                                            let mut next = this.preferences.clone();
                                            next.appearance = mode;
                                            this.save_preferences(next, cx);
                                        }))
                                }),
                        ),
                    ),
            )
            .child(
                div().child(self.t("menu.language")).child(
                    div().mt(px(10.)).flex().gap(px(8.)).children(
                        [("zh-CN", "简体中文"), ("en", "English")]
                            .into_iter()
                            .enumerate()
                            .map(|(i, (locale, label))| {
                                Button::new(("language-choice", i))
                                    .outline()
                                    .label(label)
                                    .toggled(self.locale == locale)
                                    .on_click(cx.listener(move |this, _, _, cx| {
                                        if request_locale(locale).is_err() {
                                            this.error = Some("error.preferences".into());
                                            cx.notify();
                                        }
                                    }))
                            }),
                    ),
                ),
            )
    }
}
