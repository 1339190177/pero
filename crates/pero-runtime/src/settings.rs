//! Runtime-owned preferences. Desktop forwards changes rather than owning a second store.
use pero_contracts::RemoteConnectionSettings;
use std::{
    io::{self, Write},
    path::Path,
};

pub fn load_locale(path: &Path) -> Option<String> {
    let value: serde_json::Value = serde_json::from_slice(&std::fs::read(path).ok()?).ok()?;
    value
        .get("locale")?
        .as_str()
        .map(|locale| pero_contracts::i18n::normalize(locale).into())
}

fn update(path: &Path, edit: impl FnOnce(&mut serde_json::Value)) -> io::Result<()> {
    let mut value = match std::fs::read(path) {
        Ok(bytes) => serde_json::from_slice::<serde_json::Value>(&bytes)?,
        Err(error) if error.kind() == io::ErrorKind::NotFound => serde_json::json!({}),
        Err(error) => return Err(error),
    };
    if !value.is_object() {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "Settings must be an object",
        ));
    }
    edit(&mut value);
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let pending = path.with_extension(format!(
        "{}.{}.pending",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos()
    ));
    let result = (|| {
        let mut options = std::fs::OpenOptions::new();
        options.write(true).create_new(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;
            options.mode(0o600);
        }
        let mut file = options.open(&pending)?;
        file.write_all(&serde_json::to_vec_pretty(&value)?)?;
        file.sync_all()?;
        std::fs::rename(&pending, path)
    })();
    if result.is_err() {
        let _ = std::fs::remove_file(&pending);
    }
    result
}

pub fn save_locale(path: &Path, locale: &str) -> io::Result<()> {
    update(path, |value| {
        value["locale"] = pero_contracts::i18n::normalize(locale).into()
    })
}

pub fn save_remote_connection(
    path: &Path,
    connection: Option<&RemoteConnectionSettings>,
) -> io::Result<()> {
    if connection.is_some_and(|connection| !connection.valid()) {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "Invalid remote preference",
        ));
    }
    let remote = serde_json::to_value(connection)?;
    update(path, |value| value["remote_connection"] = remote)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn preferences_survive_restart_and_preserve_other_settings() {
        let path =
            std::env::temp_dir().join(format!("pero-i18n-settings-{}.json", std::process::id()));
        std::fs::write(&path, r#"{"other":{"enabled":true}}"#).unwrap();
        save_locale(&path, "en").unwrap();
        assert_eq!(load_locale(&path).as_deref(), Some("en"));
        save_locale(&path, "zh-CN").unwrap();
        assert_eq!(load_locale(&path).as_deref(), Some("zh-CN"));
        let value: serde_json::Value =
            serde_json::from_slice(&std::fs::read(&path).unwrap()).unwrap();
        assert_eq!(value["other"]["enabled"], true);
        let connection = RemoteConnectionSettings {
            url: "https://app.openpero.com".into(),
            mode: pero_contracts::RemoteConnectionMode::Client,
            encrypted_token: "ZW5jcnlwdGVk".into(),
        };
        save_remote_connection(&path, Some(&connection)).unwrap();
        save_locale(&path, "en").unwrap();
        let value: serde_json::Value =
            serde_json::from_slice(&std::fs::read(&path).unwrap()).unwrap();
        assert_eq!(value["remote_connection"]["mode"], "client");
        assert_eq!(
            value["remote_connection"]["encrypted_token"],
            "ZW5jcnlwdGVk"
        );
        save_remote_connection(&path, None).unwrap();
        let value: serde_json::Value =
            serde_json::from_slice(&std::fs::read(&path).unwrap()).unwrap();
        assert!(value["remote_connection"].is_null());
        assert_eq!(load_locale(&path).as_deref(), Some("en"));
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            assert_eq!(
                std::fs::metadata(&path).unwrap().permissions().mode() & 0o777,
                0o600
            );
        }
        std::fs::write(&path, "invalid").unwrap();
        assert!(save_locale(&path, "en").is_err());
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "invalid");
        std::fs::remove_file(path).unwrap();
    }
}
