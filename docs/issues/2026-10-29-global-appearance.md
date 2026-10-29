---
kind: issue
status: in_progress
---

# Native appearance and global typography

Native initialized the system appearance as light and did not observe platform
changes. Workbench and Chat retained separate palette flags; Chat locale/context
updates reinstalled the global theme and reset the font size. Direct Kit theme
field writes left semantic tokens and the Base projection out of step. Fixed
10–14px declarations bypassed global text settings.

ADR 0015 defines a single appearance installation owner per GPUI application,
Host-owned preference persistence, and scoped typography. This issue tracks the
implementation and runtime checks; evidence belongs in
`docs/fitness/appearance.md`. Other retained-reader and Agent changes in the
working tree have separate owners and acceptance records.

Native acceptance also exposed duplicate accessibility IDs when search displayed
multiple font sections. Each rendered settings section now has a stable parent
ID so its stepper controls keep separate identity. Search must be exercised in
the native debug build as well as in Electron.
