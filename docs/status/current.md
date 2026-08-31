# RadishNexus 当前状态

状态日期：2026-08-31

## 当前阶段

产品定义、架构基线、仓库治理基线与 M0.5 Golden Path / M1 Web 平台基础纵向原型。

当前已经建立本地和 GitHub 远端仓库、`master` / `dev` 分支、协作规则、GitHub 模板、仓库检查器与 `Candidate Quality` 质量门；`master` Ruleset 已在远端启用。Project、Initiative、Component、Decision、Environment 和 EntityLink 的首批最小业务字段已经冻结，稳定引用、授权解析、事件 envelope 与 Activity 投影已由 ADR-0002 接受为 M0 契约基线。可丢弃的 Go + PostgreSQL 核心契约实验已经通过。正式 `server/` Go module、显式 forward-only migration runner、Thread → Decision → Ticket 权限纵向切片、版本化 Activity 重建、Decision / Ticket Nexus View 读取查询和最小 transport adapter 已经建立；正式 Component、已验证 Jenkins delivery → CI Run 原子记录和安全读取已经建立。正式 Environment、环境级写授权、显式终态 staging Deployment、`deploys` 关系、`deployment.recorded` 投影与 Workspace 作用域安全读取已经通过真实 PostgreSQL 验证。PostgreSQL 17 同 major 的版本化备份、全新空目标恢复、migration 校验和 Activity 重建已经由 ADR-0010、显式 CLI 与双实例演练建立。上述 M0 正式服务、Web 代表原型与恢复基线已通过 PR #9 的远端 `Candidate Quality`，使用 merge commit 晋级 `master` 并 fast-forward 回流 `dev`。正式 `web/` React + TypeScript 基线现已覆盖 Decision、CI Run 与 Deployment Nexus View 代表交互；本地账号的公共 login / session / logout transport、第一个 Session 作用域的 Deployment Nexus View 业务读取端点，以及同源 authenticated Web Shell 已经建立。真实 PostgreSQL + production Web build + HTTPS 浏览器现已从登录、Workspace 选择进入 canonical Deployment 并完成登出。首个正式 `deploy/` Docker Compose 开发拓扑也已从全新命名 volume 完成固定工件、显式 migration / bootstrap、唯一 Caddy HTTPS origin、文件 Secret、持久化 PostgreSQL 和认证闭环演练；完整产品导航、插件 runtime、公网生产拓扑和客户端尚未建立。

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
- [开发指南](../development/README.md)
- [M0 核心契约实验](../../experiments/m0-core-contracts/README.md)
- [正式 Go 服务](../../server/README.md)
- [Docker Compose 自部署开发拓扑](../../deploy/README.md)

## 今日进展（2026-08-31）

今日已经完成首个正式 Docker Compose 自部署开发拓扑的决策、实现和新实例证据，没有扩展产品功能面：

1. ADR-0016 已接受，冻结 Caddy、Go server、production Web build、PostgreSQL 与四类 operation 的单一职责、内部网络、持久卷、固定镜像 digest、Secret 和失败语义；
2. `deploy/compose.yaml`、固定 multi-platform 基础镜像的 multi-stage `deploy/Dockerfile`、Caddy 配置、非密 `.env` 示例和运行手册已经建立；只有 Caddy 发布唯一 HTTPS 端口，应用与 PostgreSQL 均无宿主端口；
3. 数据库密码改用按 service 授权的 Compose Secret 文件。公共 runtime config 只在内存中把密码安全编码到现有 PostgreSQL URL，server、migration、bootstrap、backup 和 restore 复用同一入口；既有完整 `DATABASE_URL` 仍兼容；
4. application、operation 与 Caddy 容器使用只读根文件系统、`no-new-privileges` 和明确 capability 集；Caddy 官方二进制所需的 `NET_BIND_SERVICE` 是唯一加回的 capability，当前仍只监听非特权 HTTPS 端口；
5. `./scripts/check-self-hosted-compose.sh` 已建立隔离演练：随机 project、临时 Secret、随机 HTTPS 端口、全新 volumes，退出时只删除本次工件，不触碰用户实例或备份；
6. 完整演练已通过 PostgreSQL ready → 显式 migration → 第一次 bootstrap 成功 → 第二次 bootstrap 拒绝 → app / Caddy healthy → CA 校验 → login `201` → Session `200` → logout `204` → 已撤销 Session `401`；
7. 演练登录刻意携带伪造 `X-Forwarded-For`、`X-Forwarded-Host` 与 `X-Forwarded-Proto`，Caddy 清洗后认证仍按可信直接 peer 和精确 origin 工作；真实 port binding 检查确认 Go server 与 PostgreSQL 没有宿主映射；
8. 真实 Chrome 再次复核 authenticated Web Shell，登录、Workspace、canonical Deployment 的 Current / Relations / Timeline 与退出均通过，最终业务页 console 为 0 warning / 0 error；
9. Go server、Web、repository checker、Compose / Caddy 静态配置、Dockerfile build check、固定镜像构建和仓库演练均已实际执行；没有新增应用依赖或改变 lockfile。

## 下一步

自部署开发拓扑已经达到本地完成线。下一优先级回到 M0.5 Golden Path 尚未贯通的上游沟通入口：先冻结单 Workspace 下 Channel / Message / Thread 的最小对象、权限、稳定引用、事件与实时收发技术边界，再交付一个能从真实讨论进入既有 Thread → Decision → Ticket 链的小而完整纵向切片。该工作必须先复用现有 Workspace membership、EntityLink、Activity、Outbox 和错误语义，不另造聊天专用身份或权限系统。

文档协同技术评估、免费书面授权模板和版本化结构化导入导出仍是独立 M0 / M1 缺口，按路线图后续逐项推进，不与第一个沟通切片同时铺开。当前 `dev` 批次尚未提交或晋级 `master`；是否提交、创建阶段 PR 或写入远程状态需项目所有者另行明确授权。

当前完成线：受控开发者可以从固定工件建立全新 Compose 实例，显式 migration 和且仅一次 bootstrap 后经唯一 HTTPS origin 登录；Secret 不进入 Compose 环境或镜像，应用和数据库不发布宿主端口，健康、日志、备份恢复与失败定位都有明确入口。既有 authenticated Web Shell 与 Deployment Nexus View 的权限、Cookie、CSRF、origin、proxy 和缓存合同保持不变。

当前停止线：不把当前 Compose 冒充公网生产、高可用或跨 major 升级方案；不自动 migration / bootstrap，不提供默认 credential，不放宽可信代理，不引入 HTTP fallback、浮动镜像或明文 Secret。下一个沟通切片不同时建设完整聊天、搜索、附件、通知、WebSocket 集群、CRDT、插件市场或 Flutter。

## 明日事项（2026-09-01）

明日核心目标是为 Golden Path 的真实讨论入口做最小边界冻结，而不是立即铺开完整聊天：

1. 盘点现有 `threads`、Workspace membership、Project、EntityLink、领域事件、Outbox 与 Activity 合同，明确哪些可直接复用、哪些对象仍缺失；
2. 对照 Golden Path 的“讨论 → Decision → Ticket”故事，冻结 Channel、Message、Thread 的最小字段、归属、公开 / restricted 可见性、作者与不可变来源边界；
3. 用可丢弃技术实验验证单进程实时收发、重连、重复提交幂等和权限变化后的订阅收敛，不提前承诺集群协议或独立消息中间件；
4. 形成 ADR 或专题设计与可执行验证计划；只有对象、权限和事件边界足够明确时，才开始一个最薄的 migration + application service + transport + Web 纵向切片。

明日完成线：能够明确解释一条 Message 如何进入 Thread、Thread 如何在不复制全文的前提下成为 Decision evidence，以及关系、Activity 和实时推送如何复用同一当前权限；至少一个真实技术实验能证明选择的实时传输方向可行并暴露失败语义。

明日停止线：不建立完整聊天 UI、附件、表情、搜索、未读、通知或多副本消息系统；不让引用授予权限，不把客户端身份、频道可见性或订阅状态当作可信服务端授权，不在边界未冻结时直接扩展公共 API。

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
