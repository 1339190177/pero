<p align="center">
  <img src="docs/assets/openpero-mark.svg" width="72" alt="Pero logo">
</p>

<h1 align="center">Pero</h1>

<p align="center">
  <b>Composable research apps and distributed agent collaboration.</b><br>
  Build a domain tool, ship it as an app, and bring it into a shared workbench.<br>
  Connect people, agents and compute as the work grows.
</p>

<p align="center">
  <a href="DEVELOPMENT.md">Development</a> ·
  <a href="#status">Status</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#repository-layout">Repository</a> ·
  <a href="#contribute">Contribute</a> ·
  <a href="README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <img src="docs/assets/openpero-overview.svg" alt="Pero workflow: a question becomes a task contract, researchers, agents, or compute do the work, the run is recorded, and the reviewed result returns to the project memory" width="900">
</p>

> [!NOTE]
> **Early stage.** The Desktop workbench, shared tasks, and remote workers run today. Multi-agent orchestration, result verification, and complete AI4S workflows are still being built. Virtual Cell can also be built as a standalone macOS application. Packages are development builds; there is no production release yet.

## Why

A useful research tool often starts as a script, a notebook or a model demo. Turning it into an application takes more work: a domain interface, a reproducible runtime, durable data and a way for others to use it. Adding Agent assistance or team collaboration brings another set of execution and coordination needs.

**Pero makes domain Apps the reusable building block.** Start with one workflow, such as exploring a cell atlas or simulating a reaction. Deliver it as a standalone application, or use the same App inside a workbench alongside other tools. Apps reuse the platform's shell, SDK and Host runtime so each domain can focus on its own methods and user experience.

As the work grows, the aim is to connect those Apps to Agent sessions, approved compute and shared projects. Methods, inputs, outputs and checks should remain explicit as execution moves between people, agents and machines. Product assembly works today; App-to-Agent tools and the full task-to-result verification loop are still being built.

| Need | Pero's approach |
| --- | --- |
| Deliver one useful tool | Package a domain App as a standalone product |
| Bring several tools together | Open Apps in the shared Desktop workbench |
| Add Agent assistance | Reuse Host-owned sessions and permissions; App integration is next |
| Scale to collaborative research | Connect tasks, compute and reviewed results through the optional control plane |

## Status

| Capability | State | Docs |
| --- | --- | --- |
| Desktop workbench: Electron + GPUI/WASM, ACP conversations, English/Chinese, themes | 🟢 Development builds | [desktop](desktop/README.md) |
| Local Apps, starting with Virtual Cell | 🟢 First version | [apps](apps/README.md), [ADR 0007](docs/adr/0007-local-app-platform.md) |
| Standalone App products: Virtual Cell + bundled Python | 🟡 macOS arm64 development package verified | [product model](docs/adr/0008-product-distributions.md), [verification](docs/fitness/products.md) |
| Tasks, runs, workers, leases, cancellation, and recovery | 🟡 Works locally; real remote-environment acceptance pending | [server](server/README.md), [ADR 0002](docs/adr/0002-remote-task-dispatch.md) |
| GitHub Issue and ROADMAP intake, project-scoped claims | 🟡 Implemented | [server](server/README.md#github-issue-intake) |
| Public task → local coding CLI → reviewed draft PR | 🟡 Local contributor flow verified; public deployment pending | [Desktop contribution guide](desktop/README.md#contribute-on-this-computer) |
| Community task board: GitHub OAuth, worker ownership, reviewed contribution points | 🟡 Implemented | [server](server/README.md#community-tasks-and-contributions-2) |
| Artifact versions, independent verification, authorized writeback | ⚪ Planned (M3) | [roadmap](docs/ROADMAP.md) |
| Environment matching, isolation, self-hosted upgrades | ⚪ Planned (M4) | [roadmap](docs/ROADMAP.md) |

🟢 usable in development builds · 🟡 implemented, acceptance incomplete · ⚪ planned. Milestones and acceptance levels are in the [roadmap](docs/ROADMAP.md).

## How it works

1. **Describe the work.** Start from a GitHub Issue, a research or business request, or a suggestion the project raises itself.
2. **Make it a contract.** Pin the inputs, method version, expected outputs, resource limits, and acceptance criteria.
3. **Assign it.** A collaborator claims it, or an approved agent or worker picks it up.
4. **Run and record.** Produce code, data, methods, or reports, with enough record to inspect or repeat the run.
5. **Review and continue.** Record the decision, update the project, and report back to GitHub when useful.

A few principles shape the design:

- **The project remembers.** Requirements, papers, datasets, methods, runs, and decisions stay together, so you can see where work stopped and why a result was accepted.
- **Agents propose, people decide.** When data changes or a check fails, the agent explains the impact and proposes a task. It never quietly changes methods or accepted conclusions.
- **Compute stays where the data is.** Tasks run on a laptop, an approved lab server, or the cloud. The coordinator can be self-hosted.
- **A successful run is not an accepted result.** Every result links to its inputs, method version, parameters, and environment; acceptance is an explicit review.

### Composable Apps and products

Composition has two scopes: **product assembly works today**; connecting domain methods, Agent tools and result verification is the next step.

| Concept | Owns | Current implementation |
| --- | --- | --- |
| **App** | Domain UI, data and optional Python backend | `apps/<name>/app.json`; Virtual Cell is the first App |
| **Product** | Identity, version, included Apps, shell, entry panel and Python policy | `products/`; validated by `packages/product` |
| **Distribution** | Assembled shell, Host, selected Apps and runtime for a platform/architecture | `target/products/`; current builder targets the build machine |
| **Host runtime** | App installation, enable state, backend processes and ACP sessions | Shared implementation reused by both shells; each standalone product starts its own Host/profile |
| **Control plane** | Shared tasks, claims, runs and remote workers | Optional `server/`; standalone Apps run locally without it |

The workbench uses GPUI/Wasm navigation to open Apps. An App product selects a fixed panel and uses a full-window Electron shell without GPUI navigation. Both reuse the App SDK, private backend proxy and Host lifecycle. Product assembly changes the delivery entry, while domain behavior stays in the App.

The direction follows **Packaged Business Capabilities (PBC)**: compose around a domain outcome, with explicit inputs, outputs and checks. A full domain package could join its App, methods, Agent tools and verification rules. Today only App/product assembly is implemented; the manifest does not yet compose those workflow contracts. Future integrations should reuse Host execution and control-plane authority. See [composition boundaries](docs/ARCHITECTURE.md#可组合应用与产品) for the current model and next steps.

### Runtime architecture

![Pero architecture: clients, Cloud coordination, Host execution, capabilities, environments, and trusted delivery](docs/assets/openpero-architecture.svg)

Solid outlines exist today; dashed ones are planned. Interface (Web, Desktop), execution location (local, lab, cloud), and deployment (self-hosted, managed) are independent choices. Runtime topology and invariants are in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md); decisions are in [docs/adr](docs/adr/README.md).

<details>
<summary><b>Safety by design</b></summary>

- Issue text, pull requests, and task payloads are **untrusted input**. Submitting work is not permission to run it.
- Execution uses scoped credentials, resource limits, approved data access, and isolation that fits each task.
- Retries account for side effects; duplicate delivery must not repeat consequential actions.
- Publishing results or changing accepted conclusions follows the project's review policy.
- Never attach an unrestricted personal or lab machine to an untrusted public task stream.

</details>

## Repository layout

| Path | What it is |
| --- | --- |
| `crates/pero-host` | Rust Host: stdio JSONL bridge that supervises ACP agent processes |
| `crates/pero-runtime` | ACP session handling, using the official Rust SDK |
| `crates/pero-ui` | GPUI workbench, compiled to WASM |
| `crates/pero-worker` | Standalone remote execution worker |
| `crates/pero-contracts` | Rust types for the protocol contracts |
| `desktop/`, `web/` | Electron shell and the WASM loader with shared assets |
| `server/` | Go control plane on PostgreSQL |
| `protocol/` | TypeSpec contracts and generated schemas |
| `apps/` | Domain Apps: manifests, UI, optional backends and evidence |
| `packages/app-sdk/`, `packages/product/` | Shared App API/UI and the immutable Product model |
| `products/` | Workbench defaults and standalone product definitions |
| `scripts/build/` | App compilation, product assembly and Desktop packaging |
| `docs/` | Architecture, ADRs, roadmap, verification records; also the openpero.com site |
| `deployment/` | Task Center host install script |

## Development

See [DEVELOPMENT.md](DEVELOPMENT.md) for setup, local development, standalone App packaging and verification commands.

## Contribute

You don't need to understand the whole platform to help. Pick one bounded piece:

| Area | A good first contribution |
| --- | --- |
| **Domain use cases** | Real problems, constraints, and acceptance criteria |
| **Task and result contracts** | Schemas, example payloads, validators |
| **GitHub connector** | Event intake, deduplication, task mapping, status updates |
| **Worker** | Capability registration, claims, heartbeats, cancellation, result submission |
| **Coordination and recovery** | Dependencies, leases, safe retries, rejecting stale results |
| **Result validation** | Artifact checks, baseline comparisons, independent review hooks |
| **Research method packs** | Fixed inputs, environments, baselines, and explicit checks |
| **R&D workflows** | Playbooks for requirements verification, testing, and review |

These are proposed areas, not existing Issues. For anything substantial, open an Issue first to agree on scope, interfaces, and acceptance criteria, and include a way to run or test it locally. AI-assisted contributions go through the same review as any other.

Domain experts are welcome too: bring problems and acceptance criteria, contribute scientific methods, or review results independently.

<details>
<summary>Working on the demo pages</summary>

Chinese copy lives in `pages/src`; English strings in `pages/locales/en.js`. Rebuild both pages with `node pages/scripts/build.mjs`.

</details>

---

<p align="center"><b>Start with one problem. Make the next contribution easier.</b></p>

<!-- Maintainer publication checklist: choose and add the repository LICENSE; confirm source ownership and third-party notices; replace prototype status with verified release status as implementation lands. No license, hosted service, or installation command is implied by this draft. -->
