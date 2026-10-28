//! Test driver for the real Pero ACP runtime; not a new protocol client.
//! Approval is restricted to explicitly named fixture calls, once per call ID.
use pero_runtime::acp::{self, AgentConfig, SessionCommand};
use serde::Deserialize;
use std::{
    collections::HashSet,
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
    #[serde(default)]
    allow_once_call_ids: HashSet<String>,
    #[serde(default)]
    cancel_when_file_exists: Option<PathBuf>,
}

fn main() {
    if let Err(error) = run() {
        eprintln!("fixture driver: {error}");
        std::process::exit(1);
    }
}

fn run() -> Result<(), Box<dyn std::error::Error>> {
    let path = std::env::args()
        .nth(1)
        .ok_or("fixture configuration path is required")?;
    let config: Config = serde_json::from_slice(&fs::read(path)?)?;
    let (tx, rx) = mpsc::channel();
    let handle = acp::start(
        config.agent,
        config.workspace,
        "production-tools-fixture".into(),
        tx,
    );
    let deadline = Instant::now() + Duration::from_secs(20);
    let mut granted = HashSet::new();
    let mut cancelled = false;
    let mut finished = false;
    let mut closed = false;
    let outcome = (|| -> Result<(), Box<dyn std::error::Error>> {
        loop {
            if Instant::now() >= deadline {
                let _ = handle.tx.send(SessionCommand::Close);
                return Err("fixture deadline exceeded".into());
            }
            if !cancelled
                && config
                    .cancel_when_file_exists
                    .as_ref()
                    .is_some_and(|p| p.is_file())
            {
                handle.tx.send(SessionCommand::Cancel)?;
                cancelled = true;
            }
            let event = match rx.recv_timeout(Duration::from_millis(10)) {
                Ok(e) => e,
                Err(mpsc::RecvTimeoutError::Timeout) => continue,
                Err(_) => return Err("runtime closed without a terminal event".into()),
            };
            println!("{}", serde_json::to_string(&event)?);
            match event.kind.as_str() {
                "session_ready" => handle
                    .tx
                    .send(SessionCommand::Prompt(config.prompt.clone()))?,
                "permission" => {
                    let permission_id = event.data["permission_id"]
                        .as_str()
                        .ok_or("missing permission identity")?;
                    let call_id = event.data["toolCall"]["toolCallId"]
                        .as_str()
                        .ok_or("missing tool call identity")?;
                    let choice = if config.allow_once_call_ids.contains(call_id)
                        && granted.insert(call_id.to_owned())
                    {
                        event.data["options"]
                            .as_array()
                            .and_then(|items| items.iter().find(|o| o["kind"] == "allow_once"))
                            .and_then(|o| o["optionId"].as_str())
                            .map(str::to_owned)
                    } else {
                        None
                    };
                    let answer = handle
                        .permissions
                        .lock()
                        .unwrap()
                        .remove(permission_id)
                        .ok_or("permission answer no longer pending")?;
                    let _ = answer.send(choice);
                }
                "turn_finished" => {
                    finished = true;
                    handle.tx.send(SessionCommand::Close)?;
                }
                "session_error" | "session_auth_required" | "session_auth_error" => {
                    let _ = handle.tx.send(SessionCommand::Close);
                    return Err(format!("runtime failed: {}", event.kind).into());
                }
                "session_closed" => {
                    closed = true;
                    return if finished {
                        Ok(())
                    } else {
                        Err("session closed without completed turn".into())
                    };
                }
                _ => {}
            }
        }
    })();
    // Even error paths must let the real runtime close its Agent process.
    if !closed {
        let _ = handle.tx.send(SessionCommand::Close);
        let end = Instant::now() + Duration::from_secs(5);
        while Instant::now() < end {
            match rx.recv_timeout(Duration::from_millis(50)) {
                Ok(event) if event.kind == "session_closed" => break,
                Err(mpsc::RecvTimeoutError::Disconnected) => break,
                _ => {}
            }
        }
    }
    outcome
}
