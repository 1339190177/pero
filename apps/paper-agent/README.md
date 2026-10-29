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
for a dedicated override when needed. Install this App's pinned PDF dependencies
in that shared environment before using PDF reading:

```sh
/path/to/shared/bin/python -m pip install ./apps/paper-agent
```

The PDF backend uses `pypdfium2==5.0.0` and `Pillow==12.0.0`. Reading text,
discovery and existing reports remain available if PDF dependencies are missing;
opening a PDF shows the installation command. The Host does not install packages.

## PDF reading

Open an arXiv or bioRxiv/medRxiv paper and choose **PDF**. The App downloads the
original (up to 16 MiB / 500 pages), saves its SHA-256 and renders the saved bytes
with EmbedPDF/PDFium. WASM is bundled locally and external font fallback is off.
Select text to highlight or discuss it; **Capture area** saves a source image.
**Annotations** opens saved quotations, notes and images, including copy/download.
**Add to matrix** places a saved annotation under a named comparison field.
The matrix's page links return to the exact saved PDF and region after restart.

Annotations are sidecar records; the original PDF is retained unchanged. Both
engines use unrotated page coordinates with a top-left origin, stored as page
fractions. Text highlights are checked against that PDF page (ignoring whitespace);
figure captures are rendered by the backend from the saved original. These checks
establish source location, not scientific validity. Repeated report quotations
offer each matching page rather than silently selecting one occurrence.

This slice provides PDF reading and a manually curated matrix. It does not add
PaperQA's retrieval/model loop, automatic figure recognition or OCR. Existing
Agent reports still use the explicitly frozen source text; downloading a PDF
does not replace an abstract snapshot with extracted full text.

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

## Scientific content and evidence matrices

Reader and Report share `ScientificContent`: React Markdown → remark GFM/math →
rehype KaTeX/highlight → React elements. Raw HTML is not enabled. Fonts ship with
App assets. ScholarQA uses markdown-to-jsx with custom Paper overrides; Pero uses
an AST-based pipeline to support formulas as well as typed evidence links.

In section Markdown, `[label](evidence:1)` opens the first item in that section's
`evidence` array. Unknown indices remain visibly unresolved. Source chips expose
all supplied quotations, not only the first quotation for a paper. Closing the
source panel restores focus; Escape closes it. The raw-source disclosure supports
exact selections when rendered formulas or Markdown formatting alter visible text.

Reports may add a structured `matrix` alongside existing sections:

```json
{"columns":[{"id":"dataset","label":"Dataset"}],"cells":[
  {"paperId":"<frozen-paper-id>","columnId":"dataset","value":"Example data",
   "status":"supported","evidence":[{"paperId":"<frozen-paper-id>","quote":"<exact source passage>"}]}
]}
```

A cell belongs to one frozen paper and column. `supported` requires an exact
quotation from that paper; `conflict` requires two distinct passages; `missing`
has an empty value and no evidence. Omitted cells display as missing. Limits:
eight columns, forty cells. Source matching establishes location, not scientific
support. JSON and Markdown exports retain the matrix and its quotations. Old
reports without a matrix retain their identity and remain readable. New comparison
research prompts request matrices; existing reports are not automatically inferred
or rewritten. Generated report cells remain read-only. PDF page anchors and the
separate manually curated matrix are described above; OCR is not included.

## Continue discovery

Open a saved paper and click **Discover**. The current paper remains the explicit
seed. **Related papers**, **References**, and **Citing papers** query OpenAlex's
`related_works`, `referenced_works`, and `cites:<work-id>` respectively. Related
results are a similarity relation, not an assertion that one paper supports another.
OpenAlex IDs and DOI source URLs resolve directly. Other inputs, or missing IDs,
show title-search candidates for explicit title/author/year confirmation.

Results retain authors, publication date, citation count, abstract and original
link. Each successful exploration stores its seed snapshot, matched work, relation,
page, provider and retrieval time. History reopens stored results without fetching
them again. Return to seed keeps the original paper available; reading a candidate
and clicking Discover explicitly starts a new exploration.

**Save** bookmarks a candidate, including metadata-only records, without changing
the current research selection. A work is saved once, retaining its first discovery
source; other paths remain in exploration history. Saved candidates survive restart.

**Add to matrix research** saves an abstract snapshot and adds the seed and candidate
to the comparison selection (maximum five papers). It adds a comparison instruction
and prepares a fresh research. Click **Start research** to generate the new matrix
using the existing Host Agent. Existing saved reports remain immutable. A record
without an abstract can be saved or opened externally; comparison requires readable
source text. This flow does not fetch an arbitrary publisher PDF or invent evidence.

Authenticated routes: `POST /api/discover`, `GET /api/discoveries/<id>`,
`POST /api/favorites`, `POST /api/discoveries/read`. Candidate operations take a
stored `batchId` and `workId`; the browser does not supply the source content.
Pages contain up to twelve works, with an explicit browsing ceiling of 100 pages;
all App document kinds retain the existing 200-record storage limit.

Interaction references: [ResearchRabbit's single-paper exploration](https://www.researchrabbit.ai/articles/guide-to-using-researchrabbit)
and [Litmaps' seed-based discovery](https://docs.litmaps.com/en/articles/9092911-search-for-articles-with-litmaps).
No product code from either service was copied. See [discovery verification](../../docs/fitness/paper-discovery.md).
