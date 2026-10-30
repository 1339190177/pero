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

Settings follow-up found that the unstyled tree's internal virtual list had no
height. The panel now sizes that list explicitly. The first numeric control also
registered the editor focus twice in native accessibility; Pero now composes
Kit Input and step buttons, with `FontSize` owning the bounds. Web's default
dialog confirmation closed Settings on Enter before the input event could save;
Settings disables that confirmation behavior. These failures are exercised by
real keyboard/input and native-window checks in the appearance evidence.

## 2026-10-30 source table follow-up

Adopting Kit's actual DataTable exposed table-specific theme tokens that were
still using Kit defaults: the dark body was black and header labels had low
contrast. The existing appearance installer now maps table surface, header,
footer, hover, selection and row borders from the resolved Pero palette through
`Theme::update`. The source reader does not install a separate widget theme.
Actual light/dark/minimum-width screenshots are part of the content-reader
acceptance. This follow-up does not close the other native appearance gates.

Kit paints a selected-cell overlay after its contents. An opaque accent fill
therefore covered the value. The table selection token now uses translucent
focus ink; the separate focus border keeps selection visible without masking
text. This is specific to Kit's overlay semantics, not a second palette.
