---
kind: verification_report
date: 2026-10-29
---

# Local ROADMAP acceptance

Scope: [local ROADMAP tracker](../issues/2026-10-29-local-roadmap.md) and
[ADR 0011](../adr/0011-local-roadmap.md). Development worktree; no release or
Cloud deployment is claimed.

## Passed

- Rust core tests: GFM ordered/unordered checklist entries, headings, nesting,
  continuation text, ordinary acceptance lists, checked items, quoted/fenced
  examples, invalid/oversized input and the actual repository ROADMAP.
- `npm run check`: TypeScript, bilingual catalogs, theme and generated contracts.
- `npm test`: 75 Rust unit/integration tests passed. An initial concurrent run
  hit `Invalid Cloud response` in the existing Worker Unicode fixture; that
  case passed alone and the complete rerun passed without a code change.
- Host, Wasm, Web and Electron development builds; native compilation.
- `npm run test:contracts`: real Host JSONL matches the generated protocol,
  including the new local snapshot event.
- Native command routing: local read/session commands are delivered while a
  Cloud command remains pending on its separate controller.
- `npm run test:local-roadmap`: actual Electron with real Host/ACP fixture and
  loopback Cloud requests held open. The loading overlay disappears, the real
  Pero ROADMAP loads, another workspace replaces it, and the visible local
  execution action writes `CONTRIBUTOR.md` in that temporary workspace. All
  Cloud requests are GETs. ROADMAP content remains unchanged. Reopening the
  goal opens the same conversation. Search, child selection, checked-source
  detail, Cloud switch, missing/invalid file recovery, docs fallback and
  light/dark/narrow views pass, with no renderer errors.
- `npm run test:sidebar`: existing Cloud task selection/back navigation, queued
  device setup, project scope and preference persistence passed with the explicit
  Cloud source switch. One screenshot run hit transient `ENOSPC`; the rerun
  completed after checking available disk space, without deleting user files.

Screenshots and results: `test-results/local-roadmap/`. The model provider is a
fixture; the local file effect is real. This proves L0/L1 development behavior,
not a signed package, production provider task, Cloud claim or accepted result.
