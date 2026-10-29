//! One pending GitHub sign-in for this native process.
//! The browser receives a challenge; the callback carries no bearer token.
use sha2::{Digest, Sha256};
use std::time::{Duration, SystemTime};

pub(crate) const DEFAULT_ORIGIN: &str = "https://app.openpero.com";
const SIGN_IN_TTL: Duration = Duration::from_secs(10 * 60);

pub(crate) struct Attempt {
    pub base: String,
    pub state: String,
    verifier: String,
    expires: SystemTime,
}

pub(crate) fn service_origin(value: &str) -> Result<String, &'static str> {
    let trimmed = value.trim();
    let value = if trimmed.is_empty() {
        DEFAULT_ORIGIN
    } else {
        trimmed
    };
    if value.len() > 2048 {
        return Err("Connection URL is too long");
    }
    let url = reqwest::Url::parse(value).map_err(|_| ORIGIN_ERROR)?;
    let host = url.host_str().unwrap_or("");
    let local = matches!(host, "127.0.0.1" | "localhost" | "::1");
    let allowed = url.scheme() == "https" || (url.scheme() == "http" && local);
    if !allowed
        || !url.username().is_empty()
        || url.password().is_some()
        || url.query().is_some()
        || url.fragment().is_some()
        || url.path() != "/"
    {
        return Err(ORIGIN_ERROR);
    }
    Ok(url.origin().ascii_serialization())
}

pub(crate) fn login_url(base: &str, state: &str, verifier: &str) -> String {
    format!(
        "{base}/connect/desktop?state={state}&code_challenge={}",
        challenge(verifier)
    )
}

impl Attempt {
    pub(crate) fn begin(base: String) -> Result<Self, &'static str> {
        let state = random_proof()?;
        let verifier = random_proof()?;
        Ok(Self {
            base,
            state,
            verifier,
            expires: SystemTime::now() + SIGN_IN_TTL,
        })
    }

    pub(crate) fn url(&self) -> String {
        login_url(&self.base, &self.state, &self.verifier)
    }

    /// Returns the authorization code when the link belongs to this attempt.
    pub(crate) fn accept(&self, value: &str, now: SystemTime) -> Result<String, &'static str> {
        let callback = parse_callback(value)?;
        if now >= self.expires || callback.state != self.state {
            return Err(CALLBACK_ERROR);
        }
        Ok(callback.code)
    }

    pub(crate) fn verifier(&self) -> &str {
        &self.verifier
    }
}

struct Callback {
    state: String,
    code: String,
}

fn parse_callback(value: &str) -> Result<Callback, &'static str> {
    if value.len() > 1024 {
        return Err(CALLBACK_ERROR);
    }
    let url = reqwest::Url::parse(value).map_err(|_| CALLBACK_ERROR)?;
    let mut state = None;
    let mut code = None;
    let mut count = 0;
    for (key, item) in url.query_pairs() {
        count += 1;
        match key.as_ref() {
            "state" if state.is_none() => state = Some(item.into_owned()),
            "code" if code.is_none() => code = Some(item.into_owned()),
            _ => return Err(CALLBACK_ERROR),
        }
    }
    let (Some(state), Some(code)) = (state, code) else {
        return Err(CALLBACK_ERROR);
    };
    if url.scheme() != "pero"
        || url.host_str() != Some("connect")
        || url.path() != "/callback"
        || !url.username().is_empty()
        || url.password().is_some()
        || url.port().is_some()
        || url.fragment().is_some()
        || count != 2
        || !is_proof(&state)
        || !code_ok(&code)
    {
        return Err(CALLBACK_ERROR);
    }
    Ok(Callback { state, code })
}

fn challenge(verifier: &str) -> String {
    base64url(&Sha256::digest(verifier.as_bytes()))
}

fn random_proof() -> Result<String, &'static str> {
    let mut bytes = [0u8; 32];
    getrandom::getrandom(&mut bytes).map_err(|_| "Could not start GitHub sign-in.")?;
    let value = base64url(&bytes);
    if !is_proof(&value) {
        return Err("Could not start GitHub sign-in.");
    }
    Ok(value)
}

fn is_proof(value: &str) -> bool {
    value.len() == 43
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || byte == b'-' || byte == b'_')
}

fn code_ok(value: &str) -> bool {
    value.len() == 64
        && value
            .bytes()
            .all(|byte| byte.is_ascii_hexdigit() && !byte.is_ascii_uppercase())
}

fn base64url(bytes: &[u8]) -> String {
    const TABLE: &[u8; 64] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
    let mut out = String::new();
    let mut index = 0;
    while index + 3 <= bytes.len() {
        let chunk = ((bytes[index] as u32) << 16)
            | ((bytes[index + 1] as u32) << 8)
            | bytes[index + 2] as u32;
        out.push(TABLE[((chunk >> 18) & 63) as usize] as char);
        out.push(TABLE[((chunk >> 12) & 63) as usize] as char);
        out.push(TABLE[((chunk >> 6) & 63) as usize] as char);
        out.push(TABLE[(chunk & 63) as usize] as char);
        index += 3;
    }
    if index < bytes.len() {
        let mut chunk = (bytes[index] as u32) << 16;
        out.push(TABLE[((chunk >> 18) & 63) as usize] as char);
        if index + 1 < bytes.len() {
            chunk |= (bytes[index + 1] as u32) << 8;
            out.push(TABLE[((chunk >> 12) & 63) as usize] as char);
            out.push(TABLE[((chunk >> 6) & 63) as usize] as char);
        } else {
            out.push(TABLE[((chunk >> 12) & 63) as usize] as char);
        }
    }
    out
}

pub(crate) const ORIGIN_ERROR: &str =
    "Use an HTTPS service origin (loopback HTTP is allowed for development)";
pub(crate) const CALLBACK_ERROR: &str =
    "This sign-in link is expired or belongs to another Desktop. Start GitHub sign-in again.";

/// A GitHub or Roadmap source opens only at its own canonical URL.
pub(crate) fn canonical_source_url(source: &serde_json::Value) -> Result<String, &'static str> {
    let repository = source["repository"].as_str().unwrap_or("");
    if !repository_ok(repository) {
        return Err("Invalid task source");
    }
    match source["kind"].as_str() {
        Some("github") => github_source(source, repository),
        Some("roadmap") => roadmap_source(source, repository),
        _ => Err("Unsupported source kind"),
    }
}

fn repository_ok(repository: &str) -> bool {
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

fn github_source(source: &serde_json::Value, repository: &str) -> Result<String, &'static str> {
    let Some(number) = source["issue_number"].as_u64() else {
        return Err("Invalid GitHub issue source");
    };
    if number < 1 || number > i64::MAX as u64 {
        return Err("Invalid GitHub issue source");
    }
    let expected = format!("https://github.com/{repository}/issues/{number}");
    if source["url"].as_str() != Some(expected.as_str()) {
        return Err("Invalid canonical source URL");
    }
    Ok(expected)
}

fn roadmap_source(source: &serde_json::Value, repository: &str) -> Result<String, &'static str> {
    let sha = source["commit_sha"].as_str().unwrap_or("");
    let path = source["path"].as_str().unwrap_or("");
    if !commit_ok(sha) || !path_ok(path) {
        return Err("Invalid Roadmap source");
    }
    let line = match source.get("line") {
        None | Some(serde_json::Value::Null) => None,
        Some(value) => {
            let Some(line) = value.as_u64() else {
                return Err("Invalid Roadmap line");
            };
            if line < 1 || line > i64::MAX as u64 {
                return Err("Invalid Roadmap line");
            }
            Some(line)
        }
    };
    let parts: Vec<&str> = path.split('/').collect();
    let expected: Vec<String> = repository
        .split('/')
        .map(str::to_string)
        .chain(["blob".into(), sha.into()])
        .chain(parts.iter().map(|part| (*part).to_string()))
        .collect();
    let url = source["url"].as_str().unwrap_or("");
    let parsed = reqwest::Url::parse(url).map_err(|_| "Invalid canonical source URL")?;
    let actual: Vec<String> = parsed
        .path_segments()
        .ok_or("Invalid canonical source URL")?
        .map(decode_segment)
        .collect::<Result<Vec<_>, _>>()
        .map_err(|_| "Invalid canonical source URL")?;
    let hash = line.map(|line| format!("#L{line}")).unwrap_or_default();
    if parsed.scheme() != "https"
        || parsed.host_str() != Some("github.com")
        || !parsed.username().is_empty()
        || parsed.password().is_some()
        || parsed.query().is_some()
        || parsed.fragment().unwrap_or("") != hash.trim_start_matches('#')
        || parsed.as_str() != url
        || actual != expected
    {
        return Err("Invalid canonical source URL");
    }
    Ok(url.to_string())
}

fn commit_ok(sha: &str) -> bool {
    matches!(sha.len(), 40 | 64) && sha.bytes().all(|byte| byte.is_ascii_hexdigit())
}

fn path_ok(path: &str) -> bool {
    !path.is_empty()
        && path.len() <= 1024
        && !path
            .bytes()
            .any(|byte| byte == b'\\' || byte < 0x20 || byte == 0x7f)
        && path
            .split('/')
            .all(|part| !part.is_empty() && part != "." && part != "..")
}

fn decode_segment(input: &str) -> Result<String, ()> {
    let bytes = input.as_bytes();
    let mut out = Vec::new();
    let mut index = 0;
    while index < bytes.len() {
        if bytes[index] == b'%' {
            if index + 2 >= bytes.len() {
                return Err(());
            }
            let high = hex(bytes[index + 1])?;
            let low = hex(bytes[index + 2])?;
            out.push((high << 4) | low);
            index += 3;
        } else {
            out.push(bytes[index]);
            index += 1;
        }
    }
    String::from_utf8(out).map_err(|_| ())
}

fn hex(byte: u8) -> Result<u8, ()> {
    match byte {
        b'0'..=b'9' => Ok(byte - b'0'),
        b'a'..=b'f' => Ok(byte - b'a' + 10),
        b'A'..=b'F' => Ok(byte - b'A' + 10),
        _ => Err(()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn accepts_the_public_origin_and_loopback() {
        assert_eq!(service_origin("").unwrap(), DEFAULT_ORIGIN);
        assert_eq!(
            service_origin(" https://app.openpero.com/ ").unwrap(),
            DEFAULT_ORIGIN
        );
        assert_eq!(
            service_origin("http://127.0.0.1:8787").unwrap(),
            "http://127.0.0.1:8787"
        );
        assert!(service_origin("http://example.com").is_err());
        assert!(service_origin("https://user:secret@app.openpero.com").is_err());
        assert!(service_origin("https://app.openpero.com/api").is_err());
        assert!(service_origin("https://app.openpero.com/?next=1").is_err());
    }

    #[test]
    fn login_url_carries_the_s256_challenge() {
        let verifier = "a".repeat(43);
        let url = login_url(DEFAULT_ORIGIN, &"s".repeat(43), &verifier);
        let parsed = reqwest::Url::parse(&url).unwrap();
        assert_eq!(parsed.path(), "/connect/desktop");
        assert_eq!(parsed.query_pairs().count(), 2);
        let challenge = parsed
            .query_pairs()
            .find(|(key, _)| key == "code_challenge")
            .unwrap()
            .1;
        assert_eq!(challenge.len(), 43);
        assert_eq!(challenge, super::challenge(&verifier));
    }

    #[test]
    fn callback_must_match_this_attempt() {
        let attempt = Attempt {
            base: DEFAULT_ORIGIN.into(),
            state: "s".repeat(43),
            verifier: "a".repeat(43),
            expires: SystemTime::UNIX_EPOCH + Duration::from_secs(100),
        };
        let code = "ab".repeat(32);
        let link = format!(
            "pero://connect/callback?code={code}&state={}",
            attempt.state
        );
        assert_eq!(
            attempt
                .accept(&link, SystemTime::UNIX_EPOCH + Duration::from_secs(50))
                .unwrap(),
            code
        );
        assert!(
            attempt
                .accept(&link, SystemTime::UNIX_EPOCH + Duration::from_secs(100))
                .is_err()
        );
        let other = format!(
            "pero://connect/callback?code={code}&state={}",
            "t".repeat(43)
        );
        assert!(attempt.accept(&other, SystemTime::UNIX_EPOCH).is_err());
        let extra = format!("{link}&next=1");
        assert!(attempt.accept(&extra, SystemTime::UNIX_EPOCH).is_err());
        assert!(
            attempt
                .accept("https://app.openpero.com/callback", SystemTime::UNIX_EPOCH)
                .is_err()
        );
    }

    #[test]
    fn source_url_stays_on_its_canonical_github_page() {
        let issue = json!({"kind":"github","repository":"phodal/pero","issue_number":9,"url":"https://github.com/phodal/pero/issues/9"});
        assert_eq!(
            canonical_source_url(&issue).unwrap(),
            "https://github.com/phodal/pero/issues/9"
        );
        let swapped = json!({"kind":"github","repository":"phodal/pero","issue_number":9,"url":"https://github.com/evil/pero/issues/9"});
        assert!(canonical_source_url(&swapped).is_err());
        let sha = "a".repeat(40);
        let page = format!("https://github.com/phodal/pero/blob/{sha}/docs/plan.md#L4");
        let roadmap = json!({"kind":"roadmap","repository":"phodal/pero","commit_sha":sha,"path":"docs/plan.md","line":4,"url":page});
        assert_eq!(canonical_source_url(&roadmap).unwrap(), page);
    }
}
