//! Retained document items. Navigation owns selection; each item owns its view.
use crate::{app::Workbench, workspace::WorkspaceItem};
use gpui_kit::{prelude::*, *};
use pero_chat_ui::content_reader::ContentReader;
use pero_contracts::content_origin::ContentOrigin;
use pero_contracts::content_document::{ContentDocument, ContentFormat};

pub(crate) struct WorkspaceDocument {
    pub item: WorkspaceItem,
    pub title: String,
    pub view: Entity<ContentReader>,
}

impl Workbench {
    pub(crate) fn open_document(
        &mut self,
        origin: ContentOrigin,
        title: String,
        document: ContentDocument,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let item = WorkspaceItem::Document {
            origin,
            source: document.id.clone(),
        };
        let view = if let Some(open) = self.documents.iter_mut().find(|open| open.item == item) {
            open.title = title;
            open.view.update(cx, |view, cx| view.replace(document, cx));
            open.view.clone()
        } else {
            let locale = self.locale.clone();
            let view = cx.new(|cx| ContentReader::new(document, locale, window, cx).workspace());
            self.documents.push(WorkspaceDocument {
                item: item.clone(),
                title,
                view: view.clone(),
            });
            view
        };
        self.navigation.open(item);
        self.sync_follow(cx);
        view.update(cx, |view, cx| view.focus(window, cx));
        cx.notify();
    }

    pub(crate) fn reconcile_documents(&mut self, cx: &mut Context<Self>) {
        let chats = &self.chat.read(cx).chats;
        self.documents.retain(|open|match &open.item {
            WorkspaceItem::Document {origin:ContentOrigin::App {app,panel},..}=>self.apps.contains_panel(app,panel),
            WorkspaceItem::Document {origin,..}=>origin.thread_id().is_none_or(|owner|chats.iter().any(|chat|chat.id==owner)),
            _=>false,
        });
        let updates = self
            .documents
            .iter()
            .filter_map(|open| {
                let WorkspaceItem::Document { origin, source } = &open.item else {
                    return None;
                };
                let document = chats
                    .iter()
                    .find(|chat| Some(chat.id.as_str()) == origin.thread_id())?
                    .tools
                    .iter()
                    .filter_map(|tool| tool.document.as_ref())
                    .find(|document| &document.id == source)?;
                Some((open.view.clone(), document.clone()))
            })
            .collect::<Vec<_>>();
        for (view, document) in updates {
            view.update(cx, |view, cx| view.replace(document, cx));
        }
    }

    pub(crate) fn document_content(&self, cx: &App) -> AnyElement {
        let view = self
            .documents
            .iter()
            .find(|open| Some(&open.item) == self.navigation.current())
            .map(|open| open.view.clone());
        div()
            .flex_1()
            .min_h_0()
            .children(view.clone())
            .when(view.is_none(), |el| el.child(self.t("content.unavailable")))
            .into_any_element()
    }

    pub(crate) fn document_label(&self, item: &WorkspaceItem, cx: &App) -> (String, &'static str) {
        self.documents
            .iter()
            .find(|open| &open.item == item)
            .map(|open| {
                let icon = match open.view.read(cx).document.format {
                    ContentFormat::Text => "file",
                    _ => "file-code",
                };
                (open.title.clone(), icon)
            })
            .unwrap_or_else(|| (self.t("content.document").into(), "file"))
    }
}
