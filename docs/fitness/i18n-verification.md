---
kind: verification_report
date: 2026-10-28
---

# Workbench internationalization

Verified the current development Electron app with both `zh-CN` and `en`:

- GPUI strings, input hints, document language/title and native menus switch live.
- Both the visible sidebar switch and native Language menu save through the Host.
- The saved English preference survives restart even when the system default supplied
  to the test is Chinese. Switching back restores Chinese labels.
- A Chinese message with emoji sent while the UI is English returns unchanged.
- Unsupported locale commands are rejected; unsupported system tags fall back to English.
- Catalog keys, nonempty translations, placeholders and static UI references pass checks.
- Native preference tests preserve unrelated settings and refuse to overwrite invalid JSON.
- English dark and 1000px window layouts were inspected. Long task titles truncate
  before their time labels; the local connection label fits the compact sidebar.
- The same WASM also switches language in a bridge-free preview window without
  changing the Desktop runtime preference.

Commands: `npm run build`, `npm run check`, `npm test`, `npm run test:i18n`.
Screenshots and the result record are generated under `test-results/i18n/`.

This verifies the current development app on macOS; it does not establish packaged
or cross-platform acceptance. Existing Agent conversation changes are preserved;
this task does not claim to validate their execution or model behavior.
