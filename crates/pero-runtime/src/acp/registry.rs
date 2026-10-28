use serde::{Deserialize, Serialize};
use std::{
    collections::BTreeMap,
    path::{Path, PathBuf},
};

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct AgentConfig {
    pub id: String,
    pub name: String,
    pub command: String,
    #[serde(default)]
    pub args: Vec<String>,
    #[serde(default)]
    pub env: BTreeMap<String, String>,
    #[serde(default)]
    pub model: Option<String>,
}
impl AgentConfig {
    pub fn available(&self) -> bool {
        let p = Path::new(&self.command);
        if p.is_absolute() {
            return p.is_file();
        }
        std::env::split_paths(&std::env::var_os("PATH").unwrap_or_default())
            .any(|dir| dir.join(p).is_file())
    }
    pub fn public(&self) -> serde_json::Value {
        serde_json::json!({"id":self.id,"name":self.name,"available":self.available(),"command":self.command,"model":self.model})
    }
}
pub fn registry(root: &Path) -> Result<Vec<AgentConfig>, String> {
    let agent_root = std::env::var_os("PERO_AGENT_ROOT")
        .map(PathBuf::from)
        .unwrap_or_else(|| root.to_path_buf());
    let grok_env=BTreeMap::from([("OPENCODE_CONFIG_CONTENT".into(),serde_json::json!({"model":"xai/grok-code-fast-1","provider":{"xai":{"npm":"@ai-sdk/xai","name":"xAI","models":{"grok-code-fast-1":{"name":"Grok Code Fast 1"}}}}}).to_string())]);
    let mut agents = vec![
        AgentConfig {
            id: "codex".into(),
            name: "Codex".into(),
            command: agent_root
                .join("node_modules/.bin/codex-acp")
                .to_string_lossy()
                .into(),
            args: vec![],
            env: BTreeMap::new(),
            model: None,
        },
        AgentConfig {
            id: "claude".into(),
            name: "Claude".into(),
            command: agent_root
                .join("node_modules/.bin/claude-agent-acp")
                .to_string_lossy()
                .into(),
            args: vec![],
            env: BTreeMap::new(),
            model: None,
        },
        AgentConfig {
            id: "grok".into(),
            name: "Grok · OpenCode".into(),
            command: "opencode".into(),
            args: vec!["acp".into()],
            env: grok_env,
            model: None,
        },
    ];
    let file = std::env::var_os("PERO_AGENTS_FILE")
        .map(PathBuf::from)
        .unwrap_or_else(|| root.join("agents.local.json"));
    if file.exists() {
        let custom: Vec<AgentConfig> =
            serde_json::from_slice(&std::fs::read(file).map_err(|e| e.to_string())?)
                .map_err(|e| e.to_string())?;
        for agent in custom {
            agents.retain(|a| a.id != agent.id);
            agents.push(agent);
        }
    }
    Ok(agents)
}
