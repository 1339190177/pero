# Workbench internationalization

`en.json` and `zh-CN.json` are the shared message catalogs for the Rust GPUI
workbench, Electron menus and web loader. Keep semantic message keys stable.

- Rust: `pero_contracts::i18n::{normalize, text}` embeds both catalogs. Views
  translate when rendering; system events carry `message_key` so existing logs
  can change language without rewriting their records.
- TypeScript: `locales/index.ts` provides the same normalization and English
  fallback. Message keys are checked against the English catalog.
- GPUI Kit: `gpui_kit::component::set_locale` is called at startup and on changes,
  keeping component-owned labels aligned with the application.
- Desktop: the first launch follows the system language. The sidebar switch or
  Language menu changes the runtime-owned preference; the Rust Host writes
  `runtime-settings.json` under Electron's user data directory. Saved language
  wins on subsequent launches. Unsupported system languages use English.
- Browser preview: follows browser language; switching lasts for the page's lifetime.

User prompts, responses, tool output, repository names and real task titles are
content, not translation keys. Example tasks are application-owned demo strings.
Named `{count}` placeholders are supported for the current counter labels. Add a
plural-aware formatting implementation before introducing grammatical plural forms.

Run `npm run check` for catalog parity, nonempty values, placeholder consistency
and Rust UI key coverage. `npm run test:i18n` launches real Electron to verify
live switching, document/native-menu language, unchanged Chinese user content,
invalid locale rejection and preferences after restarting the app.

## Local source references

The inspected Zed checkout (`37faef8`) still constructs application menus with
English literals in `crates/zed/src/zed/app_menus.rs`; it did not contain a unified
UI message catalog in the searched application crates. GPUI Kit (`ec89cfc`) has
`rust_i18n::i18n!("locales", fallback = "en")` in
`crates/component/src/lib.rs`, plus `locale()` / `set_locale()` and `locales/ui.yml`.
Pero reuses the Kit locale switch and keeps its cross-process messages in the
shared catalogs above rather than copying Zed's English literals.
