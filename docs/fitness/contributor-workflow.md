---
kind: verification_report
date: 2026-10-28
---

# Desktop contributor workflow

Implementation: [ADR 0009](../adr/0009-local-contributions.md).

## Verified source workflow

The new contributor suite uses a fresh OAuth identity with **no private project
scope or assigned Worker**. Public tasks come from the actual Go intake and
publication model, and PostgreSQL owns the claim and PR evidence.

Both the default ACP fixture and the opt-in authenticated Codex run passed:

1. Browser OAuth consent and Desktop code exchange create the new identity.
2. Public GitHub-backed work appears; an unpublished private task and its goal do
   not enter the Desktop snapshot. Actual canvas controls select and claim work.
3. Native folder selection prepares a separate clone and task branch. The
   existing seed checkout remains clean.
4. The existing ACP session writes `CONTRIBUTOR.md` in the prepared checkout.
   In the real-provider run, Codex used actual read, shell and edit tools and
   performed its own document checks; it was not an exact-reply smoke test.
5. The Host reports the real tracked/untracked diff. An edit after review blocks
   publication until a new review.
6. Git creates a real commit and pushes it to an isolated local bare repository.
   A GitHub CLI fixture supplies fork and draft-PR responses.
7. A deliberate first-registration 503 preserves the known PR. Retrying only
   registration stores the exact URL, branch, head SHA and source revision;
   repeated registration creates no second PR.
8. Chinese, dark theme and a narrow window retain the task and recovery actions.

The browser callback is delivered at Electron's native event boundary in this
suite. It does not establish OS URL dispatch or real keychain persistence. The
macOS fixture uses `--use-mock-keychain` and an isolated profile.

Evidence is generated under `test-results/contribution/` and
`test-results/contribution-real/`. The latter's `03-agent-edited.png` shows the
real Codex tools. `result.json` identifies the provider and GitHub fixture
separately. These ignored artifacts contain local test paths and are not releases.

## Regression checks

- `npm run check`: passed, including 390 bilingual catalog entries and generated
  TypeSpec contract drift checks.
- `npm test`: 49 Rust tests passed, including actual local Git contribution tests
  and six Worker process/lease integration tests.
- `go -C server test -race ./...` with required, isolated PostgreSQL: passed.
- `node tests/contracts/host.mjs`: passed; 26/40 event kinds exercised. Missing
  variants are reported explicitly by the existing contract harness.
- Existing `tests/e2e/workbench.mjs`: passed streaming, follow-up, cancellation,
  permission, 1000-chunk exact rendering, scroll retention, replay and themes.
  Stale sidebar coordinates were updated to the inspected current layout.

## Distribution and remaining acceptance

The final macOS arm64 development package was rebuilt, ad-hoc signed and checked
with deep strict signature verification. Its App was copied outside the checkout
and the contributor end-to-end suite passed from that temporary installation.
The packaging path also mounted its DMG read-only and copied an App for acceptance
before the final GitHub CLI argument correction; the final corrected App was
separately copied and rechecked. The final DMG is 98,992,233 bytes and its SHA-256 is
`8af2531f19a711f8ca18691efab4af09d3f8a86d7a3b9c9a68ab0cba4e5bd079`.
The uncompressed App is 287,358,941 bytes. Both are ignored build artifacts under
`target/package/`. This is the current working-tree candidate, not a public release.

Packaging initially reused an old release contracts artifact without session
settings. Invalidating only that package's release cache restored the build.
The installed GitHub CLI's actual command semantics were also checked against its
matching official source: fork uses the locally resolved upstream with no explicit
repository argument, and PR lookup uses a branch plus author filter. The shared
GitHub CLI fixture and local Git tests pin those arguments.

This source workflow is not a deployed service or public release. Native Agent
CLIs, Git and authenticated GitHub CLI remain user prerequisites; GitHub identity
login does not grant model authentication or repository publication credentials.

No live GitHub fork, push, PR, merge, release or production service update was
performed. PR submission does not claim independent verification, issue closure,
maintainer acceptance or contribution points.
