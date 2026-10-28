use serde::{Deserialize, Serialize};
pub mod i18n;

pub const PROTOCOL_VERSION: u32 = 1;
pub const MAX_TEXT_CHARS: usize = 4096;

#[derive(Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct EchoRequest {
    pub id: String,
    pub text: String,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct HostEvent {
    pub version: u32,
    pub kind: String,
    pub request_id: Option<String>,
    pub text: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub thread_id: Option<String>,
    #[serde(default, skip_serializing_if = "serde_json::Value::is_null")]
    pub data: serde_json::Value,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub message_key: Option<String>,
}

impl HostEvent {
    pub fn new(kind: &str, request_id: Option<String>, text: String) -> Self {
        Self {
            version: PROTOCOL_VERSION,
            kind: kind.into(),
            request_id,
            text,
            thread_id: None,
            data: serde_json::Value::Null,
            message_key: None,
        }
    }
    pub fn message(kind: &str, request_id: Option<String>, key: &str) -> Self {
        let mut event = Self::new(kind, request_id, String::new());
        event.message_key = Some(key.into());
        event
    }
}

#[derive(Debug, Deserialize)]
#[serde(tag = "action", rename_all = "snake_case", deny_unknown_fields)]
pub enum HostCommand {
    ListAgents,
    Workspace {
        path: String,
    },
    AttachFiles {
        paths: Vec<String>,
    },
    Authenticate {
        thread_id: String,
        method_id: String,
    },
    NewSession {
        thread_id: String,
        agent_id: String,
    },
    Prompt {
        thread_id: String,
        text: String,
    },
    Cancel {
        thread_id: String,
    },
    Permission {
        thread_id: String,
        request_id: String,
        option_id: Option<String>,
    },
    Model {
        thread_id: String,
        model_id: String,
    },
    Close {
        thread_id: String,
    },
}
