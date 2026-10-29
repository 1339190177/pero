use crate::conversation::{AgentChoice, Conversation};
#[cfg(all(feature = "native", not(target_family = "wasm")))]
pub(crate) use crate::native::{request_locale, send_probe, session_command};
use crate::theme::Palettes;
use gpui_kit::{
    component::input::{TextareaState, InputEvent, InputState},
    *,
};
use pero_contracts::{EchoRequest, HostEvent, MAX_TEXT_CHARS};
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
    pub composer: Entity<TextareaState>,
    pub search: Entity<InputState>,
    pub chats: Vec<Conversation>,
    pub active_chat: Option<usize>,
    pub agents: Vec<AgentChoice>,
    pub agent_index: usize,
    pub model_menu: bool,
    pub workspace: String,
    pub workspace_repositories: Vec<pero_contracts::workbench::WorkspaceRepository>,
    pub show_details: bool,
    pub chat_scroll: ScrollHandle,
    pub task_scroll: ScrollHandle,
    /// A further task page was requested and its result has not arrived yet.
    pub task_more_pending: bool,
    pub attachments: Vec<pero_contracts::context::ContextReference>,
    pub composer_focused: bool,
    pub composer_restore: Option<gpui_kit::component::input::InputContent>,
    pub composer_picker_scroll: ScrollHandle,
    pub composer_state: crate::composer_state::ComposerState,
    pub expanded_tools: std::collections::HashSet<String>,
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
        let composer = cx.new(|cx| {
            TextareaState::new(window, cx)
                .submit_on_enter(true)
                .placeholder(pero_contracts::i18n::text(&locale, "chat.placeholder"))
        });
        let search = cx.new(|cx| {
            InputState::new(window, cx)
                .placeholder(pero_contracts::i18n::text(&locale, "sessions.search"))
        });
        let mut subscriptions = subscriptions;
        subscriptions.push(
            cx.subscribe_in(&composer, window, |this, _, event, window, cx| {
                match event {
                    InputEvent::PressEnter { shift: false, .. } => {
                        if this.composer_state.open {
                            this.select_composer_candidate(this.composer_state.selected, window, cx);
                        } else { this.send_chat(window, cx); }
                    }
                    InputEvent::Change => this.update_composer_completion(cx),
                    _ => {}
                }
                cx.notify();
            }),
        );
        subscriptions.push(cx.observe(&composer, |this, _, cx| this.update_composer_completion(cx)));
        subscriptions.push(cx.subscribe(&search, |_, _, _: &InputEvent, cx| cx.notify()));
        let project_binding=cx.new(|cx|InputState::new(window,cx));
        subscriptions.push(cx.subscribe(&project_binding,|this,_,event,cx| {
            if matches!(event,InputEvent::Change) {this.preferences_status=crate::settings_menu::PreferenceSaveState::Idle;}
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
            composer,
            search,
            chats: vec![],
            active_chat: None,
            agents: vec![],
            agent_index: 0,
            model_menu: false,
            workspace: String::new(),
            workspace_repositories: vec![],
            show_details: false,
            chat_scroll: ScrollHandle::new(),
            task_scroll: ScrollHandle::new(),
            task_more_pending: false,
            attachments: vec![],
            composer_state: Default::default(),
            composer_picker_scroll: ScrollHandle::new(),
            composer_restore: None,
            composer_focused: false,
            expanded_tools: Default::default(),
            _subscriptions: subscriptions,
        }
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
        if self.receive_roadmap(&event, cx) { return; }
        if self.receive_local_execution(&event, cx) { return; }
        if self.receive_composer(&event, cx) { return; }
        if self.receive_contribution(&event, cx) {
            return;
        }
        if self.receive_apps(&event, cx) {
            return;
        }
        if self.receive_remote(&event, cx) {
            return;
        }
        if self.receive_chat(&event, cx) {
            return;
        }
        match event.kind.as_str() {
            "workbench_settings" => {
                if let Ok(preferences) = serde_json::from_value::<pero_contracts::workbench::WorkbenchPreferences>(event.data.clone()) {
                    self.preferences = preferences;
                    if self.preferences_status == crate::settings_menu::PreferenceSaveState::Saving {
                        self.preferences_status = crate::settings_menu::PreferenceSaveState::Saved;
                    }
                    self.dark = self.preferences.appearance.dark(self.system_dark);
                    self.palettes.apply(self.dark, cx);
                    self.error = None;
                }
            }
            "settings" => {
                self.locale = pero_contracts::i18n::normalize(&event.text).into();
                gpui_kit::component::set_locale(&self.locale);
            }
            "ready" => self.connected = true,
            "disconnected" => {
                self.connected = false;
                self.pending = None;
                for chat in &mut self.chats {
                    chat.pending_model = None;
                }
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
                use gpui_kit::component::input::{SelectAll,Copy,Cut,Paste,Undo,Redo,MoveHome,MoveEnd,DeleteToBeginningOfLine,DeleteToEndOfLine};
                cx.bind_keys([
                    KeyBinding::new("cmd-a",SelectAll,Some("Input")),
                    KeyBinding::new("cmd-c",Copy,Some("Input")),
                    KeyBinding::new("cmd-x",Cut,Some("Input")),
                    KeyBinding::new("cmd-v",Paste,Some("Input")),
                    KeyBinding::new("cmd-z",Undo,Some("Input")),
                    KeyBinding::new("cmd-shift-z",Redo,Some("Input")),
                    KeyBinding::new("cmd-left",MoveHome,Some("Input")),
                    KeyBinding::new("cmd-right",MoveEnd,Some("Input")),
                    KeyBinding::new("cmd-backspace",DeleteToBeginningOfLine,Some("Input")),
                    KeyBinding::new("cmd-delete",DeleteToEndOfLine,Some("Input")),
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
            "projects":ui.project_catalog(),"workspaceRepositories":ui.workspace_repositories,
            "apps": ui.apps.catalog, "activeApp": ui.apps.active, "appError": ui.apps.error, "appsLoading": ui.apps.loading,
            "locale": ui.locale, "labels": { "tasks": ui.t("nav.tasks"), "send": ui.t("probe.send") },
            "tab": ui.tab, "events": ui.events,
            "chats": ui.chats, "activeChat": ui.active_chat, "agents": ui.agents,
            "composer": ui.composer.read(cx).value().as_ref(),"composerFocused":ui.composer_focused,"workspace":ui.workspace,
            "attachments": ui.attachments.iter().filter_map(|file|Some(file.label.as_str())).collect::<Vec<_>>(),
            "expandedTools":ui.expanded_tools,
            "modelMenu":ui.model_menu,
            "completion": {"open":ui.composer_state.open,"loading":ui.composer_state.loading,"error":ui.composer_state.error,"selected":ui.composer_state.selected,"items":ui.composer_picker_items().iter().map(|i|&i.label).collect::<Vec<_>>()},
            "toolViews":ui.chats.iter().map(|chat|chat.tools.iter().filter(|tool|tool.view.is_some()).count()).collect::<Vec<_>>(),
            "scroll": {"y":f32::from(ui.chat_scroll.offset().y),"max":f32::from(ui.chat_scroll.max_offset().y)},
            "renderStats":ui.chats.iter().map(|c|c.messages.iter().map(|m|m.view.read(cx).stats()).collect::<Vec<_>>()).collect::<Vec<_>>(),
            "renderedBytes":ui.chats.iter().map(|c|c.messages.iter().map(|m|m.view.read(cx).rendered_bytes(cx)).collect::<Vec<_>>()).collect::<Vec<_>>(),
        });
        value["preferencesStatus"]=serde_json::json!(ui.preferences_status);
        value["projectBinding"]=serde_json::json!(ui.project_binding.read(cx).value().as_ref());
        value["sidebarSearch"]=serde_json::json!(ui.search.read(cx).value().as_ref());
        value["localRoadmap"] = serde_json::json!(ui.local_roadmap);
        value["localExecution"] = ui.local_execution.snapshot(cx);
    });
    value.to_string()
}

/// Browser clipboard images enter through this UI-only bridge; Host validates the turn.
#[cfg(target_family = "wasm")]
#[wasm_bindgen]
pub fn paste_image(data: &str, mime_type: &str) {
    update(|ui,cx| ui.queue_composer_image(data.to_owned(),mime_type.to_owned(),cx));
}

#[cfg(target_family = "wasm")]
#[wasm_bindgen]
pub fn set_system_theme(dark: bool) {
    update(|ui, cx| {
        ui.system_dark = dark;
        ui.dark = ui.preferences.appearance.dark(dark);
        ui.palettes.apply(ui.dark, cx);
        cx.notify();
    });
}
