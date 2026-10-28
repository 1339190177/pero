use serde::{Deserialize, Serialize};
use serde_json::Value;

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct Run {
    pub id: String,
    pub generation: u64,
    pub instance_id: String,
    pub status: String,
    pub lease_expires_at: String,
    pub spec: Spec,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct Spec {
    pub title: String,
    pub prompt: String,
    pub workspace_key: String,
    pub agent_id: String,
    #[serde(default)]
    pub acceptance: Value,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
pub struct Event {
    pub seq: u64,
    pub kind: String,
    pub text: String,
    #[serde(default, skip_serializing_if = "Value::is_null")]
    pub data: Value,
}

impl Event {
    pub fn validate(&self) -> Result<(), String> {
        if self.kind.trim().is_empty()
            || self.kind.len() > 128
            || self.text.len() > 64000
            || serde_json::to_vec(&self.data)
                .map_err(|e| e.to_string())?
                .len()
                > 64000
            || serde_json::to_vec(self).map_err(|e| e.to_string())?.len() > 65536
        {
            return Err("Worker event exceeds protocol field or output limits".into());
        }
        Ok(())
    }
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct Outcome {
    pub outcome: String,
    pub summary: String,
}

impl Outcome {
    pub fn new(outcome: &str, summary: impl Into<String>) -> Self {
        Self {
            outcome: outcome.into(),
            summary: summary.into(),
        }
    }
}
