# Roadmap

本文回答两个问题：现在做到哪里，下一步做什么。运行时拓扑和不变量见 [ARCHITECTURE](ARCHITECTURE.md)，已定的技术选择见 [ADR](adr/README.md)，验收命令见 [fitness](fitness/README.md)，测试分层见 [tests/README.md](../tests/README.md)。

## 怎么用这份文档

- 只有里程碑里的条目在排期内。Backlog 是候选池，进入里程碑前要补上验收层级和矩阵格。
- 勾选一项时，在对应的 `docs/fitness/` 验收记录里写下版本、验收层级、日期和仍开放的缺口，并在条目后链接它。
- 新增能力写清角色、授权边界、预期结果，以及它落在支持矩阵的哪一格。
- 原则和不变量写在 ARCHITECTURE，不在这里设成待办。这里只写「实现并验证某条不变量」。
- 这份文件也是 Task 来源（ADR 0002）：标题提供上下文，勾选项是 SourceItem。改写条目时尽量保持原意和位置，避免身份漂移。勾选不等于 Verification 或接受。

### 验收层级

| 层级 | 含义 |
| --- | --- |
| L0 | 编译、单元测试、契约检查通过 |
| L1 | 开发构建在真实 Desktop 中跑通，Agent 使用测试 fixture |
| L2 | 使用真实 provider、真实环境或真实外部服务跑通 |
| L3 | 签名发布包在目标系统上安装、升级、运行通过 |

## 当前状态（2026-10-28）

M0 Bootstrap 已完成：Electron 桌面壳、GPUI Wasm 工作台、Rust Host（stdio JSONL，经官方 SDK 接 ACP）、TypeSpec 契约。Go 协调服务已经有 Task、Run、Worker 的 HTTP API，M1 的验收项还没勾完。见 [ADR 0001](adr/0001-electron-gpui-bootstrap.md)。

[ADR 0002](adr/0002-remote-task-dispatch.md) 已接受，定了后续顺序：远程分派 → 任务来源 → 验证与发布 → 环境与生产运维，对应下面的 M1–M4。ACP 执行留在 `pero-runtime`，协调服务是 Go + PostgreSQL，远程执行由独立的 `pero-worker` 承担。M1 已开始。

### 待决策

- [ ] `harness/` 自研 Agent 是否进入首期。Backlog「Host、工具与受管执行」里大部分条目依赖这个决定
- [ ] App 面板、插件界面和纯 Web 入口用什么技术。工作台已经是 GPUI Wasm，ARCHITECTURE 里的 React 规划只作背景
- [ ] `pero-worker`、daemon 和 Hub 三者的关系：是同一个常驻入口，还是分开
- [ ] 本机 Project 闭环的参照 Playbook。暂定「软件：需求验证」：输入是本地 Git 仓库加需求列表，逐条给出满足、未满足、无法判断
- [ ] 领域包是否拆开，以及 Task、Run、ContextSnapshot 的准入规则

决定之后用 ADR 记录，并同步 ARCHITECTURE 的目录结构和技术栈。

## 支持矩阵

只填写验收通过的格子，格内写最高验收层级和日期，并链接验收记录。`—` 表示不适用，空格表示未测试。

| 系统 | 角色 | 本机文件与工具 | 实验室服务器与仪器 | 云端计算 | 容器 / Pod | 桌面会话（Computer Use） |
| --- | --- | --- | --- | --- | --- | --- |
| macOS | Desktop 入口、本机 Host | L1 2026-10-28（[bootstrap](fitness/bootstrap-verification.md)）；ACP L2（[provider](fitness/acp-provider-verification.md)） | | | — | |
| Windows | Desktop、有界界面自动化、托管 Worker | | | | — | |
| Linux X11 | Desktop、便携包、实验室 Host | | | | | |
| Linux Wayland | Desktop | | — | — | — | |
| Kubernetes | 只读附加 Pod / Agent Sandbox | — | — | | | |
| 浏览器 | 使用入口 | — | — | — | — | — |
| Android / iOS | 配对与对话客户端 | — | — | — | — | — |

| 部署方式 | 当前 |
| --- | --- |
| 本地开发 | L1：Desktop 开发构建 |
| 自部署协调服务 | Task / Run / Worker 的 HTTP API 已接上，验收未完成 |
| 托管 Cloud | 未评估；身份、数据驻留和租户边界评估通过之后才写入矩阵 |

未接通的系统能力先由 Host 报告不可用，再逐项接上。Wayland 在有真实支持记录之前保持未测试；Windows 在接通前报告不可用。

## 里程碑

顺序来自 ADR 0002 的交付顺序。

### M0 Bootstrap（已完成）

macOS 本机，开发构建。验收见 [bootstrap](fitness/bootstrap-verification.md)、[ACP provider](fitness/acp-provider-verification.md)、[工具调用](fitness/acp-toolcall-matrix.md)、[i18n](fitness/i18n-verification.md)。

- [x] Electron 壳通过安全自定义 scheme 加载 GPUI Wasm 三栏工作台；渲染进程没有 Node 访问
- [x] Rust Host：继承 stdio 上的 JSONL，请求 ID 原样回传，空请求和超长请求被拒绝
- [x] TypeSpec Host 契约生成 JSON Schema 与 TypeScript 类型，`npm run check` 校验一致
- [x] ACP 多轮会话：新建、提示、流式回复、取消、关闭、认证、权限请求与审批卡、切换模型
- [x] 真实 provider：Codex、Claude 返回了指定回复
- [x] 工具调用紧凑呈现：读取 / 搜索 / 抓取分组、执行、编辑 diff、失败、取消、未知种类
- [x] 会话事件写入本机 JSONL，重启后重放
- [x] 选择工作区，附加文件作为上下文
- [x] 中英文案实时切换（界面、原生菜单），语言偏好经 Host 持久化
- [x] 浅色、深色、窄窗、Host 断线状态
- [x] Go 协调服务骨架编译通过

M0 留下的缺口：

- Grok（OpenCode ACP）连接成功，但被 xAI 认证阻塞，没有拿到回复
- 会话历史默认写在 `target/sessions.jsonl`，是开发路径
- 只验证了 Chromium IME 事件，没有测试物理输入法
- 没有签名包

### M1 远程分派

**目标**：Desktop 创建 Task 并显式分派；另一台获准机器上的 Worker 领取 Run 并执行；Desktop 能看到持久化的进度，也能请求取消。

**完成判据**：

- 在真实 PostgreSQL 上测试并发领取、响应丢失、重启后持久、授权、事件重放、过期租约被拒、取消竞态（L0）
- 独立的 Rust Worker 对 ACP fixture 跑通；真实 Electron 完成创建、分派、进度、取消、错误，以及窄窗和深色检查（L1）
- 真实 provider 和已部署 Linux Worker 的验收（L2）单独记录

首个切片使用显式配置的 operator 凭据和手工创建的 Task。不包括 GitHub 登录、公开多租户托管、不可信代码隔离、自动创建 PR 和生产验收。

协调服务

- [ ] `server/` 接入 PostgreSQL：Task、Run、Worker
- [ ] 用户令牌与 Worker 令牌的 scope 分开；由 operator 预置项目范围的 Worker；匿名注册和匿名执行关闭
- [ ] Task 写明目标和验收标准；每次显式分派创建一个 Run，带不可变的输入快照
- [ ] 在一个事务里领取 Run：绑定 Worker 和进程实例身份，发放有期限的租约；每个 Worker 先只有一个 slot
- [ ] 领取、事件批次、完成请求幂等；续租和新写入要求当前 Run、generation、Worker 实例和未过期的租约
- [ ] 租约结束后，已完成的请求仍能取回原回执；旧执行不能覆盖当前结果

Worker

- [ ] 独立的 headless `pero-worker`，通过出站认证 HTTPS 连接，笔记本不开放公网监听
- [ ] 执行前先持久化 Worker 状态；重启后对账原 Run，不盲目重放被中断的 prompt
- [ ] 丢失租约就停止执行；清理或效果未知时进入 reconcile，不自动重试
- [ ] ACP 取消之后做进程监管和有界清理；无人值守时拒绝待处理的 ACP 权限请求
- [ ] Agent 配置和允许的工作区由 Worker 本机控制；workspace key 映射到显式允许的目录，Run 期间持有独占锁
- [ ] Task 载荷不能选择任意路径或可执行命令
- [ ] 执行成功只记为 Agent 结果，不等于 Verification 或接受

Desktop

- [ ] 主进程内的有界 HTTP 适配器；凭据不进渲染进程
- [ ] 远程任务控制页：列出、显式分派、读取持久化的进度、请求取消
- [ ] 远程任务的加载、空、运行中、失败、已取消、断线状态

部署

- [ ] `app.openpero.com` 同源托管 Web 与 API，Cookie 路由；绑定地址、数据库 URL 和凭据都走配置，不写死域名
- [ ] Worker 的代码执行先放在协调服务以外的机器上

### M2 任务来源

**目标**：GitHub Issue 和 ROADMAP.md 进入同一个 Task 收件箱，再从收件箱显式分派到 M1 的 Worker。

**完成判据**：本仓库的 docs/ROADMAP.md 和一个 GitHub 仓库的 Issue 同步进收件箱；编辑来源之后 Task 身份不变；身份变化不明确的条目进入人工审阅（L2）。

- [ ] SourceItem 修订：GitHub Issue 和本地 / 仓库 ROADMAP.md 进入同一个 Task 收件箱
- [ ] Source 身份在编辑后保持稳定；Markdown 标题保留上下文，父级勾选项可以作为分组；身份变化不明确时进入人工审阅
- [ ] 导入内容不授予执行权限；来源上的「完成」不生成 Verification 或 Decision
- [ ] GitHub 适配器：账户、仓库、Issue、PR、检查与状态
- [ ] 事件身份校验、去重、映射成 Task；Task 的 GitHub 候选绑定和执行绑定
- [ ] 原生签名 webhook；入站 webhook 有大小上限，凭据留在 Host；在还不能经协调服务进入时，允许直连 Host
- [ ] 云端 Task 即使由本机 Worker 执行也经 Cloud 分派；纯本地 Task 保留本地权威
- [ ] 本地 Task 迁到 Cloud 时幂等交接，并对账正在执行的本地 Run
- [ ] 工作台 Task 收件箱：来源、待审阅、已分派

### M3 验证、回写与发布

**目标**：Run 的产物经独立验证得到 Verification，由人做出 Decision 之后，才回写来源或创建草稿 PR。

**完成判据**：一个参照 Task 走完「执行 → 独立验证 → 人工 Decision → 授权回写」，每一步留下回执；未经批准的回写和 PR 被拒绝（L2）。

- [ ] ArtifactVersion 登记与追加版本，绑定产生它的系统和环境
- [ ] delivery：serve，以及 submit、status、wait、cancel、report、verify
- [ ] Verification 记录，绑定产物版本，并记下执行环境
- [ ] 门禁：fingerprint、prepare、evaluate；分开记录业务结果、证据完整性和 BLOCK / ALLOW / Pending
- [ ] 本地证据目录落盘
- [ ] Decision：accepted、rejected、rework 三种人工决定
- [ ] 代取 GitHub CI，并记成 Verification（CI 环境写入记录）
- [ ] 经授权的来源回写：Issue 状态、ROADMAP 勾选
- [ ] Publisher：草稿 PR 写入，每次单独批准
- [ ] 工作台：产物目录；人工裁决；工作成果（验证摘要、缺失检查、Decision 文案）

### M4 环境与生产运维

**目标**：Run 按环境需求分派到兼容的 Worker；执行与验证各自申请环境 Use；自部署可以升级和备份。

**完成判据**：触发、Worker、环境 Use、独立 delivery 在一个参照组合上端到端跑通，达到 Linux X11 × 实验室服务器 L2；自部署的升级、备份和恢复演练通过。

环境

- [ ] 环境：Template、Instance、Use、不可变 Evidence 快照
- [ ] 授权、开始使用、调和、释放、归档；回收只结束这一次 Use
- [ ] Host 按配置装配环境；本地文件存储
- [ ] 容器 / VM 隔离和隔离的 Git 准备
- [ ] 把已注册后端接到 Use 的生命周期

分派

- [ ] Worker 注册时声明系统、环境类型、能力、数据驻留和隔离级别
- [ ] 协调服务只把任务分给声明兼容、授权仍有效的 Worker
- [ ] 目标环境不可用时，Run 进入 waiting、blocked 或 reconcile，并写明缺的是哪一个环境；替代环境单独获准
- [ ] 环境契约相同的两个受信任 Worker 可以互换执行同一类任务
- [ ] 每个 Worker 多个 slot；任务依赖与安全重试
- [ ] 进度从轮询换成可续接的 SSE

平台与运维

- [ ] Host 按当前系统报告每项能力可用或不可用；健康检查与元信息带系统与环境
- [ ] 支持矩阵做成仓库里的单一记录；能力探测、契约和矩阵使用同一套系统、环境、部署名称
- [ ] 跨语言契约中加入环境契约（系统、执行环境、部署方式的标识）
- [ ] 契约版本不一致时拒绝待处理复制，并要求升级
- [ ] 本地、自部署、托管 Cloud 使用各自的端点、凭据和功能开关
- [ ] 自部署的升级、备份和 journal 复制单独验收

## 下一批候选（未排期）

ADR 0002 之外、已经想清楚范围的两组。排期前先补完成判据。

### 本机 Project 与 Playbook 闭环

一位研究者在 macOS 本机，不依赖协调服务，跑通 Project → Task → Run → Artifact → Verification → Decision。本地 Task 保留本地权威。

- [ ] 本地 Project journal：内容寻址追加，operationId 幂等
- [ ] 按 revision 读取，以及 lineage
- [ ] Project 创建、编辑、更新定义、归档、恢复
- [ ] 通用 `generic` 领域包：Source、DomainObject、Relation 的新增与更新
- [ ] 关系类型：REFERENCES、DEPENDS_ON、SUPPORTED_BY、VERIFIED_BY、PART_OF
- [ ] 软件领域包的最小对象：repository、requirement；本地 Git 仓库解析
- [ ] 为 Task 设置 AcceptanceContract
- [ ] Playbook 最小模型：输入槽、步骤类型（Agent、Human、Tool）、Host 接线、门禁、结果路由、digest、环境需求
- [ ] 门禁可以带结构化检查（路径、比较算子）；没有绑定能力的步骤不能开跑；环境需求不满足时停止
- [ ] Playbook 声明影响、写入目标、人工确认步骤和预算，执行前由 Host 强制
- [ ] Run 启动时冻结 ContextSnapshot：输入版本、digest、执行系统、环境标识和兼容性摘要；状态包含 waiting、blocked、reconcile
- [ ] Run 记录 Harness 修订、Agent 策略、Skill / 插件版本、工具 schema 摘要，以及系统与环境标识
- [ ] Session 为本项目创建并绑定 Run；会话上的 playbook_step
- [ ] 步骤之间用产物引用交接，由 Host 接线；Playbook 步骤收据与门禁收据
- [ ] 受管执行：单写者租约、只追加的事件日志、continuation 绑定原生 session id；取消写入 canceled
- [ ] ACP session 的 load、list、resume；审批 ask / never
- [ ] 会话历史迁出 `target/`，放到用户数据目录
- [ ] 凭据：接通钥匙串之前只用内存凭据；接通 macOS 钥匙串
- [ ] 桌面冒烟测试能结束并清理
- [ ] 参照 Playbook「软件：需求验证」：逐条满足、未满足、无法判断
- [ ] 工作台：项目列表（全部、进行中、归档）；项目分区（概览、会话与 Playbook）；Playbook 库（浏览、在项目内运行）；运行证据（目录、运行列表、启动或仅准备，证据带环境标识）

### 协作与身份

两位研究者通过自部署协调服务共享同一个 Project。

- [ ] 浏览器登录：OIDC、密码、回调、登出
- [ ] OAuth 设备流、令牌与撤销
- [ ] 当前成员与可见 Project
- [ ] 账户节点：列出、自报、重命名、撤销已登录的 Host（节点带系统标识）
- [ ] 协调服务：初始化、启动、创建和删除 Host
- [ ] 创建共享 Project：上传 journal，首条必须是项目创建
- [ ] Project 成员、邀请码、共享链接；分享、加入项目
- [ ] 按变更类型的角色门禁：Owner、Editor、Operator、Reviewer
- [ ] Host 与协调服务之间的 Attach、Import、Pull、Push、outbox 和 Replica
- [ ] Project journal 复制：pull / push，支持长轮询
- [ ] 内容寻址 blob

## Backlog

按领域分组。条目进入里程碑前，补上验收层级和矩阵格。

### 跨系统

- [ ] 多个桌面壳共用同一个 Host 权威；壳与应用契约对齐
- [ ] 持久凭据：Android 系统密钥库、iOS 钥匙串（真机另验）、Windows、Linux
- [ ] 安装与更新：macOS 签名安装包（内嵌 Work）、Windows 桌面分发包、Linux 便携包的签名、更新和发行包格式
- [ ] 后台服务安装后的运行时验收：Windows、Linux 分开
- [ ] Shell 与进程按系统验收：bash；可选 pwsh
- [ ] 防睡眠、锁屏、路径、权限和显示按系统验收；锁屏时生产路径失败关闭；macOS 任务期间 Host 侧防睡眠
- [ ] Computer Use 按系统实现：macOS、Linux X11、Windows 有界界面自动化；远程 Computer Use 以目标 Host 报告的能力为准
- [ ] 推送分三条线验收：服务端可选 APNs / FCM 与 Host 通知投递；KMP 客户端自身的推送配置；浏览器推送订阅留在客户端（协调服务不提供该订阅）
- [ ] 浏览器登录态迁移：本机 Linux target（一次性精确目标授权、加密、空的浏览器调试上下文）；Windows 导入；企业 SSO、物理输入接管、具体业务站点；已部署的跨机中继
- [ ] 托管 Cloud 的身份、数据驻留和租户边界评估
- [ ] 固定输入与权限的评测重放，不触发外部副作用

### 交付控制

- [ ] 领域包：工厂软件、汽车工程；「论文证据综合」领域包
- [ ] Playbook 步骤种类：Compute，以及 App、Skill、Connector、Plugin
- [ ] Finding 由规则派生；Insights 由可信交付过程派生
- [ ] Intervention：打开、记录修复、发布、记录效果、关闭；修改下次使用的 Playbook、策略、Skill 或 Harness 时走版本化 Intervention
- [ ] 改进机制覆盖 agent-policy、skill、tool，候选保持自己的验收标准；修改技能或工具描述后在下一次 Run 生效
- [ ] 更多领域的影响型 Verifier：读真实状态，或在隔离副本上模拟。真实状态所在的环境写进支持矩阵
- [ ] 业务视图里的约束、授权声明和 Verifier
- [ ] 长任务协议（MCP Tasks、A2A）作为既有 Host 能力上的传输适配

### 研究项目

- [ ] 软件对象：issue、service、pr、release
- [ ] 汽车对象：vehicle、ecu、firmware、can-database、recording、test-campaign、sim-model、finding
- [ ] Session：导入已有历史
- [ ] 团队资产的采纳与撤回
- [ ] 共享 Session、共享文件 blob，以及撤回
- [ ] 规划记录：milestone、计划任务、风险、看板、状态报告、评论
- [ ] 按 feature 门禁
- [ ] 回收站、撤销遗忘
- [ ] 报告草稿留在 Host
- [ ] Pulse、日报和 AI 概览作为 Host 上的派生视图
- [ ] 项目列表：回收站、协同
- [ ] 分区：资料库、管理、团队与活动
- [ ] 资料库：图书馆、制品、文件、领域对象、知识、站点、Pages
- [ ] 分享、加入项目
- [ ] 项目站点发布
- [ ] 团队成员、策略模板与审计
- [ ] 活动时间线
- [ ] 共享会话与成员洞察
- [ ] Factory 的完整性检查、业务验证、Coding 发布和 GitHub candidate
- [ ] 生产身份、完整 Verification 登记、真实 SCM 强制门禁分开验收（各自对应部署方式）

### 知识、记忆、Pages 与渠道

渠道记忆的范围是已授权的用户和来源。普通聊天、Manager、自动化和 Project 使用各自的域。渠道记忆不覆盖个人私域、跨 Host 身份绑定或任意群历史。

- [ ] 团队知识：提议、审核后发布或拒绝、撤回；拉取时可以遮住正文
- [ ] 知识的列表、单条、recall、process、reflect
- [ ] 本机引擎上的 Memory 派生 recall
- [ ] Learning：jobs、observations、pages
- [ ] 工作台知识区，含飞书候选
- [ ] 个人和项目记忆：候选经过人确认后才进入后续 turn
- [ ] 记忆页区分 personal、projects 和会话范围，支持导入；侧栏记忆入口
- [ ] Session 级记忆和项目团队知识分开存放
- [ ] Project Memory 自己的 retain、recall、reflect
- [ ] 默认不导入个人外部记忆；渠道偏好留在渠道域
- [ ] 共享 Project Pages：协调服务为权威；CRUD、更新、回执、导出、实时协作
- [ ] 未共享项目的本地 Pages：单写者
- [ ] 协调服务和 Host 两侧的 Markdown / 富文本 Pages；Pages 应用面板
- [ ] 渠道共享记忆：检索、读取、保存，以及授权范围内的历史检索；只覆盖已授权的用户和来源
- [ ] 渠道记忆命令：accept、forget、use、learn、nolearn；记忆存储上的 forget 与 exclude-learning
- [ ] 显式交付：按渠道发消息，持久 outbox，回合结束停止接纳新消息；发送结果不确定时标为 ambiguous
- [ ] 渠道页：微信、企业微信、飞书、钉钉、Teams 等的连接、二维码、草稿和共享记忆导出
- [ ] 飞书资料可以插入 Composer，停留在待发送
- [ ] 渠道上下文侧卡，挂在共享主会话旁边
- [ ] 渠道 reaction、选项、等待用户后续、通知授权、第三方草稿卡
- [ ] 飞书 @owner 的真实群发送、历史权限和通知卡片

### 账户与团队

- [ ] 注册、邮箱验证、密码重置
- [ ] 账户资料的读取与更新；账户 journal 同步
- [ ] 账户租约：列出、获取、释放
- [ ] Host 侧账户同步：连接、断开、节点、集合与冲突解决
- [ ] 工作台里的云端账号和云工作区会话标记（按部署方式区分）
- [ ] OpenID 一致性认证
- [ ] Team Workspace：创建、重命名、成员、邀请、挂载 Project、待审批的 attach
- [ ] 管理员限时取得已挂载 Project 的角色，并留下审计
- [ ] 团队策略的读写和策略回执；Project 策略回执由 Host 上报；Project 策略只读接口
- [ ] 团队级 Insights 与 Findings；管理概览
- [ ] 发布站点 API 和 deploy 后端声明
- [ ] 部署之后的团队策略回执和采用信号
- [ ] 从 Issue、研究者委托或项目变化产生的建议进入
- [ ] GitHub 其余对象：分支、比较、提交、树、blob、评论、评审
- [ ] 内部运行按需发布 Issue：需要讨论、独立贡献或公开评审时再发布

### 远程接入

远程链路承载获准的任务控制与只读预览。桌面视频流和鼠标镜像不在这条链路里。

- [ ] 设备配对（M1 由 operator 预置 Worker，不需要配对）：出站 connect、pair、devices、revoke
- [ ] 二维码或手输地址加一次性码完成配对；配对码短时过期；设备凭证有期限；撤销后清理
- [ ] 恢复已配对设备；Host 侧撤销设备
- [ ] 设备续租；每设备并发 grant 有上限；carrier lease 有最长时间
- [ ] 把 HTTP / WebSocket RPC 转到官方 Host
- [ ] Worker SDK：供第三方 Worker 使用的能力注册、任务领取、心跳、取消、结果提交；注册信息包含系统与环境
- [ ] 只读浏览器预览：显示器列表、view、display-source
- [ ] 可选 local-executor：已配对的控制器可以调用
- [ ] Desktop 远程管理
- [ ] 本地浏览器连接器：只暴露刚授权的当前标签和顶层 origin；具体业务站点的接受记录

### Host、工具与受管执行

插件走显式安装路径。managed 执行是可替换后端，沙箱、多租户 worker 池和全局 Kubernetes 编排各自有单独的环境任务。

以下条目依赖「Harness 的位置和语言」的决定。

- [ ] 子命令：chat、task、acp、web、models、plugin、channel、channel-session
- [ ] 运行参数：模型、端点、设置、凭据、附件目录、审批策略、沙箱、trace
- [ ] Profile：minimal、headless、acp、web
- [ ] 模型目录、可配置 provider、发现模型、设置热更新
- [ ] 主模型路径支持 OpenAI 兼容 Chat Completions；另有 Anthropic 兼容的 web_search
- [ ] 匿名 web_fetch 带 SSRF 边界；非 loopback 的 web 入口默认关闭
- [ ] 基础工具：ask_user_question、read、read_file、write、edit、str_replace_editor、glob、grep、list_directory、working_directory、todo_write、skill、workflow
- [ ] bash；在需要的系统上提供 pwsh
- [ ] read_image（有附件存储时）；web_fetch、web_search
- [ ] create_goal、get_goal、update_goal、exit_plan_mode
- [ ] subagent、subagent_fork、list_agents、send_message、interrupt_agent
- [ ] job_list、job_output、job_kill、ralph
- [ ] 斜杠命令：/compact、/feedback、/goal、/permission、/plan
- [ ] 能力开关：默认模型、压缩、反馈、目标、作业、权限预设、计划模式、沙箱、技能、子 Agent、遥测、待办、工作流、工作区
- [ ] Agent Teams（显式打开并持久化会话）：spawn_teammate、wait_agent、team_task_create、list、get、update
- [ ] 终端 PTY：terminal_open、send、read、signal、close、list（按系统验收）
- [ ] 外部子 Agent：Codex、Claude、ACP，名称可改；列出子 Agent 模型；子 Agent 内的 structured_output
- [ ] 日程：创建、列出、删除
- [ ] LSP 示例：lsp、lsp_diagnostics（示例应用）
- [ ] MCP 资源：列出资源、列出模板、读取资源
- [ ] 插件里运行代码，以及动态投影的 MCP / Host 工具
- [ ] MCP 客户端：stdio、Streamable HTTP、重连、工具列表变化通知
- [ ] Pero 作为 ACP Agent：initialize、authenticate、session 的 new、load、list、resume、prompt、cancel、close；session/update 推送与 session/request_permission；可由父进程以 stdio 拉起
- [ ] 远程命令：列出、执行
- [ ] Web 上的一次性审批
- [ ] 计划模式走提问流；headless 在需要提问时明确返回没有提供者
- [ ] 多 step 的单轮；工具按能否并发决定独占或并行
- [ ] Web 上的会话 fork、归档、置顶、重命名、删除
- [ ] 耐久 inbox：下一步、下一轮；子 Agent inbox 与取消
- [ ] Compact、基础压缩、工具结果裁剪
- [ ] Workspace 注册表：隐藏和排序会话
- [ ] 工作区文件只读与变更列表
- [ ] managed：run、serve、show、ls、daemon
- [ ] managed agent：run、spawn、list、inspect、output、close
- [ ] managed mail：send、receive、ack
- [ ] managed mcp：把 agent、mail、batch 暴露成 stdio MCP
- [ ] 后端：echo、process、claude、codex、remote（ACP 已在 M0）
- [ ] 常驻会话空闲回收；过期执行返回后重开
- [ ] 可选的分布式后端；默认仍是本地 JSONL
- [ ] 用重复规则在墙钟时间触发耐久 Run（时区与部署环境分开配置）
- [ ] 运行时：挂载与释放、状态机、等待稳定、类型化依赖、JSON 配置、目录、逆序清理、Signal / Query / Pipeline
- [ ] Desktop Host 装配：Windows、Linux
- [ ] 另一套 Harness 按同一份契约对齐
- [ ] Web 工作室共用同一套静态资源
- [ ] Workflow 的持久 run store

### 会话与自动化

Agent 准备下一步：数据版本、运行完成、检查失败或新文献发生变化时，说明影响，并提出一项具体任务。建议遵守项目权限、预算和当前环境的能力。

- [ ] Web 创建会话、搜索、查看事件流、提示、取消、恢复、分叉
- [ ] 切换 Agent preset、技能和斜杠命令（切换模型已在 M0）
- [ ] Goals 与 Team 任务
- [ ] Jobs 的列表、终止和事件流
- [ ] 权限预设目录
- [ ] 插件、Agent 插件、Agent preset 和插件包管理
- [ ] 设置、凭据，以及产品 RPC 投影
- [ ] 项目级和全局 Automations：列表、编辑、立即运行；重复规则、时区、忙时策略；自动化页的列表和模板
- [ ] Webhook：管理、轮换密钥、目标、运行记录；Webhooks 页
- [ ] 过滤器试跑与回测；会话前过滤
- [ ] 入站钩子：agent、GitHub，以及总开关
- [ ] 最小事件自动化同时具备触发源和完整事件
- [ ] Task Inbox：本地、watch、日历
- [ ] Pulse：Brief、提醒、活动、历史、Watches、已学习、设置
- [ ] Brief 中的待审批、待回答、冲突和运行中会话；可以一次允许或拒绝
- [ ] 收件项：完成、忽略、稍后；Focus / View
- [ ] 提醒、Learned、审计和工作回顾
- [ ] 侧栏待处理角标
- [ ] 各页保留自己的搜索；全局命令面板随后接上

### 工作台

Web 是进入同一项工作的界面之一。布局在窄屏和桌面宽度下都要可用。

- [ ] 深链接：会话、项目、收件箱、产物、插件页、远程任务
- [ ] 窄轨：新建对话、Projects、Pulse、Playbooks
- [ ] 侧栏在工作区和 App 之间切换
- [ ] 工作区：会话筛选、搜索、重命名、分支、归档
- [ ] App：按 Pack 分组、拖拽排序、打开 App 的首个面板；Apps 访问门控
- [ ] 插件可置顶到侧栏（有上限）
- [ ] 侧栏：渠道、设置、云账号、本机或远程位置（位置展示系统与环境）
- [ ] 侧栏、会话和 Dock 宽度可拖；窄屏用导航抽屉，并在会话和产物之间切换
- [ ] 空状态欢迎、浮动对话（收起、仅输入、完整对话）
- [ ] App 面板可隐藏主聊天，并使用独立的 App 会话
- [ ] Manager 协调会话任务板
- [ ] 子 Agent 会话路由
- [ ] 路由加载失败可重试或回首页
- [ ] 会话详情侧栏；有待交互时自动展开聊天
- [ ] Composer 里用 @ 引用 Agent 插件、连接器和浏览器标签；引用以 chip 进入草稿
- [ ] 扩展菜单：搜索已安装项、管理插件和连接器
- [ ] App 上下文摘要；App 可向 Composer 注入附件
- [ ] 模板只写入草稿；模板面板：技能起始提示
- [ ] Dock：多 Tab、关闭、空态打开菜单、启动器搜索
- [ ] 快捷键：终端、文件、浏览器、审查、模板
- [ ] 终端面板（多 shell）
- [ ] 项目文件面板；Wasm 壳中的文件视图
- [ ] 浏览器面板，可画中画
- [ ] 桌面面板（容器或远程桌面），可画中画
- [ ] 变更审查：候选 diff，打开、放弃或提交
- [ ] 工具页：内置面板，并跳到 Apps、插件、连接器、Playbooks
- [ ] 设置：通用项与模型，加上 Profile、同步、连接器、Agent 插件、自动化、Webhooks、渠道、远程主机
- [ ] 快捷设置：手动切换浅色、深色、跟随系统（当前只跟随系统）
- [ ] 对话区左右对调、面板全屏、隐藏 Dock
- [ ] 主题注入会话
- [ ] 原生审批出现时暂停嵌入的原生界面
- [ ] 桌面左轨：会话、Discover、App 面板、从模板新建 App
- [ ] App 槽会话
- [ ] 独立的 MCP / Resources 面板

### 方法、技能与 Playbook

科研方法要能打包：输入、环境、预期输出和检查规则放在一起。软件、工厂和汽车领域的 Playbook 用同一套任务和验收。没有对应后端绑定时，Playbook 停在未完成。确定性 Verifier 之外的检查仍要人工核对。每个科研技能在勾选时写上它要求的执行环境，换环境要另验。

- [ ] 按已安装技能加载 skill
- [ ] Playbook 库：切换、节点图例
- [ ] 工厂：需求规划、问题调查、代码评审（只评冻结的源码或 diff）
- [ ] 工厂：改动修复（限定写入和审批，缺少接线则停止）
- [ ] 工厂：代码实现（隔离工作区、固定检查、草稿 PR 仍要单独批准）
- [ ] 汽车：CAN 基线对比
- [ ] 汽车：固件回归检查、DBC 变更影响、仿真与实测相关性（仿真环境与实测环境分开记录）

### 应用与插件

内置应用由 Host 发现、构建并安装。用户卸载后留墓碑，可以从目录恢复。开发时可以监视应用源码；打包只拷贝已构建载荷。

Pack 只预览并启用一组 App、插件和连接器身份。Pack：通用、科研与实验、架构与现代化、车载与汽车电子、设计与建模。

应用可以贡献面板、Agent 工具、Agent 插件、示例和 Host 权限。Agent 写入每次要用户批准，并返回有界回执。观察和引导里的提案保留为提案，直到人接受。

- [ ] Welcome
- [ ] Calendar：月、周、日、列表；真机上的系统权限验收（按系统）
- [ ] Map：地点、分日行程、路线、瓦片
- [ ] CAD Studio：GLB、STEP、STP、P21、FCStd；可见性、线框、剖切、视图、结果场着色
- [ ] CAD：FCStd 打开带结果场的文档。精确曲面测量作为单独能力
- [ ] Data Lab：CSV 质量、描述统计、OLS、分组比较、同 ID 对账、固定方法重跑
- [ ] AI4S Lab：分子、电池、性质预测、结构与晶体等请求保持不可变，证据可导出
- [ ] Scientific Viewer：曲线、表、对比和结构查看
- [ ] Paper Agent：公开文献、不可变快照、带引用的报告、显式创建 Project Page、导出
- [ ] Virtual Cell：细胞图谱、扰动和有预算的候选提案分开记录
- [ ] Virtual Cell：真实模型、可信隔离、独立实验、远端算力、机制模拟，分开验收并写入环境格
- [ ] Factory：同意后的 Issue 输入、隔离编码、精确候选检查、显式批准的草稿 PR、验收报告、站点构建和静态交互检查
- [ ] GitHub 交付面板
- [ ] 文件类示例应用
- [ ] computer-use：在受管环境里跑程序内核（环境写入记录）
- [ ] presentations
- [ ] record-replay：原生录制、workflow creator、语义回放；覆盖 Host 浏览器和远程桌面
- [ ] data-agent
- [ ] architecture

### 连接器

托管 MCP：内置、stdio、Streamable HTTP。MCP 应用使用隔离的 HTML 界面。自定义来源：本地 stdio 进程、远程 OAuth、远程令牌、内置服务。每个连接器注明它依赖的系统和环境；Windows 上的 MCP 在接通前保持不可用。

- [ ] Microsoft 365 只读：账户、邮件、日历、云盘
- [ ] GitLab：用户、项目、仓库、Issue、MR、流水线、作业和 trace
- [ ] Linear、Gmail、Figma、Notion、Vercel
- [ ] 飞书工作空间
- [ ] 渠道传输：微信、企业微信、飞书、Slack、Teams、Discord、钉钉
- [ ] FreeCAD、OPC UA、Foxglove、FMU、MATLAB、Simulink（本机或实验室环境写明）
- [ ] Ansys Mechanical、Fluent、MAPDL、Electronics Desktop 的本地发现
- [ ] Jupyter、R、MLflow、Galaxy、Wolfram、Weights & Biases、Autodesk Fusion
- [ ] BioMCP、Open Targets、eLabFTW、BioContext、Zotero、Scite、Synapse
- [ ] 租户目录：HighByte、Seqera、LabKey、Benchling（租户与部署方式分开配置）
- [ ] 科学计算查询目录
- [ ] 公共数据目录
- [ ] 连接器页可试用，并把提示写入 Composer
- [ ] Pulse 里可嵌连接器配置，并同步 MCP、日历和个人源
- [ ] Linux 便携环境中的连接器、渠道、收件箱和 MCP 应用
- [ ] Windows 上的 MCP
- [ ] 目录可见、接通后才可安装：KiCad、Onshape、COMSOL、PyVISA、PyLabRobot、Carbon、OpenBOM、Nominal、Quilt、Wiley、BioRender

### 桌面、浏览器与设备

Desktop 是使用入口，也是一台可以执行获准任务的机器。计算可以留在个人电脑、实验室服务器或云端。

- [ ] Work：一侧是工作台，一侧是可见浏览器，分隔可拖
- [ ] 浏览器给人用：标签、地址栏、前进后退刷新、Profile、Cookie、扩展
- [ ] Browser Use：经私有 broker 操作已有标签
- [ ] 演示文稿运行时：打开、快照、操作、验证、提交、关闭
- [ ] 设计输入和可编辑的演示文稿导出
- [ ] 图表：声明式文档，自包含片段
- [ ] 数据快照：按 digest 和 query 重开，多视图不复制行
- [ ] 数据证据：来源和方法收成有界收据（含环境）
- [ ] 标注：意图、锚点、笔迹和证据，绑定会话
- [ ] Surface Graph：页面、辅助功能树、语义对象和三维的不可变观察
- [ ] 从文件管理器打开
- [ ] 从 Chrome 系导入 Cookie / localStorage，可撤销
- [ ] 受控的浏览器扩展，按 Profile 开关
- [ ] MCP 连接器设置窗口
- [ ] 原生终端；Harness 替换后会话仍在
- [ ] SSH 远程项目：配置、打开、写、读；远端 Harness（远端系统记入矩阵）
- [ ] SSH 到 VNC 的远程查看：严格校验主机，路数有上限
- [ ] 本地容器桌面：镜像和分辨率
- [ ] Kubernetes 上的 Linux 桌面：单机 Pod，可选托管权威；人工接管租约
- [ ] 日历、通讯录、提醒事项只读；通讯录保存姓名与联系字段
- [ ] 终端、Office、执行、Mobile 的本机服务（按系统迁移接上）
- [ ] Agent JavaScript Runtime：browser、computer、mobile、remote 适配，派发路径归 Host
- [ ] Program cell：运行、重置
- [ ] run_code：每次一个带权限的受监护进程
- [ ] code-mode sidecar：工具留在父进程，有界双工

### 多端客户端

能配对只覆盖配对这一格。凭据迁移、离线草稿、锁屏和睡眠各自勾选。

- [ ] 客户端分层：protocol、core、data、ui；客户端的 protocol 和 core 测试
- [ ] Android：扫码配对
- [ ] iOS：扫码；真机网络和钥匙串单独验收
- [ ] 相机扫码的真机验收（Android、iOS 分开）
- [ ] 桌面客户端：可展示配对码，可打分发包
- [ ] 多轮流式对话、队列、审批、恢复、附件、搜索、切换模型
- [ ] 官方工作台与 Host 能力状态共享
- [ ] 实验用的移动壳：配对和聊天（保持实验范围）
- [ ] 移动端凭据迁移
- [ ] 移动端离线草稿
- [ ] 移动端锁屏和睡眠策略
- [ ] 编辑器集成：拉起本机 Host、Agent 和 ACP；发布前固定到可追溯的版本（M1 之后）
- [ ] 多个桌面壳、实验移动壳和编辑器集成在收敛前按各自的矩阵格验收

### 环境、验收门禁与部署

配方脚本、高可用多副本、不可信代码沙箱和自动生产发布如果要做，各自另开任务。

- [ ] 校验配方、冻结、导出构建输入
- [ ] Kubernetes 只读附加 Pod / Agent Sandbox，并解析身份
- [ ] SandboxClaim 在真实集群上验收
- [ ] web-verify 和浏览器测试用例
- [ ] macOS 桌面环境的准备、销毁和桌面租约
- [ ] Windows 桌面环境
- [ ] 镜像构建、产物登记、凭据和界面发布仍归 Host
- [ ] WarmPool、工作负载的供应与销毁、分布式存储、Use 迁移、自动业务验证、slot 共享
- [ ] 验证 profile、target 和进程组；HTTP API 和统计
- [ ] 工作流的静态理解：GitHub Actions、Jenkins Declarative、Argo 清单
- [ ] 可选的本地 Helm / Kustomize 渲染，只跑管理员显式信任的程序
- [ ] 把已注册后端接到 Use 的生命周期
- [ ] 复用项目权限和冻结的 Run，连接 CI 身份（CI 环境写入 Verification）
- [ ] 受限的 Web 验收配方，结果由独立读取核对
- [ ] 浏览器操作交给既有 Harness；验收事实来自检查结果
- [ ] 单项目组装、不可变文件授权存储、显式恢复原来的 Use
- [ ] 未知释放保持未重试，留给操作员处理
- [ ] 单进程有界调度、本地持久化、owner / tenant 回执隔离、租户配额
- [ ] 提供 verifier、报告界面和登录
- [ ] 门禁带 SCM 发布；本地 ALLOW 在可信发布者生效前记为建议
- [ ] 脚本浏览器、真实模型、远端证据读回和 SCM 强制门禁分开验收
- [ ] daemon：check、serve、status、run、output、close、reconcile（和 `pero-worker` 的关系见待决策）
- [ ] 多工作区共享检出、本机控制令牌、并发 turn 上限；后端默认 ACP，也可选 Codex
- [ ] 中断后的 turn 标为 unknown；reconcile 记下操作员笔记
- [ ] Hub 注册、远程接入、Project / Run 映射、环境所有者、隔离工作区、跨机池
- [ ] Desktop、协调服务的统一装配，以及跨机执行绑定（绑定写明两端的系统与环境）
- [ ] 本地集群和 Linux 桌面镜像
- [ ] 托管 Session：分离后可以恢复
- [ ] 可选的账户和浏览器 Profile 持久卷（按部署方式）
- [ ] 集群内 Host 和集群内 daemon 环境
- [ ] AAOS 镜像和设备插件；本机 ARM64 环境说明
- [ ] 外置 Browser Host 和中继连接器的发布
- [ ] Cuttlefish 上的 AAOS 启动兼容
- [ ] 车辆运行时上移为车辆信号能力，适配器接在同一接口后

### 契约与工程

- [ ] Harness 契约锁定生命周期不变量和可执行样例
- [ ] Profile 契约
- [ ] 包层归属；依赖只允许向下
- [ ] 架构图由脚本生成；虚线表示尚未落地
- [ ] 运行时、Harness、桌面、项目核心的测试
- [ ] 独立模块的测试不依赖总工作区
- [ ] 构建只编译列入清单的源文件
- [ ] 应用由 Host 拥有的构建管道产出，复用单一前端运行时
- [ ] Web 原语留在 SDK 内
- [ ] 工作台公共组件有固定位置
- [ ] 依赖检查保持有效

## 边界

这些保持为边界，不设成待办：

- 不受限制的个人电脑或实验室服务器留在获准任务范围内，不接入不可信的公共任务流
- 首期范围是获准 Worker。匿名开放算力网络留待单独决定。第三方 Worker 需要明确的信任、隔离和数据访问范围
- 协调服务的账户模型保持在项目与团队共享所需的范围，应用层端到端加密留待单独决定
- 设备中继的范围是配对、转发和撤销。凭证轮换、帧级加密、桌面视频流，以及打包运行时的生命周期，留在各自的系统里
- Issue 正文、PR 和任务载荷是不可信输入。提出任务和获得执行权限是两步
- 计算可以重试，外部副作用不重试。重复事件要能对上原来的 request ID

## 术语

| 术语 | 含义 |
| --- | --- |
| Host | 执行端权威：本机文件、工具、凭据、进程和 Agent 会话都归它 |
| 协调服务 | 自部署或托管的 Go 服务：Project 权限、共享、Task 分派、产物登记与 Decision |
| Worker | 向协调服务声明能力、在限定权限内执行获准任务的 Host |
| daemon | Host 的常驻部署入口 |
| Run | 一次执行尝试，冻结 ContextSnapshot，记录执行身份和环境 |
| Use | 一次 Run 或验证对环境资源的使用；执行与验证分别申请（见 ARCHITECTURE） |
| delivery | 独立验证协议：提交候选、执行检查、返回回执 |
| Playbook | 打包的方法：输入槽、步骤、门禁、结果路由和环境需求 |
| Work | Desktop 里工作台和可见浏览器并排的视图 |
| Pack | 一组 App、插件和连接器身份，只负责预览和启用 |
| Pulse | 汇总 Brief、提醒、活动和待处理事项的视图 |
| Factory | 软件交付流程：同意后的 Issue 输入、隔离编码、候选检查、批准后的草稿 PR |
| Manager | 协调多个会话任务的会话 |
| Hub | 待定义：daemon 注册的目标，是否就是协调服务需要在 M3 前确定 |
