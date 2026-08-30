# RadishNexus 当前状态

状态日期：2026-08-30

## 当前阶段

产品定义、架构基线、仓库治理基线与 M0.5 Golden Path / M1 Web 平台基础纵向原型。

当前已经建立本地和 GitHub 远端仓库、`master` / `dev` 分支、协作规则、GitHub 模板、仓库检查器与 `Candidate Quality` 质量门；`master` Ruleset 已在远端启用。Project、Initiative、Component、Decision、Environment 和 EntityLink 的首批最小业务字段已经冻结，稳定引用、授权解析、事件 envelope 与 Activity 投影已由 ADR-0002 接受为 M0 契约基线。可丢弃的 Go + PostgreSQL 核心契约实验已经通过。正式 `server/` Go module、显式 forward-only migration runner、Thread → Decision → Ticket 权限纵向切片、版本化 Activity 重建、Decision / Ticket Nexus View 读取查询和最小 transport adapter 已经建立；正式 Component、已验证 Jenkins delivery → CI Run 原子记录和安全读取已经建立。正式 Environment、环境级写授权、显式终态 staging Deployment、`deploys` 关系、`deployment.recorded` 投影与 Workspace 作用域安全读取已经通过真实 PostgreSQL 验证。PostgreSQL 17 同 major 的版本化备份、全新空目标恢复、migration 校验和 Activity 重建已经由 ADR-0010、显式 CLI 与双实例演练建立。上述 M0 正式服务、Web 代表原型与恢复基线已通过 PR #9 的远端 `Candidate Quality`，使用 merge commit 晋级 `master` 并 fast-forward 回流 `dev`。正式 `web/` React + TypeScript 基线现已覆盖 Decision、CI Run 与 Deployment Nexus View 代表交互；本地账号的公共 login / session / logout transport、第一个 Session 作用域的 Deployment Nexus View 业务读取端点，以及同源 authenticated Web Shell 已经建立。真实 PostgreSQL + production Web build + HTTPS 浏览器现已从登录、Workspace 选择进入 canonical Deployment 并完成登出；完整产品导航、插件 runtime 和客户端尚未建立。

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
- ADR-0012 已冻结一次性本地管理员、本地账号、Argon2id verifier、服务端 opaque Session、CSRF、当前 Workspace membership 与恢复后登录态失效边界。
- ADR-0013 已冻结精确 HTTPS public origin / Host、可信代理 CIDR、客户端 IP 解析、每进程登录限流、JSON 上限和 login / session / logout 公共路由；不信任用户转发 Header，也不把进程内限流冒充跨副本全局保护。
- ADR-0014 已冻结 `GET /api/v1/workspaces/{workspace_id}/deployments/{deployment_id}/nexus-view`、路径 Workspace 选择、Session → current membership → `Principal`、显式公共 DTO、不可发现性与 `private, no-store` 缓存边界。
- ADR-0015 已冻结同源 authenticated Web Shell、显式绝对 production build root、HTML 页面 allowlist、静态资源缓存与安全 Header；它只替代 ADR-0014 中根路径保留静态检视器的阶段性决定。
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
- 最小认证 adapter 只把 Session resolver 已验证的 UserID 与 WorkspaceID 转换为 application `Principal`，不读取或信任用户身份 Header 和 OIDC claims。
- 公共 HTTP error mapping 与响应对象已覆盖认证、CSRF、安全 transport、proxy、限流、body / media type、`unauthenticated / forbidden / not found / conflict / invalid` 与未知失败；它不暴露原始错误，并由 server-generated request ID 关联。
- 第一个业务 HTTP handler 只接受安全 `GET`，复用认证 transport、当前 Workspace membership 和正式 Deployment query；无 membership、跨 Workspace、未知或不可读对象统一保持不可发现，projection 漂移在发送响应前 fail closed。
- Deployment 公共 DTO 使用显式 `data` envelope 与结构化 ref，只返回终态、nullable started time、completed / recorded time、可读 Environment / CI Run、`deploys` 和 `deployment.recorded`；不直接序列化内部 application struct。
- Decision Nexus View 代表原型已经表达 Current、Relations 和 Timeline，并覆盖 loading、empty、error、restricted placeholder 与窄屏布局。
- CI Run Nexus View 代表交互已经表达 succeeded / failed、四个受控时间、当前 Component、唯一 `ci-run.recorded` Timeline、loading、error 与窄屏布局；构建结果没有被表现为 Deployment。
- CI Run Web fixture 与后端安全投影同形，不携带 source ID、external run key、delivery receipt、digest、Secret、原始 payload 或未经治理的外部 URL；浏览器验证未发现必须新增 transport 的需求。
- Deployment Nexus View 代表交互已经表达 succeeded / failed、三个受控时间、Environment、来源 CI Run、`deploys` Relation 与唯一 `deployment.recorded` Timeline；失败态明确保留来源构建成功事实，不把失败 Deployment 改写成 CI Run 失败。
- Deployment Web fixture 不携带 authorization、调用 source、Jenkins 来源字段或执行日志；桌面与 390px 窄屏真实浏览器复核通过，并修正了旧 CI Run 页面标题漂移。
- Web 原型只消费权限过滤后的 discriminated union；`restricted` 形状不携带 EntityRef、对象类型、关系类型、标题、来源或时间，`hidden` 目标不进入客户端数据。
- Web 根路径已经改为 authenticated shell，先 bootstrap 正式 Session，再提供 login、当前 Workspace 选择、已知 Deployment ID 入口和 logout；原静态代表检视器移动到显式 `/prototype/nexus-view`，不作为真实失败 fallback。
- canonical `/workspaces/{workspace_id}/deployments/{deployment_id}` 页面先完成 Session bootstrap，再通过同源、no-store 的类型化 adapter 消费真实公共 DTO；业务 `401` 回到登录态，网络和契约错误显式失败。
- Go server 只从必需的绝对 `RADISHNEXUS_WEB_ROOT` 交付 production build，HTML 仅开放根路径、代表原型和 canonical Deployment；未知页面不使用任意 SPA fallback，哈希资源 immutable cache，HTML `no-cache`。
- Web 不保存密码、Session token、Workspace 权限快照或业务响应到 `localStorage` / `sessionStorage`；Workspace 选择不改变服务端权限，业务路由仍按当前 membership 解析。
- Web fixture 已统一修正为 `entity://type/id` canonical 引用和正式 `tkt_` 前缀；真实浏览器网络边界验证了 API request、nullable started time、Relations 与 Timeline 渲染且没有 console warning / error。
- 真实 PostgreSQL + migration + application 写入 + HTTPS + production Web build 的浏览器 fixture 已验证匿名 `401`、登录 `201`、Secure/Strict Cookie、Session `200`、Deployment `200`、窄屏、登出 `204`、Cookie 清理与 canonical URL 重新登录；业务页面没有 console warning / error。
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
- [ADR-0012：本地身份与服务端 Session 基线](../adr/0012-local-identity-and-session-foundation.md)
- [ADR-0013：公共认证 Transport 与可信代理边界](../adr/0013-public-authentication-transport.md)
- [ADR-0014：Session 作用域下的 Deployment Nexus View Transport](../adr/0014-session-scoped-deployment-nexus-view-transport.md)
- [ADR-0015：同源 Authenticated Web Shell 与显式静态资源装配](../adr/0015-same-origin-authenticated-web-shell.md)
- [开发指南](../development/README.md)
- [M0 核心契约实验](../../experiments/m0-core-contracts/README.md)
- [正式 Go 服务](../../server/README.md)

## 今日进展（2026-08-30）

今日已经完成最小备份恢复、阶段晋级、Deployment 读取/UI 纵向切片，以及本地身份与公共认证 transport：

1. ADR-0010 冻结 PostgreSQL 17 同 major、format version 1 manifest、custom archive、完整 relation 分类、Secret 排除、空目标与单事务恢复边界；
2. `nexus-backup` 与 `nexus-restore` 已建立显式命令，备份输出使用同级临时目录和完成后原子改名，恢复拒绝 `--clean`、自动覆盖和 migration 漂移；
3. 恢复命令检查 archive TOC 并显式提前装载 `entity_types`，解决 `valid_entity_id` 函数无法被 `pg_dump` 自动推断的数据依赖，同时保持原数据库 check constraint 不变；
4. 双独立 PostgreSQL 17 容器已经完成 source fixture → backup → fresh target restore → formal migration → Activity rebuild 的真实往返；所有纳入表与 Activity 全量快照一致；
5. manifest migration checksum 漂移、dump 损坏和非空目标重复恢复均已有失败验证；前两种 preflight 失败后目标保持空，重复恢复不会改变既有目标数据。
6. 阶段 PR #9 已通过 Repo Hygiene、Repository Checker Tests、M0 Core Contracts、Go Server、Web App 与聚合 `Candidate Quality`，使用 merge commit 晋级 `master` 后已 fast-forward 回流 `dev`；
7. ADR-0011、正式 application query 与真实 PostgreSQL 测试已建立 Deployment 的 Environment + CI Run 组合读取权限、not-found 失败语义、归档历史保留与敏感字段排除；
8. Web 默认代表入口已切换为 Deployment succeeded / failed，并完成 loading、error、桌面与 390px 窄屏复核；原型仍使用明示静态 fixture，没有新增临时业务 HTTP handler。
9. PostgreSQL 集成共享 runner 已统一为连续两次真实 `psql SELECT 1` 后才放行，消除了容器初始化重启窗口的一次宿主端 EOF；M0、正式服务与双实例恢复入口已重新实际通过。
10. ADR-0012 已确认 M1 先建立本地账号与服务端 Session，OIDC 延后并复用同一 user、membership 与 Session 边界；首次管理员、Workspace `owner / member`、密码、账号锁定、Cookie、CSRF、request ID、版本化错误对象和 Workspace 选择语义已经冻结。
11. migration 005、Argon2id verifier、一次性 `nexus-bootstrap --password-stdin`、opaque Session、CSRF digest、当前 membership resolver 与 PostgreSQL store 已落地；数据库不保存原始 Session / CSRF token，CLI 不接受命令参数密码或默认凭据。
12. 本地认证 unit 与真实 PostgreSQL integration 已覆盖并发 bootstrap 只允许一个成功、5 次失败锁定 15 分钟、24 小时绝对 Session、成功登录重置、跨 Workspace 拒绝与单向撤销；当时公共登录和业务 HTTP 路由仍未开放。
13. 备份 relation 分类已把 `local_accounts` 纳入权威数据、把 `user_sessions` 归为只恢复 schema；双实例往返证明恢复后的 Argon2id verifier 仍能校验原密码，旧 Session 数据为空。
14. ADR-0013 与正式 transport 已建立必需 public origin / Host、显式可信代理 CIDR、右向左客户端 IP 解析、每 IP 每分钟 5 次窗口、4096 个有界客户端和 4 个并发密码校验；不可信 plaintext、转发 Header spoofing 和畸形链均在 credential verifier 前失败。
15. `POST /api/v1/auth/sessions`、`GET /api/v1/auth/session` 与 `DELETE /api/v1/auth/session` 已开放；登录限制为 4096-byte JSON，响应只返回安全 Session context，Cookie / Origin / CSRF / database digest、`no-store`、server-generated request ID 和版本化错误对象复用同一合同。
16. 真实 PostgreSQL HTTP integration 已覆盖 login → Cookie → session → logout → revoked Session；真实 Chrome HTTPS 复核已证明 Session Cookie 为 `Secure / HttpOnly / SameSite=Strict`、CSRF Cookie 可读但同样 Secure / Strict、错误 CSRF 不撤销、正确 CSRF 清除 Cookie、第二 HTTPS Origin 登录得到 `403 csrf_failed`。
17. ADR-0014 与第一个业务 HTTP handler 已建立路径 Workspace 选择、Session → current membership → `Principal` → 正式 query、显式公共 DTO、not-found 不可发现性、投影一致性验证和 `private, no-store` / `Vary: Cookie`。
18. 真实 PostgreSQL HTTP integration 已覆盖有效 Session 读取完整 Deployment Nexus View，以及跨 Workspace 与未知 Deployment 的同形 `404 not_found`；输出不包含 authorization、Jenkins 来源、receipt、digest、Secret 或内部投影字段。
19. Web canonical Deployment 页面已通过同源 typed adapter 消费公共 snake_case DTO，覆盖 runtime validation、nullable started time、canonical EntityRef、loading、error 与 retry；根路径的静态代表状态检视器继续独立保留。
20. Web 单元、lint、严格 TypeScript 与 production build 已通过；真实 Chrome 以网络边界响应验证了 canonical URL、API 请求和成功页面，未发现 console warning / error。真实 PostgreSQL HTTP 与浏览器 adapter 目前是两段证据，尚未冒充一次完整的 HTTPS browser → PostgreSQL E2E。
21. ADR-0015 已冻结唯一 HTTPS origin、必需的绝对 `RADISHNEXUS_WEB_ROOT`、API 优先装配、三个显式 HTML 页面、未知路径 `404`、哈希资源 immutable cache、HTML `no-cache` 与同源 CSP / 安全 Header。
22. Web 根路径已从静态检视器切换为最小 authenticated shell；Session bootstrap 区分未登录和系统失败，login / logout 复用正式 transport，密码与 token 不进入浏览器 storage。
23. Session context 中的 active Workspace 可供选择，但不固定到 Session 或替代服务端授权；在尚无 Deployment list API 时，用户用已知 `dpl_` 稳定 ID 进入 canonical 页面。
24. canonical Deployment 路径会在业务读取前验证 Session，业务 `401` 回到登录态；用户也能在 canonical URL 直接登录后继续读取同一对象。原静态代表检视器移动到 `/prototype/nexus-view`。
25. Go server 已把 production Web build 与 `/api/v1` 装配到同一个 handler，缺失、相对或无效 Web root 启动失败；未知页面、缺失资源、方法、缓存和安全 Header 已有定向测试。
26. 新的显式 browser fixture 使用正式 migration、application service、PostgreSQL auth store、production Web build 和临时 HTTPS listener。真实浏览器已验证匿名 `401`、登录 `201`、Session `200`、Deployment `200`、登出 `204`、Secure / Strict Cookie、无 local/session storage、390px 无横向溢出、canonical URL 重新登录和业务页 0 warning / 0 error。

## 下一步

最小 authenticated Web Shell 与浏览器到真实 PostgreSQL 的完整证据已经达到本地完成线。下一步优先建立可复验的最小自部署开发拓扑：先冻结 TLS reverse proxy、production Web build、Go server、显式 migration / bootstrap 与 PostgreSQL 持久卷在 Docker Compose 中的职责、镜像来源和失败语义，再实现新实例启动演练。该切片继续复用现有唯一 HTTPS origin 和 API，不同时开放 Deployment 列表、写入 API 或第二个业务对象端点。

文档协同技术方案与免费书面授权模板继续作为独立 M0 缺口评估，不与自部署拓扑同时铺开。当前 `dev` 切片不立即再次晋级 `master`；积累下一组可独立演示、测试和退出的候选后再创建阶段 PR。

当前完成线：新实例能够通过显式 CLI 且只有一次成功地建立本地管理员与 Workspace owner；公共 login / session / logout 在精确 HTTPS origin / Host、可信代理、限流、Secure Cookie、CSRF、request ID 与错误对象的唯一合同下开放；Deployment Nexus View 通过路径 Workspace、当前 membership、正式 `Principal` 和 application query 安全读取。production Web build 与 `/api/v1` 在同一 origin 交付，根 Shell 能登录、选择 Workspace、用已知稳定 ID 进入 canonical 页面并登出；恢复保留账号 verifier 但不恢复登录态。

当前停止线：除三个认证路由和 Deployment Nexus View 读取外不继续开放业务 handler；不信任用户身份或转发 Header，不提供 insecure Cookie / HTTP fallback，不把每进程限流冒充多副本或公网全局保护，不让 Web 用 fixture、任意 SPA fallback 或 stale cache 隐藏真实失败。自部署拓扑闭环前不铺开 Deployment 列表 / 写入、OIDC、邀请、密码重置、MFA 或账号合并；其余 Deployment executor、Flutter、CRDT、通用 RBAC、Repository、插件市场和多 provider 停止线保持不变。

## 开放问题

- PostgreSQL 正式支持版本矩阵，以及生产升级的 forward repair 与恢复流程；
- EntityID 生成算法，Document、Repository 等尚未切片类型的前缀和后续 PostgreSQL schema；
- 文档编辑器和 CRDT；
- 首版插件运行方式；
- Initiative、Component 与 Project 的首版导航表现；
- Decision 的复核周期和替代交互；
- `.nexus` 上下文包的开放格式与脱敏规则；
- SDK 和插件的具体开放源码许可证；
- OIDC provider、claim mapping、账号关联与本地应急登录策略；
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
