<p align="center">
  <img src="docs/assets/openpero-mark.svg" width="72" alt="OpenPero logo">
</p>

<h1 align="center">OpenPero</h1>

<p align="center">
  <b>Distributed agent collaboration for research and R&amp;D.</b><br>
  Turn a question into a task contract, let people, agents, or compute do the work,<br>
  and keep the reviewed result with the project.
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="#status">Status</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#repository-layout">Repository</a> ·
  <a href="#contribute">Contribute</a> ·
  <a href="README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <img src="docs/assets/openpero-overview.svg" alt="OpenPero workflow: a question becomes a task contract, researchers, agents, or compute do the work, the run is recorded, and the reviewed result returns to the project memory" width="900">
</p>

> [!NOTE]
> **Early stage.** The Desktop workbench, shared tasks, and remote workers run today. Multi-agent orchestration, result verification, and complete AI4S workflows are still being built. There is no release or installer yet; build from source.

## Why

AI4S work connects literature, data, methods, and experiments. R&D work connects requirements, code, tests, and review. The people, code, and compute involved are usually spread across teams and machines, and a single agent run sees only one slice of it.

OpenPero organizes that work around **projects and tasks**. Experts define the problem and the acceptance criteria, contributors and agents divide the work, and reviewed results return to the project.

| | A single agent run | OpenPero |
| --- | --- | --- |
| **Context** | The current prompt | Project requirements, papers, data, methods, and decisions |
| **Collaboration** | People stitch runs together | Task claims, division of work, and handoffs |
| **Execution** | One environment | Approved local, lab, or cloud environments |
| **Done means** | "It ran" | Execution, verification, and human acceptance, recorded separately |

## Quick start

### Desktop

Requirements: Node.js 22.12+, Rust stable and `nightly-2026-10-28`, and a [gpui-kit](https://github.com/longbridge/gpui-kit) checkout at `ec89cfc`.

```bash
git clone https://github.com/phodal/pero.git && cd pero
npm ci
node node_modules/electron/install.js
rustup toolchain install nightly-2026-10-28 --profile minimal --target wasm32-unknown-unknown
export PERO_GPUI_KIT_PATH=/absolute/path/to/gpui-kit
npm run dev
```

In the app, choose **New task**, pick an agent (Codex, Claude, or Grok through OpenCode) and a workspace. Each agent uses its own local login. Agent setup, fonts, and offline builds are in [desktop/README.md](desktop/README.md).

To try a local App instead, run `npm run dev:apps`. It opens Virtual Cell in a separate profile; see [apps/README.md](apps/README.md).

### Control plane (optional)

Shared tasks, remote workers, and GitHub intake need the Go service. Requirements: Go 1.24+ and PostgreSQL.

```bash
cd server && PERO_DATABASE_URL=postgres://localhost/pero PERO_OPERATOR_TOKEN=change-me go run ./cmd/server
```

It listens on `127.0.0.1:8787`. Then [connect Desktop to it](desktop/README.md#remote-tasks) and, if you want, [bind GitHub repositories](server/README.md#github-issue-intake).

## Status

| Capability | State | Docs |
| --- | --- | --- |
| Desktop workbench: Electron + GPUI/WASM, ACP conversations, English/Chinese, themes | 🟢 Development builds | [desktop](desktop/README.md) |
| Local Apps, starting with Virtual Cell | 🟢 First version | [apps](apps/README.md), [ADR 0007](docs/adr/0007-local-app-platform.md) |
| Tasks, runs, workers, leases, cancellation, and recovery | 🟡 Works locally; real remote-environment acceptance pending | [server](server/README.md), [ADR 0002](docs/adr/0002-remote-task-dispatch.md) |
| GitHub Issue and ROADMAP intake, project-scoped claims | 🟡 Implemented | [server](server/README.md#github-issue-intake) |
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

### Architecture

![OpenPero architecture: clients, Cloud coordination, Host execution, capabilities, environments, and trusted delivery](docs/assets/openpero-architecture.svg)

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
| `apps/`, `packages/` | Local Apps, the App SDK, and product definitions |
| `docs/` | Architecture, ADRs, roadmap, verification records; also the openpero.com site |
| `deployment/` | Task Center host install script |

## Development

```bash
npm run check                    # TypeScript, generated theme, locales, contracts
npm test                         # Rust tests
npm run test:contracts           # Host stdio output against the JSON Schema
go -C server test -race ./...    # control plane; PostgreSQL cases need PERO_TEST_DATABASE_URL
```

CI runs these on Linux, Windows, and macOS, plus Bazel `//:checks`. Electron, PostgreSQL, live GitHub, and real-model suites are opt-in. Every suite and what it proves is listed in [docs/fitness](docs/fitness/README.md).

New to the code? Read [ARCHITECTURE](docs/ARCHITECTURE.md), then the [ADR index](docs/adr/README.md), then [fitness](docs/fitness/README.md). Colors follow [DESIGN.md](DESIGN.md). Regenerate the README diagrams with `npm run gen:architecture` and `npm run gen:overview`.

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
