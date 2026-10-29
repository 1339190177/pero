---
kind: feature
status: resolved
---

# Load and execute a workspace ROADMAP locally

The project task page only reads Cloud-imported tasks. An existing local checkout
therefore waits for remote source loading and Worker enrollment even though its
ROADMAP and ACP Agent are available on the machine. The reported enrollment
timeout is a separate Cloud path, not a prerequisite for local work.

Read `ROADMAP.md`, falling back to `docs/ROADMAP.md`, through the Rust Host.
Projects with a local directory default to this source; shared tasks remain
available through an explicit Cloud switch. Loading does not execute anything.
An explicit task action starts the existing local ACP conversation with the
observed goal, heading context and source location. Source checkboxes remain
source state and do not assert execution or acceptance. No Cloud registration,
claim, dispatch, or publication is required for this local path.

Verify Markdown nesting, code fences, checked items, missing/invalid files,
workspace switching and an actual Electron/Host/ACP fixture flow while Cloud
is unavailable. Keep existing worktree changes intact.

Implemented and verified locally. Real Electron loaded the repository ROADMAP
while loopback Cloud requests remained pending, and the visible task action
completed an ACP fixture turn with a real file effect in a temporary workspace.
No remote write or source checkbox mutation occurred. Search, completed-source
gating, source switching, recovery and dark/narrow views passed. Native routing
tests prove local Host commands bypass a pending Cloud controller.
See [verification and acceptance limits](../fitness/local-roadmap.md).
