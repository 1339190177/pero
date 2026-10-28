use crate::{
    journal::private_file,
    model::{Outcome, Run},
};
use fs2::FileExt;
use pero_contracts::HostEvent;
use pero_runtime::acp::{self, AgentConfig, SessionCommand};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::{
    collections::BTreeMap,
    io::{BufRead, Write},
    path::PathBuf,
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
        mpsc,
    },
    time::{Duration, Instant},
};

pub fn execution_env_allowed(key: &std::ffi::OsStr) -> bool {
    !key.to_string_lossy().starts_with("PERO_")
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Config {
    pub workspaces: BTreeMap<String, PathBuf>,
    pub agents_file: Option<PathBuf>,
}

#[derive(Deserialize, Serialize)]
pub struct Execution {
    workspace: PathBuf,
    agent: AgentConfig,
    prompt: String,
    run_id: String,
}

impl Config {
    pub fn execution(&self, run: &Run) -> Result<(Execution, std::fs::File), String> {
        let workspace = self
            .workspaces
            .get(&run.spec.workspace_key)
            .ok_or("Workspace key is not allowed on this Worker")?
            .canonicalize()
            .map_err(|_| "Allowed workspace is unavailable")?;
        if !workspace.is_dir() {
            return Err("Allowed workspace is not a directory".into());
        }
        let agents = match &self.agents_file {
            Some(path) => serde_json::from_slice::<Vec<AgentConfig>>(
                &std::fs::read(path).map_err(|_| "Agent allowlist is unavailable")?,
            )
            .map_err(|_| "Invalid Agent allowlist")?,
            None => acp::registry(&std::env::current_dir().map_err(|e| e.to_string())?)?,
        };
        let mut agent = agents
            .into_iter()
            .find(|agent| agent.id == run.spec.agent_id && agent.available())
            .ok_or("Agent is not allowed or installed on this Worker")?;
        agent.env.retain(|key, _| !key.starts_with("PERO_"));
        // All Worker instances addressing this checkout lock the same file.
        let lock = private_file(&workspace.join(".pero-worker.lock"))?;
        lock.try_lock_exclusive()
            .map_err(|_| "Workspace is busy; another Run owns its write lock")?;
        let acceptance = match &run.spec.acceptance {
            Value::String(text) => text.clone(),
            Value::Null => String::new(),
            value => value.to_string(),
        };
        let prompt = if acceptance.is_empty() {
            run.spec.prompt.clone()
        } else {
            format!(
                "{}\n\nAcceptance criteria:\n{}",
                run.spec.prompt, acceptance
            )
        };
        Ok((
            Execution {
                workspace,
                agent,
                prompt,
                run_id: run.id.clone(),
            },
            lock,
        ))
    }
}

pub fn bounded_line(reader: &mut impl BufRead, max: usize) -> Result<Option<Vec<u8>>, String> {
    let mut line = Vec::new();
    loop {
        let buffer = reader.fill_buf().map_err(|e| e.to_string())?;
        if buffer.is_empty() {
            return if line.is_empty() {
                Ok(None)
            } else {
                Err("Truncated child event".into())
            };
        }
        let take = buffer
            .iter()
            .position(|b| *b == b'\n')
            .map_or(buffer.len(), |n| n + 1);
        if line.len() + take > max {
            return Err("Child event exceeds output limit".into());
        }
        line.extend_from_slice(&buffer[..take]);
        reader.consume(take);
        if line.last() == Some(&b'\n') {
            return Ok(Some(line));
        }
    }
}

pub fn child_execute() -> Result<(), String> {
    #[cfg(not(unix))]
    {
        return Err("Worker process supervision currently requires Unix".into());
    }
    let mut input = std::io::stdin().lock();
    let execution: Execution = serde_json::from_slice(
        &bounded_line(&mut input, 512 * 1024)?.ok_or("Missing execution configuration")?,
    )
    .map_err(|_| "Invalid execution configuration")?;
    drop(input);
    // Never let an orphaned executor keep writing after its supervisor disappears.
    #[cfg(unix)]
    {
        let parent = unsafe { libc::getppid() };
        if parent <= 1 {
            return Err("Executor supervisor is unavailable".into());
        }
        std::thread::spawn(move || {
            loop {
                std::thread::sleep(Duration::from_millis(100));
                if unsafe { libc::getppid() } != parent {
                    crate::supervisor::kill_orphan_tree();
                    break;
                }
            }
        });
    }
    let (tx, rx) = mpsc::channel();
    let handle = acp::start(execution.agent, execution.workspace, execution.run_id, tx);
    let cancel = Arc::new(AtomicBool::new(false));
    let stdin_cancel = cancel.clone();
    std::thread::spawn(move || {
        let mut input = std::io::stdin().lock();
        // EOF also revokes execution authority.
        let _ = bounded_line(&mut input, 128);
        stdin_cancel.store(true, Ordering::SeqCst);
    });
    let mut cancelled_at = None;
    let mut result = None;
    let mut closing_at = None;
    loop {
        if cancel.load(Ordering::SeqCst) && cancelled_at.is_none() {
            let _ = handle.tx.send(SessionCommand::Cancel);
            cancelled_at = Some(Instant::now());
        }
        if cancelled_at.is_some_and(|at| at.elapsed() > Duration::from_secs(2)) {
            result = Some(Outcome::new("cancelled", "Execution cancelled"));
            break;
        }
        if closing_at.is_some_and(|at: Instant| at.elapsed() > Duration::from_secs(2)) {
            break;
        }
        let event = match rx.recv_timeout(Duration::from_millis(50)) {
            Ok(event) => event,
            Err(mpsc::RecvTimeoutError::Timeout) => continue,
            Err(_) => break,
        };
        if event.kind == "permission" {
            if let Some(id) = event.data["permission_id"].as_str() {
                if let Some(answer) = handle.permissions.lock().unwrap().remove(id) {
                    let _ = answer.send(None);
                }
            }
            // Do not expose tool arguments or executable approval actions remotely.
            emit(
                "permission_denied",
                "Unattended Worker denied an Agent permission request",
                Value::Null,
            )?;
            continue;
        }
        emit(&event.kind, &event.text, event.data.clone())?;
        match event.kind.as_str() {
            "session_ready" => {
                handle
                    .tx
                    .send(SessionCommand::Prompt(execution.prompt.clone()))
                    .map_err(|_| "Agent closed before prompt")?;
            }
            "session_auth_required" | "session_auth_error" => {
                result = Some(Outcome::new(
                    "failed",
                    "Agent requires local authentication",
                ));
            }
            "session_error" => {
                result = Some(Outcome::new("failed", "Agent execution failed; see events"));
            }
            "turn_finished" => {
                let reason = event.data["stopReason"].as_str().unwrap_or("");
                result = Some(if cancelled_at.is_some() || reason == "cancelled" {
                    Outcome::new("cancelled", "Execution cancelled")
                } else if reason == "end_turn" {
                    Outcome::new(
                        "succeeded",
                        "Agent turn completed; independent verification remains pending",
                    )
                } else {
                    Outcome::new("failed", format!("Agent stopped: {reason}"))
                });
            }
            "session_closed" => break,
            _ => {}
        }
        if result.is_some() && closing_at.is_none() {
            let _ = handle.tx.send(SessionCommand::Close);
            closing_at = Some(Instant::now());
        }
    }
    let result =
        result.unwrap_or_else(|| Outcome::new("failed", "Agent closed without a completed turn"));
    emit("worker_result", "", json!(result))
}

fn emit(kind: &str, text: &str, data: Value) -> Result<(), String> {
    let mut output = std::io::stdout().lock();
    let mut event = HostEvent::new(kind, None, text.to_owned());
    event.data = data;
    let bytes = serde_json::to_vec(&event).map_err(|e| e.to_string())?;
    if bytes.len() > 65536 {
        return Err("Agent event exceeds output limit".into());
    }
    output
        .write_all(&bytes)
        .and_then(|_| output.write_all(b"\n"))
        .and_then(|_| output.flush())
        .map_err(|e| e.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn lines_are_bounded_and_truncation_rejected() {
        assert!(bounded_line(&mut std::io::Cursor::new(b"long\n"), 3).is_err());
        assert!(bounded_line(&mut std::io::Cursor::new(b"partial"), 32).is_err());
        assert_eq!(
            bounded_line(&mut std::io::Cursor::new(b"ok\n"), 32)
                .unwrap()
                .unwrap(),
            b"ok\n"
        );
    }
    #[test]
    fn cloud_paths_and_commands_never_select_an_execution() {
        let config = Config {
            workspaces: BTreeMap::new(),
            agents_file: None,
        };
        let run = Run {
            id: "r".into(),
            generation: 1,
            instance_id: "i".into(),
            status: "running".into(),
            lease_expires_at: String::new(),
            spec: crate::model::Spec {
                title: String::new(),
                prompt: String::new(),
                workspace_key: "/tmp".into(),
                agent_id: "/bin/sh".into(),
                acceptance: Value::Null,
            },
        };
        assert_eq!(
            config.execution(&run).err().unwrap(),
            "Workspace key is not allowed on this Worker"
        );
    }
}
