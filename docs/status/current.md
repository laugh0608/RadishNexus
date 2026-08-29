# RadishNexus 当前状态

状态日期：2026-08-29

## 当前阶段

产品定义、架构基线、仓库治理基线与 M0 正式服务纵向切片。

当前已经建立本地和 GitHub 远端仓库、`master` / `dev` 分支、协作规则、GitHub 模板、仓库检查器与 `Candidate Quality` 质量门；`master` Ruleset 已在远端启用。Project、Initiative、Component、Decision、Environment 和 EntityLink 的首批最小业务字段已经冻结，稳定引用、授权解析、事件 envelope 与 Activity 投影已由 ADR-0002 接受为 M0 契约基线。可丢弃的 Go + PostgreSQL 核心契约实验已经通过。正式 `server/` Go module、显式 forward-only migration runner、Thread → Decision → Ticket 权限纵向切片、版本化 Activity 重建、Decision / Ticket Nexus View 读取查询和最小 transport adapter 已经建立；正式 Component、已验证 Jenkins delivery → CI Run 原子记录和 `ci-run.recorded` 投影也已达到本地完成线。正式 `web/` React + TypeScript 基线与 Decision Nexus View 代表原型已经通过本地检查和浏览器复核；尚未建立插件 runtime 或客户端，也尚未开放业务 HTTP API。

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
- 正式服务的 Activity projection version 1 已覆盖 `decision.proposed`、`decision.accepted`、`ticket.created` 和 `ci-run.recorded`，只保留引用与状态等最小安全事实。
- Activity 可以从不可变领域事件原子、幂等重建；清空 projection 或清理已投递 Outbox 状态后，重建结果、顺序和权限边界保持不变。
- ADR-0003 已接受 Go 标准库 HTTP 路由、原生 `pgx/v5` 和手写版本化 SQL，不引入 Web 框架或 ORM。
- ADR-0004 已冻结 Thread、Decision、Ticket 的 governing Project、首批角色与 restricted Thread 投影边界。
- ADR-0005 已冻结连续编号、checksum、advisory lock、单 migration 事务和显式 forward-only 执行。
- ADR-0006 已冻结 `ci-run` / `cir_`、Jenkins source 映射、完成态 CI Run、不可变 delivery receipt，以及 verified boundary 外的签名与 Secret 责任。
- 正式 application service 已完成 Thread → Proposed Decision → Accepted Decision → Ticket，并把 EntityLink、领域事件和 Outbox 与业务状态放在同一事务。
- Jenkins application service 只接收已完成来源认证和字段映射的 `VerifiedJenkinsDelivery`；receipt、CI Run、`ci-run.recorded` 和 Outbox 在同一事务提交，不保存 Secret 或原始 webhook body。
- 相同 Jenkins delivery 和 digest 只返回既有 CI Run；digest 改变或不同 delivery 映射到同一 external run 时 fail closed，事件冲突会连同 receipt 与 CI Run 一起回滚。
- 当前只接收 `succeeded / failed / canceled` 完成事实；尚未冻结 Jenkins HTTP route、HMAC/签名协议、失败审计、运行中更新或多 provider 抽象。
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
- [ADR-0006：已验证 Jenkins delivery 与 CI Run 原子记录](../adr/0006-verified-jenkins-delivery-and-ci-run.md)
- [开发指南](../development/README.md)
- [M0 核心契约实验](../../experiments/m0-core-contracts/README.md)
- [正式 Go 服务](../../server/README.md)

## 下一步事项

Jenkins CI Run 核心切片已经达到本地完成线。当前批次先收口实现并确认远端门禁，再进入独立读取切片：

1. 复核本分支 migration、事务、集成测试与文档一致性，提交后通过 PR 确认正式 Go Server、M0 Core Contracts 和 Candidate Quality 远端门禁；
2. 从最新 `dev` 单独冻结 CI Run 的用户读取授权、Current/Timeline 安全展示字段和 Component 关联解析，不从 webhook source 身份推导用户权限；
3. 读取面只暴露受控 status、时间和经权限解析的 Component，不返回 digest、receipt、Secret、原始 payload 或未经治理的 Jenkins URL；
4. 后端读取合同稳定后，再以小切片把真实 CI Run 状态接入 Nexus View / Web 代表交互，不为了演示制造临时公共 API；
5. CI Run 读取体验稳定后，后端纵向顺位保持为显式 staging Deployment → 备份恢复。

下一步完成线：Jenkins 核心 PR 合入 `dev` 且远端门禁全绿；随后 CI Run 读取切片能够在当前权限下稳定返回单一权威运行事实，并保持重复 delivery 不产生重复 Timeline。

下一步停止线：不在核心切片补 Jenkins HTTP route 或签名协议，不暴露 receipt/digest，不启动 Flutter，不选择 CRDT，不建立通用 RBAC framework、Repository 占位、插件市场或多 CI provider 抽象，不把 CI 成功冒充 Deployment。

后续顺位保持为 Jenkins CI Run 读取 → 显式 staging Deployment → 备份恢复，再独立评估文档协同方案和授权模板。

## 开放问题

- PostgreSQL 正式支持版本矩阵，以及生产升级的 forward repair 与恢复流程；
- EntityID 生成算法，Document、Repository、Deployment 等尚未切片类型的前缀和后续 PostgreSQL schema；
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
