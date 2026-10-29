//! Operator-only, fail-closed native approval admission. ACP metadata is evidence
//! only from a pinned native agent; it is never authority by itself.
use pero_runtime::acp::AgentConfig;
use ring::digest::{Context, SHA256};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::{
    collections::{BTreeMap, BTreeSet},
    io::Read,
    path::{Path, PathBuf},
};

#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Policy {
    pub agent_id: String,
    pub executable: PathBuf,
    pub sha256: String,
    pub args: Vec<String>,
    pub env: BTreeMap<String, String>,
    pub sandbox_mode: String,
    pub operations: Vec<String>,
}

impl Policy {
    pub fn validate(&self, agent: &AgentConfig, workspaces: &[PathBuf]) -> Result<(), String> {
        if self.agent_id != agent.id
            || self.args != agent.args
            || self.env != agent.env
            || self.executable != Path::new(&agent.command)
            || !matches!(self.sandbox_mode.as_str(), "workspace-write" | "read-only")
            || self.operations != ["shell"]
        {
            return Err("Native approval policy does not match trusted agent configuration".into());
        }
        protected_path(&self.executable, workspaces)?;
        if self.sha256.len() != 64
            || !self
                .sha256
                .bytes()
                .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
        {
            return Err("Native approval requires a lowercase SHA256 executable pin".into());
        }
        let mut file =
            std::fs::File::open(&self.executable).map_err(|_| "Trusted executable unavailable")?;
        let mut context = Context::new(&SHA256);
        let mut buffer = [0; 16384];
        let mut first = true;
        loop {
            let n = file
                .read(&mut buffer)
                .map_err(|_| "Cannot hash trusted executable")?;
            if n == 0 {
                break;
            }
            // Interpreter scripts would pin the interpreter, not the executed code.
            if first
                && !(buffer[..n].starts_with(b"\x7fELF")
                    || buffer[..n].starts_with(&[0xcf, 0xfa, 0xed, 0xfe]))
            {
                return Err("Native approval requires a native executable, not a script".into());
            }
            first = false;
            context.update(&buffer[..n]);
        }
        let hash: String = context
            .finish()
            .as_ref()
            .iter()
            .map(|b| format!("{b:02x}"))
            .collect();
        if hash != self.sha256 {
            return Err("Trusted executable SHA256 mismatch".into());
        }
        Ok(())
    }
}

pub fn protected_path(path: &Path, workspaces: &[PathBuf]) -> Result<(), String> {
    if !path.is_absolute()
        || path.canonicalize().ok().as_deref() != Some(path)
        || workspaces.iter().any(|root| path.starts_with(root))
    {
        return Err(
            "Approval configuration and executable must be canonical and outside workspaces".into(),
        );
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::MetadataExt;
        for ancestor in path.ancestors() {
            let metadata = ancestor
                .metadata()
                .map_err(|_| "Trusted path unavailable")?;
            if metadata.mode() & 0o022 != 0
                || (metadata.uid() != 0 && metadata.uid() != unsafe { libc::geteuid() })
            {
                return Err(
                    "Approval trusted paths must be operator-owned and not group/world writable"
                        .into(),
                );
            }
        }
    }
    Ok(())
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Descriptor {
    version: u32,
    operation: String,
    workspace: PathBuf,
    sandbox_mode: String,
    enforcement: String,
    network: String,
}

pub struct Gate {
    policy: Option<Policy>,
    workspace: PathBuf,
    session: Option<String>,
    revoked: bool,
    seen: BTreeSet<String>,
}
impl Gate {
    pub fn new(policy: Option<Policy>, workspace: PathBuf) -> Self {
        Self {
            policy,
            workspace,
            session: None,
            revoked: false,
            seen: BTreeSet::new(),
        }
    }
    pub fn ready(&mut self, session: Option<&str>) {
        // Never rearm a completed/cancelled run or accept session replacement.
        if self.session.is_some() {
            self.revoked = true;
        } else {
            self.session = session.filter(|s| !s.is_empty()).map(str::to_owned);
        }
    }
    pub fn revoke(&mut self) {
        self.revoked = true;
    }
    pub fn select(&mut self, request: &Value) -> Option<String> {
        // Bound memory even if a broken agent floods distinct approval requests.
        if self.seen.len() >= 8192 {
            self.revoke();
            return None;
        }
        let id = request.get("permission_id")?.as_str()?;
        let call = request.get("toolCall")?.get("toolCallId")?.as_str()?;
        if id.is_empty()
            || call.is_empty()
            || !self.seen.insert(format!("request:{id}"))
            || !self.seen.insert(format!("call:{call}"))
        {
            return None;
        }
        let policy = self.policy.as_ref()?;
        if self.revoked
            || self.session.as_deref() != request.get("sessionId").and_then(Value::as_str)
            || self.session.is_none()
        {
            return None;
        }
        let descriptor: Descriptor = serde_json::from_value(
            request
                .get("_meta")?
                .get("pero.dev/native-approval")?
                .clone(),
        )
        .ok()?;
        if descriptor.version != 1
            || descriptor.operation != "shell"
            || descriptor.workspace != self.workspace
            || descriptor.sandbox_mode != policy.sandbox_mode
            || descriptor.enforcement != "required"
            || descriptor.network != "deny"
        {
            return None;
        }
        let options = request.get("options")?.as_array()?;
        let allowed: Vec<_> = options
            .iter()
            .filter(|o| o.get("kind").and_then(Value::as_str) == Some("allow_once"))
            .collect();
        if allowed.len() != 1 {
            return None;
        }
        let option = allowed[0].get("optionId")?.as_str()?;
        if option.is_empty()
            || options
                .iter()
                .filter(|o| o.get("optionId").and_then(Value::as_str) == Some(option))
                .count()
                != 1
        {
            return None;
        }
        Some(option.to_owned())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    fn policy() -> Policy {
        Policy {
            agent_id: "native".into(),
            executable: "/operator/pero".into(),
            sha256: "0".repeat(64),
            args: vec![],
            env: BTreeMap::new(),
            sandbox_mode: "workspace-write".into(),
            operations: vec!["shell".into()],
        }
    }
    fn request() -> Value {
        json!({"permission_id":"p1","sessionId":"s1","toolCall":{"toolCallId":"c1","title":"ignored"},"options":[{"optionId":"yes","kind":"allow_once"}],"_meta":{"pero.dev/native-approval":{"version":1,"operation":"shell","workspace":"/workspace/allowed","sandbox_mode":"workspace-write","enforcement":"required","network":"deny"}}})
    }
    fn gate() -> Gate {
        let mut gate = Gate::new(Some(policy()), "/workspace/allowed".into());
        gate.ready(Some("s1"));
        gate
    }
    #[test]
    fn pin_config_and_protected_install_are_checked() {
        let root = std::env::current_dir()
            .unwrap()
            .join(format!("approval-fixture-{}", uuid::Uuid::new_v4()));
        std::fs::create_dir(&root).unwrap();
        // Trusted paths must use the platform's canonical spelling, including
        // the verbatim drive prefix returned by canonicalize on Windows.
        let root = root.canonicalize().unwrap();
        let binary = root.join("native");
        let bytes = b"\x7fELFsynthetic-not-executed";
        std::fs::write(&binary, bytes).unwrap();
        let mut p = policy();
        p.executable = binary.clone();
        p.sha256 = ring::digest::digest(&SHA256, bytes)
            .as_ref()
            .iter()
            .map(|b| format!("{b:02x}"))
            .collect();
        let agent: AgentConfig = serde_json::from_value(
            json!({"id":"native","name":"test","command":binary,"args":[],"env":{}}),
        )
        .unwrap();
        p.validate(&agent, &[]).unwrap();
        assert!(p.validate(&agent, std::slice::from_ref(&root)).is_err());
        let mut changed = agent.clone();
        changed.args.push("untrusted".into());
        assert!(p.validate(&changed, &[]).is_err());
        std::fs::write(&binary, b"\x7fELFreplaced").unwrap();
        assert!(p.validate(&agent, &[]).is_err());
        std::fs::write(&binary, b"#!/bin/sh\n").unwrap();
        assert!(p.validate(&agent, &[]).is_err());
        std::fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn failed_request_cannot_replay_after_repair() {
        let mut g = gate();
        let mut r = request();
        r["sessionId"] = json!("unknown");
        assert_eq!(g.select(&r), None);
        assert_eq!(g.select(&request()), None);
        let mut g = gate();
        let mut r = request();
        g.ready(Some("other"));
        r["sessionId"] = json!("other");
        assert_eq!(g.select(&r), None);
    }
    #[test]
    fn bounded_shell_is_once_only() {
        let mut g = gate();
        assert_eq!(g.select(&request()), Some("yes".into()));
        assert_eq!(g.select(&request()), None);
    }
    #[test]
    fn no_policy_no_authority() {
        let mut g = Gate::new(None, "/workspace/allowed".into());
        g.ready(Some("s1"));
        assert_eq!(g.select(&request()), None);
    }
    #[test]
    fn missing_or_forged_evidence_denied() {
        for key in [
            "version",
            "operation",
            "workspace",
            "sandbox_mode",
            "enforcement",
            "network",
        ] {
            let mut r = request();
            r["_meta"]["pero.dev/native-approval"][key] = json!("forged");
            assert_eq!(gate().select(&r), None);
        }
        let mut r = request();
        r["_meta"] = Value::Null;
        r["toolCall"]["title"] = json!("native shell workspace-write approved");
        assert_eq!(gate().select(&r), None);
    }
    #[test]
    fn write_path_escape_unknown_session_and_late_requests_denied() {
        for operation in ["write_file", "edit_file", "mcp__shell"] {
            let mut r = request();
            r["_meta"]["pero.dev/native-approval"]["operation"] = json!(operation);
            assert_eq!(gate().select(&r), None);
        }
        for path in [
            "/workspace/allowed/../outside",
            "/workspace/allowed-other",
            "../allowed",
        ] {
            let mut r = request();
            r["_meta"]["pero.dev/native-approval"]["workspace"] = json!(path);
            assert_eq!(gate().select(&r), None);
        }
        let mut r = request();
        r["sessionId"] = json!("other");
        assert_eq!(gate().select(&r), None);
        let mut g = gate();
        g.revoke();
        assert_eq!(g.select(&request()), None);
        g.ready(Some("s1"));
        assert_eq!(g.select(&request()), None);
    }
    #[test]
    fn no_persistent_or_ambiguous_allow() {
        let mut r = request();
        r["options"][0]["kind"] = json!("allow_always");
        assert_eq!(gate().select(&r), None);
        let mut r = request();
        r["options"]
            .as_array_mut()
            .unwrap()
            .push(json!({"optionId":"yes","kind":"reject_once"}));
        assert_eq!(gate().select(&r), None);
    }
}
