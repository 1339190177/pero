//! Workbench content selection and the single shared conversation dock.
use crate::{app::Workbench, workspace::WorkspaceItem};
use gpui_kit::{
    component::{button::*, *},
    prelude::*,
    *,
};
use serde_json::json;

impl Workbench {
    pub(crate) fn current_content(&self, cx: &App) -> Option<WorkspaceItem> {
        match self.navigation {
            0 => self
                .chat
                .read(cx)
                .active_chat
                .and_then(|i| self.chat.read(cx).chats.get(i))
                .map(|chat| WorkspaceItem::Conversation(chat.id.clone())),
            3 => self.remote.data["selected_task_id"]
                .as_str()
                .map(|id| WorkspaceItem::Task(id.into()))
                .or_else(|| self.selected_project.clone().map(WorkspaceItem::Project)),
            7 => self.apps.active.as_ref().map(|app| WorkspaceItem::App {
                name: app.name.clone(),
                panel: app.panel.clone(),
            }),
            _ => None,
        }
    }

    pub(crate) fn sync_content_tabs(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let projects = self.project_catalog(cx);
        let available = self
            .contents
            .items()
            .iter()
            .filter(|item| match item {
                WorkspaceItem::Conversation(id) => {
                    self.chat.read(cx).chats.iter().any(|chat| &chat.id == id)
                }
                WorkspaceItem::Project(key) => projects.iter().any(|project| &project.key == key),
                // A task may be absent from the currently loaded/filterable page.
                WorkspaceItem::Task(_) => true,
                WorkspaceItem::App { name, panel } => self.apps.catalog.iter().any(|app| {
                    &app.name == name && app.enabled && app.panels.iter().any(|p| &p.id == panel)
                }),
            })
            .cloned()
            .collect::<Vec<_>>();
        let removed = self
            .contents
            .active()
            .filter(|item| !available.contains(item))
            .cloned();
        self.contents.reconcile(&available);
        if let Some(item) = self.current_content(cx) {
            self.contents.open(item);
        } else if removed.as_ref().is_some_and(|item| {
            matches!(
                (self.navigation, item),
                (0, WorkspaceItem::Conversation(_))
                    | (3, WorkspaceItem::Project(_))
                    | (7, WorkspaceItem::App { .. })
            )
        }) {
            if let Some(next) = self.contents.active().cloned() {
                self.activate_content(next, window, cx);
            } else {
                self.navigation = if matches!(removed, Some(WorkspaceItem::App { .. })) {
                    7
                } else {
                    1
                };
            }
        }
    }

    fn content_label(&self, item: &WorkspaceItem, cx: &App) -> (String, &'static str) {
        match item {
            WorkspaceItem::Conversation(id) => (
                self.chat
                    .read(cx)
                    .chats
                    .iter()
                    .find(|chat| &chat.id == id)
                    .map(|chat| chat.title.clone())
                    .unwrap_or_else(|| self.t("chat.session").into()),
                "message-circle",
            ),
            WorkspaceItem::Project(key) => (
                self.project_catalog(cx)
                    .into_iter()
                    .find(|project| &project.key == key)
                    .map(|project| project.name)
                    .unwrap_or_else(|| self.t("nav.projects").into()),
                "folder",
            ),
            WorkspaceItem::Task(id) => (
                self.remote
                    .tasks()
                    .iter()
                    .find(|task| task["id"].as_str() == Some(id))
                    .and_then(|task| task["title"].as_str())
                    .unwrap_or(id)
                    .into(),
                "layout-list",
            ),
            WorkspaceItem::App { name, .. } => (
                self.apps
                    .catalog
                    .iter()
                    .find(|app| &app.name == name)
                    .map(|app| app.display_name.clone())
                    .unwrap_or_else(|| name.clone()),
                "layout-grid",
            ),
        }
    }

    fn activate_content(
        &mut self,
        item: WorkspaceItem,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.contents.select(&item);
        match item {
            WorkspaceItem::Conversation(id) => {
                let index = self
                    .chat
                    .read(cx)
                    .chats
                    .iter()
                    .position(|chat| chat.id == id);
                self.navigation = 0;
                self.focus_chat(index, 4, cx);
            }
            WorkspaceItem::Project(key) => self.open_project(key, window, cx),
            WorkspaceItem::Task(id) => self.open_task(id, cx),
            WorkspaceItem::App { name, panel } => {
                if self.apps.select(&name, &panel) {
                    self.navigation = 7;
                    self.command(json!({"action":"apps_open","name":name,"panel":panel}), cx);
                }
            }
        }
        self.sync_follow(cx);
        cx.notify();
    }

    fn close_content(&mut self, item: &WorkspaceItem, window: &mut Window, cx: &mut Context<Self>) {
        let was_visible = self.current_content(cx).as_ref() == Some(item);
        self.contents.close(item);
        if let WorkspaceItem::App { name, panel } = item {
            if self
                .apps
                .active
                .as_ref()
                .is_some_and(|active| &active.name == name && &active.panel == panel)
            {
                self.apps.active = None;
            }
        }
        if was_visible {
            if let Some(next) = self.contents.active().cloned() {
                self.activate_content(next, window, cx);
            } else {
                self.navigation = if matches!(item, WorkspaceItem::App { .. }) {
                    7
                } else {
                    1
                };
                self.sync_follow(cx);
            }
        }
        cx.notify();
    }

    pub(crate) fn chat_visible(&self) -> bool {
        (self.navigation == 0 && self.tab == 4) || self.chat_dock_visible()
    }

    pub(crate) fn chat_dock_visible(&self) -> bool {
        self.preferences.layout.chat_open
            && (self.navigation == 3 || (self.navigation == 7 && self.apps.active.is_some()))
    }

    pub(crate) fn chat_dock_width(&self, viewport: f32, available: f32) -> f32 {
        if !self.chat_dock_visible() {
            return 0.;
        }
        if viewport < 1050. {
            available.max(0.)
        } else {
            self.preferences
                .layout
                .chat_width()
                .min(available * 0.55)
                .max(0.)
        }
    }

    pub(crate) fn set_chat_dock(&mut self, open: bool, cx: &mut Context<Self>) {
        self.preferences.layout.chat_open = open;
        self.save_preferences(self.preferences.clone(), cx);
        self.sync_follow(cx);
    }

    pub(crate) fn content_tabs(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        let current = self.current_content(cx);
        div()
            .id("workspace-tabs")
            .h(px(48.))
            .flex_shrink_0()
            .flex()
            .items_center()
            .border_b_1()
            .border_color(p.color("border"))
            .bg(p.color("bg"))
            .child(
                div()
                    .id("workspace-tab-scroll")
                    .flex_1()
                    .min_w_0()
                    .h_full()
                    .flex()
                    .items_center()
                    .overflow_x_scroll()
                    .children(self.contents.items().iter().enumerate().map(|(i, item)| {
                        let (title, icon) = self.content_label(item, cx);
                        let select = item.clone();
                        let close = item.clone();
                        div()
                            .w(px(200.))
                            .h_full()
                            .bg(p.color(if current.as_ref() == Some(item) {
                                "surface"
                            } else {
                                "bg"
                            }))
                            .flex_shrink_0()
                            .flex()
                            .items_center()
                            .gap(px(1.))
                            .px(px(3.))
                            .border_b_2()
                            .border_color(p.color(if current.as_ref() == Some(item) {
                                "action"
                            } else {
                                "border"
                            }))
                            .child(
                                Button::new(("workspace-tab", i))
                                    .ghost()
                                    .small()
                                    .flex_1()
                                    .min_w_0()
                                    .h(px(36.))
                                    .icon(
                                        Icon::default()
                                            .path(pero_chat_ui::assets::UiIcon::named(icon).path()),
                                    )
                                    .label(title.clone())
                                    .tooltip(title.clone())
                                    .accessibility_label(title.clone())
                                    .toggled(current.as_ref() == Some(item))
                                    .on_click(cx.listener(move |this, _, window, cx| {
                                        this.activate_content(select.clone(), window, cx)
                                    })),
                            )
                            .child(
                                Button::new(("workspace-tab-close", i))
                                    .ghost()
                                    .small()
                                    .w(px(26.))
                                    .h(px(28.))
                                    .icon(Icon::default().path("icons/x.svg"))
                                    .tooltip(self.t("workspace.close_tab"))
                                    .accessibility_label(format!(
                                        "{}: {title}",
                                        self.t("workspace.close_tab")
                                    ))
                                    .on_click(cx.listener(move |this, _, window, cx| {
                                        this.close_content(&close, window, cx)
                                    })),
                            )
                    })),
            )
            .when(self.navigation == 7 && self.apps.active.is_some(), |el| {
                el.child(
                    Button::new("workspace-apps")
                        .ghost()
                        .small()
                        .w(px(32.))
                        .h(px(36.))
                        .icon(Icon::default().path("icons/layout-grid.svg"))
                        .tooltip(self.t("apps.back"))
                        .accessibility_label(self.t("apps.back"))
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.apps.active = None;
                            cx.notify();
                        })),
                )
            })
            .when(
                self.navigation == 3 || (self.navigation == 7 && self.apps.active.is_some()),
                |el| {
                    el.child(
                        Button::new("workspace-chat-toggle")
                            .ghost()
                            .small()
                            .h(px(36.))
                            .mr(px(8.))
                            .icon(Icon::default().path(if self.chat_dock_visible() {
                                "icons/panel-right-close.svg"
                            } else {
                                "icons/panel-right-open.svg"
                            }))
                            .label(self.t(if self.chat_dock_visible() {
                                "chat.close_panel"
                            } else {
                                "chat.open_panel"
                            }))
                            .toggled(self.chat_dock_visible())
                            .on_click(cx.listener(|this, _, _, cx| {
                                this.set_chat_dock(!this.preferences.layout.chat_open, cx)
                            })),
                    )
                },
            )
    }

    pub(crate) fn chat_dock(
        &self,
        width: f32,
        narrow: bool,
        window: &Window,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        div()
            .id("workspace-chat-panel")
            .flex()
            .flex_col()
            .w(px(width))
            .h_full()
            .min_h_0()
            .flex_shrink_0()
            .relative()
            .border_l_1()
            .border_color(p.color("border"))
            .child(self.chat.clone())
            .when(!narrow, |el| {
                el.child(
                    div()
                        .id("workspace-chat-resize")
                        .track_focus(&self.chat_resize_focus)
                        .role(Role::Splitter)
                        .aria_label(self.t("workspace.resize_chat"))
                        .aria_numeric_value(self.preferences.layout.chat_width().into())
                        .when(self.chat_resize_focus.is_focused(window), |el| {
                            el.bg(p.color("focus"))
                        })
                        .absolute()
                        .top_0()
                        .left_0()
                        .w(px(6.))
                        .h_full()
                        .cursor(CursorStyle::ResizeLeftRight)
                        .on_mouse_down(
                            MouseButton::Left,
                            cx.listener(|this, _, window, cx| {
                                this.chat_resize_focus.focus(window, cx);
                                this.resizing_chat = true;
                                cx.notify();
                            }),
                        )
                        .on_key_down(cx.listener(|this, event: &KeyDownEvent, _, cx| {
                            let delta = match event.keystroke.key.as_str() {
                                "left" => 20.,
                                "right" => -20.,
                                _ => return,
                            };
                            let width = this.preferences.layout.chat_width() + delta;
                            this.preferences.layout.resize_chat(width);
                            this.save_preferences(this.preferences.clone(), cx);
                            cx.stop_propagation();
                        })),
                )
            })
    }

    pub(crate) fn workspace_keys(
        &mut self,
        event: &KeyDownEvent,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if event.keystroke.modifiers.control && event.keystroke.key == "tab" {
            if event.keystroke.modifiers.shift {
                self.contents.select_previous();
            } else {
                self.contents.select_next();
            }
            if let Some(item) = self.contents.active().cloned() {
                self.activate_content(item, window, cx);
            }
            cx.stop_propagation();
        }
    }
}
