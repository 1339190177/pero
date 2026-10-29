//! Settings navigation and row-based editing share the Host preference owner.
use crate::{
    app::{Workbench, request_locale},
    settings_menu::PreferenceSaveState,
    workspace::WorkbenchPage,
};
use gpui_kit::{
    component::{
        button::*,
        checkbox::Checkbox,
        input::Input,
        menu::{DropdownMenu, PopupMenuItem},
        *,
    },
    prelude::*,
    *,
};
use pero_contracts::{
    appearance::{AccentColor, FontScope},
    workbench::Appearance,
};
use pero_theme::{TextRole, UiAppearance};

#[derive(Clone, Copy, Default, PartialEq, Eq)]
pub(crate) enum SettingsSection {
    General,
    #[default]
    Theme,
    InterfaceFont,
    ChatFont,
    CodeFont,
}
impl SettingsSection {
    const ALL: [Self; 5] = [
        Self::General,
        Self::Theme,
        Self::InterfaceFont,
        Self::ChatFont,
        Self::CodeFont,
    ];
    fn key(self) -> &'static str {
        match self {
            Self::General => "settings.general",
            Self::Theme => "settings.theme",
            Self::InterfaceFont => "settings.interface_font",
            Self::ChatFont => "settings.chat_font",
            Self::CodeFont => "settings.code_font",
        }
    }
    fn scope(self) -> Option<FontScope> {
        match self {
            Self::InterfaceFont => Some(FontScope::Interface),
            Self::ChatFont => Some(FontScope::Chat),
            Self::CodeFont => Some(FontScope::Code),
            _ => None,
        }
    }
}

fn setting_row(label: &str, description: &str, control: AnyElement, cx: &App) -> Div {
    let appearance = UiAppearance::global(cx);
    div()
        .min_w_0()
        .py(px(18.))
        .flex()
        .items_center()
        .gap(px(20.))
        .border_b(px(0.5))
        .border_color(appearance.palette.border)
        .child(
            div()
                .flex_1()
                .min_w_0()
                .flex()
                .flex_col()
                .gap(px(5.))
                .child(label.to_owned())
                .when(!description.is_empty(), |el| {
                    el.child(
                        div()
                            .text_size(appearance.text(TextRole::Caption))
                            .text_color(appearance.palette.text_muted)
                            .whitespace_normal()
                            .child(description.to_owned()),
                    )
                }),
        )
        .child(div().flex_shrink_0().child(control))
}

impl Workbench {
    fn font_settings(&self, section: SettingsSection, cx: &mut Context<Self>) -> AnyElement {
        let scope = section.scope().expect("font section");
        let preferences = self.preferences.typography;
        let size = u8::from(preferences.effective(scope));
        let inherited = preferences.selected(scope).is_none();
        let appearance = UiAppearance::global(cx).clone();
        let row_height = appearance.typography.control_height();
        let saving = self.preferences_status == PreferenceSaveState::Saving;
        let stepper = div()
            .flex()
            .items_center()
            .border_1()
            .border_color(appearance.palette.input_border)
            .rounded(px(4.))
            .overflow_hidden()
            .child(
                Button::new("font-decrease")
                    .ghost()
                    .small()
                    .label("−")
                    .accessibility_label(self.t("settings.decrease_font"))
                    .w(px(row_height))
                    .h(px(row_height))
                    .disabled(size == 12 || saving)
                    .on_click(cx.listener(move |this, _, _, cx| {
                        let mut next = this.preferences.clone();
                        next.typography.adjust(scope, -1);
                        this.save_preferences(next, cx);
                    })),
            )
            .child(
                div()
                    .min_w(px(48.))
                    .px(px(8.))
                    .text_center()
                    .child(size.to_string()),
            )
            .child(
                Button::new("font-increase")
                    .ghost()
                    .small()
                    .label("+")
                    .accessibility_label(self.t("settings.increase_font"))
                    .w(px(row_height))
                    .h(px(row_height))
                    .disabled(size == 24 || saving)
                    .on_click(cx.listener(move |this, _, _, cx| {
                        let mut next = this.preferences.clone();
                        next.typography.adjust(scope, 1);
                        this.save_preferences(next, cx);
                    })),
            );
        let family = if scope == FontScope::Code {
            "JetBrains Mono"
        } else {
            self.t("settings.system_font")
        };
        let description = self.t(match scope {
            FontScope::Interface => "settings.interface_font_hint",
            FontScope::Chat => "settings.chat_font_hint",
            FontScope::Code => "settings.code_font_hint",
        });
        div()
            .flex()
            .flex_col()
            .child(setting_row(
                self.t("settings.font_family"),
                "",
                div()
                    .text_color(appearance.palette.text_muted)
                    .child(family.to_owned())
                    .into_any_element(),
                cx,
            ))
            .when(scope != FontScope::Interface, |el| {
                el.child(setting_row(
                    self.t("settings.inherit_ui"),
                    "",
                    Checkbox::new("font-inherit")
                        .checked(inherited)
                        .disabled(saving)
                        .accessibility_label(self.t("settings.inherit_ui"))
                        .on_click(cx.listener(move |this, checked: &bool, _, cx| {
                            let mut next = this.preferences.clone();
                            next.typography.inherit(scope, *checked);
                            this.save_preferences(next, cx);
                        }))
                        .into_any_element(),
                    cx,
                ))
            })
            .child(setting_row(
                self.t("settings.font_size"),
                description,
                stepper.into_any_element(),
                cx,
            ))
            .child(
                div()
                    .pt(px(24.))
                    .flex()
                    .flex_col()
                    .gap(px(12.))
                    .child(
                        div()
                            .text_size(appearance.text(TextRole::Caption))
                            .text_color(appearance.palette.text_muted)
                            .child(self.t("settings.preview")),
                    )
                    .child(
                        div()
                            .p(px(16.))
                            .bg(appearance.palette.bg)
                            .rounded(px(4.))
                            .text_size(px(size.into()))
                            .line_height(relative(1.6))
                            .when(scope == FontScope::Code, |el| {
                                el.font_family("JetBrains Mono")
                            })
                            .whitespace_normal()
                            .child(if scope == FontScope::Code {
                                "let result = project.run();".to_owned()
                            } else {
                                self.t("settings.preview_text").to_owned()
                            }),
                    ),
            )
            .into_any_element()
    }

    fn theme_settings(&self, cx: &mut Context<Self>) -> AnyElement {
        let target = cx.entity().downgrade();
        let selected = self.preferences.appearance;
        let choices = [Appearance::System, Appearance::Light, Appearance::Dark]
            .map(|mode| (mode, self.t(mode.label_key()).to_owned()));
        let mode = Button::new("appearance-mode")
            .outline()
            .small()
            .min_w(px(144.))
            .label(self.t(selected.label_key()))
            .accessibility_label(self.t("settings.appearance"))
            .dropdown_menu(move |menu, _, _| {
                choices.iter().fold(menu, |menu, (choice, label)| {
                    let target = target.clone();
                    let choice = *choice;
                    menu.item(
                        PopupMenuItem::new(label.clone())
                            .checked(selected == choice)
                            .on_click(move |_, _, cx| {
                                let _ = target.update(cx, |this, cx| {
                                    let mut next = this.preferences.clone();
                                    next.appearance = choice;
                                    this.save_preferences(next, cx);
                                });
                            }),
                    )
                })
            });
        let target = cx.entity().downgrade();
        let selected = self.preferences.accent;
        let choices = [AccentColor::Teal, AccentColor::Blue, AccentColor::Neutral]
            .map(|accent| (accent, self.t(accent.label_key()).to_owned()));
        let accent = Button::new("appearance-accent")
            .outline()
            .small()
            .min_w(px(144.))
            .label(self.t(selected.label_key()))
            .accessibility_label(self.t("settings.accent"))
            .dropdown_menu(move |menu, _, _| {
                choices.iter().fold(menu, |menu, (choice, label)| {
                    let target = target.clone();
                    let choice = *choice;
                    menu.item(
                        PopupMenuItem::new(label.clone())
                            .checked(selected == choice)
                            .on_click(move |_, _, cx| {
                                let _ = target.update(cx, |this, cx| {
                                    let mut next = this.preferences.clone();
                                    next.accent = choice;
                                    this.save_preferences(next, cx);
                                });
                            }),
                    )
                })
            });
        div()
            .flex()
            .flex_col()
            .child(setting_row(
                self.t("settings.appearance"),
                "",
                mode.into_any_element(),
                cx,
            ))
            .child(setting_row(
                self.t("settings.accent"),
                "",
                accent.into_any_element(),
                cx,
            ))
            .into_any_element()
    }

    fn language_settings(&self, cx: &mut Context<Self>) -> AnyElement {
        let selected = self.locale.clone();
        let target = cx.entity().downgrade();
        let control = Button::new("settings-language")
            .outline()
            .small()
            .min_w(px(144.))
            .label(if selected == "en" {
                "English"
            } else {
                "简体中文"
            })
            .accessibility_label(self.t("menu.language"))
            .dropdown_menu(move |menu, _, _| {
                [("zh-CN", "简体中文"), ("en", "English")].into_iter().fold(
                    menu,
                    |menu, (value, label)| {
                        let target = target.clone();
                        menu.item(
                            PopupMenuItem::new(label)
                                .checked(selected == value)
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
        setting_row(self.t("menu.language"), "", control.into_any_element(), cx).into_any_element()
    }

    pub(crate) fn general_settings(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let appearance = UiAppearance::global(cx).clone();
        let p = &appearance.palette;
        let query = self.settings_search.read(cx).value().to_lowercase();
        let sections = SettingsSection::ALL
            .into_iter()
            .filter(|section| {
                query.is_empty()
                    || format!(
                        "{} {}",
                        self.t(section.key()),
                        match section {
                            SettingsSection::Theme => format!(
                                "{} {}",
                                self.t("settings.appearance"),
                                self.t("settings.accent")
                            ),
                            SettingsSection::General => self.t("menu.language").to_owned(),
                            _ => format!(
                                "{} {}",
                                self.t("settings.font_size"),
                                self.t("settings.font_family")
                            ),
                        }
                    )
                    .to_lowercase()
                    .contains(&query)
            })
            .collect::<Vec<_>>();
        let visible = sections
            .iter()
            .copied()
            .filter(|section| !query.is_empty() || *section == self.settings_section)
            .collect::<Vec<_>>();
        let mut content = div()
            .id("settings-content")
            .flex_1()
            .min_w_0()
            .min_h_0()
            .overflow_y_scroll()
            .px(px(28.))
            .py(px(24.));
        for section in visible.iter().copied() {
            let body = match section {
                SettingsSection::Theme => self.theme_settings(cx),
                SettingsSection::General => self.language_settings(cx),
                _ => self.font_settings(section, cx),
            };
            content = content.child(
                div()
                    .id(("settings-page", section as usize))
                    .mb(px(28.))
                    .child(
                        div()
                            .pb(px(12.))
                            .font_weight(FontWeight::SEMIBOLD)
                            .text_size(appearance.text(TextRole::Heading))
                            .child(self.t(section.key())),
                    )
                    .child(body),
            );
        }
        if visible.is_empty() {
            content = content.child(
                div()
                    .id("settings-empty")
                    .text_color(p.text_muted)
                    .child(self.t("settings.no_results")),
            );
        }
        div()
            .flex_1()
            .min_w_0()
            .min_h_0()
            .flex()
            .flex_col()
            .child(
                div()
                    .h(px(52.))
                    .px(px(20.))
                    .flex()
                    .items_center()
                    .justify_between()
                    .border_b(px(0.5))
                    .border_color(p.border)
                    .child(
                        div()
                            .font_weight(FontWeight::SEMIBOLD)
                            .child(self.t("settings.title")),
                    )
                    .child(div().flex_1())
                    .when(self.preferences_status != PreferenceSaveState::Idle, |el| {
                        el.child(
                            div()
                                .text_size(appearance.text(TextRole::Caption))
                                .text_color(p.text_muted)
                                .child(self.t(
                                    if self.preferences_status == PreferenceSaveState::Saving {
                                        "settings.saving"
                                    } else {
                                        "settings.saved"
                                    },
                                )),
                        )
                    })
                    .child(
                        Button::new("settings-close")
                            .ghost()
                            .small()
                            .icon(Icon::default().path("icons/x.svg"))
                            .accessibility_label(self.t("settings.close"))
                            .on_click(cx.listener(|this, _, _, cx| {
                                this.navigate(WorkbenchPage::Conversation, cx);
                                cx.notify();
                            })),
                    ),
            )
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .child(
                        div()
                            .w(px(204.))
                            .flex_shrink_0()
                            .flex()
                            .flex_col()
                            .bg(p.bg)
                            .border_r(px(0.5))
                            .border_color(p.border)
                            .child(
                                div().p(px(12.)).child(
                                    Input::new(&self.settings_search)
                                        .small()
                                        .aria_label(self.t("settings.search")),
                                ),
                            )
                            .child(
                                div()
                                    .id("settings-navigation")
                                    .flex_1()
                                    .overflow_y_scroll()
                                    .px(px(8.))
                                    .py(px(4.))
                                    .children(sections.into_iter().map(|section| {
                                        Button::new(SharedString::from(format!(
                                            "settings-section-{}",
                                            section.key()
                                        )))
                                        .ghost()
                                        .small()
                                        .w_full()
                                        .justify_start()
                                        .min_h(px(appearance.typography.row_height()))
                                        .selected(self.settings_section == section)
                                        .toggled(self.settings_section == section)
                                        .label(self.t(section.key()))
                                        .child(div().flex_1())
                                        .on_click(
                                            cx.listener(move |this, _, _, cx| {
                                                this.settings_section = section;
                                                cx.notify();
                                            }),
                                        )
                                    }))
                                    .child(
                                        div()
                                            .mt(px(16.))
                                            .pt(px(8.))
                                            .border_t(px(0.5))
                                            .border_color(p.border)
                                            .children(
                                                [
                                                    (
                                                        "settings.connections",
                                                        WorkbenchPage::Connections,
                                                    ),
                                                    ("settings.resources", WorkbenchPage::Workers),
                                                    ("settings.agents", WorkbenchPage::Agents),
                                                ]
                                                .into_iter()
                                                .enumerate()
                                                .map(|(i, (key, page))| {
                                                    Button::new(("settings-destination", i))
                                                        .ghost()
                                                        .small()
                                                        .w_full()
                                                        .justify_start()
                                                        .min_h(px(appearance
                                                            .typography
                                                            .row_height()))
                                                        .label(self.t(key))
                                                        .child(div().flex_1())
                                                        .on_click(cx.listener(
                                                            move |this, _, window, cx| {
                                                                this.navigate(page, cx);
                                                                if page
                                                                    == WorkbenchPage::Connections
                                                                {
                                                                    this.open_remote_connection(
                                                                        window, cx,
                                                                    );
                                                                }
                                                                cx.notify();
                                                            },
                                                        ))
                                                }),
                                            ),
                                    ),
                            ),
                    )
                    .child(content),
            )
    }
}
