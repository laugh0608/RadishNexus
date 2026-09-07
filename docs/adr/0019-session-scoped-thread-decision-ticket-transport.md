# ADR-0019：Session 作用域下的 Thread、Decision 与 Ticket 协作 Transport

状态：已接受

日期：2026-09-02

## 背景

ADR-0004 已冻结 Project 作用域的 Thread → Decision → Ticket 权限、`derived-from` / `implements` 关系与 restricted evidence 边界；正式 application service 和 PostgreSQL Store 也已经能原子完成 Proposed Decision、人工 Accepted Decision 和 Ticket。ADR-0018 随后把 Message → Thread 开放为同源 Session 短请求，但新建 Thread 尚无 canonical 读取与后续协作入口，Web 无法继续 Golden Path。

直接把 application struct 暴露给浏览器会泄漏内部投影和幂等状态，也无法在响应丢失后安全恢复创建结果。仅把 server request ID 当作幂等身份同样不成立：重试会生成新的 request ID，而且 correlation 负责追踪，不负责识别用户命令。另一方面，接受 Decision 是高风险事实，必须保留明确的当前用户确认，不能由草案、自动摘要或重试机制暗中完成。

本切片继续使用现有 15 秒短请求 listener；它不引入实时连接、完整聊天、通用工作流或新的授权体系。

## 决定

### 路由与 canonical 页面边界

首批公共路由固定为：

- `GET /api/v1/workspaces/{workspace_id}/threads/{thread_id}/nexus-view`；
- `POST /api/v1/workspaces/{workspace_id}/threads/{thread_id}/decisions`；
- `GET /api/v1/workspaces/{workspace_id}/decisions/{decision_id}/nexus-view`；
- `POST /api/v1/workspaces/{workspace_id}/decisions/{decision_id}/acceptance`；
- `POST /api/v1/workspaces/{workspace_id}/decisions/{decision_id}/tickets`；
- `GET /api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/nexus-view`。

后续 Web canonical path 分别为 `/workspaces/{workspace_id}/threads/{thread_id}`、`/workspaces/{workspace_id}/decisions/{decision_id}` 与 `/workspaces/{workspace_id}/tickets/{ticket_id}`。本 ADR 先冻结并实现服务端合同；页面交互作为下一纵向切片接入，不用 fixture 或第二套协议代替。

Workspace 继续由路径显式选择；Thread、Decision 和 Ticket 使用规范稳定 ID。未知子路径返回版本化 `not_found`，其它方法返回 `method_not_allowed` 与精确 `Allow`。六个路由均拒绝 query 参数。

### Session、当前权限与人工确认

- 所有路由复用精确 HTTPS Host、TLS / trusted proxy、Session Cookie、active Workspace membership resolver 与服务端构造的 `Principal`；不接受 actor、Workspace、Project role、source、correlation 或 causation Header。
- 三个 `GET` 不要求 CSRF，但每次读取都在 PostgreSQL repeatable-read 查询中重新判断当前 Project、Channel 与 restricted Thread 权限；未知、跨 Workspace 或不可发现对象统一为 `404`。
- 三个 `POST` 同时要求精确 Origin、CSRF Cookie / Header constant-time double-submit 和 Session 表中的 CSRF digest，然后由 application transaction 重新判断当前角色、Project 状态与 evidence 可读性。
- Proposed Decision 需要 contributor、decider 或 admin 且当前可读 Thread；Accepted Decision 只允许 decider 或 admin，并要求当前仍可读取全部 evidence。Project admin 不自动穿透 restricted Thread。
- acceptance body 必须显式携带 `"confirmed": true`；该字段只证明本次公共请求包含人工确认，不进入领域表、事件或 receipt。系统主体、自动摘要和后台重试仍不能接受 Decision。
- Ticket 只能从当前可读的 Accepted Decision 创建，且调用者必须仍有 Project contribution 能力。

### 写入请求与幂等 receipt

三个写请求分别只接受：

```json
{
  "client_operation_id": "browser-generated-printable-ascii",
  "question": "Decision question"
}
```

```json
{
  "client_operation_id": "browser-generated-printable-ascii",
  "outcome": "Accepted outcome",
  "rationale": "Human rationale",
  "confirmed": true
}
```

```json
{
  "client_operation_id": "browser-generated-printable-ascii",
  "title": "Ticket title"
}
```

`client_operation_id` 为 1..128 bytes printable ASCII，不是 EntityID。幂等范围固定为 `(workspace_id, actor_id, command_kind, target_type, target_id, client_operation_id)`；Question、Outcome / Rationale 或 Title 在 trim 后形成 canonical payload digest：

- 首次有效命令在同一事务内写入 immutable receipt、业务对象或状态、EntityLink、领域事件和 Outbox；
- 相同范围和相同 digest 的重试先重新通过当前权限，再返回 receipt 指向的权威对象；Decision / Ticket 首次创建返回 `201`，精确重试返回 `200`，acceptance 首次与精确重试均返回 `200`；
- 相同范围但 digest 变化返回 `409 conflict`，不覆盖 receipt 或既有业务事实；
- 相同 actor 在不同 target 上复用同一 operation ID 不冲突，因为 target 属于幂等范围；浏览器仍应为每次明确动作生成新值；
- receipt 不进入普通 DTO、EntityRef、领域事件 payload、Activity 或 Outbox，只保存 digest、结果 ref、事件 ID 与时间；它随 PostgreSQL 备份恢复，以免恢复后缩短幂等保证。

每个 retry 仍重新验证 Session、Workspace membership、角色、Project 状态与 evidence。receipt 不是授权能力，也不能让已撤销权限的调用方取得旧结果。

### Nexus View 与公共 DTO

成功响应继续使用 `{"data": ...}` envelope，EntityRef 为 `{ "type", "id" }`，actor 为 `{ "kind": "user", "id" }`，时间为 UTC RFC 3339 Nano。公共 adapter 在发送前验证 application 投影与路径、当前对象、关系类型、状态和时间一致；漂移返回通用 `500 internal`，不发送部分结果。

- Thread Current 只返回 ref、governing Project ref、可选可读 origin Channel、title、visibility、creator 与受控时间。messaging-origin Thread 必须同时返回一条 readable `started-from` Message relation；Message 只显示固定标题 `Message`，不复制或环境化正文。
- Decision Current 返回 ref、Project ref、question、`proposed / accepted`、nullable outcome / rationale / decided_at、proposer、deciders 与受控时间。关系和 Timeline 复用当前权限投影；不可读 evidence 只产生没有类型、ID、标题或时间的 `restricted` 占位。
- Ticket Current 返回 ref、Project ref、title、`open`、creator 与受控时间，并通过 readable `implements` relation 保留 Source Decision。
- Proposed Decision 创建响应额外返回结构化 `source_thread`；Ticket 创建响应额外返回结构化 `source_decision`。它们不复制 Thread 或 Message 正文。
- 所有响应禁止包含 `client_operation_id`、payload digest、receipt、membership、角色、Session / CSRF、内部 SQL、原始错误、领域事件 payload、Outbox 或 Secret。

### 错误、缓存与部署边界

- 成功与错误统一设置 `Cache-Control: private, no-store` 和 `Vary: Cookie`；不使用 ETag、离线缓存或 stale fallback。
- 继续复用版本化错误：`401 unauthenticated`、`403 csrf_failed / forbidden`、`404 not_found`、`409 conflict`、`400 invalid`、`413 payload_too_large`、`415 unsupported_media_type`、`405 method_not_allowed` 与不泄漏原因的 `500 internal`。
- `source_kind` 固定为 `web`，server-generated request ID 作为 correlation ID；幂等 receipt 与 correlation 各自承担去重和追踪职责。
- 不开放 credentialed CORS、insecure HTTP、SSE、WebSocket、附件、反应、搜索、未读、通知或通用 command endpoint。

## 未采用的方案

### 用 request ID 作为幂等键

request ID 每次请求都由服务端重新生成，只适合 correlation。把它用于幂等会让网络重试产生新对象；接受客户端 request ID 又会扩大可信边界并混淆日志身份。

### 只对状态转换幂等，创建失败后要求人工查找

Thread 当前没有反向 Decision 列表，Decision 当前也没有反向 Ticket 列表。响应丢失后要求用户猜测是否创建成功会诱发重复事实，也无法为 Web 提供确定的恢复路径。窄 receipt 能在不把幂等键写入业务对象的前提下恢复准确结果。

### 引用 Thread 后复制 Source Message 正文

复制正文会建立第二份权威内容、扩大权限撤销面并把 Message 泄漏到 Decision / Ticket DTO、事件或 Activity。结构化 `started-from` / `derived-from` / `implements` 引用和权限过滤投影已经足够表达来源。

### 在 public handler 内直接写数据库

这样会复制 Project 角色、restricted evidence、状态机、EntityLink、事件、Outbox 与幂等事务。transport 只拥有公共协议和安全入口；application service 与 Store 继续拥有业务和事务不变量。

## 后果

正面影响：

- 从 Message 创建的 Thread 第一次能够在同一 Session 安全边界内继续到 Proposed Decision、人工 Accepted Decision 和 Ticket；
- 模糊网络失败可以用同一 operation ID 恢复同一权威结果，不会制造重复对象或事件；
- evidence 撤销会阻止读取与 acceptance retry，receipt 不会把旧权限变成能力；
- Thread、Decision 与 Ticket 的来源保持结构化且不复制 Message / Thread 正文；
- Web 下一切片可以直接消费受运行时校验的稳定 DTO，不需要搬运 application struct 或实验协议。

成本与限制：

- 新增一张必须备份、恢复和长期保留的 immutable receipt 表；清理策略在产品承诺幂等窗口前不得擅自缩短；
- 当前每次 retry 仍执行完整授权读取，不能用 receipt 缓存绕过数据库权限；
- Thread Nexus View 当前不投影 Message / Thread 噪声 Activity；Decision / Ticket Timeline 仍依赖现有版本化 Activity 重建；
- 本切片没有反向关系列表、Decision 修订 / 拒绝 / 替代、Ticket 状态流转、成员管理或完整导航。

## 迁移与验证

migration 007 新增 `collaboration_command_receipts`，以主键固化幂等范围、以 CHECK 固化 command / target / result 组合、operation ID 与 SHA-256 格式，并拒绝 UPDATE / DELETE。receipt 到领域事件的外键为 deferred，使 receipt、业务状态与事件可以在同一事务首次写入。

自动化验证覆盖：

1. application 对稳定 ref、可打印 operation ID、UTF-8 / NUL 和 canonical payload digest 的验证；
2. Proposed Decision、acceptance 和 Ticket 的首次命令、精确重试、变化重放、事务回滚与 immutable receipt；
3. Thread / Decision / Ticket 公共 DTO、结构化来源、restricted 占位、Message 正文与 receipt 最小化，以及 projection drift fail closed；
4. Session、Origin、双提交 + 存储态 CSRF、Workspace 不可发现性、角色分离和明确 `confirmed=true`；
5. 真实 PostgreSQL 下 contributor 提案、decider evidence membership、人工确认、Ticket 创建、精确重试、变化重放、evidence 撤销和读取；
6. migration 007 随 forward-only runner 执行，receipt 进入 PostgreSQL 备份 / 恢复权威快照，Session 与 Activity 仍按既有边界排除或重建。

下一切片只建立 Thread / Decision / Ticket canonical Web 页面和受运行时校验的 adapter，并用 production build + HTTPS 浏览器完成同一 Golden Path。该浏览器闭环通过前，不接入正式实时连接。
