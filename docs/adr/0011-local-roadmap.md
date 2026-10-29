# Local workspace ROADMAP execution

Status: implemented locally, 2026-10-29. Extends ADRs 0005 and 0009.

A local **RoadmapItem** is a checklist goal observed in a workspace. It is not
a Cloud Task, claim or Run. A **LocalRoadmap** carries the workspace, relative
source path, checklist items and any intake error. These contracts are shared
by the Host and both UI shells.

The Rust runtime parses GFM checklists, headings and nested task relationships.
The Host reads `ROADMAP.md`, falling back to `docs/ROADMAP.md` only when the root
file is absent. It publishes a snapshot when the workspace changes and on an
explicit refresh. File/parse errors replace the local snapshot with a recovery
state, rather than silently showing another workspace's goals. Code fences and
quoted examples are excluded. Ordinary acceptance lists stay with their goal;
child checklist goals remain separate. Checked items describe source state.

Projects with a local directory default to the local source. Cloud tasks retain
their existing API, ownership and Worker enrollment flow behind a source switch.
Choosing a local item explicitly starts the existing Host-owned ACP conversation
in that workspace with the observed goal, heading context and source location.
Agent authentication, permission, cancellation, history and the workspace write
lease retain their existing owners. Reopening the same goal in the current UI
session opens its conversation. No new scheduler or execution state machine is
introduced, and loading a checklist never dispatches work.

Cloud refresh and device setup do not gate Electron's loading overlay. Native
Host commands have an independent writer from the blocking Cloud controller.
A delayed initial connection response does not navigate away from local work.

The local path neither claims a corresponding public task nor reports Cloud
acceptance, edits checkboxes, commits or publishes changes. Public contribution
and Worker execution continue to use their explicit existing workflows.

Verification and limits: [local ROADMAP acceptance](../fitness/local-roadmap.md).
