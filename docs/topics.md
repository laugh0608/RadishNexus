# 能力专题地图

日期：2026-11-01。用途：将长期产品目标、六条能力主线、已有子专题与任务 / 验收来源连接起来。读者为项目所有者、实现者和验证者；本页不修改领域、权限、公共协议或已接受 ADR，不复制完成状态。

[产品定义](product-definition.md)与[路线图](roadmap.md)构成整体规划；本页按能力组织大专题，原 ADR、架构文档和使用 / 运维手册承载具体子专题。M0–M7 是阶段范围与退出条件；纵向切片可以穿过多个阶段，但不因此宣布整个阶段完成。现有目录与编号继续使用，不另建一套 feature 目录或任务卡库。

当前任务顺位、阻塞、暂缓及剩余范围只见[当前状态](status/current.md)。细项完成标准见原合同，实际结果进入 status/reviews；历史快照只保留当时事实。新任务确定主要归属后沿对应专题细化，不在地图、current 和记录三处维护同一份勾选清单。

<a id="identity"></a>

## 身份、权限与管理

目标：让成员在明确作用域内进入、协作与交接管理责任。拥有账户 / Session、成员资格和管理权生命周期；引用、关系和客户端投影均不得授予权限，业务对象复用既有授权语义。

| 子专题与原合同 | 阶段 / 切片与验收来源 | 协同 |
| --- | --- | --- |
| [本地身份与未来 OIDC：ADR-0023](adr/0023-local-account-and-radish-oidc-login.md) | M1；本地与真实 provider / Radish 联调分别验收 | 全部产品入口 |
| [基础配置：ADR-0026](adr/0026-foundation-configuration-and-membership.md)、[首次初始化：ADR-0027](adr/0027-first-visit-administrator-setup.md) | M0.5 / M1；各合同的初始化、权限与失败恢复矩阵 | 工作台、部署 |
| [账户恢复：ADR-0041](adr/0041-controlled-local-account-recovery.md)、[owner 交接：ADR-0042](adr/0042-workspace-owner-handoff.md) | 原 CLI / 管理切片与审计 / receipt；[恢复证据](status/reviews/2026-10-18-controlled-account-recovery.md)、[交接证据](status/reviews/2026-10-18-workspace-owner-handoff.md) | 运维、历史访问 |
| [成员生命周期与管理交接：ADR-0045](adr/0045-membership-lifecycle-and-management-handoff.md) | G01–G22 沿原合同及[实际记录](status/reviews/2026-10-24-membership-lifecycle.md) | Ticket 资格、私密对象与上下文导入 |

完成按原合同覆盖当前资格、撤权、并发、精确重试和恢复；隔离技术验收不代替真实团队持续使用，也不开放未实现的全局应急接管。

<a id="communication-decisions"></a>

## 沟通与决策

目标：将讨论及其理由保留为可追溯的决策。拥有 Message / Thread、Decision 的来源与生命周期；后续 Ticket 仍拥有自己的执行状态，不由原讨论或决策变化隐式改写。

| 子专题与原合同 | 阶段 / 切片与验收来源 | 协同 |
| --- | --- | --- |
| [讨论到决策 / 工单的传输：ADR-0019](adr/0019-session-scoped-thread-decision-ticket-transport.md) | M0.5 / M2；原允许路径、Session 与权限矩阵；Ticket 创建是跨主线交接 | 身份、工单 |
| [Project 决策发现：ADR-0040](adr/0040-project-decision-discovery.md) | 原只读切片及[验收记录](status/reviews/2026-10-18-project-decision-discovery.md) | Project 导航、工单来源 |
| Decision 拒绝、替代与复核 | [当前接续](status/current.md#接下来的事项)负责待设计范围；尚无新 Accepted 合同，不给未来切片预造编号或完成状态 | 已建 Ticket、Document、Activity 与上下文包兼容 |

完整聊天、通知、复杂搜索等仍按路线图范围推进。Decision 生命周期先明确触发、理由、历史与兼容，再通过正式决策；本次归属整理不授予实现权。

<a id="tickets-context"></a>

## 工单与研发上下文

目标：让执行责任、状态及来源可从正式入口发现与追溯。拥有 Ticket 执行管理及研发对象的稳定关联、EntityLink / Activity / Nexus View；Repository、Component、Environment、CI Run 和 Deployment 保持各自语义。

| 子专题与原合同 | 阶段 / 切片与验收来源 | 协同 |
| --- | --- | --- |
| [Activity 与入向关系：ADR-0022](adr/0022-transactional-activity-and-incoming-relations.md) | M0.5 横切；[正常命令与双向发现证据](status/reviews/2026-09-10-activity-relations.md) | 全部对象、身份 |
| [Repository / Component：ADR-0033](adr/0033-repository-mapping-and-component-relations.md)、[Ticket / Component：ADR-0034](adr/0034-ticket-component-relations.md) | M4；原关系与权限矩阵，协作对象共属不代表已交付 | 交付集成 |
| [工单发现：ADR-0038](adr/0038-project-ticket-discovery.md)、[状态流转：ADR-0039](adr/0039-ticket-status-transitions.md) | 原只读 / 写入切片；[状态验收](status/reviews/2026-10-13-ticket-status-transitions.md) | Project 导航、Decision |
| [负责人：ADR-0046](adr/0046-ticket-assignee.md)、[优先级与筛选：ADR-0047](adr/0047-ticket-priority-and-filtering.md) | 原任务与退出矩阵；[负责人记录](status/reviews/2026-10-28-ticket-assignee.md)、[优先级记录](status/reviews/2026-10-28-ticket-priority.md)及后续有界观察 | 成员生命周期、迁移包版本 |

完成需沿真实授权与版本恢复路径成立；只读发现、最小状态、负责人、优先级是独立交付，不合称完整工单系统、通用 Timeline 分页或生产容量验收。

<a id="documents"></a>

## 文档协作

目标：让设计与执行共用可保留版本、可恢复并受权限保护的 Document。拥有正文、版本、保存冲突和编辑体验；来源关系协同上下文，身份与授权使用原合同。

| 子专题与原合同 | 阶段 / 切片与验收来源 | 协同 |
| --- | --- | --- |
| [编辑器与协同基础：ADR-0021](adr/0021-document-editor-and-collaboration-foundation.md) | 预研与候选边界；正式依赖、CRDT 与后续实验按原门槛 | Web 交付 |
| [最小 Markdown：ADR-0028](adr/0028-minimal-markdown-document.md) | M0.5 最小切片与 M5 长期范围分开；[验收记录](status/reviews/2026-09-26-markdown-document.md) | Ticket 来源、备份 / 导入 |

服务端权威 Markdown、显式保存、revision 冲突与恢复保持。自动化和隔离浏览器、原生中文 IME、普通成员独立持续使用分别验收；不由最小 Document 通过推定 CRDT 或离线协作完成。

<a id="delivery-integration"></a>

## 交付集成

目标：将可信构建和外部已完成部署事实接入研发上下文。拥有 Jenkins 输入、持续采集 / 发送、CI Run 来源及显式 staging 记录；不执行 production、不将构建成功当部署，也不由 Component 共属推导某 Ticket 已交付。

| 子专题与原合同 | 阶段 / 切片与验收来源 | 协同 |
| --- | --- | --- |
| [受认证 Jenkins 适配：ADR-0030](adr/0030-authenticated-jenkins-delivery-adapter.md)、[staging 记录：ADR-0031](adr/0031-session-scoped-staging-deployment-recording.md) | M0.5 / M3；原来源、幂等、授权与真实外部事实验收 | 身份、研发上下文 |
| [组件 / 环境配置与授权：ADR-0032](adr/0032-component-environment-configuration-and-authorization.md) | 原配置 / receipt / 恢复范围，部署记录复用显式授权 | 身份与管理 |
| [持久终态发送：ADR-0035](adr/0035-durable-jenkins-terminal-delivery.md) | A / B 与双边清理分别留证；[采集记录](status/reviews/2026-10-09-jenkins-collector.md)、[清理记录](status/reviews/2026-10-09-jenkins-cleanup.md) | 外部 controller、持久目录 |
| [checkout 来源：ADR-0037](adr/0037-ci-run-repository-checkout-provenance.md) | A 接收 / 发现与 B 真实采集分开；[原证据](status/reviews/2026-10-13-checkout-provenance.md) | Repository、后续 Ticket 具体交付关系 |
| [插件系统](architecture/plugin-system.md) | M6 的 Host API / SDK / 扩展范围；不因已有 Jenkins 切片进入通用市场 | 所有受控接入 |

交付整链恢复时按[当前依赖与暂缓条件](status/current.md#交付链保留完成线与恢复顺序)执行；离线 collector 和清理证据不代签原生 Linux 真实持续采集，合成签名输入不代签真实 SCM。

<a id="product-delivery"></a>

## 运行与产品交付

目标：让用户通过可维护的正式入口使用、恢复及迁移自己的实例。拥有部署、升级、备份、数据迁移、支持准备及共享客户端交付；不将技术准备解释为外部试用许可或生产能力。

| 子专题与原合同 | 阶段 / 切片与验收来源 | 协同 |
| --- | --- | --- |
| [schema readiness：ADR-0024](adr/0024-read-only-schema-readiness.md)、[升级失败恢复：ADR-0043](adr/0043-single-host-upgrade-failure-recovery.md) | M1；U01–U28 沿合同，结果见[隔离演练](status/reviews/2026-10-24-upgrade-failure-recovery.md)，独立操作者 U27 单列 | 身份、数据库与工件 |
| [Project 上下文包：ADR-0044](adr/0044-portable-project-context-package.md) | M1 数据迁移首片；[实际结果](status/reviews/2026-10-24-portable-context-package.md)与[有界补验](status/reviews/2026-10-28-context-package-supplement.md) | 全部对象、当前权限与历史身份 |
| [支持准备](trial-support.md)、[评估授权待决事项](evaluation-authorization-decisions.md) | 原 8 项准入及 E01–E12；技术、有效授权、持续使用分别判断 | Golden Path、运维 |
| [工作台与 Document 代表页](design/workbench-v1.md)、[客户端连接方向](architecture/client-server-connection-plan.md) | Web 既有交付与 M7 后续客户端分开；[视觉证据](status/reviews/2026-09-26-workbench-ui.md) | 所有产品页面 |

单 Project 迁移不代表 Workspace 全量迁移，恢复演练不代表生产容量；官方端点、Flutter 与更广离线能力保持原门槛。

<a id="golden-path"></a>

## Golden Path：跨能力整合验收

[Golden Path](golden-path.md)是 M0.5 的跨能力整合目标，不另维护一套功能任务状态。讨论 → 决策 → 工单 → Document / 研发对象 → 可信构建 → 外部 staging 事实 → 关系追溯与恢复，分别由上述能力合同承接。原故事、范围与验收标准不变。

| 整合责任 | 正式来源 |
| --- | --- |
| 产品价值、演示路径与持续使用观察 | [Golden Path 目的与验收](golden-path.md#验收标准) |
| 阶段范围和完整退出 | [M0–M7 路线图](roadmap.md) |
| 当前成熟度、阻塞与下一切片 | [当前状态](status/current.md) |
| 已发生的实现与验证 | 各子专题对应 status/reviews；历史状态不提供新授权 |

单个切片通过不能宣布整条 Golden Path、整个 M1 或真实团队持续使用通过。维护规则见[文档治理](governance/documentation-governance.md#planning-hierarchy)。
