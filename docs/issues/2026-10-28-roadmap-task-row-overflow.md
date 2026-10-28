---
kind: incident
status: resolved
---

# Long ROADMAP task rows overflow the narrow Desktop list

Public-source connector acceptance imported all 411 checklist items and passed
its data checks, but visual inspection of `test-results/connector/live/narrow-list.png`
showed long titles extending beyond the window. The default Button label centers
a single text element containing title and metadata. That content can grow beyond
the list rather than constraining each line independently. Wide-list screenshots
also hide metadata when the title is long.

The row must preserve a bounded title and visible source/status metadata in both
the 280px split list and the full-width narrow list. Keep the entire accessible
name and existing selection/parent/tag behavior. Verify actual Electron screenshots
after replacing the generic multiline label with explicit constrained text rows.

The Task row now contains separate, width-constrained title and metadata lines,
with ellipsis for overflowing text and the full accessible name retained. The
fixture suite passed parent navigation and tag filtering after the change.
After pushing `0aa1064`, public-source acceptance passed again from `master`:
411 ROADMAP Tasks, two Issues, no execution and no page errors. Actual
`test-results/connector/live/narrow-list.png` and `roadmap-first-page.png` in the
main checkout show left-aligned, bounded titles with source/status/line metadata
visible at both widths. `npm run check` passed.
