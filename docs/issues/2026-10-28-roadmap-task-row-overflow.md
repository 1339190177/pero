---
kind: incident
status: active
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
