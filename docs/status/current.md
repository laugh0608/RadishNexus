# RadishNexus 当前状态

状态日期：2026-08-30

## 当前阶段

产品定义、架构基线、仓库治理基线与 M0.5 Golden Path 纵向原型。

当前已经建立本地和 GitHub 远端仓库、`master` / `dev` 分支、协作规则、GitHub 模板、仓库检查器与 `Candidate Quality` 质量门；`master` Ruleset 已在远端启用。Project、Initiative、Component、Decision、Environment 和 EntityLink 的首批最小业务字段已经冻结，稳定引用、授权解析、事件 envelope 与 Activity 投影已由 ADR-0002 接受为 M0 契约基线。可丢弃的 Go + PostgreSQL 核心契约实验已经通过。正式 `server/` Go module、显式 forward-only migration runner、Thread → Decision → Ticket 权限纵向切片、版本化 Activity 重建、Decision / Ticket Nexus View 读取查询和最小 transport adapter 已经建立；正式 Component、已验证 Jenkins delivery → CI Run 原子记录和安全读取已经建立。正式 Environment、环境级写授权、显式终态 staging Deployment、`deploys` 关系、`deployment.recorded` 投影与 Workspace 作用域安全读取已经通过真实 PostgreSQL 验证。PostgreSQL 17 同 major 的版本化备份、全新空目标恢复、migration 校验和 Activity 重建已经由 ADR-0010、显式 CLI 与双实例演练建立。上述 M0 正式服务、Web 代表原型与恢复基线已通过 PR #9 的远端 `Candidate Quality`，使用 merge commit 晋级 `master` 并 fast-forward 回流 `dev`。正式 `web/` React + TypeScript 基线现已覆盖 Decision、CI Run 与 Deployment Nexus View 代表交互；尚未建立插件 runtime 或客户端，也尚未开放业务 HTTP API。

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
- 正式服务的 Activity projection version 1 已覆盖 `decision.proposed`、`decision.accepted`、`ticket.created`、`ci-run.recorded` 和 `deployment.recorded`，只保留引用与状态等最小安全事实。
- Activity 可以从不可变领域事件原子、幂等重建；清空 projection 或清理已投递 Outbox 状态后，重建结果、顺序和权限边界保持不变。
- ADR-0003 已接受 Go 标准库 HTTP 路由、原生 `pgx/v5` 和手写版本化 SQL，不引入 Web 框架或 ORM。
- ADR-0004 已冻结 Thread、Decision、Ticket 的 governing Project、首批角色与 restricted Thread 投影边界。
- ADR-0005 已冻结连续编号、checksum、advisory lock、单 migration 事务和显式 forward-only 执行。
- ADR-0006 已冻结 `ci-run` / `cir_`、Jenkins source 映射、完成态 CI Run、不可变 delivery receipt，以及 verified boundary 外的签名与 Secret 责任。
- ADR-0007 已冻结活跃 Workspace 成员 → Component → CI Run 的 M0 读取链；owner Team、Project、EntityLink 和 Jenkins source 都不授予该读取权。
- ADR-0009 已冻结 `deployment` / `dpl_`、正式 Environment、环境级显式部署授权、终态 staging Deployment 和 `deploys` 原子关系；Project 角色、owner Team、CI source 和成功构建都不授予或触发部署能力。
- ADR-0010 已冻结本地受控连接上 PostgreSQL 17 同 major 的版本化 backup manifest、custom archive、全新空目标恢复、migration 校验和 Activity 重建；它不是 `.nexus` 开放导出格式，当前工具桥接不近似转译 TLS 配置。
- ADR-0011 已冻结 active Workspace 成员同时复用 Environment 与 CI Run 当前权限的 Deployment 安全读取；环境级部署授权只控制写入，不控制历史发现。
- 正式 application service 已完成 Thread → Proposed Decision → Accepted Decision → Ticket，并把 EntityLink、领域事件和 Outbox 与业务状态放在同一事务。
- Jenkins application service 只接收已完成来源认证和字段映射的 `VerifiedJenkinsDelivery`；receipt、CI Run、`ci-run.recorded` 和 Outbox 在同一事务提交，不保存 Secret 或原始 webhook body。
- 相同 Jenkins delivery 和 digest 只返回既有 CI Run；digest 改变或不同 delivery 映射到同一 external run 时 fail closed，事件冲突会连同 receipt 与 CI Run 一起回滚。
- 当前只接收 `succeeded / failed / canceled` 完成事实；尚未冻结 Jenkins HTTP route、HMAC/签名协议、失败审计、运行中更新或多 provider 抽象。
- `RecordStagingDeployment` 只接受明确用户的 `web / api` invocation；目标必须是 active staging Environment，来源必须是 succeeded CI Run，调用者必须是 active Workspace 成员并持有该 Environment 的 active 显式授权。
- Deployment、所使用的 authorization、操作者、来源和受控时间进入不可变权威记录；Deployment、asserted user `deploys` 关系、`deployment.recorded` 与 Outbox 同事务提交，任一步失败全部回滚。
- CI Run application service 不调用 Deployment service；真实 PostgreSQL 用例已证明 CI Run 成功后、显式命令前不存在 Deployment 或 `deployment.*` 事件。当前 command 只记录外部已完成终态，不执行部署、不读取 Secret，也不支持 production、审批、回滚或运行中状态。
- `nexus-backup` 只备份与当前 migration artifact identity 完全一致且 relation 已完整分类的数据库；未知 relation、migration 漂移和非 PostgreSQL 17 来源均 fail closed。
- 备份工件固定包含 manifest 与 custom-format dump，保留稳定 ID、业务表、授权 provenance、EntityLink、领域事件、inbound receipt、必要 Outbox 与 migration history；`activity_items` 数据默认排除。
- `nexus-restore` 只接受 checksum 完整的受信工件和全新空目标，不使用 `--clean` 或自动覆盖；恢复通过显式 TOC 先装载 EntityType 注册表，再以单事务恢复其余事实、运行正式 migration 并重建 Activity。
- 双实例 PostgreSQL 17 演练已经证明恢复前后所有纳入表与 Activity 全量快照一致；manifest migration 漂移、dump 损坏和非空目标重复恢复均失败且不改变受保护目标。
- contributor 不能确认 Decision；decider 必须能读取全部 evidence 后才能人工确认；Project admin 也不会自动穿透 restricted Thread。
- Nexus View application query 已能为 Decision、Ticket、CI Run 和 Deployment 返回 Current、Relations 和 Timeline，并在同一 repeatable-read 事务中按当前权限解析。
- Deployment Current 只返回终态、started / completed / recorded 时间、当前 Environment 与来源 CI Run；`deploys` Relation 和 `deployment.recorded` Timeline 复用同一权限语义，不返回 authorization、调用 source、Jenkins receipt、digest、Secret、原始 payload 或外部 URL。
- 没有环境部署授权的 active Workspace 成员仍可读取共享 staging Deployment；非成员、暂停成员、跨 Workspace 主体或依赖对象不可读时得到 not-found，Environment 归档不隐藏既有历史。
- CI Run Current 只返回 status、开始/完成/记录/更新时间和当前 Component；`ci-run.recorded` Timeline 保留通用 `plugin` kind 但隐藏 source ID，不返回 external run key、receipt、digest、Secret、原始 payload 或 Jenkins URL。
- 非成员、暂停成员和跨 Workspace 主体读取 CI Run 均得到 not-found；Component retired 不删除或隐藏既有 CI Run 历史。
- Relations 和 Timeline 对不可读目标只返回不含 EntityRef、类型、关系类型和标题的通用占位；hidden 目标不进入结果。
- 最小认证 adapter 只把上游已验证的 UserID 与 WorkspaceID 转换为 application `Principal`，不读取或验证 Header、Cookie、Token 和 OIDC claims。
- 内部 HTTP error mapping 已覆盖 `unauthenticated / forbidden / not found / conflict / invalid` 与未知失败；它不暴露原始错误，也尚未形成公共响应对象。
- Decision Nexus View 代表原型已经表达 Current、Relations 和 Timeline，并覆盖 loading、empty、error、restricted placeholder 与窄屏布局。
- CI Run Nexus View 代表交互已经表达 succeeded / failed、四个受控时间、当前 Component、唯一 `ci-run.recorded` Timeline、loading、error 与窄屏布局；构建结果没有被表现为 Deployment。
- CI Run Web fixture 与后端安全投影同形，不携带 source ID、external run key、delivery receipt、digest、Secret、原始 payload 或未经治理的外部 URL；浏览器验证未发现必须新增 transport 的需求。
- Deployment Nexus View 代表交互已经表达 succeeded / failed、三个受控时间、Environment、来源 CI Run、`deploys` Relation 与唯一 `deployment.recorded` Timeline；失败态明确保留来源构建成功事实，不把失败 Deployment 改写成 CI Run 失败。
- Deployment Web fixture 不携带 authorization、调用 source、Jenkins 来源字段或执行日志；桌面与 390px 窄屏真实浏览器复核通过，并修正了旧 CI Run 页面标题漂移。
- Web 原型只消费权限过滤后的 discriminated union；`restricted` 形状不携带 EntityRef、对象类型、关系类型、标题、来源或时间，`hidden` 目标不进入客户端数据。
- 原型使用明确标注的静态 fixture；本轮浏览器复核没有暴露必须新增内部 handler 的需求，因此没有为了联调制造临时业务 API。
- `web/` 已建立 Prettier、Oxlint、Vitest + jsdom、严格 TypeScript、Vite production build 与 lockfile 供应链检查；`Candidate Quality` 已加入独立 `Web App` job，并已在本批次 PR 中实际通过。
- 在横向补全各模块前，先完成 Golden Path 纵向原型。
- 仓库采用 `master` 稳定分支和 `dev` 日常开发/集成分支；单维护者串行任务默认直接在 `dev` 推进，主题分支只用于明确要求、外部贡献、并行写入或风险隔离。
- `master` 允许 merge commit 和 rebase merge，禁用 squash merge，并要求变化回流 `dev`。
- `Candidate Quality` 作为稳定聚合质量门；仓库定义已加入 M0 实验、正式 Go 服务、双实例备份恢复和 Web App 的单元/状态测试、静态检查、构建与真实 PostgreSQL 集成测试。新增备份恢复步骤已在 PR #9 的 GitHub `Go Server` job 实际通过。
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
- [ADR-0007：Component 作用域下的 CI Run 读取](../adr/0007-component-scoped-ci-run-read.md)
- [ADR-0008：`dev` 优先的单维护者开发拓扑](../adr/0008-dev-first-development-governance.md)
- [ADR-0009：显式 staging Deployment 与环境级授权](../adr/0009-explicit-staging-deployment.md)
- [ADR-0010：可验证 PostgreSQL 备份与全新实例恢复](../adr/0010-verified-postgresql-backup-and-restore.md)
- [ADR-0011：Workspace 作用域下的 Deployment 安全读取](../adr/0011-workspace-scoped-deployment-read.md)
- [开发指南](../development/README.md)
- [M0 核心契约实验](../../experiments/m0-core-contracts/README.md)
- [正式 Go 服务](../../server/README.md)

## 今日进展（2026-08-30）

今日已经完成最小备份恢复、阶段晋级和 Deployment 读取/UI 纵向切片：

1. ADR-0010 冻结 PostgreSQL 17 同 major、format version 1 manifest、custom archive、完整 relation 分类、Secret 排除、空目标与单事务恢复边界；
2. `nexus-backup` 与 `nexus-restore` 已建立显式命令，备份输出使用同级临时目录和完成后原子改名，恢复拒绝 `--clean`、自动覆盖和 migration 漂移；
3. 恢复命令检查 archive TOC 并显式提前装载 `entity_types`，解决 `valid_entity_id` 函数无法被 `pg_dump` 自动推断的数据依赖，同时保持原数据库 check constraint 不变；
4. 双独立 PostgreSQL 17 容器已经完成 source fixture → backup → fresh target restore → formal migration → Activity rebuild 的真实往返；所有纳入表与 Activity 全量快照一致；
5. manifest migration checksum 漂移、dump 损坏和非空目标重复恢复均已有失败验证；前两种 preflight 失败后目标保持空，重复恢复不会改变既有目标数据。
6. 阶段 PR #9 已通过 Repo Hygiene、Repository Checker Tests、M0 Core Contracts、Go Server、Web App 与聚合 `Candidate Quality`，使用 merge commit 晋级 `master` 后已 fast-forward 回流 `dev`；
7. ADR-0011、正式 application query 与真实 PostgreSQL 测试已建立 Deployment 的 Environment + CI Run 组合读取权限、not-found 失败语义、归档历史保留与敏感字段排除；
8. Web 默认代表入口已切换为 Deployment succeeded / failed，并完成 loading、error、桌面与 390px 窄屏复核；原型仍使用明示静态 fixture，没有新增临时业务 HTTP handler。
9. PostgreSQL 集成共享 runner 已统一为连续两次真实 `psql SELECT 1` 后才放行，消除了容器初始化重启窗口的一次宿主端 EOF；M0、正式服务与双实例恢复入口已重新实际通过。

## 下一步

Deployment Nexus View 读取与 Web 代表交互已经达到本地完成线。下一步先完成本切片差异复核和 `dev` 提交；随后优先收敛 M1 公共 transport 的前置认证决策，先确认首期只做本地账号还是同时加入 OIDC，再冻结 session、首次管理员、Workspace 选择、CSRF、request ID 与版本化错误对象，最后决定第一个只读 HTTP 端点。认证契约未冻结前不以临时 Header 或 fixture handler 开放业务 API。

文档协同技术方案与免费书面授权模板继续作为独立 M0 缺口评估，不与认证/transport 同时铺开。当前 `dev` 切片不立即再次晋级 `master`；积累下一组可独立演示、测试和退出的候选后再创建阶段 PR。

当前完成线：没有部署写授权的 active Workspace 成员能够在同一 repeatable-read 查询中安全读取 Deployment Current、`deploys` Relation 和 `deployment.recorded` Timeline；非成员、暂停成员、跨 Workspace 主体与不可读依赖得到 not-found，归档 Environment 不隐藏历史，服务端输出和 Web fixture 不包含 authorization、Jenkins 来源或 Secret，桌面与窄屏代表交互通过验证。

当前停止线：不新增临时业务 HTTP handler，不把读取权限当作部署能力，不从 CI Run 构建结果自动生成或反向推断 Deployment，不开放 authorization 或外部来源敏感字段，不实现 production、审批、回滚、执行引擎、搜索、通知或 Attention Item；同时不自动覆盖数据库、不承诺跨 PostgreSQL major、远程保留或 PITR，也不启动 Flutter、CRDT、通用 RBAC、Repository 占位、插件市场或多 provider。

## 开放问题

- PostgreSQL 正式支持版本矩阵，以及生产升级的 forward repair 与恢复流程；
- EntityID 生成算法，Document、Repository 等尚未切片类型的前缀和后续 PostgreSQL schema；
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
