//! Reusable Settings content. The shell owns windows, commands and persistence.
use crate::{settings_menu::PreferenceSaveState, workspace::WorkbenchPage};
use gpui_kit::{
    component::{
        button::*,
        checkbox::Checkbox,
        input::{Input, InputEvent, InputState},
        menu::{DropdownMenu, PopupMenuItem},
        tree::{TreeItem, TreeState},
        *,
    },
    prelude::*,
    *,
};
use pero_contracts::{
    appearance::{AccentColor, FontScope, FontSize},
    workbench::{Appearance, WorkbenchPreferences},
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

#[derive(Clone)]
pub(crate) enum SettingsAction {
    Save(WorkbenchPreferences),
    Locale(&'static str),
    Navigate(WorkbenchPage),
    Close,
}

pub(crate) struct SettingsPanel {
    pub preferences: WorkbenchPreferences,
    pub locale: String,
    pub preferences_status: PreferenceSaveState,
    pub error: Option<String>,
    section: SettingsSection,
    navigation: Entity<TreeState>,
    navigation_query: Option<(String, String)>,
    search: Entity<InputState>,
    content_scroll: ScrollHandle,
    font_inputs: [Entity<InputState>; 3],
    needs_sync: bool,
    input_locale: String,
    bounds: Bounds<Pixels>,
    _subscriptions: Vec<Subscription>,
}
impl EventEmitter<SettingsAction> for SettingsPanel {}
impl SettingsPanel {
    pub(crate) fn new(
        preferences: WorkbenchPreferences,
        locale: String,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Self {
        let search = cx.new(|cx| {
            InputState::new(window, cx)
                .placeholder(pero_contracts::i18n::text(&locale, "settings.search"))
        });
        let font_inputs = [FontScope::Interface, FontScope::Chat, FontScope::Code].map(|scope| {
            cx.new(|cx| {
                InputState::new(window, cx)
                    .default_value(u8::from(preferences.typography.effective(scope)).to_string())
                    .validate(|value, _| value.bytes().all(|byte| byte.is_ascii_digit()))
            })
        });
        let navigation = cx.new(|cx| TreeState::new(cx));
        let mut subscriptions = vec![
            cx.subscribe(&search, |_, _, _: &InputEvent, cx| cx.notify()),
            cx.observe(&navigation, |this, tree, cx| {
                let Some(selected) = tree.read(cx).selected_item() else {
                    return;
                };
                let section = SettingsSection::ALL
                    .into_iter()
                    .find(|section| section.key() == selected.id.as_ref())
                    .or_else(|| {
                        (selected.id.as_ref() == "settings.appearance")
                            .then_some(SettingsSection::Theme)
                    });
                if let Some(section) = section {
                    this.section = section;
                    let sections = this.matching_sections(cx);
                    let query = this.search.read(cx).value();
                    let index = if query.is_empty() {
                        if section == SettingsSection::General {
                            0
                        } else {
                            section as usize - 1
                        }
                    } else {
                        sections
                            .iter()
                            .position(|candidate| *candidate == section)
                            .unwrap_or(0)
                    };
                    this.content_scroll.scroll_to_top_of_item(index);
                    cx.notify();
                }
            }),
        ];
        for (index, scope) in [FontScope::Interface, FontScope::Chat, FontScope::Code]
            .into_iter()
            .enumerate()
        {
            subscriptions.push(cx.subscribe(
                &font_inputs[index],
                move |this, input, event: &InputEvent, cx| {
                    cx.notify();
                    if matches!(event, InputEvent::PressEnter { .. } | InputEvent::Blur) {
                        match input
                            .read(cx)
                            .value()
                            .parse::<u8>()
                            .ok()
                            .and_then(|value| FontSize::try_from(value).ok())
                        {
                            Some(size) => {
                                if this.error.as_deref() == Some("settings.font_size_range") {
                                    this.error = None;
                                    cx.notify();
                                }
                                if size == this.preferences.typography.effective(scope) {
                                    return;
                                }
                                let mut next = this.preferences.clone();
                                next.typography.set_size(scope, size);
                                this.save_preferences(next, cx);
                            }
                            None => {
                                this.error = Some("settings.font_size_range".into());
                                cx.notify();
                            }
                        }
                    }
                },
            ));
        }
        Self {
            preferences,
            locale: locale.clone(),
            input_locale: locale,
            bounds: Bounds::default(),
            preferences_status: Default::default(),
            error: None,
            section: Default::default(),
            navigation,
            navigation_query: None,
            search,
            content_scroll: ScrollHandle::new(),
            font_inputs,
            needs_sync: false,
            _subscriptions: subscriptions,
        }
    }
    pub(crate) fn receive(
        &mut self,
        preferences: &WorkbenchPreferences,
        locale: &str,
        status: PreferenceSaveState,
        error: Option<&str>,
        cx: &mut Context<Self>,
    ) {
        self.needs_sync |= self.preferences.typography != preferences.typography;
        self.preferences = preferences.clone();
        self.locale = locale.into();
        self.preferences_status = status;
        self.error = error.map(str::to_owned);
        cx.notify();
    }
    fn t(&self, key: &str) -> &'static str {
        pero_contracts::i18n::text(&self.locale, key)
    }
    fn save_preferences(&mut self, preferences: WorkbenchPreferences, cx: &mut Context<Self>) {
        if preferences == self.preferences {
            return;
        }
        self.error = None;
        self.preferences_status = PreferenceSaveState::Saving;
        cx.emit(SettingsAction::Save(preferences));
        cx.notify();
    }
    #[cfg(target_family = "wasm")]
    pub(crate) fn snapshot(&self, cx: &App) -> serde_json::Value {
        let bounds = |value: Bounds<Pixels>| {
            serde_json::json!({
            "x":f32::from(value.origin.x),"y":f32::from(value.origin.y),
            "width":f32::from(value.size.width),"height":f32::from(value.size.height)})
        };
        serde_json::json!({"query":self.search.read(cx).value().as_ref(),"section":self.section.key(),
            "preferences":self.preferences,"error":self.error,"bounds":bounds(self.bounds),
            "fontInputs":self.font_inputs.iter().map(|input| {
                let input=input.read(cx);let mut result=bounds(input.input_bounds());
                result["value"]=serde_json::json!(input.value().as_ref());result
            }).collect::<Vec<_>>()})
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

impl SettingsPanel {
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
                                    let _ = target.update(cx, |_, cx| cx.emit(SettingsAction::Locale(value)));
                                }),
                        )
                    },
                )
            });
        setting_row(self.t("menu.language"), "", control.into_any_element(), cx).into_any_element()
    }

    fn content(&self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let appearance = UiAppearance::global(cx).clone();
        let p = &appearance.palette;
        let query = self.search.read(cx).value().to_lowercase();
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
            .filter(|section| !query.is_empty() || *section == self.section)
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
                                cx.emit(SettingsAction::Close);
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
                                    Input::new(&self.search)
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
                                        .selected(self.section == section)
                                        .toggled(self.section == section)
                                        .label(self.t(section.key()))
                                        .child(div().flex_1())
                                        .on_click(
                                            cx.listener(move |this, _, _, cx| {
                                                this.section = section;
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
                                                                cx.emit(SettingsAction::Navigate(page));
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

impl SettingsPanel {
    fn matching_sections(&self, cx: &App) -> Vec<SettingsSection> {
        let query = self.search.read(cx).value().to_lowercase();
        SettingsSection::ALL
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
            .collect()
    }

}

impl Render for SettingsPanel {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if self.input_locale != self.locale {
            let placeholder = self.t("settings.search");
            self.search.update(cx, |input, cx| {
                input.set_placeholder(placeholder, window, cx)
            });
            #[cfg(not(target_family = "wasm"))]
            window.set_window_title(self.t("settings.title"));
            self.input_locale = self.locale.clone();
        }
        let query = self.search.read(cx).value().to_lowercase();
        let navigation_query = (query.clone(), self.locale.clone());
        if self.navigation_query.as_ref() != Some(&navigation_query) {
            self.navigation_query = Some(navigation_query);
            let sections = self.matching_sections(cx);
            let mut roots = Vec::new();
            if sections.contains(&SettingsSection::General) {
                roots.push(TreeItem::new(
                    SettingsSection::General.key(),
                    self.t(SettingsSection::General.key()),
                ));
            }
            let children = sections
                .iter()
                .copied()
                .filter(|section| *section != SettingsSection::General)
                .map(|section| TreeItem::new(section.key(), self.t(section.key())))
                .collect::<Vec<_>>();
            if !children.is_empty() {
                roots.push(
                    TreeItem::new("settings.appearance", self.t("settings.appearance"))
                        .children(children)
                        .expanded(true),
                );
            }
            roots.extend(
                crate::settings_menu::SettingsDestination::ALL
                    .into_iter()
                    .filter(|destination| {
                        query.is_empty()
                            || self
                                .t(destination.label_key)
                                .to_lowercase()
                                .contains(&query)
                    })
                    .map(|destination| {
                        TreeItem::new(
                            destination.label_key,
                            self.t(destination.label_key).trim_end_matches('…'),
                        )
                    }),
            );
            let selected = TreeItem::new(self.section.key(), "");
            self.navigation.update(cx, |tree, cx| {
                tree.set_items(roots, cx);
                tree.set_selected_item(Some(&selected), cx);
            });
        }
        if self.needs_sync {
            self.needs_sync = false;
            for (index, scope) in [FontScope::Interface, FontScope::Chat, FontScope::Code]
                .into_iter()
                .enumerate()
            {
                let value = u8::from(self.preferences.typography.effective(scope)).to_string();
                self.font_inputs[index].update(cx, |input, cx| input.set_value(value, window, cx));
            }
        }
        let appearance = UiAppearance::global(cx).clone();
        div()
            .id("settings-panel")
            .on_prepaint(cx.processor(|this, bounds, _, _| this.bounds = bounds))
            .size_full()
            .flex()
            .flex_col()
            .bg(appearance.palette.surface)
            .text_color(appearance.palette.text)
            .text_size(appearance.text(TextRole::Body))
            .font_weight(FontWeight::NORMAL)
            .line_height(relative(1.4))
            .child(self.content(window, cx))
    }
}
