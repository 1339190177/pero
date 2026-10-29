use gpui_kit::{
    component::{Theme, ThemeMode},
    *,
};
use serde::Deserialize;
use std::collections::HashMap;

/// Shared geometry for the compact workbench and standalone App chat.
/// Component behavior and focus treatment remain owned by GPUI Kit.
pub struct WorkbenchStyle;

impl WorkbenchStyle {
    pub const TEXT: f32 = 13.;
    pub const TEXT_SMALL: f32 = 12.;
    pub const ICON: f32 = 16.;
    pub const ROW: f32 = 30.;
    pub const CONTROL: f32 = 28.;
    pub const TAB: f32 = 38.;
    pub const RADIUS: f32 = 4.;
    pub const GAP: f32 = 4.;
    pub const INSET: f32 = 8.;
    pub const MENU_WIDTH: f32 = 240.;
}

#[derive(Clone, Deserialize)]
pub struct Palettes {
    pub light: Palette,
    pub dark: Palette,
}

#[derive(Clone, Deserialize)]
#[serde(transparent)]
pub struct Palette(HashMap<String, String>);

impl Palette {
    pub fn color(&self, token: &str) -> Hsla {
        rgb(
            u32::from_str_radix(self.0[token].trim_start_matches('#'), 16)
                .expect("generated color token"),
        )
        .into()
    }
}

impl Palettes {
    pub fn get(&self, dark: bool) -> &Palette {
        if dark { &self.dark } else { &self.light }
    }

    pub fn apply(&self, dark: bool, cx: &mut App) {
        Theme::change(
            if dark {
                ThemeMode::Dark
            } else {
                ThemeMode::Light
            },
            None,
            cx,
        );
        let p = self.get(dark);
        let theme = Theme::global_mut(cx);
        theme.font_family = "Noto Sans CJK SC".into();
        theme.font_size = px(WorkbenchStyle::TEXT);
        theme.radius = px(WorkbenchStyle::RADIUS);
        theme.radius_lg = px(WorkbenchStyle::INSET);
        theme.mono_font_family = "JetBrains Mono".into();
        let c = &mut theme.colors;
        c.background = p.color("surface");
        c.foreground = p.color("text");
        c.border = p.color("border");
        c.input = p.color("input-border");
        c.popover = p.color("surface");
        c.popover_foreground = p.color("text");
        c.list_active_border = p.color("focus");
        c.muted = p.color("surface-muted");
        c.muted_foreground = p.color("text-muted");
        c.primary = p.color("action");
        c.primary_foreground = p.color("on-action");
        c.primary_hover = p.color("action-hover");
        c.primary_active = p.color("action-hover");
        c.button_primary = p.color("action");
        c.button_primary_foreground = p.color("on-action");
        c.button_primary_hover = p.color("action-hover");
        c.button_primary_active = p.color("action-hover");
        c.ring = p.color("focus");
        c.selection = p.color("accent-soft");
        c.list_active = p.color("accent-soft");
        c.list_hover = p.color("surface-muted");
        c.sidebar = p.color("bg");
        c.sidebar_foreground = p.color("text");
        c.sidebar_border = p.color("border");
        c.sidebar_accent = p.color("accent-soft");
        c.sidebar_accent_foreground = p.color("accent-text");
        c.sidebar_primary = p.color("action");
        c.sidebar_primary_foreground = p.color("on-action");
        c.secondary = p.color("surface-muted");
        c.secondary_foreground = p.color("text");
        c.secondary_hover = p.color("surface-muted");
        c.secondary_active = p.color("accent-soft");
        c.accent = p.color("accent-soft");
        c.accent_foreground = p.color("accent-text");
        (c.success, c.success_foreground) = (p.color("success"), p.color("on-success"));
        (c.success_hover, c.success_active) = (c.success, c.success);
        (c.warning, c.warning_foreground) = (p.color("warning"), p.color("on-warning"));
        (c.warning_hover, c.warning_active) = (c.warning, c.warning);
        (c.info, c.info_foreground) = (p.color("info"), p.color("on-info"));
        (c.info_hover, c.info_active) = (c.info, c.info);
        (c.danger, c.danger_foreground) = (p.color("error"), p.color("on-error"));
        (c.danger_hover, c.danger_active) = (c.danger, c.danger);
        theme.tokens.secondary_hover = p.color("surface-muted").into();
        theme.tokens.secondary_active = p.color("accent-soft").into();
        theme.tokens.button_primary = p.color("action").into();
        theme.tokens.button_primary_hover = p.color("action-hover").into();
        theme.tokens.button_primary_active = p.color("action-hover").into();
    }
}
