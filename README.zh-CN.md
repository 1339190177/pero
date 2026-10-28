<p align="center">
  <img src="docs/assets/openpero-mark.svg" width="72" alt="Pero 标志">
</p>

<h1 align="center">Pero</h1>

<p align="center">
  <b>可组合的科研应用与分布式 Agent 协作。</b><br>
  把领域工具做成应用，独立交付，也能组合进共享工作台。<br>
  随着工作展开，连接研究者、Agent 与算力。
</p>

<p align="center">
  <a href="DEVELOPMENT.md">开发指南</a> ·
  <a href="#当前状态">当前状态</a> ·
  <a href="#工作方式">工作方式</a> ·
  <a href="#仓库结构">仓库结构</a> ·
  <a href="#参与贡献">参与贡献</a> ·
  <a href="README.md">English</a>
</p>

<p align="center">
  <img src="docs/assets/openpero-overview.zh-CN.svg" alt="Pero 工作流：研究问题整理成任务契约，由研究者、Agent 或算力完成，运行被记录，复核后的成果回到项目记忆" width="900">
</p>

> [!NOTE]
> **项目处于早期阶段。** Desktop 工作台、共享任务和远程 Worker 已经可以运行。多 Agent 编排、结果验证，以及完整的 AI4S 工作流仍在建设中。Virtual Cell 已可构建为独立 macOS 应用；安装包仍属开发构建，尚无正式发布版。

## 为什么做

一个有用的科研工具，往往从脚本、Notebook 或模型 Demo 开始。把它交给别人使用，还需要领域界面、可复现的运行环境、持久数据和交付方式。再加入 Agent 辅助或团队协作，又要处理执行与协调。

**Pero 以领域 App 作为可复用的组合单元。** 从一个具体工作流开始，例如探索细胞图谱或模拟化学反应：可以打包为独立应用，也可以将同一个 App 放进工作台，与其他工具一起使用。App 复用平台提供的壳、SDK 和 Host 运行时，让领域开发集中在方法和使用体验上。

随着工作展开，Pero 的目标是将这些 App 接入 Agent 会话、获准算力与共享项目。无论由研究者、Agent 还是计算程序执行，方法、输入、输出与检查规则都应清楚可查。目前产品装配已可用，App 与 Agent 工具的接线，以及任务到结果验证的完整闭环仍在建设中。

| 需求 | Pero 的方式 |
| --- | --- |
| 交付一个实用工具 | 将领域 App 打包为独立产品 |
| 组合多个工具 | 在 Desktop 工作台中打开不同 App |
| 加入 Agent 辅助 | 复用 Host 管理的会话与权限；App 接线是下一步 |
| 扩展到团队科研 | 通过可选控制面连接任务、算力与复核后的成果 |

## 当前状态

| 能力 | 状态 | 文档 |
| --- | --- | --- |
| Desktop 工作台：Electron + GPUI/WASM、ACP 对话、中英文、主题 | 🟢 开发构建可用 | [desktop](desktop/README.md) |
| 本地 App，首个为 Virtual Cell | 🟢 首版 | [apps](apps/README.md)、[ADR 0007](docs/adr/0007-local-app-platform.md) |
| 独立 App 产品：Virtual Cell + 随包 Python | 🟡 macOS arm64 开发包已验证 | [产品模型](docs/adr/0008-product-distributions.md)、[验收记录](docs/fitness/products.md) |
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

### 可组合的 App 与产品

组合分为两个范围：**产品装配已经可用**；领域方法、Agent 工具与结果验证的组合是下一步。

| 概念 | 拥有的职责 | 当前实现 |
| --- | --- | --- |
| **App** | 领域界面、数据与可选 Python 后端 | `apps/<name>/app.json`，首个实例为 Virtual Cell |
| **Product** | 产品身份、版本、App 清单、壳、入口面板与 Python 策略 | `products/`，由 `packages/product` 校验 |
| **Distribution** | 面向平台与架构组装的壳、Host、所选 App 和运行时 | `target/products/`，当前仅构建本机目标 |
| **Host runtime** | App 安装、启停、后端进程与 ACP 会话 | 两种壳复用同一实现；各独立产品启动自己的 Host 与配置目录 |
| **控制面** | 共享任务、认领、Run 与远程 Worker | 可选的 `server/`；独立 App 无需它即可本地运行 |

工作台通过 GPUI/Wasm 导航打开 App；独立产品选择固定面板，使用全窗口 Electron 壳，无需 GPUI 导航。两者复用 App SDK、私有后端代理与 Host 生命周期。Product 组合交付入口，领域行为保留在 App 中。

整体方向借鉴 **PBC（Packaged Business Capabilities，封装业务能力）**：围绕领域目标组合功能，明确输入、输出与检查规则。完整领域包可以组合 App、方法、Agent 工具与验证规则；目前实现的是 App 与产品装配，清单尚不能组合这些工作流契约。后续接线继续复用 Host 执行与控制面权威。当前模型和演进边界见[可组合应用与产品](docs/ARCHITECTURE.md#可组合应用与产品)。

### 运行时架构

![Pero 架构：客户端、Cloud 协调、Host 执行、能力生态、执行环境与可信交付](docs/assets/openpero-architecture.svg)

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
| `apps/` | 领域 App：清单、界面、可选后端与证据 |
| `packages/app-sdk/`、`packages/product/` | 共享 App API/控件与不可变 Product 模型 |
| `products/` | 工作台默认配置与独立产品定义 |
| `scripts/build/` | App 编译、产品装配与 Desktop 打包 |
| `docs/` | 架构、ADR、路线图、验收记录；同时是 openpero.com 站点 |
| `deployment/` | Task Center 主机安装脚本 |

## 开发

环境准备、本地开发、独立 App 打包与验证命令统一见 [DEVELOPMENT.md](DEVELOPMENT.md)。

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
