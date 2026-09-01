# RadishNexus 当前状态

状态日期：2026-09-01

## 当前阶段

产品定义、架构基线、仓库治理基线与 M0.5 Golden Path / M1 Web 平台基础纵向原型。

当前已经建立本地和 GitHub 远端仓库、`master` / `dev` 分支、协作规则、GitHub 模板、仓库检查器与 `Candidate Quality` 质量门；`master` Ruleset 已在远端启用。Project、Initiative、Component、Decision、Environment 和 EntityLink 的首批最小业务字段已经冻结，稳定引用、授权解析、事件 envelope 与 Activity 投影已由 ADR-0002 接受为 M0 契约基线。可丢弃的 Go + PostgreSQL 核心契约实验已经通过。正式 `server/` Go module、显式 forward-only migration runner、Thread → Decision → Ticket 权限纵向切片、版本化 Activity 重建、Decision / Ticket Nexus View 读取查询和最小 transport adapter 已经建立；正式 Component、已验证 Jenkins delivery → CI Run 原子记录和安全读取已经建立。正式 Environment、环境级写授权、显式终态 staging Deployment、`deploys` 关系、`deployment.recorded` 投影与 Workspace 作用域安全读取已经通过真实 PostgreSQL 验证。PostgreSQL 17 同 major 的版本化备份、全新空目标恢复、migration 校验和 Activity 重建已经由 ADR-0010、显式 CLI 与双实例演练建立。上述 M0 正式服务、Web 代表原型与恢复基线已通过 PR #9 的远端 `Candidate Quality`，使用 merge commit 晋级 `master` 并 fast-forward 回流 `dev`。正式 `web/` React + TypeScript 基线现已覆盖 Decision、CI Run 与 Deployment Nexus View 代表交互；本地账号的公共 login / session / logout transport、第一个 Session 作用域的 Deployment Nexus View 业务读取端点，以及同源 authenticated Web Shell 已经建立。真实 PostgreSQL + production Web build + HTTPS 浏览器现已从登录、Workspace 选择进入 canonical Deployment 并完成登出。首个正式 `deploy/` Docker Compose 开发拓扑也已从全新命名 volume 完成固定工件、显式 migration / bootstrap、唯一 Caddy HTTPS origin、文件 Secret、持久化 PostgreSQL 和认证闭环演练。Channel / Message / messaging-origin Thread 的最小字段、幂等、权限、来源与实时恢复语义已由 ADR-0017 冻结并通过零依赖单进程 HTTP + SSE 可丢弃实验；migration 006、正式 command application service、canonical Message application query 与 PostgreSQL / 备份恢复验证已经落地。ADR-0018 现已进一步冻结并实现单 Channel 历史、发送和从 Message 发起 Thread 的 Session 作用域短请求；canonical Channel Web 页面也已接入权限过滤分页、幂等发送和结构化 Thread 来源，并通过真实 Session + PostgreSQL + production build + HTTPS 浏览器复核。正式实时连接尚未建立；完整产品导航、插件 runtime、公网生产拓扑和客户端尚未建立。

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
- ADR-0016 已冻结首个 Docker Compose 开发拓扑的 Caddy / Go server / PostgreSQL / operation 职责、固定镜像 digest、内部网络、文件 Secret、显式初始化与失败语义；该拓扑不是公网生产、高可用或跨 PostgreSQL major 方案。
- ADR-0017 已冻结 `channel` / `chn_`、`message` / `msg_`、Message 写入幂等、messaging-origin Thread 的 Channel 权限层、`started-from` 来源、正文最小化和 canonical resync；SSE 只用于 M0.5 单进程实验，M2 版本化 WebSocket 目标不变。
- ADR-0018 已冻结三个 Session 作用域 Channel Message 短请求、版本化 opaque cursor、显式安全 DTO、写入 Origin / 双重 CSRF、状态码、不可发现性与 `private, no-store`；它不开放实验 SSE 或长连接 fallback。
- migration 006 已正式注册 Channel、Message 与 `started-from`，并以外键、唯一约束和 deferred constraint trigger 固化同 Workspace / Project / Channel 来源、不可变 Message、单一 Thread 来源与幂等边界。
- 正式 application service 已能原子创建 Message 和从 Message 发起 Thread；事件与实时 Outbox 不携带正文或 `client_operation_id`，messaging-origin Thread 继续贯通既有 Decision / Ticket 链。
- canonical Message application query 已按当前 Channel + Thread 权限过滤正文，以 `(created_at, message_id)` exclusive keyset 稳定向更旧内容分页；公共 transport 用版本 1 opaque cursor 封装该边界，每次翻页仍重新授权。
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
- Channel Message 公共 transport 已开放 canonical history、幂等发送和从 Message 发起 Thread；GET 不要求 CSRF，两个 POST 同时验证精确 Origin、double-submit 与数据库 digest，并把 server request ID 作为 `web` invocation correlation。
- Message / Thread 公共 DTO 只返回结构化来源、author、正文、可见性与受控时间，不返回 `client_operation_id`、角色、membership、事件或 Outbox；application 投影与路径 scope 不一致时 fail closed。
- Decision Nexus View 代表原型已经表达 Current、Relations 和 Timeline，并覆盖 loading、empty、error、restricted placeholder 与窄屏布局。
- CI Run Nexus View 代表交互已经表达 succeeded / failed、四个受控时间、当前 Component、唯一 `ci-run.recorded` Timeline、loading、error 与窄屏布局；构建结果没有被表现为 Deployment。
- CI Run Web fixture 与后端安全投影同形，不携带 source ID、external run key、delivery receipt、digest、Secret、原始 payload 或未经治理的外部 URL；浏览器验证未发现必须新增 transport 的需求。
- Deployment Nexus View 代表交互已经表达 succeeded / failed、三个受控时间、Environment、来源 CI Run、`deploys` Relation 与唯一 `deployment.recorded` Timeline；失败态明确保留来源构建成功事实，不把失败 Deployment 改写成 CI Run 失败。
- Deployment Web fixture 不携带 authorization、调用 source、Jenkins 来源字段或执行日志；桌面与 390px 窄屏真实浏览器复核通过，并修正了旧 CI Run 页面标题漂移。
- Web 原型只消费权限过滤后的 discriminated union；`restricted` 形状不携带 EntityRef、对象类型、关系类型、标题、来源或时间，`hidden` 目标不进入客户端数据。
- Web 根路径已经改为 authenticated shell，先 bootstrap 正式 Session，再提供 login、当前 Workspace 选择、已知 Deployment / Channel ID 入口和 logout；原静态代表检视器移动到显式 `/prototype/nexus-view`，不作为真实失败 fallback。
- canonical `/workspaces/{workspace_id}/deployments/{deployment_id}` 页面先完成 Session bootstrap，再通过同源、no-store 的类型化 adapter 消费真实公共 DTO；业务 `401` 回到登录态，网络和契约错误显式失败。
- canonical `/workspaces/{workspace_id}/channels/{channel_id}` 页面复用同一 Session bootstrap，通过运行时校验的 adapter 分页读取、发送 Message 并从 Message 创建 Thread；模糊发送失败保留同一幂等键，`200` 精确重试不追加重复项。
- Channel 后续请求返回 `404` 时，Web 会立即移除已经渲染的 Message 正文和本地草稿；`401` 回到登录态，Thread 创建不复制 Source Message 正文。
- Go server 只从必需的绝对 `RADISHNEXUS_WEB_ROOT` 交付 production build，HTML 仅开放根路径、代表原型、canonical Deployment 和 canonical Channel；未知页面不使用任意 SPA fallback，哈希资源 immutable cache，HTML `no-cache`。
- Web 不保存密码、Session token、Workspace 权限快照或业务响应到 `localStorage` / `sessionStorage`；Workspace 选择不改变服务端权限，业务路由仍按当前 membership 解析。
- Web fixture 已统一修正为 `entity://type/id` canonical 引用和正式 `tkt_` 前缀；真实浏览器网络边界验证了 API request、nullable started time、Relations 与 Timeline 渲染且没有 console warning / error。
- 真实 PostgreSQL + migration + application 写入 + HTTPS + production Web build 的浏览器 fixture 已进一步验证 Channel history `200`、Message `201`、Source Message → Thread `201`、390px 长 ID 无横向溢出、无 Web Storage、登出 `204`、Cookie 清理与 canonical Channel URL 重新登录；除预期匿名 Session `401` resource entry 外没有新增 console warning / error。
- 全新 Compose project 演练已验证 PostgreSQL readiness → 显式 migration → 唯一 bootstrap → app / Caddy 启动 → HTTPS login / Session / logout；第二次 bootstrap 被拒绝，伪造 `X-Forwarded-*` 不改变可信边界，Go server 和 PostgreSQL 均无宿主端口。
- 所有 database-backed CLI 与 server 现在可以通过 `RADISHNEXUS_DATABASE_PASSWORD_FILE` 读取单行文件 Secret 并在内存中装配 PostgreSQL URL；现有完整 `DATABASE_URL` 方式保持兼容，歧义、相对路径、空值、多行和读取失败均 fail closed。
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
- [ADR-0016：最小 Docker Compose 自部署开发拓扑](../adr/0016-minimal-docker-compose-self-hosting.md)
- [ADR-0017：Channel / Message 边界与单进程实时收发实验](../adr/0017-channel-message-boundary-and-single-process-realtime.md)
- [ADR-0018：Session 作用域下的 Channel Message 短请求 Transport](../adr/0018-session-scoped-channel-message-transport.md)
- [开发指南](../development/README.md)
- [M0 核心契约实验](../../experiments/m0-core-contracts/README.md)
- [消息实时收发实验](../../experiments/messaging-realtime/README.md)
- [正式 Go 服务](../../server/README.md)
- [Docker Compose 自部署开发拓扑](../../deploy/README.md)

## 今日进展（2026-09-01）

今日完成了 Golden Path 沟通入口从边界冻结到正式 PostgreSQL application slice 的第一段推进；可丢弃 SSE 实验仍未接入正式产品：

1. 盘点确认既有 Workspace membership、Project 角色、restricted Thread、EntityLink、领域事件、Outbox、Activity 和公共错误语义可以复用；正式缺口集中在 Channel、Message、Message → Thread 来源和实时恢复；
2. ADR-0017 已接受，冻结 `channel` / `chn_` 与 `message` / `msg_` 最小字段、16 KiB 不可变 UTF-8 正文、Project / restricted Channel、发送能力和归档语义；
3. Message 幂等范围固定为 `(workspace_id, channel_id, author_id, client_operation_id)`；相同正文返回既有对象，不同正文冲突，幂等键不进入 EntityRef、领域事件、Activity 或其他读者可见投影；
4. messaging-origin Thread 新增不可变 origin Channel 授权层，并用 `thread --started-from--> message` asserted + user EntityLink 保留来源；Thread → Decision 继续 `derived-from`，Decision → Ticket 继续 `implements`，全链不复制 Message 正文；
5. 可丢弃 `experiments/messaging-realtime/` 采用零新增依赖的 Go HTTP command + SSE，验证 cursor、`Last-Event-ID` 有界回放、process generation、canonical resync、空闲权限撤销和慢消费者隔离；这不替代 M2 WebSocket 目标；
6. 竞态测试已证明 64 路并发重试只有一个创建者，跨 Channel Message ID 不重复，断线可补发，回放过期只返回无业务数据控制事件，权限撤销后连接关闭且重连不可发现，1,000 次发布不受慢订阅阻塞；
7. migration 006 已注册 Channel、Message、Channel membership 和 `started-from`，以数据库约束固化 Message 不可变、同 Channel reply、单一 Thread 来源与 printable ASCII 幂等键；
8. 正式 application service 已在真实事务中实现 Message 创建、同正文重试、变化正文冲突和从 Message 发起 Thread，复用当前 Workspace / Project / Channel / Thread 权限；
9. `message.created` 与 `thread.started` 只写安全引用并交给独立 `realtime-dispatcher` Outbox consumer；Message 正文和幂等键不进入事件、Activity 或关系投影；
10. 真实 PostgreSQL 已验证并发重试只有一个创建者、权限撤销立即隐藏既有 Message / Thread、事件冲突整单回滚、跨 Channel reply 和无来源 Thread 失败；`Message → Thread → Decision` 已贯通；
11. PostgreSQL 17 双实例备份恢复已纳入非空 Channel、Message 和 messaging-origin Thread fixture，恢复前后权威表快照一致，Activity 仍从事件重建且不复制消息噪声；
12. 正式 server 的统一 15 秒写超时仍是长连接接入前的明确边界；可丢弃实验与 PostgreSQL command slice 未绕过它开放长连接，也未新增第三方依赖或 lockfile 变化。
13. canonical Message application query 已建立：每页 `1..100` 条，最新页向旧页使用排他 keyset，同时间戳由 Message ID 决胜，页内保持时间正序，DTO 不含 `client_operation_id`；
14. 真实 PostgreSQL 已验证分页无重复、新消息只在刷新头页时出现、restricted Thread 回复在 limit 前过滤、Channel 权限撤销后查询不可发现，归档 Channel 仍可读取历史。
15. ADR-0018 已接受，冻结单 Channel 历史、发送和从 Message 发起 Thread 的 Session 路由、opaque cursor、显式 DTO、CSRF / Origin、错误和 `private, no-store`；
16. 正式 handler 已复用认证 adapter 与 application service，真实 Session + PostgreSQL 已验证 `201` 创建、`200` 精确重试、`409` 变化重放、跨 Channel Source `404`、非法 cursor、存储态 CSRF、Channel membership 与 Session 撤销；
17. authenticated Web Shell 已新增已知 `chn_` 入口和精确 canonical Channel HTML allowlist；未知嵌套路由仍返回 `404`，未引入 router、状态库或新依赖；
18. Channel adapter 对 Message / Thread DTO、opaque cursor、结构化 ref、受控时间、状态码和 CSRF fail closed；安全入口错误不会被误报为普通角色拒绝；
19. 页面已覆盖 initial loading、empty、分页、发送、精确重试、Thread 创建、局部错误和权限撤销；模糊发送失败为未变化正文保留同一 `client_operation_id`，后续 `404` 清空已显示正文；
20. 真实 HTTPS 浏览器已从 contributor 登录进入 `chn_project`，完成权威 Message 写入和 Source Message → Thread 创建；390px 下随机长 ID 的页面宽度保持 `390 == 390`，登出后 Cookie 与页面正文均不再保留；
21. 最新 server 与 production Web 已重新构建进一次性 Compose application image，Caddy 唯一 HTTPS origin、显式 migration / bootstrap、认证闭环、可信代理清洗和任务专属资源清理继续通过。

## 下一步

下一优先级是把新建 Thread 接入既有 Decision / Ticket 权威链，而不是扩展完整聊天或直接搬运实验 transport：

1. 盘点并冻结 Session 作用域下 Thread → Proposed Decision → Accepted Decision → Ticket 的最小公共读写合同，复用既有权限、CSRF、错误和不可发现性；
2. 让刚创建的 Thread 进入最小 canonical 协作页面，保留 `started-from` / `derived-from` / `implements` 结构化来源，不复制 Message 正文；
3. 覆盖 contributor / decider 分权、restricted evidence、人工确认、重复命令和权限撤销，并用真实 PostgreSQL 与浏览器验证；
4. 完成这段短请求协作链的真实浏览器复核后，再决定是否保留临时 SSE transport；正式实时入口仍须先解决独立 timeout、heartbeat、Caddy flush、连接上限与优雅关闭。

文档协同技术评估、免费书面授权模板和版本化结构化导入导出仍是独立 M0 / M1 缺口，按路线图后续逐项推进，不与第一个沟通切片同时铺开。ADR / 可丢弃实验、正式 PostgreSQL command slice、canonical query 与 Session transport 已在 `dev` 分别提交为 `e5c5c55`、`ef6c838`、`d1484af`、`5420d52`；当前 Web 闭环随本批次记录，均未晋级 `master`，创建阶段 PR 或写入远程状态仍需项目所有者另行明确授权。

当前完成线：正式数据库与 application service 已证明 Message 能以稳定身份和幂等 command 进入 Channel、能够不复制正文地发起 Thread 并成为 Decision evidence；canonical query、Session transport 与 Web 页面已以稳定、无重复、权限过滤的 opaque cursor 分页恢复权威正文，并安全开放幂等发送与 Thread 来源；关系、Activity、备份恢复和权限撤销复用同一语义。Thread 后续协作页面与正式实时连接仍属于下一完成线。

当前停止线：实验不是正式 server、公共 API 或产品协议；不建立完整聊天 UI、附件、表情、搜索、未读、通知、多副本 fan-out 或独立消息中间件；不让引用、旧 membership、客户端身份或订阅状态授予权限；不在独立长连接合同落地前把 SSE / WebSocket 挂入现有 15 秒 listener。

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
