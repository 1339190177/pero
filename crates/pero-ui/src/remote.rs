//! Cloud-owned tasks. The main-process adapter owns HTTP and credentials.
use crate::app::Workbench;
use gpui_kit::{
    component::{
        button::*,
        input::{Editor, EditorState, Input, InputEvent, InputState},
        *,
    },
    prelude::*,
    *,
};
use pero_contracts::HostEvent;
use serde_json::{Value, json};

pub(crate) struct RemoteState {
    pub data: Value,
    pub creating: bool,
    submitting: bool,
    pub title: Entity<InputState>,
    pub prompt: Entity<EditorState>,
    pub acceptance: Entity<EditorState>,
    pub project: Entity<InputState>,
    pub workspace: Entity<InputState>,
    agent: Option<String>,
    confirm_cancel: Option<String>,
    form_error: Option<&'static str>,
    _subscriptions: Vec<Subscription>,
}

impl RemoteState {
    pub fn new(window: &mut Window, cx: &mut Context<Workbench>) -> Self {
        let title = cx.new(|cx| InputState::new(window, cx));
        let prompt = cx.new(|cx| {
            EditorState::new(window, cx)
                .line_number(false)
                .soft_wrap(true)
        });
        let acceptance = cx.new(|cx| {
            EditorState::new(window, cx)
                .line_number(false)
                .soft_wrap(true)
        });
        let project = cx.new(|cx| InputState::new(window, cx).default_value("default"));
        let workspace = cx.new(|cx| InputState::new(window, cx).default_value("default"));
        let mut subscriptions = vec![];
        for input in [&title, &project, &workspace] {
            subscriptions.push(cx.subscribe(input, |_, _, _: &InputEvent, cx| cx.notify()));
        }
        for input in [&prompt, &acceptance] {
            subscriptions.push(cx.subscribe(input, |_, _, _: &InputEvent, cx| cx.notify()));
        }
        Self {
            data: json!({"configured":false,"loading":false,"tasks":[],"workers":[],"detail":null}),
            creating: false,
            submitting: false,
            title,
            prompt,
            acceptance,
            project,
            workspace,
            agent: None,
            confirm_cancel: None,
            form_error: None,
            _subscriptions: subscriptions,
        }
    }
    fn loading(&self) -> bool {
        self.data["loading"].as_bool().unwrap_or(false)
    }
    fn configured(&self) -> bool {
        self.data["configured"].as_bool().unwrap_or(false)
    }
    fn tasks(&self) -> &[Value] {
        self.data["tasks"]
            .as_array()
            .map(Vec::as_slice)
            .unwrap_or(&[])
    }
    fn workers(&self) -> &[Value] {
        self.data["workers"]
            .as_array()
            .map(Vec::as_slice)
            .unwrap_or(&[])
    }
    pub fn snapshot(&self, cx: &App) -> Value {
        json!({"state":self.data,"creating":self.creating,"confirm_cancel":self.confirm_cancel,
            "title":self.title.read(cx).value().as_ref(),"prompt":self.prompt.read(cx).value().as_ref(),
            "acceptance":self.acceptance.read(cx).value().as_ref(),
            "project":self.project.read(cx).value().as_ref(),"workspace":self.workspace.read(cx).value().as_ref(),
            "agent":self.agent,"form_error":self.form_error})
    }
}

fn string<'a>(value: &'a Value, key: &str) -> &'a str {
    value[key].as_str().unwrap_or("")
}

/// Display the observed event content without promoting Agent output to acceptance.
fn remote_event_summary(event: &Value) -> String {
    let data = &event["data"];
    for text in [
        string(event, "text"),
        string(data, "text"),
        string(&data["content"], "text"),
        string(data, "summary"),
    ] {
        if !text.is_empty() {
            return text.to_string();
        }
    }
    match (string(data, "title"), string(data, "status")) {
        ("", status) => status.to_string(),
        (title, "") => title.to_string(),
        (title, status) => format!("{title} · {status}"),
    }
}

impl Workbench {
    pub fn receive_remote(&mut self, event: &HostEvent, cx: &mut Context<Self>) -> bool {
        if event.kind == "remote_open" {
            self.open_remote(cx);
            return true;
        }
        if event.kind != "remote_state" {
            return false;
        }
        self.remote.data = event.data.clone();
        if !self.remote.loading() && self.remote.submitting {
            self.remote.submitting = false;
            if self.remote.data["error"].is_null() {
                self.remote.creating = false;
            }
        }
        let selected = self.remote.data["selected_task_id"].as_str();
        if self
            .remote
            .confirm_cancel
            .as_deref()
            .is_some_and(|id| Some(id) != selected)
        {
            self.remote.confirm_cancel = None;
        }
        cx.notify();
        true
    }
    pub fn open_remote(&mut self, cx: &mut Context<Self>) {
        self.navigation = 3;
        self.sync_follow(cx);
        self.command(json!({"action":"remote_refresh"}), cx);
    }
    fn remote_agent(&self) -> String {
        self.remote
            .agent
            .clone()
            .or_else(|| self.agents.get(self.agent_index).map(|a| a.id.clone()))
            .unwrap_or_else(|| "codex".into())
    }
    fn create_remote(&mut self, cx: &mut Context<Self>) {
        if !self.remote.configured() || self.remote.loading() || self.remote.submitting {
            return;
        }
        let title = self.remote.title.read(cx).value().trim().to_string();
        let prompt = self.remote.prompt.read(cx).value().trim().to_string();
        let acceptance = self.remote.acceptance.read(cx).value().trim().to_string();
        let project = self.remote.project.read(cx).value().trim().to_string();
        let workspace = self.remote.workspace.read(cx).value().trim().to_string();
        if title.is_empty()
            || prompt.is_empty()
            || acceptance.is_empty()
            || project.is_empty()
            || workspace.is_empty()
        {
            self.remote.form_error = Some("remote.required");
            cx.notify();
            return;
        }
        if title.chars().count() > 200
            || prompt.chars().count() > 64000
            || acceptance.chars().count() > 16000
            || project.len() > 128
            || workspace.len() > 128
        {
            self.remote.form_error = Some("remote.too_long");
            cx.notify();
            return;
        }
        self.remote.form_error = None;
        self.remote.submitting = true;
        self.command(
            json!({"action":"remote_create","title":title,"prompt":prompt,"acceptance":acceptance,
            "project_id":project,"workspace_key":workspace,"agent_id":self.remote_agent()}),
            cx,
        );
    }
    fn remote_status(&self, state: &str) -> String {
        let key = match state {
            "draft" => "remote.state.draft",
            "queued" => "remote.state.queued",
            "running" => "remote.state.running",
            "cancel_requested" => "remote.state.cancelling",
            "cancelled" => "remote.state.cancelled",
            "succeeded" => "remote.state.succeeded",
            "failed" => "remote.state.failed",
            "reconcile" => "remote.state.needs_attention",
            _ => return state.to_string(),
        };
        self.t(key).to_string()
    }
    pub fn remote_view(&self, narrow: bool, cx: &mut Context<Self>) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        let selected = self.remote.data["selected_task_id"].as_str().unwrap_or("");
        let detail_visible = self.remote.creating || !selected.is_empty();
        let loading = self.remote.loading();
        let configured = self.remote.configured();
        div().flex_1().min_w_0().h_full().flex().flex_col()
            .child(div().px(px(20.)).h(px(58.)).flex_shrink_0().flex().items_center().gap(px(10.)).border_b_1().border_color(p.color("border"))
                .child(div().flex_1().min_w_0().text_size(px(20.)).font_weight(FontWeight::BOLD).child(self.t("nav.remote")))
                .child(Button::new("remote-refresh").outline().small().label(self.t(if loading{"remote.refreshing"}else{"remote.refresh"}))
                    .disabled(loading).on_click(cx.listener(|this,_,_,cx|this.command(json!({"action":"remote_refresh"}),cx))))
                .child(Button::new("remote-new").primary().small().label(self.t("remote.new")).disabled(!configured||loading)
                    .on_click(cx.listener(|this,_,window,cx|{
                        this.remote.title.update(cx, |input,cx| input.set_value("",window,cx));
                        this.remote.prompt.update(cx, |input,cx| input.set_value("",window,cx));
                        this.remote.acceptance.update(cx, |input,cx| input.set_value("",window,cx));
                        this.remote.creating=true;
                        this.remote.submitting=false;
                        this.remote.form_error=None;
                        this.remote.confirm_cancel=None;
                        let focus=this.remote.title.focus_handle(cx);window.focus(&focus, cx);cx.notify();}))))
            .when(self.remote.data["error"].is_string(),|el|el.child(div().id("remote-error").role(Role::Alert)
                .aria_label(string(&self.remote.data,"error").to_string())
                .w_full().min_w_0().whitespace_normal().px(px(20.)).py(px(10.)).border_b_1().border_color(p.color("border"))
                .text_color(p.color("error")).child(string(&self.remote.data,"error").to_string())))
            .when(!configured,|el|el.child(div().p(px(24.)).flex().flex_col().gap(px(12.))
                .child(div().font_weight(FontWeight::BOLD).child(self.t("remote.unconfigured")))
                .child(self.t("remote.configure"))))
            .when(configured,|el|el
                .child(div().flex_1().min_h_0().flex()
                    .when(!narrow||!detail_visible,|el|el.child(self.remote_list(narrow,cx)))
                    .when(!narrow||detail_visible,|el|el.child(div().flex_1().min_w_0().flex().flex_col()
                        .when(narrow&&detail_visible,|el|el.child(Button::new("remote-back").ghost().small().label(self.t("remote.back"))
                            .on_click(cx.listener(|this,_,_,cx|{this.remote.creating=false;this.remote.data["selected_task_id"]=Value::Null;
                                this.command(json!({"action":"remote_select","task_id":null}),cx);}))))
                        .when(self.remote.creating,|el|el.child(self.remote_form(cx)))
                        .when(!self.remote.creating,|el|el.child(self.remote_detail(cx)))))))
    }
    fn remote_list(&self, narrow: bool, cx: &mut Context<Self>) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        let selected = self.remote.data["selected_task_id"].as_str().unwrap_or("");
        div()
            .id("remote-list")
            .h_full()
            .min_h_0()
            .flex()
            .flex_col()
            .flex_shrink_0()
            .when(narrow, |el| el.flex_1())
            .when(!narrow, |el| {
                el.w(px(280.)).border_r_1().border_color(p.color("border"))
            })
            .child(
                div()
                    .px(px(16.))
                    .py(px(12.))
                    .text_color(p.color("text-muted"))
                    .child(format!(
                        "{} {} · {} {}",
                        self.remote.tasks().len(),
                        self.t("remote.tasks"),
                        self.remote.workers().len(),
                        self.t("remote.workers")
                    )),
            )
            .child(
                div()
                    .id("remote-task-scroll")
                    .flex_1()
                    .min_h_0()
                    .overflow_y_scroll()
                    .px(px(8.))
                    .when(self.remote.tasks().is_empty(), |el| {
                        el.child(
                            div()
                                .p(px(12.))
                                .text_color(p.color("text-muted"))
                                .child(self.t(if self.remote.loading() {
                                    "remote.loading"
                                } else {
                                    "remote.empty"
                                })),
                        )
                    })
                    .children(self.remote.tasks().iter().enumerate().map(|(i, task)| {
                        let id = string(task, "id").to_string();
                        let title = string(task, "title").to_string();
                        let status = self.remote_status(string(task, "state"));
                        let chosen = id == selected && !self.remote.creating;
                        div().py(px(4.)).child(
                            Button::new(("remote-task", i))
                                .ghost()
                                .w_full()
                                .h(px(68.))
                                .bg(p.color(if chosen { "accent-soft" } else { "surface" }))
                                .label(format!("{title}\n{status}"))
                                .on_click(cx.listener(move |this, _, _, cx| {
                                    this.remote.creating = false;
                                    this.remote.confirm_cancel = None;
                                    this.remote.data["selected_task_id"] = json!(id);
                                    this.command(
                                        json!({"action":"remote_select","task_id":id}),
                                        cx,
                                    );
                                })),
                        )
                    })),
            )
            .when(
                self.remote.data["has_more"].as_bool().unwrap_or(false),
                |el| {
                    el.child(
                        div()
                            .p(px(8.))
                            .border_t_1()
                            .border_color(p.color("border"))
                            .child(
                                Button::new("remote-more")
                                    .outline()
                                    .small()
                                    .w_full()
                                    .label(self.t("remote.more"))
                                    .disabled(self.remote.loading())
                                    .on_click(cx.listener(|this, _, _, cx| {
                                        this.command(json!({"action":"remote_more"}), cx);
                                    })),
                            ),
                    )
                },
            )
    }
    fn remote_form(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        let busy = self.remote.loading() || self.remote.submitting;
        let agent = self.remote_agent();
        div()
            .id("remote-form")
            .flex_1()
            .min_h_0()
            .overflow_y_scroll()
            .p(px(20.))
            .flex()
            .flex_col()
            .gap(px(10.))
            .child(
                div()
                    .text_size(px(18.))
                    .font_weight(FontWeight::BOLD)
                    .child(self.t("remote.new")),
            )
            .child(self.t("remote.title"))
            .child(
                Input::new(&self.remote.title)
                    .aria_label(self.t("remote.title"))
                    .accessibility_id("remote.title")
                    .disabled(busy),
            )
            .child(self.t("remote.prompt"))
            .child(
                Editor::new(&self.remote.prompt)
                    .h(px(132.))
                    .aria_label(self.t("remote.prompt"))
                    .disabled(busy),
            )
            .child(self.t("remote.acceptance"))
            .child(
                Editor::new(&self.remote.acceptance)
                    .h(px(88.))
                    .aria_label(self.t("remote.acceptance"))
                    .disabled(busy),
            )
            .child(
                div()
                    .flex()
                    .gap(px(12.))
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .child(self.t("remote.project"))
                            .child(
                                Input::new(&self.remote.project)
                                    .aria_label(self.t("remote.project"))
                                    .disabled(busy),
                            ),
                    )
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .child(self.t("remote.workspace"))
                            .child(
                                Input::new(&self.remote.workspace)
                                    .aria_label(self.t("remote.workspace"))
                                    .disabled(busy),
                            ),
                    ),
            )
            .child(div().mt(px(4.)).child(self.t("remote.agent")))
            .child(
                div()
                    .flex()
                    .flex_wrap()
                    .gap(px(6.))
                    .when(self.agents.is_empty(), |el| el.child(agent.clone()))
                    .children(self.agents.iter().enumerate().map(|(i, a)| {
                        let id = a.id.clone();
                        Button::new(("remote-agent", i))
                            .outline()
                            .small()
                            .label(a.name.clone())
                            .bg(p.color(if agent == a.id {
                                "accent-soft"
                            } else {
                                "surface"
                            }))
                            .disabled(busy)
                            .on_click(cx.listener(move |this, _, _, cx| {
                                this.remote.agent = Some(id.clone());
                                cx.notify();
                            }))
                    })),
            )
            .when(self.remote.form_error.is_some(), |el| {
                el.child(
                    div()
                        .id("remote-form-error")
                        .role(Role::Alert)
                        .aria_label(self.t(self.remote.form_error.unwrap()))
                        .whitespace_normal()
                        .text_color(p.color("error"))
                        .child(self.t(self.remote.form_error.unwrap())),
                )
            })
            .child(
                div()
                    .mt(px(8.))
                    .flex()
                    .items_center()
                    .gap(px(10.))
                    .child(
                        Button::new("remote-create")
                            .primary()
                            .label(self.t(if busy {
                                "remote.creating"
                            } else {
                                "remote.create"
                            }))
                            .disabled(busy)
                            .on_click(cx.listener(|this, _, _, cx| this.create_remote(cx))),
                    )
                    .child(
                        Button::new("remote-discard")
                            .ghost()
                            .label(self.t("remote.close_form"))
                            .disabled(busy)
                            .on_click(cx.listener(|this, _, _, cx| {
                                this.remote.creating = false;
                                cx.notify();
                            })),
                    ),
            )
            .child(
                div()
                    .text_size(px(12.))
                    .text_color(p.color("text-muted"))
                    .child(self.t("remote.draft_hint")),
            )
    }
    fn remote_detail(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        let detail = &self.remote.data["detail"];
        let task = &detail["task"];
        let id = string(task, "id").to_string();
        let selected = self.remote.data["selected_task_id"].as_str().unwrap_or("");
        let active = !id.is_empty() && id == selected;
        let state = string(task, "state");
        let run = &detail["run"];
        let can_dispatch = matches!(state, "draft" | "succeeded" | "failed" | "cancelled");
        let can_cancel = matches!(state, "queued" | "running");
        let events = detail["events"]
            .as_array()
            .map(Vec::as_slice)
            .unwrap_or(&[]);
        let confirming = self.remote.confirm_cancel.as_deref() == Some(id.as_str());
        let busy = self.remote.loading();
        div().id("remote-detail").flex_1().min_w_0().min_h_0().overflow_x_hidden().overflow_y_scroll().whitespace_normal().p(px(20.))
            .when(!active,|el|el.child(div().py(px(24.)).text_color(p.color("text-muted"))
                .child(self.t(if selected.is_empty(){"remote.select"}else{"remote.loading"}))))
            .when(active,|el|el
                .child(div().flex().items_start().gap(px(12.))
                    .child(div().flex_1().min_w_0().text_size(px(20.)).font_weight(FontWeight::BOLD).child(string(task,"title").to_string()))
                    .child(div().text_color(p.color("text-muted")).child(self.remote_status(state))))
                .child(div().mt(px(10.)).flex().flex_wrap().gap(px(8.))
                    .child(Button::new("remote-dispatch").primary().small().label(self.t("remote.dispatch")).disabled(!can_dispatch||busy)
                        .on_click(cx.listener({let id=id.clone();move|this,_,_,cx|this.command(json!({"action":"remote_dispatch","task_id":id}),cx)})))
                    .when(can_cancel,|el|el.child(Button::new("remote-cancel").outline().small().label(self.t("remote.cancel")).disabled(busy)
                        .on_click(cx.listener({let id=id.clone();move|this,_,_,cx|{this.remote.confirm_cancel=Some(id.clone());cx.notify();}})))))
                .when(confirming,|el|el.child(div().mt(px(12.)).p(px(12.)).border_1().border_color(p.color("input-border"))
                    .child(self.t("remote.confirm_stop"))
                    .child(div().mt(px(8.)).flex().gap(px(8.))
                        .child(Button::new("remote-confirm-cancel").outline().small().label(self.t("remote.confirm_cancel"))
                            .on_click(cx.listener({let id=id.clone();move|this,_,_,cx|{this.remote.confirm_cancel=None;
                                this.command(json!({"action":"remote_cancel","task_id":id}),cx);}})))
                        .child(Button::new("remote-keep-running").ghost().small().label(self.t("remote.keep_running"))
                            .on_click(cx.listener(|this,_,_,cx|{this.remote.confirm_cancel=None;cx.notify();}))))))
                .child(div().mt(px(16.)).text_size(px(12.)).text_color(p.color("text-muted"))
                    .child(format!("{} · {} · {}",string(task,"project_id"),string(task,"workspace_key"),string(task,"agent_id"))))
                .when(state=="queued",|el|el.child(div().mt(px(12.)).child(self.t("remote.waiting_worker"))))
                .when(state=="cancel_requested",|el|el.child(div().mt(px(12.)).child(self.t("remote.stopping"))))
                .when(matches!(state,"succeeded"|"failed"|"reconcile"),|el|el.child(div().mt(px(12.)).text_color(p.color("text-muted"))
                    .child(self.t(if state=="succeeded"{"remote.execution_only"}else{"remote.failure_recovery"}))))
                .child(div().mt(px(20.)).font_weight(FontWeight::BOLD).child(self.t("remote.prompt")))
                .child(div().w_full().min_w_0().whitespace_normal().mt(px(6.)).child(string(task,"prompt").to_string()))
                .child(div().mt(px(16.)).font_weight(FontWeight::BOLD).child(self.t("remote.acceptance")))
                .child(div().w_full().min_w_0().whitespace_normal().mt(px(6.)).child(string(task,"acceptance").to_string()))
                .when(!run.is_null(),|el|el
                    .child(div().mt(px(20.)).pt(px(12.)).border_t_1().border_color(p.color("border")).font_weight(FontWeight::BOLD).child(self.t("remote.run")))
                    .child(div().mt(px(6.)).text_size(px(12.)).text_color(p.color("text-muted"))
                        .child(format!("{} · {}",string(run,"id"),string(run,"worker_id"))))
                    .when(!string(run,"summary").is_empty(),|el|el.child(div().w_full().min_w_0().whitespace_normal().mt(px(8.)).child(string(run,"summary").to_string()))))
                .child(div().mt(px(20.)).pt(px(12.)).border_t_1().border_color(p.color("border")).font_weight(FontWeight::BOLD).child(self.t("remote.events")))
                .when(detail["events_truncated"].as_bool().unwrap_or(false),|el|el.child(div().mt(px(8.)).text_size(px(12.)).text_color(p.color("text-muted"))
                    .child(self.t("remote.recent_events"))
                    .when_some(detail["events_first_seq"].as_u64().zip(detail["events_last_seq"].as_u64()), |el,(first,last)|el.child(format!(" · {first}–{last}")))))
                .when(events.is_empty(),|el|el.child(div().mt(px(8.)).text_color(p.color("text-muted")).child(self.t("remote.no_events"))))
                .children(events.iter().map(|event|{
                    let kind=string(event,"kind");
                    let payload=remote_event_summary(event);
                    div().w_full().min_w_0().whitespace_normal().py(px(8.)).border_b_1().border_color(p.color("border"))
                        .child(div().text_size(px(12.)).text_color(p.color("text-muted")).child(kind.to_string()))
                        .when(!payload.is_empty(),|el|el.child(div().w_full().min_w_0().whitespace_normal().mt(px(4.)).child(payload.to_string())))
                })))
    }
}
