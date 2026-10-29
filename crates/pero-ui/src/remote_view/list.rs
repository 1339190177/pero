use super::*;
use crate::cloud::Group;

impl Workbench {
    pub(crate) fn remote_list(&self, narrow: bool, cx: &mut Context<Self>) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        let selected = self.remote.data["selected_task_id"].as_str().unwrap_or("");
        let loading = self.remote.loading();
        let visible = self
            .remote
            .tasks()
            .iter()
            .filter(|task| self.remote.task_visible(task))
            .collect::<Vec<_>>();
        let segmented = |id: &'static str,
                         items: Vec<(
            String,
            bool,
            Box<dyn Fn(&mut Workbench, &mut Context<Workbench>)>,
        )>,
                         cx: &mut Context<Self>| {
            div()
                .flex()
                .gap(px(2.))
                .p(px(2.))
                .rounded(px(6.))
                .bg(p.color("surface-muted"))
                .children(
                    items
                        .into_iter()
                        .enumerate()
                        .map(|(i, (label, on, action))| {
                            Button::new((id, i))
                                .ghost()
                                .xsmall()
                                .label(label)
                                .toggled(on)
                                .disabled(loading)
                                .when(on, |b| b.bg(p.color("surface")))
                                .on_click(cx.listener(move |this, _, _, cx| action(this, cx)))
                        }),
                )
        };
        let client_filters = self.remote.client().then(|| {
            segmented(
                "remote-filter",
                ["remote.available", "remote.mine", "filter.all"]
                    .into_iter()
                    .enumerate()
                    .map(|(i, key)| {
                        (
                            self.t(key).to_string(),
                            self.remote.filter == i,
                            Box::new(move |this: &mut Workbench, cx: &mut Context<Workbench>| {
                                this.remote.filter = i;
                                cx.notify();
                            })
                                as Box<dyn Fn(&mut Workbench, &mut Context<Workbench>)>,
                        )
                    })
                    .collect(),
                cx,
            )
        });
        let source_filters = (!self.remote.repositories().is_empty()).then(|| {
            segmented(
                "remote-source-filter",
                [
                    ("", "filter.all"),
                    ("github", "remote.issues"),
                    ("roadmap", "remote.roadmap"),
                ]
                .into_iter()
                .map(|(kind, key)| {
                    (
                        self.t(key).to_string(),
                        string(&self.remote.data, "source_kind") == kind,
                        Box::new(move |this: &mut Workbench, cx: &mut Context<Workbench>| {
                            this.command(
                                json!({"action":"remote_filter","source_kind":kind,"tag":""}),
                                cx,
                            )
                        })
                            as Box<dyn Fn(&mut Workbench, &mut Context<Workbench>)>,
                    )
                })
                .collect(),
                cx,
            )
        });
        let mut rows: Vec<AnyElement> = vec![];
        let mut index = 0usize;
        for group in Group::ALL {
            let tasks = visible
                .iter()
                .filter(|task| phase(task).group() == group)
                .collect::<Vec<_>>();
            if tasks.is_empty() {
                continue;
            }
            rows.push(
                div()
                    .flex()
                    .justify_between()
                    .px(px(16.))
                    .pt(px(14.))
                    .pb(px(6.))
                    .text_size(px(12.))
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(p.color("text-muted"))
                    .child(self.t(group.label_key()))
                    .child(tasks.len().to_string())
                    .into_any_element(),
            );
            for task in tasks {
                rows.push(self.task_row(task, index, selected, cx).into_any_element());
                index += 1;
            }
        }
        div()
            .id("remote-list")
            .h_full()
            .min_h_0()
            .flex()
            .flex_col()
            .flex_shrink_0()
            .when(narrow, |el| el.flex_1())
            .when(!narrow, |el| el.w(px(360.)).border_r_1().border_color(p.color("border")))
            .child(
                div()
                    .px(px(16.))
                    .pt(px(16.))
                    .pb(px(12.))
                    .flex()
                    .flex_col()
                    .gap(px(10.))
                    .border_b_1()
                    .border_color(p.color("border"))
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap(px(6.))
                            .child(
                                div()
                                    .flex_1()
                                    .min_w_0()
                                    .text_size(px(20.))
                                    .font_weight(FontWeight::BOLD)
                                    .child(self.t("nav.tasks")),
                            )
                            .when(
                                self.remote.operator()
                                    && self.remote.data["github"]["configured"].as_bool().unwrap_or(false),
                                |el| {
                                    let running = self.remote.data["github"]["running"].as_bool().unwrap_or(false);
                                    el.child(
                                        Button::new("remote-sync")
                                            .ghost()
                                            .small()
                                            .label(self.t(if running { "remote.syncing_issues" } else { "remote.sync_issues" }))
                                            .disabled(loading || running)
                                            .on_click(cx.listener(|this, _, _, cx| {
                                                this.command(json!({"action":"remote_sync_issues"}), cx)
                                            })),
                                    )
                                },
                            )
                            .child(
                                Button::new("remote-refresh")
                                    .ghost()
                                    .small()
                                    .label(self.t(if loading { "remote.refreshing" } else { "remote.refresh" }))
                                    .disabled(loading)
                                    .on_click(cx.listener(|this, _, _, cx| {
                                        this.command(json!({"action":"remote_refresh"}), cx)
                                    })),
                            )
                            .when(self.remote.operator(), |el| {
                                el.child(
                                    Button::new("remote-new")
                                        .primary()
                                        .small()
                                        .icon(Icon::default().path("icons/plus.svg"))
                                        .label(self.t("remote.new"))
                                        .disabled(loading)
                                        .on_click(cx.listener(|this, _, window, cx| this.start_remote_form(window, cx))),
                                )
                            }),
                    )
                    .child(
                        div()
                            .text_size(px(12.))
                            .text_color(p.color("text-muted"))
                            .truncate()
                            .child(self.scope_label()),
                    )
                    .when(client_filters.is_some() || source_filters.is_some(), |el| {
                        el.child(
                            div()
                                .flex()
                                .flex_wrap()
                                .gap(px(8.))
                                .children(client_filters)
                                .children(source_filters),
                        )
                    })
                    .when(!string(&self.remote.data, "tag").is_empty(), |el| {
                        el.child(
                            div().child(
                                Button::new("remote-clear-tag")
                                    .outline()
                                    .xsmall()
                                    .label(format!("#{} ×", string(&self.remote.data, "tag")))
                                    .accessibility_label(self.t("remote.clear_tag"))
                                    .on_click(cx.listener(|this, _, _, cx| {
                                        let kind = string(&this.remote.data, "source_kind").to_string();
                                        this.command(json!({"action":"remote_filter","source_kind":kind,"tag":""}), cx)
                                    })),
                            ),
                        )
                    }),
            )
            .child(
                div()
                    .id("remote-task-scroll")
                    .flex_1()
                    .min_h_0()
                    .overflow_y_scroll()
                    .track_scroll(&self.task_scroll)
                    .on_scroll(cx.listener(|this, _, _, cx| this.request_more_tasks(cx)))
                    .pb(px(8.))
                    .when(visible.is_empty(), |el| {
                        el.child(
                            div()
                                .px(px(16.))
                                .py(px(32.))
                                .flex()
                                .flex_col()
                                .items_center()
                                .gap(px(6.))
                                .text_color(p.color("text-muted"))
                                .whitespace_normal()
                                .child(self.t(if loading {
                                    "remote.loading"
                                } else if self.remote.client() {
                                    "remote.no_matching_tasks"
                                } else {
                                    "remote.empty"
                                })),
                        )
                    })
                    .children(rows)
                    .when(
                        !visible.is_empty()
                            && self.remote.data["has_more"].as_bool().unwrap_or(false)
                            && (loading || self.task_more_pending),
                        |el| {
                            el.child(
                                div()
                                    .id("remote-task-more")
                                    .px(px(16.))
                                    .py(px(12.))
                                    .text_size(px(12.))
                                    .text_color(p.color("text-muted"))
                                    .child(self.t("remote.loading")),
                            )
                        },
                    ),
            )
    }

    pub(crate) fn task_row(
        &self,
        task: &Value,
        i: usize,
        selected: &str,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        let p = self.palettes.get(self.dark);
        let id = string(task, "id").to_string();
        let title = string(task, "title").to_string();
        let phase = phase(task);
        let chosen = id == selected && !self.remote.creating;
        let owner = if self.remote.mine(task) {
            self.t("remote.mine").to_string()
        } else {
            string(&task["claimed_by"], "name").to_string()
        };
        let worker = string(&task["run"], "worker_id");
        let meta = [source_label(task), owner]
            .into_iter()
            .filter(|t| !t.is_empty())
            .collect::<Vec<_>>()
            .join(" · ");
        let tags = task_tags(task)
            .iter()
            .filter_map(Value::as_str)
            .take(3)
            .map(|tag| format!("#{tag}"))
            .collect::<Vec<_>>()
            .join(" ");
        let (status, status_color) = match phase {
            Phase::Running if !worker.is_empty() => (
                Some(
                    self.t("cloud.note.running_on")
                        .replace("{worker}", &self.worker_name(worker)),
                ),
                "text-muted",
            ),
            Phase::Running => (Some(self.t("cloud.body.running").to_string()), "text-muted"),
            Phase::Queued => (Some(self.t("cloud.note.queued").to_string()), "warning"),
            Phase::Failed | Phase::Reconcile => {
                (Some(self.t("cloud.note.failed").to_string()), "error")
            }
            Phase::Succeeded => (Some(self.t("cloud.note.succeeded").to_string()), "success"),
            _ if source_changed(task) => (
                Some(self.t("cloud.note.source_changed").to_string()),
                "warning",
            ),
            _ => (None, "text-muted"),
        };
        // Heading paths stay in the detail and the accessible name. A row is the
        // title plus one status line; tags only appear when there is no status.
        let mut parts = Vec::new();
        if let Some(status) = status.clone() {
            parts.push(status);
        }
        if !meta.is_empty() {
            parts.push(meta.clone());
        }
        if status.is_none() && !tags.is_empty() {
            parts.push(tags.clone());
        }
        let secondary = parts.join(" · ");
        let context = source_context(task);
        let label = format!("{title}\n{} · {secondary}", self.t(phase.label_key()));
        let label = if context.is_empty() {
            label
        } else {
            format!("{label}\n{context}")
        };
        let depth = observed_source(task)["depth"].as_u64().unwrap_or(0).min(3) as f32;
        div()
            .id(("remote-task", i))
            .w_full()
            .min_w_0()
            .pl(px(16. + depth * 10.))
            .pr(px(16.))
            .py(px(10.))
            .flex()
            .flex_col()
            .gap(px(4.))
            .cursor_pointer()
            .border_l_2()
            .border_color(p.color(if chosen { "action" } else { "surface" }))
            .when(chosen, |el| el.bg(p.color("accent-soft")))
            .when(!chosen, |el| el.hover(|el| el.bg(p.color("surface-muted"))))
            .role(Role::Button)
            .aria_label(label)
            .child(
                div()
                    .w_full()
                    .truncate()
                    .font_weight(FontWeight::SEMIBOLD)
                    .child(title),
            )
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(8.))
                    .min_w_0()
                    .child(phase_tag(p, self.t(phase.label_key()), phase.tone()))
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .truncate()
                            .text_size(px(12.))
                            .text_color(p.color(status_color))
                            .child(secondary),
                    ),
            )
            .on_click(cx.listener(move |this, _, _, cx| {
                this.remote.creating = false;
                this.remote.confirm_cancel = None;
                this.remote.data["selected_task_id"] = json!(id);
                this.command(json!({"action":"remote_select","task_id":id}), cx);
            }))
    }

    pub(crate) fn follow_task_pages(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !self.task_list_mounted(window)
            || self.task_more_pending
            || self.remote.loading()
            || self.remote.data["error"].is_string()
            || !self.remote.data["has_more"].as_bool().unwrap_or(false)
        {
            return;
        }
        // Bounds are from the previous layout. An unmeasured list needs one
        // frame before deciding it does not fill the viewport.
        let unmeasured = self.task_scroll.bounds().size.height <= px(1.);
        if !unmeasured && !self.task_scroll_wants_more() {
            return;
        }
        cx.on_next_frame(window, |this, _, cx| this.request_more_tasks(cx));
    }

    fn task_list_mounted(&self, window: &Window) -> bool {
        if self.navigation != 3 || !self.remote.configured() || self.remote.connection.editing {
            return false;
        }
        if self.remote.client() && self.remote.public_contributions {
            return false;
        }
        let width: f32 = window.viewport_size().width.into();
        let selected = self.remote.data["selected_task_id"].as_str().unwrap_or("");
        !(width < 1050. && (self.remote.creating || !selected.is_empty()))
    }

    fn task_scroll_wants_more(&self) -> bool {
        if self.task_more_pending
            || self.remote.loading()
            || self.remote.data["error"].is_string()
            || !self.remote.data["has_more"].as_bool().unwrap_or(false)
            || self.task_scroll.bounds().size.height <= px(1.)
        {
            return false;
        }
        // Offset is negative toward the bottom. A short list has no overflow.
        self.task_scroll.max_offset().y + self.task_scroll.offset().y <= px(240.)
    }

    fn request_more_tasks(&mut self, cx: &mut Context<Self>) {
        if !self.task_scroll_wants_more() {
            return;
        }
        self.task_more_pending = true;
        self.command(json!({"action": "remote_more"}), cx);
    }
}
