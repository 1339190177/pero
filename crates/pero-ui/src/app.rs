#[cfg(all(feature = "native", not(target_family = "wasm")))]
pub(crate) use crate::native::{request_locale, send_probe, session_command};
use crate::theme::Palettes;
use gpui_kit::{
    component::input::{InputEvent, InputState},
    *,
};
use pero_chat_ui::{ChatAction, ChatPanel};
use pero_contracts::{EchoRequest, HostEvent, MAX_TEXT_CHARS};
use serde_json::{Value, json};
#[cfg(target_family = "wasm")]
use std::{borrow::Cow, cell::RefCell, rc::Rc, sync::Arc};
#[cfg(target_family = "wasm")]
use wasm_bindgen::prelude::*;

#[cfg(target_family = "wasm")]
thread_local! {
    static APPLICATION: RefCell<Option<ApplicationHandle>> = const { RefCell::new(None) };
    static WORKBENCH: RefCell<Option<WeakEntity<Workbench>>> = const { RefCell::new(None) };
}

#[cfg(target_family = "wasm")]
#[wasm_bindgen]
extern "C" {
    #[wasm_bindgen(js_namespace = window, js_name = peroSendProbe, catch)]
    fn send_probe(json: &str) -> Result<(), JsValue>;
    #[wasm_bindgen(js_namespace = window, js_name = peroSetLocale, catch)]
    pub(crate) fn request_locale(locale: &str) -> Result<(), JsValue>;
    #[wasm_bindgen(js_namespace = window, js_name = peroSessionCommand, catch)]
    pub(crate) fn session_command(json: &str) -> Result<(), JsValue>;
}

pub(crate) struct Workbench {
    pub input: Entity<InputState>,
    pub locale: String,
    pub input_locale: String,
    pub palettes: Palettes,
    pub dark: bool,
    pub system_dark: bool,
    pub preferences: pero_contracts::workbench::WorkbenchPreferences,
    pub preferences_status: crate::settings_menu::PreferenceSaveState,
    pub settings_menu_open: bool,
    pub recent_tasks: Vec<String>,
    pub selected_project: Option<String>,
    pub project_binding: Entity<InputState>,
    pub collapsed_projects: std::collections::HashSet<String>,
    pub project_tab: crate::projects::ProjectTab,
    pub(crate) local_roadmap: crate::local_roadmap::RoadmapState,
    pub task_status: crate::projects::TaskStatusFilter,
    pub pending_project_workspace: bool,
    pub navigation: usize,
    pub remote: crate::remote::RemoteState,
    pub local_execution: crate::local_execution::LocalExecution,
    pub contribution: crate::contributions::ContributionState,
    pub apps: crate::apps::AppWorkspace,
    pub app_surface: String,
    pub tab: usize,
    pub connected: bool,
    pub pending: Option<String>,
    pub next_request: u64,
    pub received: usize,
    pub last_echo: String,
    pub error: Option<String>,
    pub events: Vec<HostEvent>,
    pub chat: Entity<ChatPanel>,
    pub search: Entity<InputState>,
    pub workspace: String,
    pub workspace_repositories: Vec<pero_contracts::workbench::WorkspaceRepository>,
    pub task_scroll: ScrollHandle,
    /// A further task page was requested and its result has not arrived yet.
    pub task_more_pending: bool,
    _subscriptions: Vec<Subscription>,
}

impl Workbench {
    pub(crate) fn new(
        palettes: Palettes,
        dark: bool,
        locale: String,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Self {
        let input = cx.new(|cx| {
            InputState::new(window, cx)
                .placeholder(pero_contracts::i18n::text(&locale, "input.placeholder"))
        });
        let subscriptions = vec![cx.subscribe_in(&input, window, |this, _, event, _, cx| {
            if matches!(event, InputEvent::PressEnter { .. }) {
                this.send(cx);
            }
            cx.notify();
        })];
        let chat = cx.new(|cx| ChatPanel::new(palettes.clone(), dark, locale.clone(), window, cx));
        let search = cx.new(|cx| {
            InputState::new(window, cx)
                .placeholder(pero_contracts::i18n::text(&locale, "sessions.search"))
        });
        let mut subscriptions = subscriptions;
        subscriptions.push(cx.observe(&chat, |_, _, cx| cx.notify()));
        subscriptions.push(cx.subscribe(&chat, |this, _, event, cx| {
            match event {
                ChatAction::Command(command) => this.command(
                    serde_json::to_value(command).expect("serializable command"),
                    cx,
                ),
                ChatAction::ChooseWorkspace => {
                    this.command(json!({"action":"choose_workspace"}), cx)
                }
                ChatAction::ChooseContext { kind, request_id } => this.command(
                    json!({"action":"choose_context","kind":kind,"request_id":request_id}),
                    cx,
                ),
                ChatAction::Activated => {
                    if this.navigation != 7 || !this.apps.chat_open {
                        this.navigation = 0;
                    }
                    this.tab = 4;
                    this.error = None;
                    this.sync_follow(cx);
                }
                ChatAction::ReturnToReview => {
                    this.remote.public_contributions = true;
                    this.navigation = 3;
                    this.sync_follow(cx);
                }
            }
            cx.notify();
        }));
        subscriptions.push(cx.subscribe(&search, |_, _, _: &InputEvent, cx| cx.notify()));
        let project_binding = cx.new(|cx| InputState::new(window, cx));
        subscriptions.push(cx.subscribe(&project_binding, |this, _, event, cx| {
            if matches!(event, InputEvent::Change) {
                this.preferences_status = crate::settings_menu::PreferenceSaveState::Idle;
            }
            cx.notify();
        }));
        let remote = crate::remote::RemoteState::new(window, cx);
        let local_execution = crate::local_execution::LocalExecution::new(&locale, window, cx);
        Self {
            remote,
            local_execution,
            contribution: Default::default(),
            apps: crate::apps::AppWorkspace::default(),
            app_surface: String::new(),
            input,
            input_locale: String::new(),
            locale,
            palettes,
            dark,
            system_dark: dark,
            preferences: Default::default(),
            preferences_status: Default::default(),
            settings_menu_open: false,
            recent_tasks: Vec::new(),
            selected_project: None,
            project_binding,
            collapsed_projects: Default::default(),
            project_tab: Default::default(),
            local_roadmap: Default::default(),
            task_status: Default::default(),
            pending_project_workspace: false,
            navigation: 1,
            tab: 4,
            connected: false,
            pending: None,
            next_request: 0,
            received: 0,
            last_echo: String::new(),
            error: None,
            events: Vec::new(),
            chat,
            search,
            workspace: String::new(),
            workspace_repositories: vec![],
            task_scroll: ScrollHandle::new(),
            task_more_pending: false,
            _subscriptions: subscriptions,
        }
    }

    pub fn sync_follow(&mut self, cx: &mut Context<Self>) {
        let visible = (self.navigation == 0 && self.tab == 4)
            || (self.navigation == 7 && self.apps.active.is_some() && self.apps.chat_open);
        self.chat
            .update(cx, |chat, cx| chat.set_visible(visible, cx));
    }

    pub fn focus_chat(&mut self, index: Option<usize>, tab: usize, cx: &mut Context<Self>) {
        self.tab = tab;
        self.chat.update(cx, |chat, cx| {
            chat.set_scope(None, cx);
            chat.focus_chat(index, cx);
        });
        self.sync_follow(cx);
        cx.notify();
    }

    pub fn new_chat(&mut self, cx: &mut Context<Self>) {
        self.navigation = 0;
        self.tab = 4;
        self.error = None;
        self.chat
            .update(cx, |chat, cx| chat.new_chat_in_scope(None, cx));
        self.sync_follow(cx);
        cx.notify();
    }

    pub fn command(&mut self, value: Value, cx: &mut Context<Self>) {
        if value["action"] == "choose_workspace"
            && (self.navigation != 3 || self.project_tab != crate::projects::ProjectTab::Settings)
        {
            self.pending_project_workspace = false;
        }
        if session_command(&value.to_string()).is_err() {
            self.chat.update(cx, |panel, cx| {
                if let Some(chat) = panel
                    .active_chat
                    .and_then(|index| panel.chats.get_mut(index))
                {
                    chat.pending_model = None;
                    chat.pending_setting = None;
                }
                cx.notify();
            });
            self.error = Some("error.connection".into());
        }
        cx.notify();
    }

    pub fn send(&mut self, cx: &mut Context<Self>) {
        let text = self.input.read(cx).value().to_string();
        if !self.connected || self.pending.is_some() {
            return;
        }
        if text.trim().is_empty() || text.chars().count() > MAX_TEXT_CHARS {
            self.error = Some("error.input_length".into());
            cx.notify();
            return;
        }
        self.next_request += 1;
        let id = format!("probe-{}", self.next_request);
        let request = EchoRequest {
            id: id.clone(),
            text,
        };
        self.error = None;
        self.pending = Some(id);
        if send_probe(&serde_json::to_string(&request).expect("serializable request")).is_err() {
            self.error = Some("error.send".into());
            self.pending = None;
        }
        cx.notify();
    }

    pub(crate) fn receive(&mut self, event: HostEvent, cx: &mut Context<Self>) {
        if self.receive_roadmap(&event, cx) {
            return;
        }
        if self.receive_local_execution(&event, cx) {
            return;
        }
        if self.receive_contribution(&event, cx) {
            return;
        }
        if self.receive_apps(&event, cx) {
            return;
        }
        if self.receive_remote(&event, cx) {
            return;
        }
        let handled = self.chat.update(cx, |chat, cx| chat.receive(&event, cx));
        if event.kind == "agents" {
            self.workspace = event.text.clone();
        }
        if event.kind == "workspace" {
            self.workspace = event.text.clone();
            if let Ok(identity) = serde_json::from_value::<
                pero_contracts::workbench::WorkspaceIdentity,
            >(event.data.clone())
            {
                self.workspace_repositories
                    .retain(|entry| entry.workspace != event.text);
                if let Some(repository) = identity.repository {
                    self.workspace_repositories.push(
                        pero_contracts::workbench::WorkspaceRepository {
                            workspace: event.text.clone(),
                            repository,
                        },
                    );
                }
            }
            self.reconcile_project_selection(cx);
            if self.pending_project_workspace {
                self.pending_project_workspace = false;
                if let Some(id) = self
                    .current_project(cx)
                    .and_then(|project| project.shared_id)
                {
                    let mut preferences = self.preferences.clone();
                    preferences.bind(event.text.clone(), id);
                    self.save_preferences(preferences, cx);
                    self.selected_project = Some(format!("workspace:{}", event.text));
                }
            }
        }
        if handled && !matches!(event.kind.as_str(), "ready" | "disconnected" | "settings") {
            cx.notify();
            return;
        }
        match event.kind.as_str() {
            "workbench_settings" => {
                if let Ok(preferences) = serde_json::from_value::<
                    pero_contracts::workbench::WorkbenchPreferences,
                >(event.data.clone())
                {
                    self.preferences = preferences;
                    if self.preferences_status == crate::settings_menu::PreferenceSaveState::Saving
                    {
                        self.preferences_status = crate::settings_menu::PreferenceSaveState::Saved;
                    }
                    self.dark = self.preferences.appearance.dark(self.system_dark);
                    self.palettes.apply(self.dark, cx);
                    self.chat
                        .update(cx, |chat, cx| chat.set_context(self.dark, &self.locale, cx));
                    self.error = None;
                }
            }
            "settings" => {
                self.locale = pero_contracts::i18n::normalize(&event.text).into();
                gpui_kit::component::set_locale(&self.locale);
                self.chat
                    .update(cx, |chat, cx| chat.set_context(self.dark, &self.locale, cx));
            }
            "ready" => self.connected = true,
            "disconnected" => {
                self.connected = false;
                self.pending = None;
                self.chat.update(cx, |panel, cx| {
                    for chat in &mut panel.chats {
                        chat.pending_model = None;
                    }
                    cx.notify();
                });
                self.error = Some(event.message_key.clone().unwrap_or_else(|| {
                    if event.text.is_empty() {
                        "error.connection".into()
                    } else {
                        event.text.clone()
                    }
                }));
            }
            "error" => {
                if event.message_key.as_deref() == Some("error.preferences") {
                    self.preferences_status = crate::settings_menu::PreferenceSaveState::Idle;
                }
                self.error = Some(event.message_key.clone().unwrap_or_else(|| {
                    if event.text.is_empty() {
                        "error.connection".into()
                    } else {
                        event.text.clone()
                    }
                }));
                if event.request_id == self.pending {
                    self.pending = None;
                }
            }
            "echo" if event.request_id == self.pending => {
                self.pending = None;
                self.received += 1;
                self.last_echo = event.text.clone();
            }
            _ => {}
        }
        self.events.push(event);
        if self.events.len() > 100 {
            self.events.remove(0);
        }
        cx.notify();
    }
    pub fn t(&self, key: &str) -> &'static str {
        pero_contracts::i18n::text(&self.locale, key)
    }
    pub fn event_text(&self, event: &HostEvent) -> String {
        event
            .message_key
            .as_deref()
            .map(|key| self.t(key).to_string())
            .unwrap_or_else(|| event.text.clone())
    }
}

#[cfg(target_family = "wasm")]
#[wasm_bindgen]
pub fn run(theme_json: &str, dark: bool, locale: &str) -> Result<(), JsValue> {
    let locale = pero_contracts::i18n::normalize(locale).to_string();
    console_error_panic_hook::set_once();
    let palettes: Palettes =
        serde_json::from_str(theme_json).map_err(|e| JsValue::from_str(&e.to_string()))?;
    gpui_kit::platform::web_init();
    use gpui_kit::web::{CanvasFontFallback, WebBackendPreference, WebPlatform};
    let platform = Rc::new(WebPlatform::new_with_backend_and_font_fallback(
        false,
        WebBackendPreference::Auto,
        CanvasFontFallback::EmojiAndCjk,
    ));
    let http = Arc::new(platform.fetch_http_client());
    let handle = Application::with_platform(platform)
        .with_assets(crate::assets::UiAssets)
        .with_http_client(http)
        .run_embedded(move |cx| {
            // Catalog glyphs are bundled; arbitrary CJK text uses the canvas fallback above.
            cx.text_system()
                .add_fonts(vec![
                    Cow::Borrowed(
                        include_bytes!("../../../web/public/fonts/Inter-Regular.ttf").as_slice(),
                    ),
                    Cow::Borrowed(
                        include_bytes!("../../../web/public/fonts/Workbench-regular.ttf")
                            .as_slice(),
                    ),
                    Cow::Borrowed(
                        include_bytes!("../../../web/public/fonts/Workbench-bold.ttf").as_slice(),
                    ),
                    Cow::Borrowed(
                        include_bytes!("../../../web/public/fonts/IBMPlexSans-Regular.ttf")
                            .as_slice(),
                    ),
                    Cow::Borrowed(
                        include_bytes!("../../../web/public/fonts/JetBrainsMono-Regular.ttf")
                            .as_slice(),
                    ),
                ])
                .expect("bundled fonts");
            gpui_kit::init(cx);
            // Wasm has no target_os=macos; bind ordinary editing to the actual OS.
            if cx.platform_info().is_mac() {
                use gpui_kit::component::input::{
                    Copy, Cut, DeleteToBeginningOfLine, DeleteToEndOfLine, MoveEnd, MoveHome,
                    Paste, Redo, SelectAll, Undo,
                };
                cx.bind_keys([
                    KeyBinding::new("cmd-a", SelectAll, Some("Input")),
                    KeyBinding::new("cmd-c", Copy, Some("Input")),
                    KeyBinding::new("cmd-x", Cut, Some("Input")),
                    KeyBinding::new("cmd-v", Paste, Some("Input")),
                    KeyBinding::new("cmd-z", Undo, Some("Input")),
                    KeyBinding::new("cmd-shift-z", Redo, Some("Input")),
                    KeyBinding::new("cmd-left", MoveHome, Some("Input")),
                    KeyBinding::new("cmd-right", MoveEnd, Some("Input")),
                    KeyBinding::new("cmd-backspace", DeleteToBeginningOfLine, Some("Input")),
                    KeyBinding::new("cmd-delete", DeleteToEndOfLine, Some("Input")),
                ]);
            }
            gpui_kit::component::set_locale(&locale);
            palettes.apply(dark, cx);
            gpui_kit::open_window(WindowOptions::default(), cx, |window, cx| {
                let view = cx.new(|cx| Workbench::new(palettes, dark, locale, window, cx));
                WORKBENCH.with(|slot| *slot.borrow_mut() = Some(view.downgrade()));
                view
            })
            .expect("workbench window");
        });
    APPLICATION.with(|slot| *slot.borrow_mut() = Some(handle));
    Ok(())
}

#[cfg(target_family = "wasm")]
#[wasm_bindgen]
pub fn receive_host_event(json: &str) -> Result<(), JsValue> {
    let event: HostEvent =
        serde_json::from_str(json).map_err(|e| JsValue::from_str(&e.to_string()))?;
    if event.version != pero_contracts::PROTOCOL_VERSION {
        return Err(JsValue::from_str("Unsupported event version"));
    }
    update(|ui, cx| ui.receive(event, cx));
    Ok(())
}

#[cfg(target_family = "wasm")]
fn update(f: impl FnOnce(&mut Workbench, &mut Context<Workbench>)) {
    APPLICATION.with(|app| {
        if let Some(app) = app.borrow().as_ref() {
            app.update(|cx| {
                WORKBENCH.with(|view| {
                    if let Some(view) = view.borrow().as_ref() {
                        let _ = view.update(cx, f);
                    }
                })
            });
        }
    });
}

/// Read-only diagnostic state. Tests still enter text through Chromium input.
#[cfg(target_family = "wasm")]
#[wasm_bindgen]
pub fn snapshot() -> String {
    let mut value = serde_json::json!({"ready": false});
    update(|ui, cx| {
        value = serde_json::json!({
            "ready": true, "remote": ui.remote.snapshot(cx), "contribution": ui.contribution.data, "publicContributions": ui.remote.public_contributions, "connected": ui.connected, "input": ui.input.read(cx).value().as_ref(),
            "received": ui.received, "lastEcho": ui.last_echo, "pending": ui.pending,
            "error": ui.error, "dark": ui.dark, "navigation": ui.navigation,
            "preferences":ui.preferences,"selectedProject":ui.selected_project,"projectTab":ui.project_tab,"taskStatus":ui.task_status,
            "projects":ui.project_catalog(cx),"workspaceRepositories":ui.workspace_repositories,
            "apps": ui.apps.catalog, "activeApp": ui.apps.active, "appError": ui.apps.error, "appsLoading": ui.apps.loading, "appChatOpen": ui.apps.chat_open,
            "locale": ui.locale, "labels": { "tasks": ui.t("nav.tasks"), "send": ui.t("probe.send") },
            "tab": ui.tab, "events": ui.events, "workspace": ui.workspace,
        });
        value["preferencesStatus"] = serde_json::json!(ui.preferences_status);
        value["projectBinding"] = serde_json::json!(ui.project_binding.read(cx).value().as_ref());
        value["sidebarSearch"] = serde_json::json!(ui.search.read(cx).value().as_ref());
        if let Some(fields) = ui.chat.read(cx).snapshot(cx).as_object() {
            value.as_object_mut().expect("workbench snapshot").extend(
                fields
                    .iter()
                    .filter(|(key, _)| {
                        !matches!(
                            key.as_str(),
                            "connected" | "error" | "dark" | "locale" | "workspace"
                        )
                    })
                    .map(|(key, value)| (key.clone(), value.clone())),
            );
        }
        value["localRoadmap"] = serde_json::json!(ui.local_roadmap);
        value["localExecution"] = ui.local_execution.snapshot(&ui.locale, cx);
    });
    value.to_string()
}

/// Browser clipboard images enter through this UI-only bridge; Host validates the turn.
#[cfg(target_family = "wasm")]
#[wasm_bindgen]
pub fn paste_image(data: &str, mime_type: &str) {
    update(|ui, cx| {
        ui.chat.update(cx, |chat, cx| {
            chat.queue_composer_image(data.to_owned(), mime_type.to_owned(), cx)
        })
    });
}

#[cfg(target_family = "wasm")]
#[wasm_bindgen]
pub fn set_system_theme(dark: bool) {
    update(|ui, cx| {
        ui.system_dark = dark;
        ui.dark = ui.preferences.appearance.dark(dark);
        ui.palettes.apply(ui.dark, cx);
        ui.chat
            .update(cx, |chat, cx| chat.set_context(ui.dark, &ui.locale, cx));
        cx.notify();
    });
}
