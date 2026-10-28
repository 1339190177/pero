# OpenPero

**An open-source AI research workspace for people, agents, and compute.**

Bring papers, data, methods, and results into one research project. Turn questions into tasks, coordinate work across collaborators and computing environments, and bring reviewed results back to the project.

**Keep the context. Share the work. Build on the results.**

[English](README.md) · [简体中文](README.zh-CN.md) · [Interactive prototype](docs/index.html) · [English prototype](docs/en/index.html)

> **Early-stage project.** The included prototype runs numerical calculations and exports results in your browser. Task coordination, GitHub integration, collaborators, and remote execution are simulated. The platform capabilities below describe the direction we are building toward, not a production service.

## Why OpenPero?

A research project is more than a conversation. It has a question, source material, methods, previous results, unresolved issues, and decisions that need to survive the next session.

OpenPero is being built around that continuity. The project—not the chat—is the unit of work. People contribute judgment and expertise; agents help prepare and carry out tasks; workers connect those tasks to the right tools and computing environments.

The goal is not to replace scientific judgment. It is to make research easier to continue, share, inspect, and reproduce.

## What we are building

### A project that keeps its context

Keep the research question, papers, datasets, methods, runs, and decisions together. Pick up where the work stopped, understand why a result was accepted, and see which questions remain open.

### An agent that prepares the next step

Follow relevant changes: a new dataset version, a completed run, a failed check, or a paper that may affect the project. Explain what changed, identify potentially affected work, and propose a concrete next task.

Actions should follow project permissions and budgets. Important changes to methods, accepted conclusions, or public results remain explicit decisions—not side effects of an agent run.

### Work shared across people, agents, and compute

Define tasks that a collaborator can claim, an agent can work on, or a worker can execute. Coordinate dependencies, execution attempts, cancellation, and recovery without assuming that all work happens on one machine.

Cloud-based coordination is a core architectural direction. A coordinator should also be self-hostable; computation can stay on a personal machine, an approved lab server, or cloud infrastructure. Hosting the coordinator does not require moving every dataset into the cloud.

### Results with an inspectable history

Associate each result with its inputs, method version, parameters, environment, and execution record. Compare it with the baseline and review the limitations before accepting it into the project.

**A successful run is not the same as an accepted research result.** Traceability supports review; it does not by itself establish scientific validity.

## From an issue to a research result

GitHub is an entry point for collaboration—not the entire research runtime.

A proposed workflow:

1. **Describe the work.** Start with a GitHub Issue, a researcher request, or a project-triggered suggestion.
2. **Make it actionable.** Fix the inputs and method versions, expected outputs, resource constraints, and acceptance criteria.
3. **Authorize and assign.** A collaborator claims the work, or an approved agent or worker receives it.
4. **Run and record.** Produce artifacts together with the execution details needed to inspect or repeat the work.
5. **Review and continue.** Check the result, record the decision, update the project, and report back to GitHub when appropriate.

For example, a project can ask contributors to rerun an existing method on a new dataset and explain differences from an approved baseline. One contributor adapts the method, a worker runs the comparison, and another contributor reviews the result. They share a task contract rather than needing the same machine or agent.

Not every internal run needs an Issue. Publish work to GitHub when it benefits from discussion, independent contribution, or public review. Closing an Issue is not a substitute for recording acceptance.

## Architecture direction

![OpenPero architecture: clients, Cloud coordination, Host execution, capabilities, environments, and trusted delivery](docs/assets/openpero-architecture.svg)

Solid outlines mark existing foundations; dashed modules and connections are planned. Regenerate the diagram with `node scripts/generate-architecture.mjs`.

| Part | Responsibility |
| --- | --- |
| **Project** | Research goals, shared context, accepted results, open questions, and decisions. |
| **Task** | A bounded contribution with fixed inputs, expected outputs, constraints, and acceptance criteria. |
| **Coordinator** | Dependencies, authorization, assignment, execution state, cancellation, and recovery. |
| **Worker** | Execute approved tasks with declared capabilities and scoped access. |
| **Result and review** | Preserve artifacts and provenance; distinguish execution success, validation, and acceptance. |
| **Web / Desktop / GitHub** | Different ways to enter, follow, contribute to, and review the same work. |

Web and Desktop are interfaces. Local, lab, and cloud are execution locations. Self-hosting and a potential managed Cloud are deployment choices. These should remain separate decisions.

A coordinator can remain available while a worker is offline, but it cannot make an unavailable local tool or dataset run remotely without an authorized, compatible replacement.

## Try the interactive prototype

Open [`docs/index.html`](docs/index.html) locally in a browser after downloading or cloning this repository. The English page is [`docs/en/index.html`](docs/en/index.html). No installation, account, API key, or remote service is required for the demo. GitHub may show the HTML source rather than execute it; open the downloaded file to interact with it.

Chinese copy lives in `pages/src`. English strings live in `pages/locales/en.js`. Rebuild both pages with `node pages/scripts/build.mjs`. Colors follow `DESIGN.md`.

The prototype walks through preparing a task, selecting a simulated execution route, comparing numerical results, reviewing them, and accepting them into the page's project state. It also exports task drafts, Issue templates, JSON records, and CSV data.

| In the prototype | Scope |
| --- | --- |
| Numerical calculations and JSON / CSV export | Executed locally in the browser. |
| Task preparation, cancellation, review, and acceptance | Interactive, page-local workflow. |
| GitHub events, agent suggestions, contributors, and remote workers | Simulated; no external integration. |
| Persistent projects, authenticated collaboration, and hosted Cloud | Not provided by this prototype. |

The numerical example uses a synthetic damped oscillator and compares a numerical solution with an analytical reference. It is not a replication of a published paper or validation against a physical experiment. Page state resets on reload; export records before leaving.

## Contribute

You should not need to understand the whole platform to make a useful contribution. We want clear, bounded tasks with examples and acceptance checks.

**Build OpenPero.** Contribute task contracts, the coordinator, worker adapters, connectors, validation, interfaces, or documentation.

**Contribute research methods.** Package a runnable method with its inputs, environment requirements, expected outputs, and checks. Replicate a result, compare alternatives, or independently review a contribution.

**Connect execution environments.** Help build adapters for approved local and remote resources. An open, anonymous compute network is not the initial goal; third-party workers require explicit trust, isolation, and data-access decisions.

### Initial contribution areas

These are proposed work areas, not a list of existing Issues or completed modules.

| Area | A useful first contribution |
| --- | --- |
| Task and result contracts | Schemas, example payloads, and validators. |
| GitHub connector | Authenticated event intake, deduplication, task mapping, and status updates. |
| Worker SDK | Capability registration, task claims, heartbeats, cancellation, and result submission. |
| Coordination and recovery | Dependency handling, leases, safe retries, and rejection of stale results. |
| Result validation | Artifact checks, baseline comparisons, and independent review hooks. |
| Research method packs | One small, runnable example with pinned inputs and explicit verification rules. |

Before implementing a substantial change, use an Issue to agree on scope, interfaces, and acceptance criteria. Include a way to run or test the contribution locally. AI-assisted contributions should meet the same review and verification requirements as other contributions.

## Execution boundaries

Issue text, pull requests, and task payloads are untrusted inputs. Submitting work is not permission to execute it.

The execution design should enforce scoped credentials, resource limits, approved data access, and isolation appropriate to each task. Retrying a task must account for side effects; duplicate delivery must not silently duplicate consequential actions. Public results and changes to accepted conclusions require the project's review policy.

Do not attach an unrestricted personal or lab machine to an untrusted public task stream.

## Roadmap

| Stage | Outcome to validate |
| --- | --- |
| **1. One reproducible task** | A fixed input, executable method, inspectable result, and clear acceptance decision. |
| **2. A shared project** | Persistent context and task history, with GitHub collaboration. |
| **3. Two trusted workers** | Task routing, cancellation, failure recovery, and independently reviewable results. |
| **4. Proactive research** | Relevant changes produce useful suggestions or policy-authorized tasks. |
| **5. Reusable methods and wider collaboration** | Method packs, more execution adapters, self-hosting, and evaluation of managed Cloud. |

**Start with one research question. Make the next contribution easier.**

<!-- Maintainer publication checklist: choose and add the repository LICENSE; confirm source ownership and third-party notices; replace prototype status with verified release status as implementation lands. No license, hosted service, or installation command is implied by this draft. -->
