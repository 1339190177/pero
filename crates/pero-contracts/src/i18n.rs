use std::{collections::BTreeMap, sync::OnceLock};

pub fn normalize(tag: &str) -> &'static str {
    let tag = tag.replace('_', "-").to_ascii_lowercase();
    if tag == "zh" || tag == "zh-cn" || tag.starts_with("zh-hans") {
        "zh-CN"
    } else {
        "en"
    }
}

fn catalog(locale: &str) -> &'static BTreeMap<String, String> {
    static EN: OnceLock<BTreeMap<String, String>> = OnceLock::new();
    static ZH: OnceLock<BTreeMap<String, String>> = OnceLock::new();
    if normalize(locale) == "zh-CN" {
        ZH.get_or_init(|| {
            serde_json::from_str(include_str!("../../../locales/zh-CN.json"))
                .expect("Chinese catalog")
        })
    } else {
        EN.get_or_init(|| {
            serde_json::from_str(include_str!("../../../locales/en.json")).expect("English catalog")
        })
    }
}

pub fn text(locale: &str, key: &str) -> &'static str {
    catalog(locale)
        .get(key)
        .or_else(|| catalog("en").get(key))
        .map(String::as_str)
        .unwrap_or("[missing translation]")
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn locales_and_catalogs_are_consistent() {
        assert_eq!(normalize("zh_Hans_CN"), "zh-CN");
        assert_eq!(normalize("en-US"), "en");
        assert_eq!(normalize("fr"), "en");
        assert_eq!(text("fr", "nav.tasks"), "Tasks");
        assert_eq!(
            catalog("en").keys().collect::<Vec<_>>(),
            catalog("zh-CN").keys().collect::<Vec<_>>()
        );
        for key in catalog("en").keys() {
            assert!(!text("zh-CN", key).is_empty());
            assert_eq!(
                text("en", key).contains("{count}"),
                text("zh-CN", key).contains("{count}")
            );
        }
    }
}
