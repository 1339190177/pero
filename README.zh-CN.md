<p align="center">
  <img src="docs/assets/openpero-mark.svg" width="72" alt="OpenPero 标志">
</p>

<h1 align="center">OpenPero</h1>

<p align="center">
  <b>面向科研与研发的分布式 Agent 协作。</b><br>
  把问题写成任务契约，交给研究者、Agent 或算力完成，<br>
  复核后的成果留在项目里。
</p>

<p align="center">
  <a href="#快速开始">快速开始</a> ·
  <a href="#当前状态">当前状态</a> ·
  <a href="#工作方式">工作方式</a> ·
  <a href="#仓库结构">仓库结构</a> ·
  <a href="#参与贡献">参与贡献</a> ·
  <a href="README.md">English</a>
</p>

<p align="center">
  <img src="docs/assets/openpero-overview.zh-CN.svg" alt="OpenPero 工作流：研究问题整理成任务契约，由研究者、Agent 或算力完成，运行被记录，复核后的成果回到项目记忆" width="900">
</p>

> [!NOTE]
> **项目处于早期阶段。** Desktop 工作台、共享任务和远程 Worker 已经可以运行。多 Agent 编排、结果验证，以及完整的 AI4S 工作流仍在建设中。目前没有发布版或安装包，需要从源码构建。

## 为什么做

AI4S 需要串起文献、数据、方法与实验；研发协作需要衔接需求、代码、测试与评审。相关的人、代码和算力通常分散在不同团队和机器上，单次 Agent 执行只能看到其中一小块。

OpenPero 以**项目与任务**组织这些工作：专家定义问题和验收标准，贡献者与 Agent 分工，复核后的成果交回项目。

| | 单次 Agent 执行 | OpenPero |
| --- | --- | --- |
| **背景** | 当前的提示词 | 项目里的需求、文献、数据、方法与决定 |
| **协作** | 由人串联各次执行 | 任务认领、分工与交接 |
| **执行** | 一个运行环境 | 获准的本机、实验室服务器或云端 |
| **完成的标准** | “跑通了” | 执行、验证与人工验收分别记录 |

## 快速开始

### Desktop

需要 Node.js 22.12+、Rust stable 与 `nightly-2026-10-28`，以及 `ec89cfc` 版本的 [gpui-kit](https://github.com/longbridge/gpui-kit) 本地检出。

```bash
git clone https://github.com/phodal/pero.git && cd pero
npm ci
node node_modules/electron/install.js
rustup toolchain install nightly-2026-10-28 --profile minimal --target wasm32-unknown-unknown
export PERO_GPUI_KIT_PATH=/absolute/path/to/gpui-kit
npm run dev
```

在应用中选择 **New task**，再选一个 Agent（Codex、Claude，或经 OpenCode 使用的 Grok）和工作区。各 Agent 使用自己的本机登录。Agent 配置、字体与离线构建见 [desktop/README.md](desktop/README.md)。

想试用本地 App，可运行 `npm run dev:apps`，它会在独立配置中打开 Virtual Cell，见 [apps/README.md](apps/README.md)。

### 控制面（可选）

共享任务、远程 Worker 和 GitHub 同步需要 Go 服务。需要 Go 1.24+ 和 PostgreSQL。

```bash
cd server && PERO_DATABASE_URL=postgres://localhost/pero PERO_OPERATOR_TOKEN=change-me go run ./cmd/server
```

服务监听 `127.0.0.1:8787`。之后[让 Desktop 连接它](desktop/README.md#remote-tasks)，需要时再[绑定 GitHub 仓库](server/README.md#github-issue-intake)。

## 当前状态

| 能力 | 状态 | 文档 |
| --- | --- | --- |
| Desktop 工作台：Electron + GPUI/WASM、ACP 对话、中英文、主题 | 🟢 开发构建可用 | [desktop](desktop/README.md) |
| 本地 App，首个为 Virtual Cell | 🟢 首版 | [apps](apps/README.md)、[ADR 0007](docs/adr/0007-local-app-platform.md) |
| Task、Run、Worker、租约、取消与恢复 | 🟡 本地流程已接通，真实远程环境验收待完成 | [server](server/README.md)、[ADR 0002](docs/adr/0002-remote-task-dispatch.md) |
| GitHub Issue 与 ROADMAP 同步、项目范围认领 | 🟡 已实现 | [server](server/README.md#github-issue-intake) |
| 社区任务广场：GitHub OAuth、Worker 归属、验收记分 | 🟡 已实现 | [server](server/README.md#community-tasks-and-contributions-2) |
| 产物版本、独立验证、授权回写 | ⚪ 规划中（M3） | [Roadmap](docs/ROADMAP.md) |
| 环境匹配、隔离、自部署升级 | ⚪ 规划中（M4） | [Roadmap](docs/ROADMAP.md) |

🟢 开发构建可用 · 🟡 已实现，验收未完成 · ⚪ 规划中。里程碑与验收层级见 [Roadmap](docs/ROADMAP.md)。

## 工作方式

1. **提出工作。** 从 GitHub Issue、研究或业务委托，或项目自身变化产生的建议开始。
2. **写成契约。** 固定输入与方法版本，写明预期产物、资源限制和验收条件。
3. **分配任务。** 由合作者认领，或交给获准的 Agent / Worker。
4. **执行并记录。** 产出代码、数据、方法或报告，同时保存检查和复跑所需的信息。
5. **复核并接续。** 记录决定、更新项目，必要时回写 GitHub。

设计遵循几条原则：

- **项目会记住。** 需求、文献、数据集、方法、运行和决定放在一起，随时能看到工作停在哪里、为什么接受某个结果。
- **Agent 提议，人来决定。** 数据变化或检查失败时，Agent 说明影响并提出任务，不会悄悄修改方法或已接受的结论。
- **计算留在数据所在的地方。** 任务可以在个人电脑、获准的实验室服务器或云端执行，协调服务可以自部署。
- **跑通不等于验收。** 每个结果关联输入、方法版本、参数和环境；接受结果是一次明确的复核。

### 架构

![OpenPero 架构：客户端、Cloud 协调、Host 执行、能力生态、执行环境与可信交付](docs/assets/openpero-architecture.svg)

实线框表示已有基础，虚线表示规划中。使用入口（Web、Desktop）、执行位置（本机、实验室、云端）和部署方式（自部署、托管）是相互独立的选择。运行时拓扑与不变量见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)，技术决定见 [docs/adr](docs/adr/README.md)。

<details>
<summary><b>安全边界</b></summary>

- Issue 正文、PR 和任务载荷都是**不可信输入**。提出任务不等于获得执行权限。
- 执行时限制凭据、资源与数据访问，并提供与任务匹配的隔离。
- 重试必须考虑副作用，重复投递不能悄悄重复高影响操作。
- 公开结果或修改已接受的结论，需遵守项目的审阅规则。
- 不要把不受限的个人电脑或实验室服务器接到不可信的公共任务流上。

</details>

## 仓库结构

| 路径 | 内容 |
| --- | --- |
| `crates/pero-host` | Rust Host：stdio JSONL 桥接，托管 ACP Agent 进程 |
| `crates/pero-runtime` | ACP 会话，基于官方 Rust SDK |
| `crates/pero-ui` | GPUI 工作台，编译为 WASM |
| `crates/pero-worker` | 独立的远程执行 Worker |
| `crates/pero-contracts` | 协议契约的 Rust 类型 |
| `desktop/`、`web/` | Electron 壳，以及 WASM 加载与共享资源 |
| `server/` | 基于 PostgreSQL 的 Go 控制面 |
| `protocol/` | TypeSpec 契约与生成的 Schema |
| `apps/`、`packages/` | 本地 App、App SDK 与产品定义 |
| `docs/` | 架构、ADR、路线图、验收记录；同时是 openpero.com 站点 |
| `deployment/` | Task Center 主机安装脚本 |

## 开发

```bash
npm run check                    # TypeScript、生成的主题、文案目录、契约
npm test                         # Rust 测试
npm run test:contracts           # Host stdio 输出对照 JSON Schema
go -C server test -race ./...    # 控制面；PostgreSQL 用例需要 PERO_TEST_DATABASE_URL
```

CI 在 Linux、Windows 和 macOS 上运行以上命令，并运行 Bazel `//:checks`。Electron、PostgreSQL、公开 GitHub 和真实模型套件需显式运行。各套件及其验证范围见 [docs/fitness](docs/fitness/README.md)。

第一次看代码，建议依次阅读 [ARCHITECTURE](docs/ARCHITECTURE.md)、[ADR 索引](docs/adr/README.md) 和 [fitness](docs/fitness/README.md)。色彩遵循 [DESIGN.md](DESIGN.md)。重新生成 README 图示：`npm run gen:architecture` 与 `npm run gen:overview`。

## 参与贡献

不必理解整个平台也能参与。挑一块边界清楚的工作即可：

| 方向 | 适合上手的贡献 |
| --- | --- |
| **业务场景** | 真实问题、领域约束与验收标准 |
| **任务与结果契约** | Schema、样例数据、校验器 |
| **GitHub 连接器** | 事件接入、去重、任务映射、状态回写 |
| **Worker** | 能力注册、任务领取、心跳、取消、结果提交 |
| **协调与故障恢复** | 任务依赖、租约、安全重试、拒绝过期结果 |
| **结果验证** | 产物检查、基线比较、独立复核接口 |
| **科研方法包** | 固定输入、环境、基线与检查规则 |
| **研发工作流** | 需求验证、测试与评审的 Playbook |

以上是建议方向，不代表已有对应 Issue。较大的改动请先开 Issue 对齐范围、接口与验收条件，并附上本地运行或测试方式。AI 辅助完成的贡献，与其他贡献一样需要审阅。

也欢迎业务与领域专家参与：提供问题与验收标准，贡献科研方法，或独立复核结果。

<details>
<summary>修改演示页面</summary>

中文文案在 `pages/src`，英文对照在 `pages/locales/en.js`。运行 `node pages/scripts/build.mjs` 重新生成两个页面。

</details>

---

<p align="center"><b>从一个问题开始，让下一份贡献更容易发生。</b></p>

<!-- 维护者发布检查：确定仓库许可证并添加 LICENSE；核实源代码归属和第三方声明；随实现推进，用已验证的发布状态替换原型说明。本稿不指定许可证、不承诺托管服务，也不虚构安装命令。 -->
