# Pero


## UI

Optimize for task completion, not visual minimalism. Every screen should make it obvious what matters, what the user can do, what is happening, and what needs attention.

- **Task-first**: Design around user goals, not internal modules. Every screen should clearly communicate its primary content, actions, and current state.
- **Visual minimalism**: Use black, white, and gray as primary colors. Establish hierarchy through spacing, typography, and subtle borders. Avoid excessive shadows, nested cards, and decoration.
- **Concise copy**: Remove redundant explanations, repeated statuses, and generic AI-style filler. Don't move unnecessary text into tooltips. Reveal secondary details on demand; keep critical warnings visible.
- **Information density**: Avoid excessive whitespace and card-heavy layouts. Prefer tables, lists, split views, and progressive disclosure for complex workbenches.
- **Complete states**: Cover Loading, Empty, Running, Success, Error, and Disabled. Empty states should suggest the next action; errors should provide recovery paths; long-running tasks should expose progress and results.
- **Clear interactions**: Provide feedback for every action. Confirm or allow undo for destructive operations. Avoid nonfunctional controls, duplicate actions, and unnecessary dialogs.
- **Accessibility**: Support keyboard navigation, visible focus states, sufficient contrast, and semantic labels. Never rely solely on color or hover to convey meaning.
- **Responsive & themed**: Use theme tokens consistently. Verify light/dark modes, narrow windows, overflow, and varying content lengths.
- **Real verification**: Inspect the running UI, key interactions, error states, and browser behavior. Code review alone is insufficient.
- **Engineering constraints**: Persist Desktop preferences through harness runtime settings. Follow `ui-workbench.sidebarBrowser` registration and Go/frontend namespaces. Prefer Point-owned slots and wrappers over upstream patches.

**Principle: Optimize for task completion, not visual minimalism.**

## Git Discipline

### Baby-Step Commits (Enforced)

- One commit = one concern (feature, fix, or refactor) with Conventional Commits format.
- No kitchen-sink commits; split mixed concerns.
- Target budget: under 10 files and under 1000 changed lines per commit.
- Include related GitHub issue ID when applicable.

### Co-Author Format

- If closing an issue in commit text, verify against `main` first: `gh issue view <issue-id>`.
- Always add co-author information.
- Only ONE co-author line is allowed. If multiple agents contributed, aggregate into ONE entry

Format example: Co-authored-by: <AgentName> (<You-Model>) <Email>
Valid examples (choose EXACTLY ONE):

Co-authored-by: Kiro AI (Claude Opus 4.6) <kiro@kiro.dev>
Co-authored-by: GitHub Copilot Agent (GPT 5.4) <198982749+copilot@users.noreply.github.com>
Co-authored-by: QoderAI (Qwen 3.5 Max) <qoder_ai@qoder.com>
Co-authored-by: gemini-cli (...) <218195315+gemini-cli@users.noreply.github.com>
Co-authored-by: Codex (GPT 5.5) <codex@openai.com>

## Pull Request

- For UI-affecting changes, include browser screenshots or recordings in PR body (prefer `agent-browser` captures).
- Attach e2e screenshots/recordings when available.

## Issue Feedback Loop

- Before creating a new issue, search `docs/issues/` for existing incident context.
- For non-trivial failures, create/update `docs/issues/YYYY-MM-DD-short-description.md` first (focus on WHAT/WHY), then escalate to GitHub.
- Use one canonical active local tracker per problem. If you need supporting material, record it as a non-issue note via `kind: analysis`, `kind: progress_note`, or `kind: verification_report` instead of opening another active tracker for the same problem.
- Use `kind: github_mirror` only for GitHub-synced mirror files. Those mirrors are reference material, not canonical active local trackers.
- If a local record tracks a GitHub issue, populate `github_issue`, `github_state`, and `github_url` so issue review can detect status drift automatically.
- When resolved, update the local issue record and close the GitHub issue.
- Run issue hygiene/garbage collection at least once every 7 days. Track the last sweep time in `docs/issues/issue-gc-state.yaml` (`last_reviewed_at`).
- If `last_reviewed_at` is 7+ days old when an agent reads this contract, the agent should invoke `AskUserQuestion` first: whether to run issue sync/cleanup now.
- After finishing an issue GC pass, update `docs/issues/issue-gc-state.yaml` with the new `last_reviewed_at`.


## Repository Map

- `docs/issues/`: Incident and repro records. Capture WHAT happened and WHY it mattered.
- `docs/coding-style.md`: Canonical coding style guidance for Rust, TypeScript/frontend, naming, and testing preferences.
- `docs/REFACTOR.md`: Long-file refactor playbook.
- `docs/references/`: Distilled external references for frequent dependencies.
- `docs/release-guide.md`: Full release guide for CLI/Desktop/distribution.
- `docs/RELEASE_CHECKLIST.md`: Quick release checklist.

## Reading Order

When starting work on this repository, read in this order:

1. `docs/ARCHITECTURE.md` — runtime topology and boundaries.
2. `docs/adr/README.md` — decision index, then relevant ADRs.
3. `docs/fitness/README.md` — quality gates and verification flow.
4. Task-specific files in `docs/design-docs/` or `docs/exec-plans/`.

## Design

涉及色彩的设计与实现，遵循 [DESIGN.md](DESIGN.md)。
