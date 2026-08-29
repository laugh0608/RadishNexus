# RadishNexus 当前状态

状态日期：2026-08-29

## 当前阶段

产品定义、架构基线、仓库治理基线与 M0 正式服务纵向切片。

当前已经建立本地和 GitHub 远端仓库、`master` / `dev` 分支、协作规则、GitHub 模板、仓库检查器与 `Candidate Quality` 质量门；`master` Ruleset 已在远端启用。Project、Initiative、Component、Decision、Environment 和 EntityLink 的首批最小业务字段已经冻结，稳定引用、授权解析、事件 envelope 与 Activity 投影已由 ADR-0002 接受为 M0 契约基线。可丢弃的 Go + PostgreSQL 核心契约实验已经通过。正式 `server/` Go module、显式 forward-only migration runner、Thread → Decision → Ticket 权限纵向切片、版本化 Activity 重建、Decision / Ticket Nexus View 读取查询和最小 transport adapter 已经建立。正式 `web/` React + TypeScript 基线与 Decision Nexus View 代表原型已经通过本地检查和浏览器复核；尚未建立插件或客户端，也尚未开放业务 HTTP API。

## 当前结论

- 项目名确定为 `RadishNexus`。
- 产品定位为自部署优先的研发团队沟通、协作与交付枢纽。
- 自部署使用不按席位计费或限额。
- 核心采用 source-available 和单独书面授权模式；书面授权可以免费。
- SDK、公共协议和插件开放源码，并使用各自独立许可证。
- Web App 是第一产品形态，采用 React + TypeScript。
- 首个正式 Web 基线使用 Node 24 LTS、npm 11、React 19、Vite 8 与 TypeScript 6；router、状态库、编辑器和组件库尚未冻结。
- 前期不开发移动端和 PC 客户端。
- 后续客户端统一采用 Flutter，不采用 Tauri。
- 服务端以 Go 为主，Rust 只进入有明确收益的边界。
- 初期采用模块化单体。
- 插件系统按真实收益渐进建设，Jenkins 是第一个验证场景。
- 聊天、工单和文档作为内建模块，不为了插件化而插件化。
- Decision 是一等业务对象，保留问题、结论、理由、证据和替代关系。
- Project、Initiative、Component、Repository 和 Environment 已明确分工。
- CI Run 与 Deployment 是不同交付事实。
- EntityLink 和 Activity 是上下文关联与 Nexus View 的基础。
- 六类 M0 核心对象已经冻结共同身份字段、最小业务字段和首批不变量。
- ADR-0002 使用类型加不透明 ID 作为稳定 EntityRef，并把 Workspace 作为独立解析上下文；M0 不支持跨 Workspace 关系。
- ADR-0002 把 asserted / derived 与 user / system / plugin / import 分开记录，并规定引用不授予目标权限。
- ADR-0002 保持领域事件事实、Outbox 投递状态、Activity 和 Audit 的不同职责。
- M0 核心契约实验已经验证 Decision evidence、领域事件和 Outbox 的单事务写入。
- 跨 Workspace EntityLink、缺少 evidence 的 Decision 和重复 Jenkins delivery 已有真实 PostgreSQL 失败路径或幂等测试。
- 正式服务的 Activity projection version 1 已覆盖 `decision.proposed`、`decision.accepted` 和 `ticket.created`，只保留引用与状态等最小安全事实。
- Activity 可以从不可变领域事件原子、幂等重建；清空 projection 或清理已投递 Outbox 状态后，重建结果、顺序和权限边界保持不变。
- ADR-0003 已接受 Go 标准库 HTTP 路由、原生 `pgx/v5` 和手写版本化 SQL，不引入 Web 框架或 ORM。
- ADR-0004 已冻结 Thread、Decision、Ticket 的 governing Project、首批角色与 restricted Thread 投影边界。
- ADR-0005 已冻结连续编号、checksum、advisory lock、单 migration 事务和显式 forward-only 执行。
- 正式 application service 已完成 Thread → Proposed Decision → Accepted Decision → Ticket，并把 EntityLink、领域事件和 Outbox 与业务状态放在同一事务。
- contributor 不能确认 Decision；decider 必须能读取全部 evidence 后才能人工确认；Project admin 也不会自动穿透 restricted Thread。
- Nexus View application query 已能为 Decision 和 Ticket 返回 Current、Relations 和 Timeline，并在同一 repeatable-read 事务中按当前权限解析。
- Relations 和 Timeline 对不可读目标只返回不含 EntityRef、类型、关系类型和标题的通用占位；hidden 目标不进入结果。
- 最小认证 adapter 只把上游已验证的 UserID 与 WorkspaceID 转换为 application `Principal`，不读取或验证 Header、Cookie、Token 和 OIDC claims。
- 内部 HTTP error mapping 已覆盖 `unauthenticated / forbidden / not found / conflict / invalid` 与未知失败；它不暴露原始错误，也尚未形成公共响应对象。
- Decision Nexus View 代表原型已经表达 Current、Relations 和 Timeline，并覆盖 loading、empty、error、restricted placeholder 与窄屏布局。
- Web 原型只消费权限过滤后的 discriminated union；`restricted` 形状不携带 EntityRef、对象类型、关系类型、标题、来源或时间，`hidden` 目标不进入客户端数据。
- 原型使用明确标注的静态 fixture；本轮浏览器复核没有暴露必须新增内部 handler 的需求，因此没有为了联调制造临时业务 API。
- `web/` 已建立 Prettier、Oxlint、Vitest + jsdom、严格 TypeScript、Vite production build 与 lockfile 供应链检查；`Candidate Quality` 已加入独立 `Web App` job，并已在本批次 PR 中实际通过。
- 在横向补全各模块前，先完成 Golden Path 纵向原型。
- 仓库采用 `master` 稳定分支、`dev` 集成分支和主题分支；普通 PR 默认进入 `dev`。
- `master` 允许 merge commit 和 rebase merge，禁用 squash merge，并要求变化回流 `dev`。
- `Candidate Quality` 作为稳定聚合质量门；仓库定义已加入 M0 实验、正式 Go 服务和 Web App 的单元/状态测试、静态检查、构建与真实 PostgreSQL 集成测试，新增 Web job 与聚合门已在 GitHub 实际通过。
- GitHub 远端默认分支为 `master`，`master` Ruleset 已启用并要求 PR、严格状态检查和已解决对话。
- GitHub Private vulnerability reporting 已启用；未修复漏洞优先通过仓库 Security Advisory 私下报告，入口和备用联系方式以 [SECURITY.md](../../SECURITY.md) 为准。

## 当前文档基线

- [产品定义](../product-definition.md)
- [领域模型](../domain-model.md)
- [Golden Path](../golden-path.md)
- [决策基线](../decision-baseline.md)
- [总体架构](../architecture/overview.md)
- [核心实体、授权与事件契约](../architecture/core-contracts.md)
- [插件系统](../architecture/plugin-system.md)
- [许可与分发策略](../licensing-strategy.md)
- [产品路线图](../roadmap.md)
- [仓库治理](../governance/README.md)
- [ADR-0001：分支与 PR 治理](../adr/0001-branch-and-pr-governance.md)
- [ADR-0002：稳定实体引用与事件投影边界](../adr/0002-stable-entity-reference-and-event-projection.md)
- [ADR-0003：Go 服务端基础栈与数据访问](../adr/0003-go-service-foundation.md)
- [ADR-0004：Project 作用域下的协作对象与权限](../adr/0004-project-scoped-collaboration-permissions.md)
- [ADR-0005：Forward-only PostgreSQL migration runner](../adr/0005-forward-only-postgresql-migrations.md)
- [开发指南](../development/README.md)
- [M0 核心契约实验](../../experiments/m0-core-contracts/README.md)
- [正式 Go 服务](../../server/README.md)

## 下一步事项

Decision Nexus View 代表原型已经达到本地完成线。当前批次先收口 Web 工程并确认远端门禁，再进入下一个独立后端纵向切片：

1. 把远端门禁全绿的代表原型、Web 工程基线与治理同步 PR 合入 `dev`，再从最新 `dev` 开始下一独立切片；
2. 原型没有暴露必须新增内部 handler 的需求，本批次不为了“接上后端”制造临时业务 API；公开错误对象、分页、游标和通用资源模型继续保持未冻结；
3. Web PR 合并后，从最新 `dev` 独立准备 Jenkins CI Run 切片，先沿 Golden Path、插件隔离、Webhook 来源校验与 delivery 幂等边界冻结最小输入；
4. Jenkins 切片只把成功验证和幂等接收的交付事实映射为独立 CI Run，不把构建成功解释为已经部署，也不提前实现通用插件市场；
5. CI Run 读取体验稳定后，后端纵向顺位保持为显式 staging Deployment → 备份恢复。

下一步完成线：Web PR 合入 `dev` 后，Jenkins 重复 delivery 不重复创建 CI Run，来源、失败和审计边界可复验，CI Run 仍与 Deployment 保持不同实体和生命周期。

下一步停止线：不为代表原型补临时公共 API，不启动 Flutter，不选择 CRDT，不建立通用 RBAC framework，不扩建完整 Web Shell、聊天界面或插件市场，不把 CI 成功冒充 Deployment。

后续顺位保持为 Jenkins CI Run → 显式 staging Deployment → 备份恢复，再独立评估文档协同方案和授权模板。

## 开放问题

- PostgreSQL 正式支持版本矩阵，以及生产升级的 forward repair 与恢复流程；
- EntityID 生成算法、其余 Golden Path 类型前缀和后续 PostgreSQL schema；
- 文档编辑器和 CRDT；
- 首版插件运行方式；
- Initiative、Component 与 Project 的首版导航表现；
- Decision 的复核周期和替代交互；
- `.nexus` 上下文包的开放格式与脱敏规则；
- SDK 和插件的具体开放源码许可证；
- 认证第一阶段只做本地账号，还是同时加入 OIDC；
- 消息和文档搜索边界；
- 免费书面授权的签发与撤销规则；
- 免费评估是否公开授予，或使用离线自助签发；

## 停止线

在对象、权限、事件和插件最小实验完成前：

- 不创建大量微服务；
- 不同时开发 Flutter；
- 不建设插件市场；
- 不承诺完整离线文档；
- 不接入大量 CI/CD 平台；
- 不建设完整软件目录、图数据库或战略项目组合模块；
- 不把 AI 生成内容直接确认为 Decision、状态更新或外部操作；
- 不为了展示功能数量扩张到音视频、代码托管或复杂项目组合管理。
