use gpui_kit::{
    component::{Theme, ThemeMode},
    *,
};
use serde::Deserialize;
use std::collections::HashMap;

#[derive(Clone, Deserialize)]
pub(crate) struct Palettes {
    pub light: Palette,
    pub dark: Palette,
}

#[derive(Clone, Deserialize)]
#[serde(transparent)]
pub(crate) struct Palette(HashMap<String, String>);

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
        theme.font_size = px(14.);
        theme.mono_font_family = "JetBrains Mono".into();
        let c = &mut theme.colors;
        c.background = p.color("surface");
        c.foreground = p.color("text");
        c.border = p.color("border");
        c.input = p.color("input-border");
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
        theme.tokens.button_primary = p.color("action").into();
        theme.tokens.button_primary_hover = p.color("action-hover").into();
        theme.tokens.button_primary_active = p.color("action-hover").into();
    }
}
