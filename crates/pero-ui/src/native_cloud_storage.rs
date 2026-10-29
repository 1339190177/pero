//! Saved Cloud identity belongs to the native shell, outside renderer state.
use super::*;
use crate::native_secrets::SecretStore;
use serde::{Deserialize, Serialize};
use std::path::Path;

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq)]
pub(crate) struct CloudAccount {
    pub url: String,
    pub owner_id: String,
    pub client_mode: bool,
}
#[derive(Deserialize, Serialize)]
struct SavedConnection {
    url: String,
    token: String,
    client_mode: bool,
}

impl CloudSession {
    pub(crate) fn with_store(data: &Path) -> Self {
        Self::load_vault(SecretStore::new(data, "cloud"))
    }
    fn load_vault(vault: SecretStore) -> Self {
        let mut session = Self::new();
        match vault.load::<SavedConnection>() {
            Ok(Some(saved)) => match desktop_login::service_origin(&saved.url) {
                Ok(url)
                    if !saved.token.is_empty()
                        && saved.token.len() <= 512
                        && !saved.token.chars().any(char::is_control) =>
                {
                    session.base = url;
                    session.token = saved.token;
                    session.client_mode = saved.client_mode;
                    session.reload();
                }
                _ => {
                    session.error = Some("Saved Cloud connection is invalid. Sign in again.".into())
                }
            },
            Ok(None) => {}
            Err(error) => session.error = Some(error),
        }
        session.vault = Some(vault);
        session
    }
    pub(super) fn persist_connection(&mut self) {
        if let Some(vault) = &self.vault {
            if let Err(error) = vault.save(&SavedConnection {
                url: self.base.clone(),
                token: self.token.clone(),
                client_mode: self.client_mode,
            }) {
                self.error = Some(error);
            }
        }
    }
    pub(super) fn forget_connection(&mut self) {
        if let Some(vault) = &self.vault {
            if let Err(error) = vault.clear() {
                self.error = Some(error);
            }
        }
    }
    pub(crate) fn device_account(&self) -> Option<CloudAccount> {
        self.configured().then_some(())?;
        let owner_id = self.identity["id"].as_str().filter(|id| !id.is_empty())?;
        Some(CloudAccount {
            url: self.base.clone(),
            owner_id: owner_id.into(),
            client_mode: self.client_mode,
        })
    }
    pub(crate) fn has_connection(&self) -> bool {
        self.configured()
    }
    pub(crate) fn enroll_local_worker(&self, body: &Value) -> Result<Value, String> {
        if self.device_account().is_none() {
            return Err("Connect your Cloud account before enabling local execution.".into());
        }
        let path = if self.client_mode {
            "/api/client/v1/workers"
        } else {
            "/api/v1/workers/enroll"
        };
        self.post(path, body)
    }
}

#[cfg(test)]
impl CloudSession {
    pub(crate) fn verified_fixture(account: CloudAccount) -> Self {
        let mut session = Self::new();
        session.base = account.url;
        session.token = "client-fixture-token".into();
        session.client_mode = account.client_mode;
        session.identity = json!({"id":account.owner_id});
        session
    }
}

#[cfg(test)]
impl CloudSession {
    pub(crate) fn http_fixture(
        requests: usize,
        mut response: impl FnMut(usize, &str) -> Option<Value> + Send + 'static,
    ) -> (Self, std::thread::JoinHandle<Vec<Value>>) {
        use std::{
            io::{BufRead, BufReader, Read, Write},
            net::TcpListener,
            time::{Duration, Instant},
        };
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        listener.set_nonblocking(true).unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let server = std::thread::spawn(move || {
            let deadline = Instant::now() + Duration::from_secs(8);
            let mut bodies = Vec::new();
            while bodies.len() < requests {
                let (mut stream, _) = match listener.accept() {
                    Ok(connection) => connection,
                    Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
                        assert!(Instant::now() < deadline, "Cloud fixture requests missing");
                        std::thread::sleep(Duration::from_millis(10));
                        continue;
                    }
                    Err(error) => panic!("{error}"),
                };
                stream
                    .set_read_timeout(Some(Duration::from_secs(2)))
                    .unwrap();
                let mut input = BufReader::new(&mut stream);
                let mut line = String::new();
                input.read_line(&mut line).unwrap();
                let route = line.split_whitespace().nth(1).unwrap().to_string();
                let mut length = 0;
                let mut authorized = false;
                loop {
                    line.clear();
                    input.read_line(&mut line).unwrap();
                    if line == "\r\n" {
                        break;
                    }
                    if let Some((name, value)) = line.split_once(':') {
                        if name.eq_ignore_ascii_case("content-length") {
                            length = value.trim().parse::<usize>().unwrap();
                        }
                        if name.eq_ignore_ascii_case("authorization") {
                            authorized = value.trim() == "Bearer client-fixture-token";
                        }
                    }
                }
                assert!(authorized);
                let mut body = vec![0; length];
                input.read_exact(&mut body).unwrap();
                drop(input);
                bodies.push(if body.is_empty() {
                    Value::Null
                } else {
                    serde_json::from_slice::<Value>(&body).unwrap()
                });
                if let Some(body) = response(bodies.len() - 1, &route) {
                    let body = body.to_string();
                    write!(stream,"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",body.len()).unwrap();
                }
            }
            bodies
        });
        (
            Self::verified_fixture(CloudAccount {
                url,
                owner_id: "owner".into(),
                client_mode: true,
            }),
            server,
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn saved_connection_reauthenticates_after_restart_and_disconnect_forgets_it() {
        let (connection, server) = CloudSession::http_fixture(4, |_, path| {
            Some(match path {
                "/api/client/v1/me" => json!({"id":"owner"}),
                "/api/client/v1/github/status" => json!({}),
                "/api/client/v1/workers" => json!({"workers":[]}),
                "/api/client/v1/tasks?limit=50" => json!({"tasks":[],"has_more":false}),
                path => panic!("Unexpected Cloud route {path}"),
            })
        });
        let vault = SecretStore::memory();
        vault
            .save(&SavedConnection {
                url: connection.base,
                token: connection.token,
                client_mode: true,
            })
            .unwrap();
        let mut restored = CloudSession::load_vault(vault);
        server.join().unwrap();
        assert_eq!(restored.device_account().unwrap().owner_id, "owner");
        assert_eq!(restored.token, "client-fixture-token");
        assert!(
            !restored
                .remote_event()
                .data
                .to_string()
                .contains("client-fixture-token")
        );
        restored.command(&json!({"action":"remote_disconnect"}));
        assert!(restored.device_account().is_none());
        let vault = restored.vault.take().unwrap();
        assert!(vault.load::<SavedConnection>().unwrap().is_none());
        assert!(!CloudSession::load_vault(vault).has_connection());
    }
}
