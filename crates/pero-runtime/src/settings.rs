//! Runtime-owned preferences. Desktop forwards changes rather than owning a second store.
use std::{io, path::Path};

pub fn load_locale(path: &Path) -> Option<String> {
    let value: serde_json::Value = serde_json::from_slice(&std::fs::read(path).ok()?).ok()?;
    value
        .get("locale")?
        .as_str()
        .map(|locale| pero_contracts::i18n::normalize(locale).into())
}

pub fn save_locale(path: &Path, locale: &str) -> io::Result<()> {
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
    value["locale"] = pero_contracts::i18n::normalize(locale).into();
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let pending = path.with_extension("pending");
    std::fs::write(&pending, serde_json::to_vec_pretty(&value)?)?;
    std::fs::rename(pending, path)
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
        std::fs::write(&path, "invalid").unwrap();
        assert!(save_locale(&path, "en").is_err());
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "invalid");
        std::fs::remove_file(path).unwrap();
    }
}
