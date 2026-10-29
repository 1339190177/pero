---
kind: feature
status: in_progress
github_issue: 14
github_state: open
github_url: https://github.com/phodal/pero/issues/14
---

# Project-first sidebar and task workspace

## What and why

The sidebar currently separates Cloud tasks and infrastructure from local
sessions. A second session column takes space from the conversation. Routine
Host readiness and task totals repeat status without offering an action.

The approved design uses one sidebar, recent work grouped by project, and a
project workspace whose first tab is Tasks. Projects belongs directly below
New chat. Insights replaces the proposed Pulse name. Global preferences and
connections belong in a footer settings popover.

## Scope

- New chat, Projects, project-grouped recent sessions/tasks, Insights, Apps,
  and a fixed footer settings menu. Remove Cloud/Local groups and routine
  running/attention/Host-ready summaries.
- Project workspaces open Tasks first; preserve task creation, filtering,
  dispatch, progress, cancellation, source navigation and pagination.
- Project sources and execution resources use the existing owners. Agent
  configuration is named separately from execution environments. Do not
  invent environment provisioning, Playbook or a Desktop task marketplace.
- Global connections and execution resources open from the settings menu;
  language and appearance have second-level choices. Preferences use the
  Host settings store, including light/dark/system appearance.
- Insights shows actual actionable session/task state and links back to the
  owning record. It does not manufacture approvals or unsupported reminders.
- Preserve unrelated in-flight composer/native changes in the working tree.

## Acceptance

- One sidebar; Projects immediately below New chat; Insights naming is
  consistent in English, Chinese and the roadmap.
- Project selection opens its task view and never shows another project's
  tasks. Local history retains its original workspace grouping after replay.
- Sidebar collapse, recent work selection, new chat and task detail work.
- Settings menus work with pointer and keyboard, dismiss on Escape/outside
  click, and offer explicit language/appearance choices.
- Appearance and language survive restart; save failures have visible feedback.
- Light/dark, narrow windows, empty/error states and actual Desktop interactions
  are verified with screenshots and an isolated fixture profile.

## Verification

Pending implementation.


## Duplicate local/shared Project groups (2026-10-29)

Observed: the sidebar showed two `pero` groups. The first represented the local
checkout and its conversations; the second represented shared Project `pero`.
The catalog merged only saved workspace bindings, so matching display names did
not establish identity. The actual checkout's origin is
`git@github.com:phodal/pero.git`, while the shared source declares owner `phodal`
and repository `pero`.

The Host now reports the opened checkout's actual origin as normalized repository
identity in its workspace event. The catalog reconciles that full identity with
an unambiguous shared repository mapping. Explicit saved bindings take priority;
a different owner/host or a repository serving multiple shared Projects remains
separate. Known workspace aliases retain their conversations in the reconciled
group, and selection follows the retained identity when metadata arrives.

Verification: six contract tests passed; three Project domain tests passed,
covering SSH/HTTPS normalization, same-name repositories under other owners,
ambiguous mappings and explicit binding precedence. A Host shell test initialized
a temporary Git checkout, read its actual configured origin and verified that a
non-repository directory supplies no repository identity. Native check, Host/Wasm/Web/Desktop development builds passed. The actual
Electron sidebar regression passed with no saved Project bindings: one `pero`
group contains local history and shared tasks. A second temporary checkout named
`pero` with origin `another-owner/pero` remained separate after the Host observed
its metadata. Existing task navigation, settings, restart, light/dark/system and
narrow-window checks also passed. Screenshots:
`test-results/sidebar/01-project-tasks.png` (merged group) and
`test-results/sidebar/13-distinct-repository-homonym.png` (distinct repository).
These are isolated development/fixture checks, not packaged release acceptance.
