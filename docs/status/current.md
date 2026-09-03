# RadishNexus 当前状态

状态日期：2026-09-03

## 当前阶段

产品定义、架构基线、仓库治理基线与 M0.5 Golden Path / M1 Web 平台基础纵向原型。

当前已经建立本地和 GitHub 远端仓库、`master` / `dev` 分支、协作规则、GitHub 模板、仓库检查器与 `Candidate Quality` 质量门；`master` Ruleset 已在远端启用。Project、Initiative、Component、Decision、Environment 和 EntityLink 的首批最小业务字段已经冻结，稳定引用、授权解析、事件 envelope 与 Activity 投影已由 ADR-0002 接受为 M0 契约基线。可丢弃的 Go + PostgreSQL 核心契约实验已经通过。正式 `server/` Go module、显式 forward-only migration runner、Thread → Decision → Ticket 权限纵向切片、版本化 Activity 重建、Thread / Decision / Ticket Nexus View 读取查询和 Session transport 已经建立；正式 Component、已验证 Jenkins delivery → CI Run 原子记录和安全读取已经建立。正式 Environment、环境级写授权、显式终态 staging Deployment、`deploys` 关系、`deployment.recorded` 投影与 Workspace 作用域安全读取已经通过真实 PostgreSQL 验证。PostgreSQL 17 同 major 的版本化备份、全新空目标恢复、migration 校验和 Activity 重建已经由 ADR-0010、显式 CLI 与双实例演练建立。上述 M0 正式服务、Web 代表原型与恢复基线已通过 PR #9 的远端 `Candidate Quality`，使用 merge commit 晋级 `master` 并 fast-forward 回流 `dev`。正式 `web/` React + TypeScript 基线现已覆盖 Decision、CI Run 与 Deployment Nexus View 代表交互；本地账号的公共 login / session / logout transport、第一个 Session 作用域的 Deployment Nexus View 业务读取端点，以及同源 authenticated Web Shell 已经建立。真实 PostgreSQL + production Web build + HTTPS 浏览器现已从登录、Workspace 选择进入 canonical Deployment 并完成登出。首个正式 `deploy/` Docker Compose 开发拓扑也已从全新命名 volume 完成固定工件、显式 migration / bootstrap、唯一 Caddy HTTPS origin、文件 Secret、持久化 PostgreSQL 和认证闭环演练。Channel / Message / messaging-origin Thread 的最小字段、幂等、权限、来源与实时恢复语义已由 ADR-0017 冻结；migration 006、正式 command application service、canonical Message application query 与 PostgreSQL / 备份恢复验证已经落地。ADR-0018 已冻结并实现单 Channel 历史、发送和从 Message 发起 Thread 的 Session 作用域短请求；canonical Channel Web 页面也已接入权限过滤分页、幂等发送和结构化 Thread 来源，并通过真实 Session + PostgreSQL + production build + HTTPS 浏览器复核。ADR-0019 与 migration 007 现已进一步冻结并实现 Thread → Proposed Decision → 人工 Accepted Decision → Ticket 的 Session 路由、结构化来源和不可变 command receipt；对应 canonical Web 页面已通过 contributor / decider 双账号真实浏览器验收。ADR-0020 现已接受并实现唯一正式 Go server 内的 Session 作用域单进程 Message SSE，真实 PostgreSQL、竞态测试与固定 Caddy Compose HTTPS 已证明 `ready`、`message.created`、回放、撤权、资源上限和关闭边界；旧的可丢弃实时实验已删除。canonical Channel Web 现已按“先建立 `ready` 边界、再读取 canonical history、随后合并增量”接入 SSE，并通过真实 PostgreSQL、production build、固定 Caddy HTTPS 与内置浏览器验收；完整产品导航、插件 runtime、公网生产拓扑和 Flutter 客户端仍未建立。

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
- ADR-0017 已冻结 `channel` / `chn_`、`message` / `msg_`、Message 写入幂等、messaging-origin Thread 的 Channel 权限层、`started-from` 来源、正文最小化和 canonical resync；其可丢弃实验已在正式切片覆盖后删除。
- ADR-0018 已冻结三个 Session 作用域 Channel Message 短请求、版本化 opaque cursor、显式安全 DTO、写入 Origin / 双重 CSRF、状态码、不可发现性与 `private, no-store`；写入仍不迁移到长连接。
- ADR-0019 已冻结六个 Session 作用域 Thread / Decision / Ticket 读写路由、人工 acceptance、结构化来源、显式 DTO 与 target-scoped 用户 command receipt；receipt 不进入普通读取、事件或 Activity，也不会绕过当前权限。
- ADR-0020 已冻结 `GET /api/v1/workspaces/{workspace_id}/channels/{channel_id}/events`、`ready / message.created / resync-required / access-revoked`、process generation + Channel scope opaque cursor、1024 ID replay、15 秒 heartbeat、5 秒写 deadline、256 / 4 / 64 连接上限和先关闭 hub 的 graceful shutdown；hub 不缓存正文或权限结果。
- migration 006 已正式注册 Channel、Message 与 `started-from`，并以外键、唯一约束和 deferred constraint trigger 固化同 Workspace / Project / Channel 来源、不可变 Message、单一 Thread 来源与幂等边界。
- migration 007 已新增 immutable `collaboration_command_receipts`；同一 Workspace、actor、command、target 与 `client_operation_id` 的 canonical payload 精确重试返回原结果，digest 变化冲突，receipt 与业务事实、EntityLink、事件和 Outbox 同事务提交。
- 正式 application service 已能原子创建 Message 和从 Message 发起 Thread；事件与实时 Outbox 不携带正文或 `client_operation_id`，messaging-origin Thread 继续贯通既有 Decision / Ticket 链。
- canonical Message application query 已按当前 Channel + Thread 权限过滤正文，以 `(created_at, message_id)` exclusive keyset 稳定向更旧内容分页；公共 transport 用版本 1 opaque cursor 封装该边界，每次翻页仍重新授权。
- 正式 application service 已完成 Thread → Proposed Decision → Accepted Decision → Ticket，并把 command receipt、EntityLink、领域事件和 Outbox 与业务状态放在同一事务；接受动作只允许用户主体，公共入口还要求显式 `confirmed=true`。
- Jenkins application service 只接收已完成来源认证和字段映射的 `VerifiedJenkinsDelivery`；receipt、CI Run、`ci-run.recorded` 和 Outbox 在同一事务提交，不保存 Secret 或原始 webhook body。
- 相同 Jenkins delivery 和 digest 只返回既有 CI Run；digest 改变或不同 delivery 映射到同一 external run 时 fail closed，事件冲突会连同 receipt 与 CI Run 一起回滚。
- 当前只接收 `succeeded / failed / canceled` 完成事实；尚未冻结 Jenkins HTTP route、HMAC/签名协议、失败审计、运行中更新或多 provider 抽象。
- `RecordStagingDeployment` 只接受明确用户的 `web / api` invocation；目标必须是 active staging Environment，来源必须是 succeeded CI Run，调用者必须是 active Workspace 成员并持有该 Environment 的 active 显式授权。
- Deployment、所使用的 authorization、操作者、来源和受控时间进入不可变权威记录；Deployment、asserted user `deploys` 关系、`deployment.recorded` 与 Outbox 同事务提交，任一步失败全部回滚。
- CI Run application service 不调用 Deployment service；真实 PostgreSQL 用例已证明 CI Run 成功后、显式命令前不存在 Deployment 或 `deployment.*` 事件。当前 command 只记录外部已完成终态，不执行部署、不读取 Secret，也不支持 production、审批、回滚或运行中状态。
- `nexus-backup` 只备份与当前 migration artifact identity 完全一致且 relation 已完整分类的数据库；未知 relation、migration 漂移和非 PostgreSQL 17 来源均 fail closed。
- 备份工件固定包含 manifest 与 custom-format dump，保留稳定 ID、业务表、授权 provenance、EntityLink、领域事件、inbound / collaboration command receipt、必要 Outbox 与 migration history；`activity_items` 数据默认排除。
- `nexus-restore` 只接受 checksum 完整的受信工件和全新空目标，不使用 `--clean` 或自动覆盖；恢复通过显式 TOC 先装载 EntityType 注册表，再以单事务恢复其余事实、运行正式 migration 并重建 Activity。
- 双实例 PostgreSQL 17 演练已经证明恢复前后所有纳入表与 Activity 全量快照一致；manifest migration 漂移、dump 损坏和非空目标重复恢复均失败且不改变受保护目标。
- contributor 不能确认 Decision；decider 必须能读取全部 evidence 后才能人工确认；Project admin 也不会自动穿透 restricted Thread。
- Nexus View application query 已能为 Thread、Decision、Ticket、CI Run 和 Deployment 返回 Current、Relations 和 Timeline，并在同一 repeatable-read 事务中按当前权限解析；messaging-origin Thread Current 保留可读 origin Channel，`started-from` Message 投影不环境化正文。
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
- Thread / Decision / Ticket 协作 transport 已开放三类 Nexus View 和三个写命令；GET 不要求 CSRF，POST 同时验证精确 Origin、double-submit 与数据库 digest，server request ID 只作 correlation，不替代 `client_operation_id`。
- 协作 DTO 只返回 Current、权限过滤后的 Relation / Timeline、结构化 `source_thread` / `source_decision` 与受控时间；restricted evidence 不含类型、ID、关系名、标题或时间，Message / Thread 正文、receipt、digest、角色和 membership 均不进入响应。
- Decision Nexus View 代表原型已经表达 Current、Relations 和 Timeline，并覆盖 loading、empty、error、restricted placeholder 与窄屏布局。
- CI Run Nexus View 代表交互已经表达 succeeded / failed、四个受控时间、当前 Component、唯一 `ci-run.recorded` Timeline、loading、error 与窄屏布局；构建结果没有被表现为 Deployment。
- CI Run Web fixture 与后端安全投影同形，不携带 source ID、external run key、delivery receipt、digest、Secret、原始 payload 或未经治理的外部 URL；浏览器验证未发现必须新增 transport 的需求。
- Deployment Nexus View 代表交互已经表达 succeeded / failed、三个受控时间、Environment、来源 CI Run、`deploys` Relation 与唯一 `deployment.recorded` Timeline；失败态明确保留来源构建成功事实，不把失败 Deployment 改写成 CI Run 失败。
- Deployment Web fixture 不携带 authorization、调用 source、Jenkins 来源字段或执行日志；桌面与 390px 窄屏真实浏览器复核通过，并修正了旧 CI Run 页面标题漂移。
- Web 原型只消费权限过滤后的 discriminated union；`restricted` 形状不携带 EntityRef、对象类型、关系类型、标题、来源或时间，`hidden` 目标不进入客户端数据。
- Web 根路径已经改为 authenticated shell，先 bootstrap 正式 Session，再提供 login、当前 Workspace 选择、已知 Deployment / Channel ID 入口和 logout；原静态代表检视器移动到显式 `/prototype/nexus-view`，不作为真实失败 fallback。
- canonical `/workspaces/{workspace_id}/deployments/{deployment_id}` 页面先完成 Session bootstrap，再通过同源、no-store 的类型化 adapter 消费真实公共 DTO；业务 `401` 回到登录态，网络和契约错误显式失败。
- canonical `/workspaces/{workspace_id}/channels/{channel_id}` 页面复用同一 Session bootstrap，通过运行时校验的 adapter 分页读取、发送 Message 并从 Message 创建 Thread；模糊发送失败保留同一幂等键，`200` 精确重试不追加重复项。
- canonical Channel 页面先建立原生 `EventSource` 并等待 `ready`，再读取 canonical history；history 期间到达的 `message.created` 进入缓冲，完成后按稳定 Message ID 去重合并。控制事件或 DTO 漂移会关闭连接并 fail closed。
- 浏览器自动重连继续使用原生 `Last-Event-ID`；`resync-required` 会创建新连接并全量重读，单次断线错误链只做一次 Session + canonical history 诊断，不建立隐藏 polling。`access-revoked`、诊断所得 `404` 与 Session `401` 分别清理正文和草稿或回到登录态，组件卸载会关闭流。
- Channel 后续请求返回 `404` 时，Web 会立即移除已经渲染的 Message 正文和本地草稿；`401` 回到登录态，Thread 创建不复制 Source Message 正文。
- canonical Thread / Decision / Ticket 页面复用同一 Session bootstrap 和 ADR-0019 六个短请求，分别承载 Proposed Decision、显式人工 acceptance 与 Ticket 创建；网络歧义且表单未变化时保留原 `client_operation_id`，成功后提供稳定 canonical 链接。
- 协作 Web adapter 对 Current、Relation、Timeline、状态、结构化来源和受控时间严格校验；restricted evidence 不携带类型、ID、关系名、标题或时间，Ticket 页面只展示权限过滤后的 Source Decision 与 `implements`。
- 协作后续请求返回 `404` 时，Web 会清除已渲染对象、草稿和成功结果；`401` 回到登录态，contributor 不能借由客户端状态绕过 acceptance 的服务端分权。
- Go server 只从必需的绝对 `RADISHNEXUS_WEB_ROOT` 交付 production build，HTML 仅开放根路径、代表原型以及 canonical Deployment、Channel、Thread、Decision 和 Ticket；未知或多余嵌套路由不使用任意 SPA fallback，哈希资源 immutable cache，HTML `no-cache`。
- Web 不保存密码、Session token、Workspace 权限快照或业务响应到 `localStorage` / `sessionStorage`；Workspace 选择不改变服务端权限，业务路由仍按当前 membership 解析。
- Web fixture 已统一修正为 `entity://type/id` canonical 引用和正式 `tkt_` 前缀；真实浏览器网络边界验证了 API request、nullable started time、Relations 与 Timeline 渲染且没有 console warning / error。
- 真实 PostgreSQL + migration + application 写入 + HTTPS + production Web build 的既有浏览器 fixture 已验证 Channel history `200`、Message `201`、Source Message → Thread `201`、390px 长 ID 无横向溢出、无 Web Storage、登出 `204`、Cookie 清理与 canonical Channel URL 重新登录；除预期匿名 Session `401` resource entry 外没有新增 console warning / error。
- fixture 现已额外生成 restricted Thread、contributor / decider 两个账号并挂载正式协作 handler；内置浏览器已在连接前信任精确导出的 fixture 证书，并完成 contributor 创建 Proposed Decision、越权 acceptance `403`、decider 明确 acceptance `200`、contributor 创建 Ticket `201` 的真实分权链。
- 浏览器已核对 `started-from`、`derived-from` 与 `implements` 的结构化来源和 canonical 跳转；撤销临时 Thread membership 后，Decision evidence 降级为不含关系名、类型、ID、标题和时间的 restricted 占位，Thread 后续命令返回真实 `404` 并清除已渲染内容、草稿与成功结果。
- Thread / Decision / Ticket 三个 canonical 页面在 390px 视口下的根节点、body 和全部可见元素均无横向溢出；双账号登出 / 重登保持原 canonical URL 和服务端 Session 语义。Web Storage 本轮只通过源码与自动化测试复核，未读取浏览器存储。
- 本轮临时证书的导出文件与 TLS 对端 SHA-256 均为 `468174FD18AE990A0A1E10568E30F9819A8ACD23224C319F4EC3EB4F6F2980D9`；严格 TLS 客户端验证为 `verify=0`，未绕过证书告警。验收结束后证书已按该指纹从登录钥匙串删除并复核无匹配，fixture 容器、状态目录和临时证书文件均已清理。
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
- [ADR-0019：Session 作用域下的 Thread、Decision 与 Ticket 协作 Transport](../adr/0019-session-scoped-thread-decision-ticket-transport.md)
- [ADR-0020：Session 作用域下的单进程 Message 实时增量](../adr/0020-session-scoped-single-process-message-realtime.md)
- [ADR-0021：Document 正文、编辑器与协同分层（提议）](../adr/0021-document-editor-and-collaboration-foundation.md)
- [开发指南](../development/README.md)
- [M0 核心契约实验](../../experiments/m0-core-contracts/README.md)
- [正式 Go 服务](../../server/README.md)
- [Docker Compose 自部署开发拓扑](../../deploy/README.md)

## 今日进展（2026-09-03）

今日完成正式单进程 Message 实时服务端合同、canonical Channel Web 消费状态机和 Caddy HTTPS 真实浏览器验收：

1. ADR-0020 已接受，固定 Session 作用域 SSE 路由、事件、cursor、回放、当前权限、heartbeat、写 deadline、连接上限、Caddy flush 与 graceful shutdown；写 command 继续使用既有短请求；
2. 新增正式进程内 hub，以随机 process generation、Channel scope digest 与 position 生成 canonical opaque cursor；每个 Channel 只保留最近 1024 个 Message ID，不缓存正文、作者、幂等键、Session 或权限结果；
3. 发布路径只在 PostgreSQL command 已成功提交且 `Created=true` 后通知；精确幂等 retry 不重复通知，慢消费者只收到合并 wake，不反压 Message transaction；
4. 新增 application `AuthorizeChannelRead` 与 `GetChannelMessage`，SSE 每条业务事件和 15 秒 heartbeat 都重新解析当前 Session、Workspace membership 与 Channel 权限；restricted Thread reply 对无权 Channel 读者被跳过且不泄漏 ref 或正文；
5. 正式事件只含 `ready`、`message.created`、`resync-required` 与 `access-revoked`；后两者数据固定为空对象，Message 继续复用短请求的最小 DTO；
6. 唯一 `http.Server` 保持普通请求 15 秒 `WriteTimeout`，只对完成认证授权的 SSE 清除全局 deadline；每次 write / flush 设置 5 秒 deadline，进程关闭先 shutdown hub 再执行 HTTP graceful shutdown；
7. 连接硬上限固定为每进程 256、每用户 4、每 Channel 64，超限在流开始前返回 `429 rate_limited`；generation 改变、跨 Channel、未来或过期 cursor 发送无业务数据的 `resync-required` 后关闭；
8. 正式 Go 全量 `go test -race ./...` 与真实 PostgreSQL integration 已通过，覆盖在线增量、断线回放、无 wake heartbeat 撤权、显式 access wake、慢消费者、资源释放、restricted reply、Session 撤销和提交后通知；
9. `check-self-hosted-compose.sh` 现会在全新实例登录后建立最小 Project / Channel，经固定 Caddy HTTPS 保持 SSE 打开并确认及时收到 `ready`，再走正式 CSRF 短请求创建 Message 并在同一流收到最小化 `message.created`；本次实跑通过；
10. Caddy 配置无需为全部响应强制 `flush_interval -1`；固定 2.11.4 对 `text/event-stream` 的识别已由真实门禁验证，更换代理或版本必须重跑；
11. 可丢弃的 `experiments/messaging-realtime` 第二 server、独立脚本和 CI job 已删除，竞态验证并入正式 Go Server job；本批次未新增依赖、migration 或 lockfile 变化；
12. 上一完成线的浏览器验收状态已先单独提交为 `e73d58d`，服务端 SSE 合同与正式实现已提交为 `d5b4c1e`；没有 push、PR 或远程写入；
13. Web 新增严格 `EventSource` adapter 与纯 reducer，固定 connect → `ready` → canonical history → buffered incremental merge 的顺序，并对事件名、cursor、空控制数据和 Message DTO 做运行时校验；
14. `message.created` 以稳定 Message ID 去重，相同 ID 内容漂移或 `ready` 前消息会关闭并 fail closed；`resync-required` 建立全新边界并全量重读，写 command 仍走既有幂等短请求；
15. 浏览器自动重连复用原生 `Last-Event-ID`，一次错误链只做一次 Session + canonical history 诊断；`access-revoked`、后续 `404` 与 Session `401` 分别清理正文 / 草稿或回到登录态，组件卸载关闭流；
16. authenticated browser fixture 改为真实 production Web build + PostgreSQL fixture upstream + 固定 Caddy 2.11.4 HTTPS，显式导出本次 Caddy CA，并保留 contributor / decider、restricted Thread 与数据库容器标识供验收；
17. 内置浏览器以 contributor 保持 canonical Channel SSE；decider 通过同一正式 HTTPS Session 短请求创建 Message 后，contributor 页面无需刷新即从 1 条增至 2 条。内置浏览器当前共享单一 Cookie 上下文，因此双账号浏览器登录采用 contributor → decider → contributor 的顺序复核，不把两个标签页冒充隔离上下文；
18. Caddy 重启后原生 EventSource 带 `Last-Event-ID` 自动恢复，随后创建的 Message 正常增量到达；contributor 无权读取的 restricted Thread reply 由 decider 写入后未出现在 contributor 页面，decider 重登可见该 reply；
19. canonical Channel 在 390px 下满足 `documentElement.scrollWidth === body.scrollWidth === innerWidth === 390` 且无可见元素横向溢出；登出 / 重登保持 canonical URL。最终暂停 contributor membership 后，heartbeat 发出 `access-revoked`，页面清空已渲染正文和未提交草稿；浏览器 console 无 warning / error；
20. Web Storage 只经源码与自动化测试复核，未读取浏览器存储。最终验收 CA SHA-256 为 `6414F94D0D09B628B76FE3178CD131778D85ABC527FDBE6C27DD3A06FAEA0297`，TLS leaf SHA-256 为 `D0B2225B633F90C1B46F6CA7BDCFC69A654C124F8BCE360469B326F711E2609C`；严格 CA 验证返回 `200`，未绕过告警。验收后 fixture 正常 PASS，CA 已按指纹从登录钥匙串删除，相关容器、卷、状态目录、证书与请求临时文件均已清理；
21. `check-web.sh` 的格式、lint、70 项 Vitest、严格 TypeScript、production build 与依赖基线通过；`check-server.sh`、真实 PostgreSQL integration、browser fixture、脚本语法、`git diff --check` 与 `check-repo.sh` 均通过。本客户端切片未新增依赖、migration 或 lockfile 变化；
22. 已完成 Document 编辑器与协同候选的首轮官方资料和 npm registry 元数据复核，确认 Golden Path 的简单 Markdown、M5 在线协同与 M7 离线同步必须分层，不把后两者提前并入 M0.5；
23. ADR-0021 已以“提议”状态记录 M0.5 服务端版本化 Markdown、可移植 snapshot、编辑器私有表示、协同内核和 transport 的分层；Tiptap 3 / ProseMirror 是第一实验候选，Lexical 是共享 corpus 对照，Yjs 只在选出编辑器后进入无网络内存收敛实验；
24. Tiptap Markdown 当前仍为 Beta，Tiptap 的 Comments / Snapshots 等商业能力不会成为自部署核心依赖；Yjs 官方 WebSocket server 也不会直接作为生产 server。Automerge 因当前离线 / 跨端需求尚未成立且 ProseMirror binding 仍为 `0.2.0` 而延后；
25. 本轮只读取官方资料和 registry 元数据，没有安装依赖、改变 lockfile、创建 migration、启动实时 transport 或读写 Web Storage。下一步是经项目所有者明确授权后，在隔离实验中运行共享 Markdown corpus。

## 最近完成的浏览器验收（2026-09-02）

该日在已经完成的 Thread → Decision → Ticket 正式服务端公共切片上继续交付并验收 canonical Web 协作闭环；代码、自动化、HTTPS fixture server 与交互式浏览器复核均已完成：

1. 盘点确认既有 application command、Project 角色、restricted Thread、EntityLink、Activity、Session、CSRF、公共错误和不可发现性可以复用；缺口集中在 Thread 权威读取、公共 DTO、人工确认与模糊写入重试；
2. ADR-0019 已接受，冻结 Thread / Decision / Ticket 三个 Nexus View 和 Proposed Decision、acceptance、Ticket 三个 Session 写路由，继续使用同源短请求而不接入实验 SSE；
3. migration 007 新增 `collaboration_command_receipts`，以 Workspace、actor、command、target 与 `client_operation_id` 为幂等范围，以 canonical payload SHA-256 检测变化重放，并以 deferred event FK 保持首次命令同事务；
4. receipt 只保存 digest、结果 ref、事件 ID 与时间，禁止 UPDATE / DELETE；它不进入 EntityRef、业务对象、事件 payload、Activity、Outbox 或公共 DTO，但已纳入 PostgreSQL 备份 / 恢复权威表清单；
5. application service 已对 Thread / Decision 稳定 ref、printable ASCII operation ID、UTF-8 / NUL 与 canonical payload 进行 transport-independent 验证，并为三个命令返回首次 / 精确重试结果；
6. PostgreSQL Store 已在每次首次和 retry 前重新检查当前 Project、角色、Channel 与 evidence 权限；精确 retry 返回同一 Decision / Ticket，digest 变化返回 conflict，receipt 不会把旧 membership 变成授权能力；
7. Nexus View application query 已新增 Thread Current，并补齐 Decision acceptance 字段与 Ticket creator；messaging-origin Thread 在 repeatable-read 中同时解析 origin Channel 和 `started-from` Message，Message 关系标题固定为 `Message`，不读取正文；
8. 正式 HTTP handler 已接入 server，六个路由复用 TLS / Host / proxy / Session / CSRF 边界；acceptance 必须显式 `confirmed=true`，server request ID 继续只作 correlation；
9. 公共 adapter 已对 Current、Relation、Timeline、状态、结构化来源和受控时间 fail closed；restricted evidence 只返回无类型、ID、关系名、标题和时间的占位，响应不含 receipt、digest、角色、membership、Message / Thread 正文或原始错误；
10. 单元测试已覆盖首次 `201`、精确重试 `200`、人工确认、严格 JSON、method / path / query、Session / CSRF、结构化来源、restricted 占位和 projection drift；
11. 真实 PostgreSQL + 真实 Session 已贯通 restricted Source Message → Thread → contributor Proposed Decision → decider 人工 Accepted Decision → contributor Ticket，并验证精确重试、变化重放、contributor 不可确认与 evidence membership 撤销后 acceptance retry 被拒绝；
12. 固定摘要 PostgreSQL 17 镜像已重新拉取，正式 server 全量 PostgreSQL 集成、双实例备份恢复、Go test / vet / module 与仓库基线检查均通过；本批次未新增第三方依赖或改变 lockfile。
13. 已建立 Thread / Decision / Ticket 严格运行时校验 adapter 与三个 canonical 页面，分别承载提案、显式人工 acceptance、Ticket 创建和权限过滤后的 Current / Relations / Timeline；
14. Channel 创建 Thread 后已返回稳定 canonical 链接；authenticated shell 也可按正式 `thr_` / `dec_` / `tkt_` ID 进入协作页面，不新增 list API 或客户端权限缓存；
15. 三个写交互在表单未变化的模糊失败后复用同一 `client_operation_id`，`401` 回到登录态，后续 `404` 清除对象、草稿与成功结果；客户端不会把 contributor 的拒绝或 restricted evidence 解释为成功；
16. production Web handler 已显式开放 Thread / Decision / Ticket HTML 路径并继续拒绝未知嵌套路由；Vitest 新增 adapter、页面、shell 与 Channel canonical 链接覆盖，Web 现有 55 项测试和 production build 均通过；
17. authenticated browser fixture 已生成 restricted Thread、contributor / decider 两个账号并挂载正式协作 handler；真实 PostgreSQL、migration、production build 与 HTTPS fixture server 正常通过；
18. 在连接内置浏览器前，从当前 TLS 对端导出 fixture 证书并核对文件 / 对端 SHA-256 完全一致；证书临时加入登录钥匙串后，严格 TLS 客户端返回 `200` 与 `verify=0`，浏览器未经过证书告警或 interstitial；
19. contributor 已在 canonical Thread 创建 Proposed Decision，越权 acceptance 的真实 API 返回 `403` 且 Decision 保持 proposed；decider 在可读取 restricted Thread evidence 后明确接受，acceptance 返回 `200` 并渲染 accepted outcome / rationale；
20. contributor 登出后以 decider 重登、decider 登出后再以 contributor 重登均保持原 canonical URL；contributor 创建 Ticket 返回 `201`，Ticket → Decision → Thread 的 canonical 来源跳转依次保留 `implements`、`derived-from` 与 `started-from`；
21. Thread、Decision 与 Ticket 在 390px 视口下均满足 `documentElement.scrollWidth === body.scrollWidth === innerWidth === 390`，全部可见元素无横向越界，长标题与稳定 ID 正常换行；
22. 临时撤销 contributor 的 restricted Thread membership 后，Thread 的后续创建命令真实返回 `404`，已渲染标题、未提交草稿与已有成功结果全部清除；Decision 仍可读，但来源只显示不泄漏类型、ID、关系名、标题或时间的 restricted evidence 占位；
23. Web Storage 仅通过源码搜索与现有自动化测试复核，未读取浏览器存储；验收结束后恢复浏览器视口、正常停止 fixture，并按完整 SHA-256 指纹删除临时证书、复核登录钥匙串无匹配，再删除导出文件。

## 下一步

下一优先级是执行 ADR-0021 阶段 A 的隔离实验，不直接开始正式 CRDT、WebSocket 或大面积页面实现：

1. 在独立 `experiments/document-editor` package 中锁定 Tiptap 3 / ProseMirror 与 Lexical 的精确 MIT 依赖，不改变正式 `web/package.json`；安装前先获得对 package、版本、lockfile 和清理范围的明确授权；
2. 让两个候选消费同一 Markdown corpus，记录输入、内部 JSON、规范化输出、第二次往返、结构摘要和丢失诊断，重点覆盖软 / 硬换行、列表、代码、中文 / emoji、危险 HTML、未知节点和 schema upgrade；
3. 只有阶段 A 选出编辑器后，才用 Yjs 做无 WebSocket、无 IndexedDB 的双文档内存收敛实验；它不证明权限、持久化或生产 transport；
4. 依据实测结果接受或修订 ADR-0021，再冻结 Document 最小字段、revision、权限、EntityLink、事件、备份和导出合同。

免费书面授权模板和版本化结构化导入导出仍是独立 M0 / M1 缺口，在 Document 技术评估后再按路线图逐项推进。当前 `dev` 已包含消息边界、正式 PostgreSQL command / query、Session transport、Thread → Decision → Ticket 协作闭环和已完成验收的 canonical Channel 实时 Web 闭环。所有这些变更均未晋级 `master`，创建阶段 PR 或写入远程状态仍需项目所有者另行明确授权。

当前完成线：正式数据库、application service 与 Session transport 已证明 Message 能以稳定身份和幂等 command 进入 Channel、能够不复制正文地发起 Thread，并以不可变 receipt 和结构化关系继续形成 Proposed Decision、人工 Accepted Decision 与 Ticket；对应 canonical 协作 Web 页面已经通过 contributor / decider 双账号真实浏览器验收。正式单进程 SSE 已在唯一 Go server 与 Caddy HTTPS 下证明最小化增量、当前权限、有界回放、撤权、慢消费者、连接上限和关闭语义；canonical Channel Web 已按先边界、后历史、再增量的状态机消费该入口，并完成跨账号增量、重连、restricted reply、撤权、登出重登与窄屏真实浏览器验收。

当前停止线：不把 SSE replay 当作权威存储，不把引用、旧 membership、客户端身份或订阅状态当作授权；不把写 command 迁移到 SSE，不建立隐藏 polling fallback；不建立完整聊天 UI、附件、表情、搜索、未读、通知、多副本 fan-out、独立消息中间件或 WebSocket。Document 合同冻结前也不接入新的实时 transport。

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
