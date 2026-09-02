# RadishNexus Go Server

这是 RadishNexus 唯一的正式 Go 服务 module。当前纵向切片包含：

- 标准库 HTTP 存活与就绪检查；
- 基于原生 `pgx/v5`、连续编号与 SHA-256 漂移检测的显式 PostgreSQL migration；
- Channel / Message / messaging-origin Thread 的正式 schema、权限和幂等 application service；
- 权限过滤、稳定 keyset 分页的 canonical Channel Message application query；
- Thread → Decision → Ticket 的幂等 application service 与 immutable command receipt；
- 已验证 Jenkins delivery → 完成态 CI Run 的 application service；
- 显式授权用户记录终态 staging Deployment 的 application service；
- Project 角色、restricted Thread 和关系投影权限；
- 与业务状态同事务写入的不可变领域事件与 Outbox 投递状态；
- 正式 Component、CI Run 与不可变 inbound delivery receipt schema；
- 正式 Environment、环境级部署授权与不可变 Deployment schema；
- 从领域事件原子、幂等重建的 Activity projection version 1；
- 为 Thread、Decision、Ticket、CI Run 和 Deployment 返回 Current、Relations 和 Timeline 的权限过滤 Nexus View query；
- 一次性本地管理员 bootstrap、Argon2id credential、账号锁定、opaque Session、CSRF digest 与当前 Workspace membership resolver；
- 把已验证 Session 用户转换为 application `Principal` 的认证 adapter；
- Secure `__Host-` Cookie、精确 HTTPS Origin / Host、可信代理、客户端 IP 登录限流、request ID 与版本化安全错误对象；
- Session 作用域的 Deployment Nexus View 公共只读 handler 与显式安全 DTO；
- Session 作用域的 Channel Message 历史、幂等发送和 Message → Thread 公共 handler；
- Session 作用域的 Thread / Decision / Ticket Nexus View、人工 acceptance 与幂等写入 handler；
- 同源 authenticated Web Shell、显式 production build root、页面 allowlist 与安全静态资源缓存；
- 可选文件 Secret 覆盖数据库 URL 密码的公共 runtime config；
- PostgreSQL 17 同 major 的版本化备份、全新空目标恢复、migration 校验与 Activity 重建命令。

公共 transport 已开放 `/api/v1/auth/sessions` 与 `/api/v1/auth/session` 的 login / resolve / logout 闭环、Deployment Nexus View 读取、单 Channel Message 历史 / 发送 / Message → Thread，以及 Thread → Decision → Ticket 协作短请求；同一个 Go server 从显式 Web build root 交付 authenticated shell 和已注册页面。认证入口要求精确 HTTPS public origin、精确 Host、显式可信代理链、客户端 IP 限流、受控 JSON、Secure Cookie 和 CSRF，不接受可信用户 Header、insecure Cookie 或 credentialed CORS。Jenkins 核心同样不读取请求或验证签名；只有完成来源认证、重放校验和字段映射的调用方才能构造 `VerifiedJenkinsDelivery`。inbound 与 collaboration command receipt 只保存规范化 SHA-256 和最终引用，不保存 Secret、原始 webhook body 或业务正文。

Channel / Message migration 006 固化 Channel membership、Message 不可变和幂等唯一范围、同 Channel reply、messaging-origin Thread 的 `origin_channel_id` 与唯一 `started-from` Message 来源；创建 Message 或 Thread 时，业务事实、安全最小化事件与 `realtime-dispatcher` Outbox 在同一事务提交。canonical query 返回最新一页并按 `(created_at, message_id)` 以 exclusive keyset 向更旧内容翻页，先过滤当前不可读 Thread 回复，且不返回 `client_operation_id`。公共 transport 用版本 1 opaque cursor 封装 keyset，每次请求重新验证 Session、Workspace、Channel 与 Thread 权限；当前仍没有正式实时连接，实验 SSE Header 和进程内 cursor 不属于公共协议。

collaboration migration 007 以 `(workspace, actor, command, target, client_operation_id)` 固化 Proposed Decision、Decision acceptance 与 Ticket 创建的幂等范围。首次命令在同一事务写入 immutable receipt、业务状态、关系、事件和 Outbox；相同 canonical payload 返回原结果，变化重放冲突。每次 retry 仍重新授权，receipt 不进入领域事件、Activity、普通 DTO 或客户端可见状态，但属于必须备份恢复的权威事实。

当前 Activity 白名单包含 `decision.proposed`、`decision.accepted`、`ticket.created`、`ci-run.recorded` 和 `deployment.recorded`。重建通过 `postgres.Store.RebuildActivityProjection` 显式触发，不依赖 Outbox 投递状态，也尚未建立常驻 projector worker。Activity 只保存引用和状态等最小安全事实；Nexus View 在读取时按当前权限重新解析 subject，不能读取的目标只形成通用 restricted 占位。

CI Run 的 M0 用户读取由所属 Component 控制：同一 Workspace 的活跃成员可读，非成员、暂停成员和跨 Workspace 主体得到 not-found；owner Team 和 Jenkins source 都不授予读取权。CI Run Current 只返回 status、受控时间与当前 Component，Timeline 隐藏 plugin/source ID，并且不返回 external run key、receipt、digest、Secret、原始 payload 或外部 URL。该 query 仍是内部 application contract，尚未形成 HTTP 或公共响应 schema。

staging Deployment 只记录外部已经完成的终态事实，不执行部署。目标必须是 active staging Environment，来源必须是 succeeded CI Run，调用者必须是 active Workspace 用户并持有该 Environment 的显式授权；Project 角色、owner Team 和 CI source 不隐式授予部署能力。Deployment、`deploys` 关系、`deployment.recorded` 和 Outbox 同事务提交。

Deployment 的 M0 读取与写授权分离：同一 Workspace 的 active 成员只有同时能读取目标 Environment 与来源 CI Run 时才可读取；非成员、暂停成员和跨 Workspace 主体得到 not-found，Environment 归档不隐藏既有历史。Current 只返回终态、受控时间、Environment 与来源 CI Run；Relations 和 Timeline 复用当前权限，不返回 authorization ID、调用 source、Jenkins receipt、digest、Secret、原始 payload 或外部 URL。该 query 已通过独立公共 DTO 开放为第一个只读业务端点；授权管理入口、production、审批、回滚和执行引擎均未建立。

本地认证以不可变小写 ASCII login、Argon2id verifier、5 次失败后 15 分钟账号锁定和 24 小时绝对有效的服务端 Session 为基线。数据库只保存 Session / CSRF token 的 SHA-256 digest；Session 不固定 Workspace，业务调用必须以当前 active membership 解析 `VerifiedUser`。登录 transport 另按客户端 IP 每分钟限制 5 次尝试、每进程最多并发 4 个密码校验并有界跟踪 4096 个客户端；多副本或公网部署仍必须在 reverse proxy / gateway 增加全局限流。OIDC、邀请、密码重置、MFA 与其它业务 HTTP 路由尚未建立。不可读资源由 application service 返回 `not found`，Deployment handler 还会把不可用 membership 收敛为同形 `not_found`。

## 本地检查

从仓库根运行：

```text
./scripts/check-server.sh
./scripts/check-server-postgres.sh
```

## 数据库迁移

迁移不会在服务启动时隐式执行。进入 `server/`、设置 `DATABASE_URL` 后显式运行：

```text
go run ./cmd/nexus-migrate
```

runner 使用 session advisory lock 防止并发执行，每个 migration 单独事务提交，并拒绝已应用文件发生漂移。它只支持向前迁移；当前已建立 PostgreSQL 17 同 major 的最小恢复，跨版本生产升级仍需补齐失败中断、forward repair、兼容窗口和恢复限制。

直接运行仍可把完整连接串放在 `DATABASE_URL`。受控部署也可以让 `DATABASE_URL` 只包含 PostgreSQL 用户、地址和数据库名，再用绝对路径 `RADISHNEXUS_DATABASE_PASSWORD_FILE` 输入单行密码；所有 server、migration、bootstrap、backup 和 restore 入口复用同一解析逻辑。两处同时提供密码、相对路径、空值、多行或读取失败都会显式拒绝，错误不会回显 Secret。正式 Compose 用法见 [`deploy/README.md`](../deploy/README.md)。

## 首次本地管理员

先显式完成 migration，再在新实例上执行一次 bootstrap。命令只从标准输入读取密码；密码不得放入命令参数、环境变量、日志或 shell history：

```text
read -r -s bootstrap_password
printf '\n'
printf '%s\n' "$bootstrap_password" | DATABASE_URL=... go run ./cmd/nexus-bootstrap \
  --login admin \
  --display-name "First Admin" \
  --workspace-name "First Workspace" \
  --password-stdin
unset bootstrap_password
```

密码必须为 15–128 个 Unicode 字符且最多 1024 bytes。命令通过 PostgreSQL transaction advisory lock 保证只有一个调用成功，创建 local account、user、Workspace 和 `owner` membership；已经存在任何本地账号时失败，不提供覆盖或默认密码。成功输出只包含稳定 user / Workspace ID 与规范化 login。

密码 verifier 属于受保护的权威恢复数据，会进入 PostgreSQL 运维备份；`user_sessions` 只备份 schema、不备份数据，恢复后所有旧 Session 与 CSRF token 失效。该边界不授权 `.nexus` 可移植导出携带 credential。

## 公共认证入口

完成 migration 和一次性 bootstrap 后，server 还要求以下部署配置：

```text
DATABASE_URL=...
RADISHNEXUS_PUBLIC_ORIGIN=https://nexus.example.com
RADISHNEXUS_TRUSTED_PROXY_CIDRS=127.0.0.1/32
RADISHNEXUS_HTTP_ADDR=127.0.0.1:8080
RADISHNEXUS_WEB_ROOT=/srv/radishnexus/web
```

`RADISHNEXUS_PUBLIC_ORIGIN` 必须是浏览器实际访问的精确 HTTPS origin；`RADISHNEXUS_TRUSTED_PROXY_CIDRS` 只列出直接连接 server 的 TLS reverse proxy 地址，不能为了方便信任用户网络。proxy 必须覆盖公网传入的 `X-Forwarded-Proto` / `X-Forwarded-For`、保留原始 `Host`，并向 server 提供单值 `X-Forwarded-Proto: https` 和完整客户端链。server 不提供 HTTP Cookie fallback。

`RADISHNEXUS_WEB_ROOT` 必须是 `npm run build` 产出的 Vite `dist` 绝对目录；server 不隐式构建前端、不依赖工作目录，也不会在 build 缺失时退回 fixture。reverse proxy 应把页面、静态资源和 `/api/v1` 全部转发到这个 server，保持唯一 HTTPS origin。

当前公共认证路由为：

- `POST /api/v1/auth/sessions`：JSON `login_name` / `password`，成功返回 `201`、Session context 和两个 Secure Cookie；
- `GET /api/v1/auth/session`：用 Session cookie 返回当前 user、active Workspace membership 与绝对过期时间；
- `DELETE /api/v1/auth/session`：要求精确 `Origin`、CSRF cookie 与 `X-CSRF-Token`，成功撤销 Session、清除 Cookie 并返回 `204`。

登录 JSON 最大 4096 bytes，不接受未知字段；所有认证响应均 `no-store`，错误使用带 server-generated `request_id` 的稳定 JSON envelope。进程内 IP 限流不替代 reverse proxy 的全局限流、TLS、Header 清洗和安全日志责任。完整边界见 [ADR-0013](../docs/adr/0013-public-authentication-transport.md)。

## Authenticated Web Shell

正式 Web 页面为：

- `/`：Session bootstrap、login、Workspace 选择、已知 Deployment ID 入口和 logout；
- `/workspaces/{workspace_id}/deployments/{deployment_id}`：先验证 Session，再消费正式 Deployment Nexus View DTO；
- `/workspaces/{workspace_id}/channels/{channel_id}`：先验证 Session，再分页读取 Message、幂等发送并从 Message 发起 Thread；
- `/prototype/nexus-view`：与真实入口隔离的静态代表状态检视器。

只有上述 HTML 路径和 build 的 `/assets/` 文件会由 Web handler 交付；未知路径返回 `404`。HTML 使用 `no-cache`，哈希资源使用长期 immutable cache，认证和业务 API 继续 `no-store`。完整 same-origin、CSP、启动失败、Session bootstrap 和 fixture 边界见 [ADR-0015](../docs/adr/0015-same-origin-authenticated-web-shell.md)。

## Deployment Nexus View 读取

当前 Deployment 公共业务路由为：

- `GET /api/v1/workspaces/{workspace_id}/deployments/{deployment_id}/nexus-view`：用 Session cookie 在路径 Workspace 中重新验证 active membership，转换为 application `Principal` 后读取权限过滤的 Deployment Current、Relations 和 Timeline。

成功响应使用显式 `data` envelope、结构化 `{type, id}` ref、nullable `started_at` 与安全可见实体，不直接序列化内部 `goldenpath.NexusView`。无 membership、跨 Workspace、未知或不可读对象保持不可发现；所有结果使用 `Cache-Control: private, no-store` 和 `Vary: Cookie`。完整公共 DTO、错误、缓存和 Web 消费边界见 [ADR-0014](../docs/adr/0014-session-scoped-deployment-nexus-view-transport.md)。

## Channel Message 短请求

当前公共消息路由为：

- `GET /api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages`：读取 canonical history，`limit` 缺省 `50`、范围 `1..100`，可用版本化 opaque `before` cursor 向更旧消息翻页；
- `POST /api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages`：用 `client_operation_id`、`body` 和可选 `thread_id` 创建 Message，首次返回 `201`，完全相同重试返回 `200`，变化重放返回 `409`；
- `POST /api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages/{message_id}/threads`：用 `title` 和 `project / restricted` visibility 从路径 Channel 内的 Source Message 创建 Thread。

三个路由均通过当前 Session 与 Workspace / Channel / Thread 权限；两个 POST 还同时要求精确 `Origin`、CSRF Cookie / Header 一致和数据库 CSRF digest。成功与错误统一使用 `Cache-Control: private, no-store` 和 `Vary: Cookie`；显式 DTO 不返回 `client_operation_id`、membership、事件或内部 keyset。完整 cursor、JSON 上限、状态码、最小化和跨 Channel 来源边界见 [ADR-0018](../docs/adr/0018-session-scoped-channel-message-transport.md)。

同源 Web Shell 已把这三个短请求接入 canonical Channel 页面。浏览器会在模糊发送失败后为未变化正文保留同一幂等键；Session 失效回到登录态，后续 `404` 清除已显示正文。production Web handler 只额外开放精确 Channel 页面路径，不把未知嵌套路由变成任意 SPA fallback。

## Thread、Decision 与 Ticket 协作短请求

当前公共协作路由为：

- `GET /api/v1/workspaces/{workspace_id}/threads/{thread_id}/nexus-view`；
- `POST /api/v1/workspaces/{workspace_id}/threads/{thread_id}/decisions`；
- `GET /api/v1/workspaces/{workspace_id}/decisions/{decision_id}/nexus-view`；
- `POST /api/v1/workspaces/{workspace_id}/decisions/{decision_id}/acceptance`；
- `POST /api/v1/workspaces/{workspace_id}/decisions/{decision_id}/tickets`；
- `GET /api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/nexus-view`。

三个 GET 每次重新检查当前 Workspace、Project、Channel 与 restricted Thread 权限；三个 POST 还要求精确 Origin、double-submit + 存储态 CSRF。写请求携带 printable ASCII `client_operation_id`；首次 Decision / Ticket 创建返回 `201`，精确重试返回 `200`，变化重放返回 `409`。acceptance 只允许 decider / admin、要求当前可读全部 evidence 和显式 `confirmed=true`，首次与精确重试均返回 `200`。

Thread DTO 只通过结构化 ref 返回 origin Channel 和 `started-from` Message，不返回 Message 正文；Decision restricted evidence 只形成无类型、ID、关系名、标题和时间的占位；Ticket 通过 `implements` 保留 Source Decision。所有响应均为 `private, no-store`，不返回 receipt、digest、operation ID、角色、membership、事件或 Outbox。完整边界见 [ADR-0019](../docs/adr/0019-session-scoped-thread-decision-ticket-transport.md)。对应 canonical Web 页面仍是下一切片，未知 HTML 路径继续 `404`。

## 最小备份与恢复

当前备份是 PostgreSQL 17 同 major 的整库运维工件，不是 `.nexus` 开放导出格式。工件目录包含 `manifest.json` 和 custom-format `database.dump`；源库必须完整匹配当前 migrations，所有非系统 relation 必须已经由当前二进制分类。`activity_items` 只保留 schema，不备份投影数据；恢复成功后命令从不可变领域事件重建 Activity。

进入 `server/` 并设置 `DATABASE_URL` 后，备份到一个尚不存在的新目录：

```text
go run ./cmd/nexus-backup --output /path/to/new-backup-directory
```

恢复到一个全新的空 PostgreSQL 17 数据库：

```text
go run ./cmd/nexus-restore --input /path/to/completed-backup-directory
```

两个命令默认从 `PATH` 查找 PostgreSQL 17 的 `pg_dump` 和 `pg_restore`。受控环境可以用 `RADISHNEXUS_PG_DUMP` 与 `RADISHNEXUS_PG_RESTORE` 指定固定绝对路径。连接密码只通过 `DATABASE_URL` 和子进程环境传递；不要把真实 DSN 写入命令参数、日志或仓库文件。

当前 M0 工具桥接只接受显式 `sslmode=disable` 的本地或受控私有连接；TLS 配置会直接失败，因为 `pgx` TLS config 不能被安全近似成 libpq 的 `verify-ca / verify-full` 参数。不要为了使用备份命令而关闭远程数据库原有 TLS 要求；远程 TLS 工具连接需要后续独立冻结。

恢复命令拒绝非空目标，不执行 `--clean`、DROP 或自动覆盖；它以单事务恢复 archive，再显式运行正式 migration 并重建 Activity。manifest checksum、dump SHA-256、PostgreSQL major、migration history、表分类或恢复 TOC 不符合当前合同都会在可能时于目标写入前失败。失败目标不得原地清理后冒充恢复成功，应丢弃并重新创建全新数据库。

双实例往返验证从仓库根运行：

```text
./scripts/check-server-backup-restore.sh
```

该脚本使用两个独立的固定 PostgreSQL 17 容器，验证包含 Channel、Message、messaging-origin Thread 和 collaboration command receipt 的完整 Golden Path fixture、恢复前后所有纳入表、Activity 重建，以及 manifest 漂移、dump 损坏和非空目标失败路径；不会隐式拉取缺失镜像。
