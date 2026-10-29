//! Native composition-root credential storage. No plaintext fallback.
use serde::{Serialize, de::DeserializeOwned};
use sha2::{Digest, Sha256};
use std::path::Path;

pub(crate) struct SecretStore {
    service: String,
    account: String,
    #[cfg(test)]
    memory: Option<std::sync::Arc<std::sync::Mutex<Option<Vec<u8>>>>>,
}
impl SecretStore {
    pub fn new(data: &Path, kind: &str) -> Self {
        Self {
            service: format!("com.openpero.native.{kind}"),
            account: format!("{:x}", Sha256::digest(data.to_string_lossy().as_bytes())),
            #[cfg(test)]
            memory: None,
        }
    }
    pub fn load<T: DeserializeOwned>(&self) -> Result<Option<T>, String> {
        #[cfg(test)]
        if let Some(memory) = &self.memory {
            return memory
                .lock()
                .unwrap()
                .as_ref()
                .map(|bytes| {
                    serde_json::from_slice(bytes)
                        .map_err(|_| "Saved credentials are invalid".into())
                })
                .transpose();
        }
        #[cfg(target_os = "macos")]
        {
            match security_framework::passwords::get_generic_password(&self.service, &self.account)
            {
                Ok(bytes) if bytes.len() <= 65536 => serde_json::from_slice(&bytes)
                    .map(Some)
                    .map_err(|_| "Saved credentials are invalid. Reconnect this device.".into()),
                Ok(_) => Err("Saved credentials exceed the supported size".into()),
                Err(error) if error.code() == -25300 => Ok(None),
                Err(_) => {
                    Err("Unlock the system keychain and retry saving or reconnecting.".into())
                }
            }
        }
        #[cfg(not(target_os="macos"))]
        Err("Native secure credential storage is unavailable on this platform; use Electron Desktop.".into())
    }
    pub fn save(&self, value: &impl Serialize) -> Result<(), String> {
        let bytes = serde_json::to_vec(value).map_err(|_| "Could not encode device credentials")?;
        #[cfg(test)]
        if let Some(memory) = &self.memory {
            *memory.lock().unwrap() = Some(bytes);
            return Ok(());
        }
        #[cfg(target_os = "macos")]
        return security_framework::passwords::set_generic_password(
            &self.service,
            &self.account,
            &bytes,
        )
        .map_err(|_| "Could not save credentials. Unlock the system keychain and retry.".into());
        #[cfg(not(target_os="macos"))]
        Err("Native secure credential storage is unavailable on this platform; use Electron Desktop.".into())
    }
    pub fn clear(&self) -> Result<(), String> {
        #[cfg(test)]
        if let Some(memory) = &self.memory {
            *memory.lock().unwrap() = None;
            return Ok(());
        }
        #[cfg(target_os = "macos")]
        return match security_framework::passwords::delete_generic_password(
            &self.service,
            &self.account,
        ) {
            Ok(()) => Ok(()),
            Err(error) if error.code() == -25300 => Ok(()),
            Err(_) => {
                Err("Could not remove saved credentials. Unlock the keychain and retry.".into())
            }
        };
        #[cfg(not(target_os = "macos"))]
        Err("Native secure credential storage is unavailable".into())
    }
    #[cfg(test)]
    pub fn memory() -> Self {
        Self {
            service: String::new(),
            account: String::new(),
            memory: Some(Default::default()),
        }
    }
}
