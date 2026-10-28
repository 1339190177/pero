<p align="center">
  <img src="docs/assets/openpero-mark.svg" width="72" alt="OpenPero logo">
</p>

<h1 align="center">OpenPero</h1>

<p align="center">
  <b>Research that doesn't reset when the chat does.</b><br>
  An open-source AI-for-Science workspace where people, agents, and compute work on the same project.
</p>

<p align="center">
  <a href="https://www.openpero.com/en/"><b>▶ Try the live demo</b></a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#architecture">Architecture</a> ·
  <a href="#roadmap">Roadmap</a> ·
  <a href="#contribute">Contribute</a> ·
  <a href="README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <img src="docs/assets/openpero-overview.svg" alt="OpenPero workflow: a question becomes a task contract, researchers, agents, or compute do the work, the run is recorded, and the reviewed result returns to the project memory" width="900">
</p>

> [!NOTE]
> **Early stage.** The [live demo](https://www.openpero.com/en/) runs numerical calculations in your browser and simulates GitHub, agents, collaborators and remote workers. The repository also includes the locally verified implementation below. The research workflow and roadmap describe the broader product direction.

## Run the current implementation

- [Desktop](desktop/README.md): Electron + GPUI, ACP conversations, Chinese/English, themes, local sessions and shared remote tasks.
- [Go control plane](server/README.md): PostgreSQL Task/Run/Worker state, leases, cancellation/recovery, GitHub Issue intake, project-scoped claims and opt-in automatic dispatch.
- [Community board](server/README.md#community-tasks-and-contributions-2): public tasks, GitHub OAuth, Worker ownership, reviewed contribution points and reversals.
- [Build and verification](docs/fitness/README.md): npm/Cargo development and the Bazel Go/code-generation pilot. Fonts use local files or build caches before pinned downloads; generated binaries are ignored.

Local UI and protocol checks use provider fixtures. Deployment, real OAuth credentials and independent acceptance require their own setup and evidence. [Worktree integration record](docs/fitness/worktree-integration.md).

## The problem

AI makes a single research session faster. Then the session ends, and the context goes with it.

Next week, nobody remembers which dataset version was used, which parameters produced that figure, why the result was accepted, or who was supposed to check it. A research project needs memory, collaborators, and an audit trail. A chat window has none of those.

OpenPero makes **the project**, not the conversation, the unit of work.

| | Chat-based AI research | OpenPero |
| --- | --- | --- |
| **Context** | Lives in one conversation | Lives in the project: question, papers, data, methods, decisions |
| **Agent** | Acts when you ask | Notices changes and proposes the next task |
| **Execution** | Whatever machine you're on | Routed to a collaborator, an agent, or the compute where the data already is |
| **Done means** | "It ran" | Ran → checked → reviewed → accepted, each step recorded |

## Try it in 30 seconds

**[Open the live demo →](https://www.openpero.com/en/)** No install, no account, no API key.

- **Change a parameter, rerun the method.** Drag the damping ratio; an RK4 solver reruns in the browser and is compared with the baseline and the analytical solution.
- **Open the record behind the figure.** Inputs, method version, parameters, error metrics, and the check that was applied.
- **Walk a task through review.** Prepare it, pick a (simulated) execution route, review, then accept it into project state.
- **Take it with you.** Export task drafts, GitHub Issue templates, JSON records, and CSV data.

The example is a synthetic damped oscillator, not a replication of a published paper. State resets on reload, so export before you leave. To run it offline, open [`docs/en/index.html`](docs/en/index.html) ([中文](docs/index.html)) from a local clone.

## How it works

The diagram at the top is the whole loop. Step by step:

1. **Describe the work.** Start from a GitHub Issue, a researcher's request, or a suggestion the project raises itself.
2. **Make it a contract.** Pin the inputs and method version, the expected outputs, resource limits, and acceptance criteria.
3. **Assign it.** A collaborator claims it, or an approved agent or worker picks it up.
4. **Run and record.** Produce artifacts together with everything needed to inspect or repeat the run.
5. **Review and continue.** Record the decision, update the project, and report back to GitHub when useful.

**Example:** *"Rerun our method on the new dataset and explain any difference from the approved baseline."* One contributor adapts the method, a lab server runs the comparison, and another contributor reviews it. They share a task contract, not a machine or an agent.

## What makes it different

**🧠 The project remembers.** Question, papers, datasets, methods, runs, and decisions stay together. Come back a month later and see where the work stopped, why a result was accepted, and what is still open.

**🔔 The agent prepares the next step.** A new dataset version, a finished run, a failed check, a relevant paper: the agent explains what changed, what it might affect, and proposes a concrete task. It works within project permissions and budgets, and it never quietly changes methods or accepted conclusions.

**🌐 Compute stays where the data is.** Tasks can run on a laptop, an approved lab server, or the cloud. The coordinator can be self-hosted, and hosting it does not mean moving your data to the cloud.

**🔍 Every result has a history.** Each result links to its inputs, method version, parameters, environment, and execution record. A successful run is not an accepted result. Acceptance is an explicit, reviewed decision.

## Architecture

![OpenPero architecture: clients, Cloud coordination, Host execution, capabilities, environments, and trusted delivery](docs/assets/openpero-architecture.svg)

Solid outlines are existing foundations; dashed ones are planned. Regenerate the diagrams with `npm run gen:architecture` and `npm run gen:overview`.

| Part | Responsibility |
| --- | --- |
| **Project** | Goals, shared context, accepted results, open questions, decisions |
| **Task** | A bounded contribution with fixed inputs, expected outputs, constraints, and acceptance criteria |
| **Coordinator** | Dependencies, authorization, assignment, execution state, cancellation, recovery |
| **Worker** | Runs approved tasks with declared capabilities and scoped access |
| **Result & review** | Keeps artifacts and provenance; separates execution success, validation, and acceptance |
| **Web / Desktop / GitHub** | Different ways into the same work |

Interface (Web, Desktop), execution location (local, lab, cloud), and deployment (self-hosted, managed) are independent choices.

<details>
<summary><b>Safety by design</b></summary>

- Issue text, pull requests, and task payloads are **untrusted input**. Submitting work is not permission to run it.
- Execution uses scoped credentials, resource limits, approved data access, and isolation that fits each task.
- Retries account for side effects; duplicate delivery must not repeat consequential actions.
- Publishing results or changing accepted conclusions follows the project's review policy.
- Never attach an unrestricted personal or lab machine to an untrusted public task stream.

</details>

## Roadmap

| | Stage | What we need to prove |
| --- | --- | --- |
| 🟡 | **1. One reproducible task** | Fixed input, runnable method, inspectable result, clear acceptance decision |
| ⚪ | **2. A shared project** | Persistent context and task history, with GitHub collaboration |
| ⚪ | **3. Two trusted workers** | Routing, cancellation, failure recovery, independently reviewable results |
| ⚪ | **4. Proactive research** | Relevant changes produce useful suggestions or policy-approved tasks |
| ⚪ | **5. Reuse at scale** | Method packs, more execution adapters, self-hosting, managed Cloud evaluation |

🟡 in progress · ⚪ planned

## Contribute

You don't need to understand the whole platform to help. Pick one bounded piece:

| Area | A good first contribution |
| --- | --- |
| **Task & result contracts** | Schemas, example payloads, validators |
| **GitHub connector** | Authenticated event intake, deduplication, task mapping, status updates |
| **Worker SDK** | Capability registration, claims, heartbeats, cancellation, result submission |
| **Coordination & recovery** | Dependencies, leases, safe retries, rejecting stale results |
| **Result validation** | Artifact checks, baseline comparisons, independent review hooks |
| **Research method packs** | One small runnable example with pinned inputs and explicit checks |

These are proposed areas, not existing Issues. For anything substantial, open an Issue first to agree on scope, interfaces, and acceptance criteria, and include a way to run or test it locally. AI-assisted contributions go through the same review as any other.

**Researchers welcome too.** Package a method with its inputs, environment, expected outputs, and checks. Replicate a result, compare alternatives, or review someone else's contribution.

<details>
<summary>Working on the demo pages</summary>

Chinese copy lives in `pages/src`; English strings in `pages/locales/en.js`. Rebuild both pages with `node pages/scripts/build.mjs`. Colors follow [`DESIGN.md`](DESIGN.md).

</details>

---

<p align="center"><b>Start with one research question. Make the next contribution easier.</b></p>

<!-- Maintainer publication checklist: choose and add the repository LICENSE; confirm source ownership and third-party notices; replace prototype status with verified release status as implementation lands. No license, hosted service, or installation command is implied by this draft. -->
