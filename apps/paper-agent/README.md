# Paper Agent

Paper research in Pero: collect sources, read the exact saved text, freeze a
question and selected papers, run the existing Host Agent, then review cited
reports and reported experiments together. The Python domain implementation
ports Point Paper Agent's snapshot, evidence and report rules. The UI is a new
Pero App implementation using the shared SDK.

## Try it

Use Python 3.10 or newer. From the repository root:

```sh
npm run dev:paper
```

This builds and opens an isolated development profile at
`target/apps-preview-paper-agent`. Open **Add papers** to search alphaXiv,
OpenAlex, bioRxiv or PubMed, read a supported ID/URL, or import text/Markdown.
**Load synthetic example** explicitly imports a labelled example without
starting an Agent. Opening the App never starts research.

Existing profiles seed builtins only once. To add this App to an existing
profile, use **Apps → Install from folder**, choose this built directory and
enable it. Domain data lives in Host-managed `app-data/paper-agent` and survives
disable, uninstall and reinstall. The App defaults to the shared Python
environment; see [Python environment settings](../README.md#python-environments)
for a dedicated override when needed. Its backend uses only the standard library.

## Workflow and boundaries

1. Imported papers become immutable snapshots with source identity, content
   kind, retrieval time and text digest. Abstracts, third-party reports and full
   text remain distinct. Only alphaXiv offers the full-text retrieval action;
   original source/PDF links remain available. Public availability does not
   transfer publication rights. Searches send the entered query to the selected
   public services; confidential terms need appropriate permission.
2. Select 1–5 papers and state the research question. **Freeze research input**
   saves the input without execution. Inputs are capped at 80,000 selected
   characters and the Host's 128,000-byte frozen context budget. Changes require
   a new request identity. A selected passage can be added to the question;
   when the SDK provides App chat it can also fill an unsent Host chat draft.
3. **Start research** explicitly starts an available Host Agent in the displayed
   Host workspace. The manifest's `agent` grant is checked by the Host on each
   operation. The Host owns the ACP process, journal, approvals, authentication
   and cancellation. Handle requests for input or approval in the corresponding
   Host conversation; the App offers a chat shortcut when that capability exists.
   Hiding a panel retains it; disabling/removing the App closes its sessions.
4. Reports are checked against the exact input and selected paper IDs before
   saving. Quoted evidence must be an exact contiguous source passage. Invalid
   output is retained for inspection, correction and explicit validation.
   Existing Point report v1 JSON is accepted only when it matches the current
   frozen input; this is not a migration of Point's private data directories.
5. Experiments retain code, commands, criteria and observations. Missing tools,
   data or authorization must be `blocked`/`not-run`. Reported experiment results
   stay `agent-reported`, session links stay `client-reported`, and scientific
   acceptance stays `unverified`. Selecting code verification expresses intent;
   execution permissions and actual limits remain Host-owned.
6. Export Markdown/JSON or print the report, citations, source provenance and
   experiment appendix. Print/PDF uses the system print dialog. Reopening a
   saved report requires no Agent execution. Project Pages, independent research
   verification, domain Agent tools and remote App execution are not connected.

The App has no model credentials or execution loop. It communicates through
its own authenticated backend proxy and a narrow Host Agent endpoint. Unknown
start outcomes reconcile using the same frozen request ID; they do not launch
another session. The Host journal retains App ownership for restart recovery.
Python dependency/process separation does not restrict filesystem or network
access beyond the current user's permissions.

## Checks

```sh
npm --prefix apps/paper-agent run test
npm --prefix apps/paper-agent run typecheck
npm --prefix apps/paper-agent run build
node tests/contracts/app-agent.mjs
npm run test:paper
```

The backend tests are hermetic; their HTTP check needs loopback access. The real
Electron suite uses synthetic sources and an ACP fixture, without real model
requests or Internet access. Live source retrieval and scientific acceptance
are separate evidence. See [verification](../../docs/fitness/paper-agent.md) and
[notices](THIRD_PARTY_NOTICES.md).
