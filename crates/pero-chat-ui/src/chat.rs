use crate::{
    conversation::{Conversation, StreamItem},
    panel::ChatPanel,
};
use gpui_kit::{
    component::{button::*, *},
    prelude::*,
    *,
};
use pero_contracts::HostEvent;
use serde_json::json;

enum RenderItem {
    Message(usize),
    Tools(Vec<usize>),
}
fn render_items(chat: &Conversation) -> Vec<RenderItem> {
    let mut rows = Vec::new();
    for item in &chat.timeline {
        match *item {
            StreamItem::Message(i) => rows.push(RenderItem::Message(i)),
            StreamItem::Tool(i) => {
                if chat.tools.get(i).is_some_and(|tool| {
                    tool.cancelled && !crate::tool_ui::visible_output(&tool.content, &tool.output)
                }) {
                    continue;
                }
                let compact = chat
                    .tools
                    .get(i)
                    .is_some_and(|t| matches!(t.kind.as_str(), "read" | "search" | "fetch"));
                if compact {
                    if let Some(RenderItem::Tools(tools)) = rows.last_mut() {
                        if tools.iter().all(|index| {
                            chat.tools.get(*index).is_some_and(|t| {
                                matches!(t.kind.as_str(), "read" | "search" | "fetch")
                            })
                        }) {
                            tools.push(i);
                            continue;
                        }
                    }
                }
                rows.push(RenderItem::Tools(vec![i]));
            }
        }
    }
    rows
}

impl ChatPanel {
    pub fn sync_follow(&mut self, cx: &mut Context<Self>) {
        for (i, chat) in self.chats.iter_mut().enumerate() {
            let enabled = self.active_chat == Some(i) && self.visible;
            let open_tools = if enabled {
                render_items(chat)
                    .into_iter()
                    .filter_map(|item| match item {
                        RenderItem::Tools(indices) => {
                            let first = chat.tools.get(*indices.first()?)?;
                            self.expanded_tools
                                .contains(&format!("{}/{}", chat.id, first.id))
                                .then_some(indices)
                        }
                        _ => None,
                    })
                    .flatten()
                    .collect::<std::collections::HashSet<_>>()
            } else {
                std::collections::HashSet::new()
            };
            for message in &chat.messages {
                message.view.update(cx, |view, _| {
                    view.set_scroll_handle(enabled.then(|| self.chat_scroll.clone()))
                });
            }
            for (index, tool) in chat.tools.iter_mut().enumerate() {
                tool.set_body_open(open_tools.contains(&index), cx);
                if let Some(view) = &tool.view {
                    view.update(cx, |view, _| {
                        view.set_scroll_handle(enabled.then(|| self.chat_scroll.clone()))
                    });
                }
            }
        }
    }
    pub fn focus_chat(&mut self, index: Option<usize>, cx: &mut Context<Self>) {
        if self.active_chat != index {
            if let Some(chat) = self.active_chat.and_then(|i| self.chats.get_mut(i)) {
                chat.draft = self
                    .composer_restore
                    .clone()
                    .unwrap_or_else(|| self.composer.read(cx).content());
                chat.draft_references = self.composer_state.references.clone();
            }
            self.composer_state = Default::default();
            self.attachments.clear();
            if let Some(chat) = index.and_then(|i| self.chats.get(i)) {
                self.composer_restore = Some(chat.draft.clone());
                self.composer_state.references = chat.draft_references.clone();
            } else {
                self.composer_restore = Some(gpui_kit::component::input::InputContent::new(""));
            }
        }
        let previous=self.active_chat.and_then(|i|self.chats.get(i)).map(|c|c.id.clone());
        self.active_chat = index;
        let thread_id=index.and_then(|i|self.chats.get(i)).map(|c|c.id.clone());
        if previous!=thread_id {cx.emit(crate::ChatAction::Selected{thread_id});}
        self.sync_follow(cx);
        cx.notify();
    }
    pub fn delete_chat(&mut self, index: usize, cx: &mut Context<Self>) {
        let Some(chat) = self.chats.get(index) else {
            self.deleting_chat = None;
            cx.notify();
            return;
        };
        if chat.state == "draft" {
            self.forget_chat(index, cx);
            return;
        }
        let id = chat.id.clone();
        self.command(json!({"action":"delete","thread_id":id}), cx);
    }
    fn forget_chat(&mut self, index: usize, cx: &mut Context<Self>) {
        let Some(chat) = self.chats.get(index) else {
            return;
        };
        let id = chat.id.clone();
        let active = self.active_chat == Some(index);
        self.expanded_reasoning.retain(|(thread, _)| thread != &id);
        self.expanded_tools
            .retain(|key| !key.starts_with(&format!("{id}/")));
        self.chats.remove(index);
        self.active_chat = match self.active_chat {
            Some(current) if current == index => None,
            Some(current) if current > index => Some(current - 1),
            current => current,
        };
        if self.deleting_chat.as_deref() == Some(id.as_str()) {
            self.deleting_chat = None;
        }
        if active {
            self.composer_restore = Some(gpui_kit::component::input::InputContent::new(""));
            self.composer_state = Default::default();
            self.attachments.clear();
        }
        self.sync_follow(cx);
        cx.notify();
    }
    pub fn new_chat(&mut self, cx: &mut Context<Self>) {
        self.new_draft(cx);
        cx.emit(crate::ChatAction::Activated);
    }

    pub(crate) fn new_draft(&mut self, cx: &mut Context<Self>) {
        let agent = self
            .agents
            .get(self.agent_index)
            .map(|a| a.id.clone())
            .unwrap_or("codex".into());
        #[cfg(target_family = "wasm")]
        let now = js_sys::Date::now() as u64;
        #[cfg(not(target_family = "wasm"))]
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default()
            .as_millis() as u64;
        let id = format!("chat-{}-{}", now, self.chats.len());
        self.chats
            .push(Conversation::new(id, agent, self.t("chat.new").into()));
        self.chats.last_mut().unwrap().workspace = self.workspace.clone();
        self.chats.last_mut().unwrap().scope = self.scope.clone();
        self.error = None;
        self.focus_chat(Some(self.chats.len() - 1), cx);
    }
    pub fn connect_chat(&mut self, cx: &mut Context<Self>) {
        if let Some(i) = self.active_chat {
            if self.chats[i].state == "draft" {
                self.chats[i].state = "connecting".into();
                self.command(json!({"action":"new_session","thread_id":self.chats[i].id,"agent_id":self.chats[i].agent,"scope":self.chats[i].scope}),cx);
            }
        }
    }
    pub fn send_chat(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let text = self.composer.read(cx).value().trim_end().to_string();
        let contexts = self.attachments.clone();
        if text.trim().is_empty() && contexts.is_empty()
            || text.chars().count() > 64000
            || !self.connected
            || self.composer_state.loading && self.composer_state.resolve_range.is_some()
        {
            return;
        }
        if self.active_chat.is_none() {
            self.new_chat(cx);
        }
        let index = self.active_chat.unwrap();
        let chat = &mut self.chats[index];
        if !matches!(chat.state.as_str(), "draft" | "idle")
            || chat.pending_model.is_some()
            || chat.pending_setting.is_some()
        {
            return;
        }
        if contexts
            .iter()
            .any(|r| r.kind == pero_contracts::context::ContextKind::Image)
            && chat.state != "draft" && !chat.capabilities.images
        {
            chat.error =
                Some(pero_contracts::i18n::text(&self.locale, "composer.image_unsupported").into());
            cx.notify();
            return;
        }
        if chat.messages.is_empty() {
            chat.title = Conversation::title_from(&text);
        }
        chat.error = None;
        let value = if chat.state == "draft" {
            chat.state = "connecting".into();
            chat.queued = Some(text);
            chat.queued_contexts = contexts;
            json!({"action":"new_session","thread_id":chat.id,"agent_id":chat.agent,"scope":chat.scope})
        } else {
            chat.state = "running".into();
            json!({"action":"prompt","thread_id":chat.id,"text":text,"contexts":contexts})
        };
        self.attachments.clear();
        self.composer_state = Default::default();
        chat.draft = gpui_kit::component::input::InputContent::new("");
        chat.draft_references.clear();
        self.composer
            .update(cx, |input, cx| input.set_value("", window, cx));
        crate::stream::scroll_after_layout(&self.chat_scroll, window);
        self.command(value, cx);
    }
    pub fn receive_chat(&mut self, event: &HostEvent, cx: &mut Context<Self>) -> bool {
        if event.kind == "replay" {
            // A refresh is a history snapshot, not a reset of the retained editor
            // or of newer live events that this surface has already observed.
            let selected = self
                .active_chat
                .and_then(|index| self.chats.get(index))
                .map(|chat| chat.id.clone());
            let retained = self
                .chats
                .iter()
                .map(|chat| chat.id.clone())
                .collect::<std::collections::HashSet<_>>();
            if let Some(events) = event.data["events"].as_array() {
                for value in events {
                    if let Ok(replayed) = serde_json::from_value::<HostEvent>(value.clone()) {
                        if replayed
                            .thread_id
                            .as_ref()
                            .is_some_and(|id| !retained.contains(id))
                        {
                            self.receive_chat(&replayed, cx);
                        }
                    }
                }
            }
            for chat in &mut self.chats {
                let active = event.data["active"]
                    .as_array()
                    .is_some_and(|ids| ids.iter().any(|id| id.as_str() == Some(&chat.id)));
                if !active && chat.state != "draft" {
                    chat.state = "closed".into();
                    chat.queued = None;
                    chat.queued_contexts.clear();
                    chat.permission = None;
                    chat.pending_model = None;
                    chat.pending_setting = None;
                    chat.flush(cx);
                }
            }
            let initial_empty = !self.replayed
                && self.pending_prompts.is_empty()
                && self.composer.read(cx).value().is_empty()
                && selected
                    .as_ref()
                    .and_then(|id| self.chats.iter().find(|chat| &chat.id == id))
                    .is_some_and(|chat| chat.state == "draft" && chat.draft.text().is_empty());
            let restored = initial_empty
                .then(|| {
                    self.chats
                        .iter()
                        .rposition(|chat| chat.scope == self.scope && chat.state != "draft")
                })
                .flatten();
            let next = restored
                .or_else(|| {
                    selected
                        .as_ref()
                        .and_then(|id| self.chats.iter().position(|chat| &chat.id == id))
                })
                .or_else(|| self.chats.iter().rposition(|chat| chat.scope == self.scope));
            self.replayed = true;
            self.focus_chat(next, cx);
            cx.notify();
            return true;
        }
        if event.kind == "agents" {
            self.agents = serde_json::from_value(event.data.clone()).unwrap_or_default();
            self.workspace = event.text.clone();
            cx.notify();
            return true;
        }
        if event.kind == "context_files" {
            self.next_request += 1;
            self.composer_state.pending_files = event
                .data
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(|file| {
                    let source = file["path"].as_str()?.to_string();
                    Some(pero_contracts::context::ContextReference {
                        id: format!("file:{}:{source}", self.next_request),
                        kind: pero_contracts::context::ContextKind::File,
                        label: file["name"].as_str()?.into(),
                        source,
                        content: pero_contracts::context::ContextContent::Text {
                            text: file["text"].as_str()?.into(),
                        },
                    })
                })
                .collect();
            cx.notify();
            return true;
        }
        if event.kind == "workspace" {
            self.workspace = event.text.clone();
            cx.notify();
            return true;
        }
        let Some(id) = event.thread_id.as_ref() else {
            return false;
        };
        if event.kind == "session_created" && !self.chats.iter().any(|c| &c.id == id) {
            self.chats.push(Conversation::new(
                id.clone(),
                event.data["agent"].as_str().unwrap_or("ACP").into(),
                self.t("chat.new").into(),
            ));
        }
        let Some(index) = self.chats.iter().position(|c| &c.id == id) else {
            return true;
        };
        if event.kind == "session_deleted" {
            self.forget_chat(index, cx);
            return true;
        }
        let chat = &mut self.chats[index];
        match event.kind.as_str() {
            "session_created" => {
                chat.workspace = event.data["workspace"].as_str().unwrap_or_default().into();
                chat.scope = event
                    .data
                    .get("scope")
                    .and_then(|value| serde_json::from_value(value.clone()).ok());
            }
            "agent_info" => {
                chat.capabilities =
                    pero_contracts::composer_capabilities::AgentCapabilities::from_initialize(
                        &event.data,
                    )
                    .unwrap_or_default();
                chat.auth_methods = event.data["authMethods"]
                    .as_array()
                    .cloned()
                    .unwrap_or_default()
            }
            "session_authenticated" => {
                chat.state = "idle".into();
                chat.error = None;
            }
            "session_auth_required" | "session_auth_error" => {
                chat.auth_methods = event.data["authMethods"]
                    .as_array()
                    .cloned()
                    .unwrap_or_else(|| chat.auth_methods.clone());
                chat.error = Some(event.text.clone());
                chat.state = "auth_required".into();
            }
            "user_message" => {
                if chat.messages.is_empty() {
                    chat.title = Conversation::title_from(&event.text);
                }
                chat.push_user(event.text.clone(), cx);
                if let Some(message) = chat.messages.last_mut() {
                    message.attached_files = event.data["contexts"]
                        .as_array()
                        .into_iter()
                        .flatten()
                        .filter_map(|r| r["label"].as_str().map(str::to_string))
                        .collect();
                }
            }
            "session_connecting" => chat.state = "connecting".into(),
            "session_ready" => {
                chat.session_id = event.data["sessionId"].as_str().unwrap_or_default().into();
                chat.config(&event.data);
                chat.state = "idle".into();
                if let Some(text) = chat.queued.take() {
                    chat.state = "running".into();
                    let contexts = std::mem::take(&mut chat.queued_contexts);
                    if !chat.capabilities.images && contexts.iter().any(|r|r.kind==pero_contracts::context::ContextKind::Image){
                        chat.state="idle".into();chat.error=Some(pero_contracts::i18n::text(&self.locale,"composer.image_unsupported").into());
                        self.pending_prompts.push(crate::panel::ComposerInsertion{thread_id:chat.id.clone(),text,contexts,replace:false});
                        cx.notify();return true;
                    }
                    let value = json!({"action":"prompt","thread_id":chat.id,"text":text,"contexts":contexts});
                    self.command(value, cx);
                }
            }
            "session_update" => {
                if !chat.update(&event.data, cx) {
                    return true;
                }
            }
            "model_changed" => {
                chat.config(&event.data);
                chat.pending_model = None;
            }
            "configuration_changed" => {
                if let Ok(settings) = serde_json::from_value(event.data.clone()) {
                    chat.settings = settings;
                }
                chat.pending_setting = None;
            }
            "session_usage" => {
                if let Ok(usage) = serde_json::from_value(event.data.clone()) {
                    chat.usage = Some(usage);
                }
            }
            "turn_started" => chat.state = "running".into(),
            "turn_finished" => {
                chat.flush(cx);
                chat.state = "idle".into();
                chat.permission = None;
            }
            "permission" => chat.permission = Some(event.data.clone()),
            "session_error" => {
                chat.pending_setting = None;
                chat.pending_model = None;
                chat.flush(cx);
                chat.error = Some(event.text.clone());
                chat.state = "idle".into();
                chat.permission = None;
            }
            "session_closed" => {
                chat.pending_setting = None;
                chat.pending_model = None;
                chat.flush(cx);
                chat.state = "closed".into();
                chat.permission = None;
            }
            _ => {}
        }
        if self.composer_state.open
            && matches!(
                self.composer_state.completion.as_ref().map(|q| &q.kind),
                Some(crate::composer_state::CompletionKind::Slash)
            )
            && event.data["sessionUpdate"] == "available_commands_update"
        {
            self.refresh_composer_commands();
        }
        self.sync_follow(cx);
        cx.notify();
        true
    }
    pub fn chat_view(&self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if crate::stream::near_bottom(&self.chat_scroll) {
            crate::stream::scroll_after_layout(&self.chat_scroll, window);
        }
        let p = self.palettes.get(self.dark);
        let chat = self.active_chat.and_then(|i| self.chats.get(i));
        let busy = chat.is_some_and(|c| c.busy());
        let id = chat.map(|c| c.id.clone()).unwrap_or_default();
        let agent = chat
            .map(|c| c.agent.clone())
            .or_else(|| self.agents.get(self.agent_index).map(|a| a.id.clone()))
            .unwrap_or_else(|| "codex".into());
        div().flex_1().min_h_0().flex().flex_col()
            .when(self.show_return_to_review,|el|el.child(
                div().px(px(16.)).py(px(8.)).border_b_1().border_color(p.color("border")).child(
                    Button::new("contribution-return").outline().small().label(self.t("contribution.return_review")).on_click(cx.listener(|_,_,_,cx|{
                        cx.emit(crate::ChatAction::ReturnToReview);
                    })))))
            .child(div().id("messages").flex_1().min_h_0().overflow_y_scroll().track_scroll(&self.chat_scroll).px(px(16.)).py(px(10.))
                .when(chat.is_none_or(|c|c.messages.is_empty()),|el|el.child(div().py(px(24.)).flex().flex_col().gap(px(12.))
                    .child(div().text_size(px(18.)).font_weight(FontWeight::BOLD).child(self.t("chat.welcome")))
                    .child(div().text_color(p.color("text-muted")).child(self.t("chat.description")))))
                .children(chat.into_iter().flat_map(render_items).enumerate().filter_map(|(i,item)|{
                    let conversation=chat?;
                    match item {
                        RenderItem::Message(index)=>{
                            let message=conversation.messages.get(index)?;
                            if message.role=="thought" {
                                let key = (conversation.id.clone(), index);
                                let open = self.expanded_reasoning.contains(&key);
                                return Some(div().id(("thought",i)).mb(px(10.))
                                    .child(Button::new(("thought-toggle",i)).ghost().small().icon(if open { IconName::ChevronDown } else { IconName::ChevronRight }).label(self.t("chat.reasoning"))
                                        .on_click(cx.listener(move |this,_,_,cx|{if !this.expanded_reasoning.remove(&key) { this.expanded_reasoning.insert(key.clone()); } cx.notify();})))
                                    .when(open,|el|el.child(message.view.clone())).into_any_element());
                            }
                            Some(div().id(("message",i)).mb(px(10.)).flex().flex_col().gap(px(3.))
                                .child(div().text_size(px(11.)).font_weight(FontWeight::BOLD).text_color(p.color("text-muted")).child(if message.role=="user"{self.t("chat.you").to_string()}else{agent.clone()}))
                                .child(message.view.clone())
                                .when(!message.attached_files.is_empty(),|el|el.child(div().flex().flex_wrap().gap(px(8.)).text_size(px(11.)).text_color(p.color("text-muted"))
                                    .children(message.attached_files.iter().map(|name|div().flex().items_center().gap(px(4.)).child(self.icon("file").size(px(13.))).child(name.clone())))))
                                .into_any_element())
                        },
                        RenderItem::Tools(indices)=>{
                            let tools=indices.iter().filter_map(|index|conversation.tools.get(*index)).collect::<Vec<_>>();
                            if tools.is_empty() { return None; }
                            Some(self.tool_group(&conversation.id, tools, i, cx))
                        }

                    }
                }))
                .when(busy,|el|el.child(div().text_color(p.color("text-muted")).child(self.t("chat.thinking"))))
                .when(chat.and_then(|c|c.error.as_ref()).is_some(),|el|el.child(div().p(px(14.)).bg(p.color("error-bg")).text_color(p.color("error")).rounded(px(6.)).child(chat.and_then(|c|c.error.clone()).unwrap_or_default()))))
            .children(chat.filter(|c|c.state=="auth_required").into_iter().map(|chat|{
                div().mx(px(16.)).mb(px(10.)).p(px(12.)).bg(p.color("warning-bg")).rounded(px(4.))
                    .child(div().font_weight(FontWeight::BOLD).child(self.t("chat.signin")))
                    .child(div().mt(px(8.)).flex().gap(px(8.)).children(chat.auth_methods.iter().enumerate().map(|(i,method)|{
                        let method_id=method["id"].as_str().unwrap_or_default().to_string();let thread=chat.id.clone();let interactive=method["type"].as_str()==Some("terminal");
                        Button::new(("auth-method",i)).outline().small().label(if interactive{format!("{} · {}",method["name"].as_str().unwrap_or("Login"),self.t("chat.terminal_login"))}else{method["name"].as_str().unwrap_or("Login").to_string()}).disabled(interactive)
                            .on_click(cx.listener(move|this,_,_,cx|this.command(json!({"action":"authenticate","thread_id":thread,"method_id":method_id}),cx)))
                    })))
            }))
            .children(chat.and_then(|c|c.permission.as_ref()).into_iter().map(|permission|{
                let request=permission["permission_id"].as_str().unwrap_or_default().to_string();
                div().mx(px(28.)).mb(px(12.)).p(px(14.)).rounded(px(6.)).bg(p.color("warning-bg"))
                    .child(div().font_weight(FontWeight::BOLD).child(self.t("chat.permission")))
                    .child(div().my(px(10.)).child(permission["toolCall"]["title"].as_str().unwrap_or_default().to_string()))
                    .child(div().flex().gap(px(8.)).children(permission["options"].as_array().into_iter().flatten().enumerate().map(|(i,option)|{
                        let option_id=option["optionId"].as_str().unwrap_or_default().to_string();let thread=id.clone();let request=request.clone();
                        Button::new(("permission",i)).label(option["name"].as_str().unwrap_or("Allow").to_string()).on_click(cx.listener(move|this,_,_,cx|{
                            this.command(json!({"action":"permission","thread_id":thread,"request_id":request,"option_id":option_id}),cx);
                            if let Some(i)=this.active_chat{this.chats[i].permission=None;}cx.notify();
                        }))
                    })))
            }))
            .children(chat.and_then(|c| c.usage.as_ref()).map(|usage| crate::usage::render_usage(usage, &self.locale, p)))
            .child(self.composer_view(cx))
    }
}
