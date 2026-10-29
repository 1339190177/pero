//! The Worker control endpoint is an origin, never a route or credential URL.
use reqwest::Url;
pub struct WorkerOrigin(Url);
impl WorkerOrigin {
    pub fn parse(value: &str) -> Result<Self, String> {
        let url = Url::parse(value).map_err(|_| "Invalid Worker server origin")?;
        let local = url
            .host_str()
            .is_some_and(|host| matches!(host, "localhost" | "127.0.0.1" | "[::1]" | "::1"));
        if (url.scheme() != "https" && !(url.scheme() == "http" && local))
            || url.host_str().is_none()
            || !url.username().is_empty()
            || url.password().is_some()
            || url.query().is_some()
            || url.fragment().is_some()
            || url.path() != "/"
        {
            return Err("Worker server must be an HTTPS origin, or HTTP loopback, without credentials or a path".into());
        }
        Ok(Self(url))
    }
    pub fn into_url(self) -> Url {
        self.0
    }
    pub fn as_str(&self) -> &str {
        self.0.as_str()
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn worker_origin_requires_a_credential_free_origin() {
        for (url, valid) in [
            ("https://app.openpero.com", true),
            ("http://127.0.0.1:8787", true),
            ("http://example.com", false),
            ("https://u:p@example.com", false),
            ("https://example.com/api", false),
        ] {
            assert_eq!(WorkerOrigin::parse(url).is_ok(), valid);
        }
    }
}

/// A process crash may reconnect only before it owns execution or requests review.
pub enum RestartPlan {
    Retry {
        attempt: u32,
        delay: std::time::Duration,
    },
    Halt,
}
impl RestartPlan {
    pub fn after_exit(
        status: pero_contracts::local_worker::LocalWorkerStatus,
        attempt: u32,
    ) -> Self {
        use pero_contracts::local_worker::LocalWorkerStatus::*;
        if !matches!(status, Starting | Idle | Offline) || attempt >= 3 {
            return Self::Halt;
        }
        let attempt = attempt + 1;
        Self::Retry {
            attempt,
            delay: std::time::Duration::from_secs(1u64 << attempt),
        }
    }
}
#[cfg(test)]
mod restart_tests {
    use super::*;
    use pero_contracts::local_worker::LocalWorkerStatus::*;
    #[test]
    fn only_nonexecuting_crashes_receive_bounded_restart() {
        for (status, attempt, retry) in [
            (Starting, 0, true),
            (Idle, 2, true),
            (Offline, 3, false),
            (Running, 0, false),
            (NeedsAttention, 0, false),
            (Unavailable, 0, false),
            (Stopped, 0, false),
        ] {
            assert_eq!(
                matches!(
                    RestartPlan::after_exit(status, attempt),
                    RestartPlan::Retry { .. }
                ),
                retry
            );
        }
    }
}
