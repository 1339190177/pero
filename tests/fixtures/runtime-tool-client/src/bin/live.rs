//! Opt-in live smoke driver using the production ACP runtime. No wire client.
use pero_runtime::acp::{self, AgentConfig, SessionCommand};
use serde::Deserialize;
use serde_json::{Value, json};
use std::{
    collections::{HashMap, HashSet},
    fs,
    path::PathBuf,
    sync::mpsc,
    time::{Duration, Instant},
};
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Config {
    agent: AgentConfig,
    workspace: PathBuf,
    prompt: String,
    expected_content: String,
}
fn permitted(name: &str, args: &Value, expected: &str) -> bool {
    match name {
        "write_file" => *args == json!({"path":"live-result.txt","content":expected}),
        "shell" => {
            *args == json!({"command":"pwd"}) || *args == json!({"command":"pwd","timeout_ms":1000})
        }
        _ => false,
    }
}
fn main() {
    if std::env::var_os("DEEPSEEK_API_KEY").is_none_or(|v| v.is_empty()) {
        eprintln!("live test refused: credential environment absent");
        std::process::exit(2);
    }
    if run().is_err() {
        eprintln!("live ACP scenario failed (details deliberately suppressed)");
        std::process::exit(1);
    }
}
fn run() -> Result<(), Box<dyn std::error::Error>> {
    let config: Config =
        serde_json::from_slice(&fs::read(std::env::args().nth(1).ok_or("config")?)?)?;
    let workspace = config.workspace.clone();
    let (tx, rx) = mpsc::channel();
    let handle = acp::start(
        config.agent,
        workspace.clone(),
        "live-bounded-smoke".into(),
        tx,
    );
    let mut calls: HashMap<String, (String, Value)> = HashMap::new();
    let mut duplicate = HashSet::new();
    let mut granted = HashSet::new();
    let mut completed = HashSet::new();
    let mut granted_names = HashSet::new();
    let mut denied = 0;
    let mut shell_cwd = false;
    let mut readback = false;
    let mut finished = false;
    let deadline = Instant::now() + Duration::from_secs(60);
    let result = (|| -> Result<(), Box<dyn std::error::Error>> {
        loop {
            if Instant::now() >= deadline {
                return Err("deadline".into());
            }
            let e = match rx.recv_timeout(Duration::from_millis(50)) {
                Ok(e) => e,
                Err(mpsc::RecvTimeoutError::Timeout) => continue,
                Err(_) => return Err("closed".into()),
            };
            match e.kind.as_str() {
                "session_ready" => handle
                    .tx
                    .send(SessionCommand::Prompt(config.prompt.clone()))?,
                "session_update" => {
                    let id = e.data["toolCallId"].as_str().unwrap_or("").to_owned();
                    if e.data["sessionUpdate"] == "tool_call" {
                        if calls.contains_key(&id) {
                            duplicate.insert(id.clone());
                        }
                        calls.insert(
                            id,
                            (
                                e.data["title"].as_str().unwrap_or("").to_owned(),
                                e.data["rawInput"].clone(),
                            ),
                        );
                    } else if e.data["sessionUpdate"] == "tool_call_update"
                        && e.data["status"] == "completed"
                    {
                        if let Some((name, args)) = calls.get(&id) {
                            let text = e.data["content"].to_string();
                            if name == "read_file"
                                && args["path"] == "live-result.txt"
                                && text.contains(&config.expected_content)
                            {
                                readback = true;
                            }
                            if name == "shell"
                                && granted.contains(&id)
                                && text.contains(workspace.to_str().ok_or("workspace")?)
                            {
                                shell_cwd = true;
                            }
                            if granted.contains(&id) {
                                completed.insert(name.clone());
                            }
                        }
                    }
                }
                "permission" => {
                    let pid = e.data["permission_id"].as_str().ok_or("permission")?;
                    let id = e.data["toolCall"]["toolCallId"].as_str().unwrap_or("");
                    let safe = !id.is_empty()
                        && !duplicate.contains(id)
                        && !granted.contains(id)
                        && calls.get(id).is_some_and(|(name, args)| {
                            !granted_names.contains(name)
                                && permitted(name, args, &config.expected_content)
                        });
                    let choice = if safe {
                        e.data["options"]
                            .as_array()
                            .and_then(|o| o.iter().find(|v| v["kind"] == "allow_once"))
                            .and_then(|v| v["optionId"].as_str())
                            .map(str::to_owned)
                    } else {
                        None
                    };
                    if choice.is_some() {
                        granted.insert(id.to_owned());
                        granted_names.insert(calls[id].0.clone());
                    } else {
                        denied += 1;
                    }
                    let answer = handle
                        .permissions
                        .lock()
                        .map_err(|_| "lock")?
                        .remove(pid)
                        .ok_or("stale")?;
                    let _ = answer.send(choice);
                }
                "turn_finished" => {
                    if e.data["stopReason"] != "end_turn" {
                        return Err("incomplete turn".into());
                    }
                    finished = true;
                    handle.tx.send(SessionCommand::Close)?;
                }
                "session_error" | "session_auth_required" | "session_auth_error" => {
                    return Err("runtime".into());
                }
                "session_closed" => break,
                _ => {}
            }
        }
        if !finished
            || !completed.contains("write_file")
            || !completed.contains("shell")
            || !shell_cwd
            || !readback
            || fs::read_to_string(workspace.join("live-result.txt"))? != config.expected_content
        {
            return Err("evidence missing".into());
        }
        println!(
            "{}",
            json!({"scenario":"explicit-allow-once","passed":true,"write_readback":true,"shell_pwd_verified":true,"permission_grants":granted.len(),"permission_denials":denied})
        );
        Ok(())
    })();
    let _ = handle.tx.send(SessionCommand::Close);
    if result.is_err() {
        let end = Instant::now() + Duration::from_secs(5);
        while Instant::now() < end {
            match rx.recv_timeout(Duration::from_millis(50)) {
                Ok(e) if e.kind == "session_closed" => break,
                Err(mpsc::RecvTimeoutError::Disconnected) => break,
                _ => {}
            }
        }
    }
    result
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn approval_is_exact() {
        assert!(permitted("shell", &json!({"command":"pwd"}), "x"));
        for command in ["pwd; env", "pwd\nls", "/bin/pwd", "cat /etc/passwd"] {
            assert!(!permitted("shell", &json!({"command":command}), "x"));
        }
        assert!(!permitted(
            "write_file",
            &json!({"path":"../live-result.txt","content":"x"}),
            "x"
        ));
        assert!(!permitted(
            "write_file",
            &json!({"path":"live-result.txt","content":"wrong"}),
            "x"
        ));
        assert!(!permitted(
            "shell",
            &json!({"command":"pwd","other":true}),
            "x"
        ));
    }
}
