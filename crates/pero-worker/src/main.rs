mod control;
mod executor;
mod journal;
mod model;
mod supervisor;

use control::Control;
use journal::{Active, Claim, Journal};
use model::Outcome;
use std::{
    path::PathBuf,
    time::{Duration, Instant},
};

fn main() {
    let result = if std::env::args().nth(1).as_deref() == Some("--execute") {
        executor::child_execute()
    } else {
        run()
    };
    if let Err(error) = result {
        eprintln!("pero-worker: {error}");
        std::process::exit(1);
    }
}

fn required(name: &str) -> Result<String, String> {
    std::env::var(name)
        .ok()
        .filter(|v| !v.trim().is_empty())
        .ok_or_else(|| format!("Set {name}"))
}

fn terminal(status: &str) -> bool {
    matches!(status, "succeeded" | "failed" | "cancelled" | "reconcile")
}

fn clear(journal: &mut Journal) -> Result<(), String> {
    journal.state.active = None;
    journal.state.pending_claim = None;
    journal.save()
}

fn complete(control: &Control, journal: &mut Journal) -> Result<(), String> {
    let active = journal.state.active.as_ref().ok_or("Missing Run journal")?;
    let result = active.result.clone().ok_or("Missing execution outcome")?;
    let run = active.run.clone();
    // Persisted result is retried verbatim, preserving the server's idempotent receipt.
    match control.complete(&run, &result) {
        Ok(_) => clear(journal),
        Err(error) => match control.inspect(&run) {
            Ok(current) if terminal(&current.status) => clear(journal),
            Ok(current)
                if current.status == "cancel_requested" && result.outcome == "succeeded" =>
            {
                journal.state.active.as_mut().unwrap().result = Some(Outcome::new(
                    "reconcile",
                    "Cancellation raced with Agent completion; execution stopped; review candidate effects",
                ));
                journal.save()?;
                complete(control, journal)
            }
            _ => Err(format!("{error}; result retained for reconciliation")),
        },
    }
}

fn recover(control: &Control, journal: &mut Journal) -> Result<bool, String> {
    let mut needs_review = false;
    // A lost claim response is recovered using the ORIGINAL process instance and key.
    if let Some(claim) = &journal.state.pending_claim {
        let request_id = claim.request_id.clone();
        let run = control.claim(claim)?;
        journal.state.pending_claim = None;
        if let Some(run) = run {
            journal.state.active = Some(Active {
                run,
                request_id,
                events: vec![],
                acknowledged: 0,
                result: None,
            });
        }
        journal.save()?;
    }
    if let Some(active) = journal.state.active.as_ref() {
        needs_review = active
            .result
            .as_ref()
            .is_none_or(|result| result.outcome == "reconcile");
        if control
            .inspect(&active.run)
            .is_ok_and(|current| terminal(&current.status))
        {
            clear(journal)?;
            return Ok(needs_review);
        }
        if active.result.is_none() {
            journal.state.active.as_mut().unwrap().result = Some(Outcome::new(
                "reconcile",
                "Worker restarted after acquiring execution; prompt was not replayed; inspect previous effects",
            ));
            journal.save()?;
        }
        complete(control, journal)?;
    }
    Ok(needs_review)
}

fn run() -> Result<(), String> {
    #[cfg(not(unix))]
    {
        return Err("Worker process supervision currently requires Unix".into());
    }
    let control = Control::new(&required("PERO_CLOUD_URL")?, required("PERO_WORKER_TOKEN")?)?;
    let config_path = PathBuf::from(required("PERO_WORKER_CONFIG")?);
    let mut config: executor::Config = serde_json::from_slice(
        &std::fs::read(&config_path).map_err(|_| "Worker configuration is unavailable")?,
    )
    .map_err(|_| "Invalid Worker configuration")?;
    let config_root = config_path
        .canonicalize()
        .map_err(|e| e.to_string())?
        .parent()
        .unwrap()
        .to_path_buf();
    for path in config.workspaces.values_mut() {
        if path.is_relative() {
            *path = config_root.join(&*path);
        }
    }
    if let Some(path) = config.agents_file.as_mut() {
        if path.is_relative() {
            *path = config_root.join(&*path);
        }
    }
    let mut journal = Journal::open(PathBuf::from(required("PERO_WORKER_STATE")?))?;
    if recover(&control, &mut journal)? {
        eprintln!("Interrupted execution reconciled; review effects before restarting this Worker");
        return Ok(());
    }
    let instance_id = uuid::Uuid::new_v4().to_string();
    let once = std::env::args().any(|arg| arg == "--once");
    loop {
        journal.state.pending_claim = Some(Claim {
            request_id: uuid::Uuid::new_v4().to_string(),
            instance_id: instance_id.clone(),
        });
        journal.save()?;
        let started = Instant::now();
        let claim = journal.state.pending_claim.as_ref().unwrap();
        let request_id = claim.request_id.clone();
        let claimed = control.claim(claim)?;
        journal.state.pending_claim = None;
        let Some(run) = claimed else {
            journal.save()?;
            if once {
                return Ok(());
            }
            std::thread::sleep(Duration::from_secs(2));
            continue;
        };
        if terminal(&run.status) {
            journal.save()?;
            continue;
        }
        journal.state.active = Some(Active {
            run: run.clone(),
            request_id,
            events: vec![],
            acknowledged: 0,
            result: None,
        });
        // This durable record MUST precede workspace access, Agent spawn and prompt submission.
        journal.save()?;
        let result = match config.execution(&run) {
            Ok((execution, _workspace_lock)) => {
                supervisor::execute(&control, &mut journal, execution, started)
                    .unwrap_or_else(|error| Outcome::new("reconcile", error))
            }
            Err(error) => Outcome::new("failed", error),
        };
        eprintln!("Run {}: {}", run.id, result.outcome);
        let stop = result.outcome == "reconcile";
        journal.state.active.as_mut().unwrap().result = Some(result);
        journal.save()?;
        // Once execution stops, retain and upload its complete evidence before completion.
        while journal
            .state
            .active
            .as_ref()
            .is_some_and(|active| active.acknowledged < active.events.last().map_or(0, |e| e.seq))
        {
            let active = journal.state.active.as_ref().unwrap();
            if let Err(error) = control.renew(&active.run) {
                // Lease expiry may already have produced a terminal reconcile state.
                if control
                    .inspect(&active.run)
                    .is_ok_and(|current| terminal(&current.status))
                {
                    break;
                }
                return Err(format!("{error}; stopped Run retained for reconciliation"));
            }
            let acknowledged = control.flush(journal.state.active.as_ref().unwrap())?;
            journal.state.active.as_mut().unwrap().acknowledged = acknowledged;
            journal.save()?;
        }
        complete(&control, &mut journal)?;
        if stop {
            return Err("Execution requires reconciliation; Worker stopped receiving tasks".into());
        }
        if once {
            return Ok(());
        }
    }
}
