//! Pages the native preview may open, and the manual task a client is not allowed to create.
use serde_json::{Value, json};

pub(crate) fn source_page(repository: &str, url: &str) -> Option<String> {
    github_page(repository, url, "/")?;
    Some(url.to_string())
}

pub(crate) fn pull_page(repository: &str, url: &str) -> Option<String> {
    let path = github_page(repository, url, "/pull/")?;
    let number = path.rsplit_once("/pull/")?.1;
    if number.is_empty()
        || number.starts_with('0')
        || number.contains('/')
        || !number.bytes().all(|byte| byte.is_ascii_digit())
        || reqwest::Url::parse(url).ok()?.fragment().is_some()
    {
        return None;
    }
    Some(url.to_string())
}

fn github_page(repository: &str, url: &str, suffix_prefix: &str) -> Option<String> {
    if !repository_shape(repository) {
        return None;
    }
    let parsed = reqwest::Url::parse(url).ok()?;
    let prefix = format!("/{repository}{suffix_prefix}");
    if parsed.scheme() == "https"
        && parsed.host_str() == Some("github.com")
        && parsed.username().is_empty()
        && parsed.password().is_none()
        && parsed.query().is_none()
        && parsed.path().starts_with(&prefix)
        && parsed.path().len() > prefix.len()
    {
        Some(parsed.path().to_string())
    } else {
        None
    }
}

fn repository_shape(repository: &str) -> bool {
    let Some((owner, name)) = repository.split_once('/') else {
        return false;
    };
    !owner.is_empty()
        && !name.is_empty()
        && name != "."
        && name != ".."
        && !repository.contains("//")
        && owner
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || byte == b'_' || byte == b'-')
        && name
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'_' | b'-' | b'.'))
}

pub(crate) fn task_spec(value: &Value) -> Result<Value, &'static str> {
    let mut spec = json!({
        "title": bounded_field(value, "title", 240)?,
        "prompt": bounded_field(value, "prompt", 64000)?,
        "acceptance": bounded_field(value, "acceptance", 16000)?,
        "project_id": bounded_field(value, "project_id", 128)?,
        "workspace_key": bounded_field(value, "workspace_key", 128)?,
        "agent_id": bounded_field(value, "agent_id", 128)?,
    });
    if value.get("parent_task_id").is_some() {
        let Some(parent) = identifier(value.get("parent_task_id")) else {
            return Err("Invalid task identifier");
        };
        spec["parent_task_id"] = Value::String(parent);
    }
    if value.get("tags").is_some() {
        let Some(tags) = value["tags"].as_array() else {
            return Err("Invalid task tags");
        };
        if tags.len() > 32 {
            return Err("Invalid task tags");
        }
        spec["tags"] = Value::Array(
            tags.iter()
                .map(|tag| bounded_text(tag.as_str(), 128).map(Value::String))
                .collect::<Result<Vec<_>, _>>()?,
        );
    }
    Ok(spec)
}

fn identifier(value: Option<&Value>) -> Option<String> {
    value
        .and_then(Value::as_str)
        .filter(|id| {
            !id.is_empty()
                && id.len() <= 128
                && id
                    .bytes()
                    .all(|byte| byte.is_ascii_alphanumeric() || byte == b'_' || byte == b'-')
        })
        .map(str::to_string)
}

fn bounded_field(value: &Value, key: &str, max: usize) -> Result<String, &'static str> {
    bounded_text(value[key].as_str(), max)
}

fn bounded_text(value: Option<&str>, max: usize) -> Result<String, &'static str> {
    let Some(value) = value else {
        return Err("Invalid remote task field");
    };
    let value = value.trim();
    if value.is_empty()
        || value.chars().count() > max
        || value.chars().any(|char| char == '\0')
        || (max <= 2048 && value.chars().any(|char| char == '\r' || char == '\n'))
    {
        return Err("Invalid remote task field");
    }
    Ok(value.to_string())
}

pub(crate) fn idempotency_key() -> String {
    let mut bytes = [0u8; 16];
    if getrandom::getrandom(&mut bytes).is_err() {
        return String::new();
    }
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}
