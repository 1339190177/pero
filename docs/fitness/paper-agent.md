---
kind: verification_report
date: 2026-10-29
status: verified_local
---

# Paper Agent Python port and new UI

This local development slice ports Point Paper Agent's domain behavior into a
Python standard-library backend and rewrites its UI with Pero SDK components.
Source reference: Point `dcd549a9cc8807eda5ce682eef4621afbd9a02d9`.
Initial tested Pero base: `8cc525f81e03c4e97aca365417fadb0d49e0e429` plus the uncommitted
Paper Agent/Host bridge changes. Concurrent shared-chat work is present in the
same worktree and development builds; this record does not attribute or accept
all of those changes as part of this App. No release package was built here.

Environment: macOS arm64, Python 3.14.7, Node 26.8.1, real Electron and Rust Host.
App data and test profiles were disposable and cleaned up. The App uses the
existing shared default Python policy, with optional App-specific venv overrides.

## Final commit base

During commit preparation the independent shared-chat work was committed through
`07cacf2d1bcc8d9fe799150bca43458333d96924`. The Paper-only snapshot was rebased onto
that committed baseline, preserving its App chat scope and usage contracts while
excluding the still-uncommitted workspace-tab and icon changes. The scoped
snapshot again passed the full build, check (534 messages), 40 runtime + one Host
+ nine contract tests, the App Agent stdio check and all 10 Paper Electron checks.
No unexpected browser errors occurred. Its final Host SHA-256 is
`594590d35a5c0abe527ac060e35856231029840245f47f61773b67871ecbe4e0`, UI JS is
`b521a9a58506574b9e3d5c11866d754368bbb1acf97208593c90c2198c0adf6e`, and UI CSS
remains `31f67f0d9ee43dd8ccb392fb6622f216a6d97bda1b167ec55496b28300299e73`.

## Earlier independent commit validation

Before committing, a clean snapshot was exported from the base above and only
the Paper Agent changes were applied. App Chat scopes, shared GPUI extraction,
usage reporting and their build changes were excluded. The App UI detects the
optional chat SDK export and hides that shortcut when unavailable; research
sessions continue to use the independent Host Agent interface.

On this isolated source snapshot, `npm run build`, `npm run check` (523 messages),
App typecheck, all 10 Python tests, 40 runtime + one Host + eight contract tests,
the App Agent stdio check, all 10 Paper Agent Electron checks and all nine
Virtual Cell Electron regression checks passed. Python
was explicitly selected as `/opt/homebrew/bin/python3` (3.14.7); Python 3.10+
is required. Dependency and font caches were cloned into the temporary snapshot
to keep this verification offline. No other worktree sources or assets were reset.

The independent snapshot's loaded UI files were checked against its production
files, with these artifact identities:

| Artifact | SHA-256 |
| --- | --- |
| Host | `6a1166c28ab69352a02c5ca8ded3bf6479082abc0c16c9fd8d3c09e3c65bbb78` |
| UI JS | `d9cc794f780d67ddb7de24a40ece3dfa98029425fbc57c10bc4ac6949e5c9f4c` |
| UI CSS | `31f67f0d9ee43dd8ccb392fb6622f216a6d97bda1b167ec55496b28300299e73` |

## Earlier combined-worktree verification

| Check | Result |
| --- | --- |
| `npm run build` | App assets, GPUI Wasm, Host, Worker, Web and Desktop passed |
| `npm run check` | TypeScript, theme, 534 bilingual messages and generated contracts passed |
| `npm --prefix apps/paper-agent run typecheck` | Passed |
| Python `unittest discover -s apps/paper-agent/test` | 10 tests passed: sources, frozen inputs, exact evidence, report/experiment rules, export and authenticated loopback HTTP |
| `node scripts/cargo.mjs test --offline -p pero-runtime -p pero-host -p pero-contracts` | 40 runtime, one Host and nine contract tests passed |
| `node tests/contracts/app-agent.mjs` | Real Host App ownership, cross-App read/cancel refusal, same-ID reconciliation, changed-input refusal, cancellation, journal recovery and disabled-App refusal passed |
| `node tests/e2e/paper-agent.mjs` | 10 actual Desktop workflow checks passed; no unexpected browser errors |
| `node tests/e2e/apps.mjs` | Virtual Cell's 10 real Desktop regression checks passed; no browser errors |

The Paper Agent Desktop suite explicitly imports a labelled synthetic reading,
checks that no Agent starts implicitly, and runs the existing ACP lifecycle with
a dedicated deterministic fixture. It proves valid report auto-save, invented
quotation refusal, invalid JSON retention/correction, exact evidence navigation
and focus restoration, native Markdown/JSON download, cancellation and restart
without rerunning research. A separate App without the grant is refused by the
actual Host. The fixture performs no real model call or scientific experiment.

Print-media assertions check source URLs, content digests, reference numbering
and exact quotations. The native print dialog itself was not tested. English,
Chinese, light/dark and a 1000px window were checked. Narrow QA waits for the
actual App surface and iframe to reach 760px before checking overflow and taking
the screenshot. Final screenshots were visually inspected for hierarchy,
clipping, evidence visibility and normal paragraph wrapping.

The two intentional report-validation requests returned HTTP 400; the intentional
grant-denial request returned HTTP 503 with the Host error. These expected console
entries are recorded separately from unexpected errors. The first two incomplete
E2E runs were test selector mistakes (tab count suffix and print-only quote
location), corrected before the final full pass.

## Live source observation

An explicit `PAPER_AGENT_LIVE=1` direct backend-adapter probe retrieved alphaXiv's
full text for arXiv `1706.03762`, **Attention Is All You Need**, with original
publisher metadata: 39,901 characters, digest
`sha256:fbe293f0d64bf94f438e742e2610ebbd8bc90d1278b699d6fdbf96e23590626b`.
This probe used no private research input and did not run an Agent. OpenAlex,
bioRxiv and PubMed adapters were checked with deterministic HTTP fixtures; their
live endpoints and quotas were not accepted by this observation.

## Tested artifacts

The Electron test independently fetched its actually served UI files and checked
their hashes against the frozen production files:

| Artifact | SHA-256 |
| --- | --- |
| `target/debug/pero-host` | `d27d79d381facc83f79b9c2f8b58b3d7649140f6b8041cf86a790de63dc98ff2` |
| `apps/paper-agent/dist/index.js` | `b117da5a1afe3b9175c78f08906a0f3c20b654cd169c9de8ccdf5f9e0f7217ab` |
| `apps/paper-agent/dist/index.css` | `31f67f0d9ee43dd8ccb392fb6622f216a6d97bda1b167ec55496b28300299e73` |

Machine-readable results, screenshots and native downloads live in the ignored
`test-results/paper-agent/` directory: `result.json`, `empty.png`, `source.png`,
`report.png`, `evidence.png`, `cancelled.png`, `dark-zh.png`, `narrow.png`,
`research.md` and `research.json`. Virtual Cell results are in `test-results/apps/`.

## Remaining acceptance

Real model-generated research, code experiments, provider authentication/tool
approval in the Paper Agent workflow, original-PDF/system-browser interaction,
native printing, signed packaging/upgrades, Windows/Linux and scientific
correctness need their own verification. The old Point implementation's records
do not establish these. Report experiments remain Agent-reported and scientific
acceptance remains unverified. No Project Page, independent verification or
automatic knowledge promotion is claimed.

Try the isolated preview with `npm run dev:paper`; see the
[App workflow](../../apps/paper-agent/README.md).
