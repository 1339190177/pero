# GitHub ROADMAP tasks and Desktop connector configuration

Status: accepted, 2026-10-28. Extends ADR 0003.

## Decision

The Go GitHub source imports one Task per Issue and one Task per Markdown
checkbox in an explicitly configured ROADMAP. The `auto` path tries
`ROADMAP.md`, then `docs/ROADMAP.md`; an explicit repository-relative Markdown
path is also supported. Existing repositories without this setting keep their
Issue-only behavior.

This repository keeps its checklist in root `ROADMAP.md`. Each item uses
`- [ ] Title. Description.`: the first `. ` separates the short Task title from
its description, while the full item remains the execution goal. Items without
that separator retain their original first-line title.

Headings retain group context. Nested checkbox items bind to a parent Task.
Issue labels and ROADMAP hashtags become tags. A checked item is a closed source,
not an accepted Run. ROADMAP intake creates drafts and never authorizes automatic
execution. The same project-scoped Task/Run and ownership rules apply.

Source identity is separate from source revision. Explicit `<!-- pero:id:... -->`
markers retain identity across wording or location changes. Fallback identity
uses checklist context and title; description-only edits retain identity.
Ambiguous items produce an incomplete-source
result. A complete snapshot can mark removed items missing. A partial fetch or
parse cannot close previously observed work. Claimed or executed goals remain
frozen, with current metadata exposed separately as `latest_source`.

Repository contents are read through the fixed GitHub API at a pinned commit.
Source links identify that commit and line. File/commit changes alone do not
invalidate every unchanged checklist goal. Credentials and automatic execution
policy remain with the Go service.

Desktop adds a GitHub connector connection form for the cloud origin and a
project-scoped access token. Provider credentials remain server-owned; clients
see only authorized repository bindings. Non-secret connection settings and an
OS-encrypted credential are persisted through the existing harness runtime
settings owner. The renderer receives neither stored credentials nor ciphertext.
Disconnect is explicit; a saved configuration never escalates to inherited
operator credentials. Source tabs, context paths and tags make imported work
visible without requiring all paginated Tasks to be loaded first.

## Verification

Use parser fixtures and real PostgreSQL for identity, nesting, tags, checked and
missing items, partial snapshots, deduplication and immutable execution goals.
Actual Electron must configure the connector, restart, display Issues and
ROADMAP Tasks, and preserve project scope and credential boundaries. A separate
opt-in run reads `phodal/pero` from GitHub and compares its imported checklist
with the pinned repository content. Viewing Tasks never dispatches them.

## Public task browser

The public board loads the complete current published task outline, excluding
removed sources. Tree and panorama share this one snapshot, search, phase filter,
category selection and inspected task. The existing paginated administration and
historical task routes retain their behavior. A phase is a browsing category;
source closure and accepted work remain separate.

Roadmap headings define a functional domain → feature → task hierarchy. A
conventional document title `Roadmap` is omitted from browsing, while the original
source context and identity stay intact. No parallel category registry is added.
The default scope excludes accepted and source-closed work. Domain roots open to
collapsed feature folders with matching-task counts; choosing a domain reveals
its tasks. Completed work remains available through the state filter.

The category rail and panorama use the same domains. Search includes feature
paths, scope and acceptance, revealing all ancestors. Category counts follow the
current search and state scope. Clearing filters restores the unfinished scope
and previous branch expansion. Panorama presents each domain's features and
returns to the corresponding tree branch when selected.

Branch expansion and inspection are separate. Arrow keys navigate the full
hierarchy; Enter opens one inline scope, acceptance check and source/Desktop
actions, while Escape closes it. No task detail navigation is required. URL
parameters preserve filters and view; only a currently matching selection adds
a task hash, so a hidden selection cannot erase a search on reload.

Only published tasks and public repository headings enter the outline. Private
publications retain reviewed text. `store.RoadmapGoal` owns short-title parsing;
the public brief omits a redundant matching title prefix and separates an explicit
`验收：` check without changing the stored goal. Without JavaScript the same HTML remains a readable native disclosure list.
No task-inspection navigation, new execution state or client-side claim authority
is introduced by either view.
