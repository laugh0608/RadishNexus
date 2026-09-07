# ADR-0018：Session 作用域下的 Channel Message 短请求 Transport

状态：已接受

日期：2026-09-01

## 背景

ADR-0017 已冻结 Channel、Message、messaging-origin Thread、Message 写入幂等与 canonical resync 的领域边界。migration 006、正式 PostgreSQL command application service 和 canonical Message query 随后已经建立，能够在事务内重新验证当前 Channel / Thread 权限、原子写入业务事实与安全最小化事件，并按稳定 keyset 恢复权威正文。

现在需要把单 Channel 历史、发送和从 Message 发起 Thread 暴露给同源 Web。公共入口不能直接搬运可丢弃 SSE 实验的 Header、进程内 cursor 或 fixture 身份，也不能把 application struct、`client_operation_id`、旧 membership 或客户端传入的 actor 当成协议。它必须复用 ADR-0012 至 ADR-0014 已建立的 Session、CSRF、精确 Origin / Host、可信代理、公共错误和不可发现性边界。

本切片只建立普通短请求。正式 server 仍有统一 15 秒写超时；实时连接必须另行冻结 listener、heartbeat、背压、连接上限、Caddy flush 与优雅关闭合同。

## 决定

### 路由与方法

首批公共消息路由固定为：

- `GET /api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages`：读取一个 Channel 的 canonical Message 历史；
- `POST /api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages`：创建 Message 或按作者幂等键取得既有 Message；
- `POST /api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages/{message_id}/threads`：从路径 Channel 内的 Source Message 发起 Thread。

路径中的 Workspace、Channel 和 Message 使用规范稳定 ID。Workspace 仍由路径显式选择，不固定到 Session，也不从 Header、请求 JSON 或 Web 状态推断。未注册子路径返回版本化 `not_found`；其它方法返回 `method_not_allowed` 和精确 `Allow`。

### Session、权限、Origin 与 CSRF

- 三个路由都复用精确 HTTPS Host、TLS / trusted proxy、Session Cookie 和当前 active Workspace membership resolver，再把 `VerifiedUser` 转成 application `Principal`；不接受用户、角色、Workspace、actor 或 source Header。
- Workspace membership 不可用对外收敛为 `not_found`。Channel、Message、Thread、跨 Workspace对象和跨 Channel Source Message 的可读性继续由 application service 使用当前 PostgreSQL 权限判断；引用、旧 membership 和 cursor 都不授予权限。
- `GET` 不改变服务端状态，因此不要求 CSRF token，但每次分页请求仍重新验证 Session、Workspace membership、Channel 和 Thread reply 的当前读取权限。
- 两个 `POST` 必须同时通过精确 `Origin`、CSRF Cookie 与 `X-CSRF-Token` 的 constant-time double-submit 比较，以及 Session 表中 CSRF digest 的验证。通过浏览器 Cookie 形状但不匹配存储 digest 的 token 仍返回 `csrf_failed`。
- 用户写入的 `Invocation` 固定为 `source_kind=web`，server-generated request ID 同时作为 `correlation_id`；客户端不能提供 actor、source、correlation 或 causation。

### 历史分页与 opaque cursor

- `limit` 缺省为 `50`，允许 `1..100` 的规范十进制；重复、空值、前导零、未知 query 参数或格式错误直接返回 `invalid`。
- `before` 是可选、排他的版本化 opaque cursor。版本 1 使用未经 padding 的 URL-safe Base64 包装 canonical JSON，其中只包含 `v=1`、UTC RFC 3339 Nano `created_at` 和 `message_id`；外部调用方不得构造或依赖内部字段布局。
- cursor 不签名，因为它不是授权能力，只选择调用方已经获准读取的历史位置；解码后仍由 application service 验证稳定引用，并在同一个授权查询中重新判断当前 Channel / Thread 权限。非规范编码、未知版本、额外字段、无效时间或 Message ID 一律返回 `invalid`。
- application 继续按 `(created_at, message_id)` 排他 keyset 从最新页向更旧记录读取，并在 `limit` 前过滤当前不可读 Thread reply。公共页内按时间正序返回，`older_cursor` 为 nullable opaque string；cursor 必须与该页最旧 Message 完全一致，否则 transport fail closed。

### 写入请求与幂等

创建 Message 的 JSON 只接受：

```json
{
  "client_operation_id": "browser-generated-printable-ascii",
  "body": "authoritative UTF-8 body",
  "thread_id": "thr_optional"
}
```

`thread_id` 可以省略或为 `null`。JSON 最大 128 KiB，以允许 16 KiB 合法正文经过最坏情况 JSON escaping；未知字段、多对象、错误 media type、空白正文、NUL、超限正文、非法引用或幂等键均拒绝。第一次创建返回 `201`；同一 Workspace、Channel、作者和 `client_operation_id` 的完全相同重试返回同一 Message 和 `200`；正文或 Thread 目标变化返回 `409 conflict`。

从 Message 发起 Thread 的 JSON 只接受：

```json
{
  "title": "Thread title",
  "visibility": "project"
}
```

`visibility` 只允许 `project / restricted`，JSON 最大 8 KiB。application command 同时携带路径 `channel_id` 和 `message_id`，必须证明 Source Message 属于该 Channel；不能把路径 Channel 当成仅用于展示的冗余参数。一个 Message 只允许作为一个 Thread 的 `started-from` 来源；重复发起或其它状态冲突返回 `409`，当前不另设 Thread command 幂等键。

### 公共 DTO 与最小化

成功响应统一使用 `{"data": ...}` envelope，EntityRef 统一为 `{ "type", "id" }`，时间统一为 UTC RFC 3339 Nano。transport 在发送前验证 application 结果与请求 Workspace、Channel、Message、actor、正文、幂等事实、Thread 来源、可见性、时间和页序一致；任何投影漂移返回通用 `internal`，不发送部分结果。

Message DTO 只包含：

- Message、Channel 和 nullable Thread 的结构化 ref；
- `{kind: "user", id}` author；
- 权威 `body` 和 `created_at`。

Thread 创建 DTO 只包含 Thread、Channel、Source Message ref、规范化 `title`、`visibility` 和 `created_at`。响应不包含 `client_operation_id`、Workspace / Project role、membership、Session / CSRF、内部 keyset、事件、Outbox、关系 metadata、Secret、外部来源或原始错误。

### 错误、缓存与部署边界

- 成功与错误统一设置 `Cache-Control: private, no-store` 和 `Vary: Cookie`，不使用 ETag、离线缓存或 stale fallback。
- 继续复用版本化错误对象与 server-generated request ID：认证失效为 `401 unauthenticated`，CSRF 为 `403 csrf_failed`，可见但无写能力为 `403 forbidden`，不可发现为 `404 not_found`，状态或幂等冲突为 `409 conflict`，无效输入为 `400 invalid`，超限为 `413 payload_too_large`，错误 media type 为 `415 unsupported_media_type`，未知内部失败为不泄漏原因的 `500 internal`。
- 不开放 credentialed CORS、insecure HTTP、WebSocket、SSE 或 polling fallback。reverse proxy 继续负责唯一 HTTPS origin、可信 Header 覆盖与全局资源保护。

## 未采用的方案

### 直接暴露 application keyset

把 `created_at` 和 `message_id` 作为独立 query 参数会过早冻结内部查询布局，也鼓励客户端自行拼接边界。版本化 opaque cursor 允许以后替换内部表示，同时保持“向更旧历史排他翻页”的公共语义。

### 签名或加密 cursor

cursor 不包含额外授权或不可见数据，篡改后也必须重新通过相同的当前权限查询。签名会引入密钥生命周期和轮换合同，却不能替代授权；加密还会把无必要的 Secret 管理带入首个短请求。若以后 cursor 携带权限快照、租户路由或敏感内部状态，必须重新评估。

### 复用实验 SSE command 和 cursor

实验使用 process-local generation、Header 和有界 replay 来验证实时恢复，不是公共业务协议。把它接到正式 15 秒 listener 会混淆短请求、canonical history 与长连接的生命周期，并让可丢弃实现变成兼容负担。

### 让 transport 直接操作 PostgreSQL

这样能少一次 adapter 调用，但会复制当前权限、幂等、事件、Outbox 和来源约束。transport 只负责公共协议与安全入口；事务和领域不变量继续由 application service 与 Store 拥有。

## 后果

正面影响：

- 单 Channel 历史、发送和 Message → Thread 第一次拥有同一 Session 安全边界下的正式公共协议；
- cursor、DTO 和错误与内部 struct 解耦，幂等键、旧权限和实验 cursor 不会成为读取能力；
- 写请求同时验证浏览器态与数据库中的 CSRF digest，真实 Session 撤销和权限撤销即时生效；
- 嵌套 Channel 路径进入 application command，跨 Channel Source Message 无法借由 transport 歧义建立 Thread；
- Web 可以在不引入完整聊天、实时连接或第二套 fixture API 的前提下交付下一纵向切片。

成本与限制：

- 每个写请求当前会分别解析 Session CSRF digest 和 Workspace membership，增加一次数据库读取；在没有测量证据前不以缓存或合并查询放宽边界；
- opaque cursor 仍能被已授权调用方解码出它刚读取过的时间和 Message ID；它提供协议封装，不提供保密性；
- Thread 发起只有唯一 Source 约束而没有独立幂等键，响应丢失后的重复请求会得到冲突；Web 必须显式处理，后续如需自动恢复应新增受治理的 command 幂等合同；
- 当前没有 Channel 列表、成员管理、Message 编辑 / 删除、附件、搜索、未读、通知或实时推送入口。

## 迁移与验证

本 ADR 不新增 migration、依赖或 lockfile。正式实现新增标准库 HTTP handler，并让 `StartThreadFromMessage` application input / command 显式携带 `channel_id`；PostgreSQL Store 在读取 Source Message 后验证其 Channel 与路径 scope 完全一致。

自动化验证覆盖：

1. 公共 cursor 正反向编解码、未知版本、非规范时间、额外字段、非法 ID、重复和未知 query 参数；
2. 显式 Message / Thread DTO、页序和 cursor 一致性、`client_operation_id` 最小化与 application projection fail closed；
3. GET 不要求 CSRF，POST 同时要求 exact Origin、double-submit 与数据库 digest；
4. 第一次 Message 创建 `201`、相同重试 `200`、变化重放 `409`，以及 Message → Thread `201`；
5. 真实 Session + PostgreSQL 下的 canonical page、跨 Channel Source、无效 cursor、CSRF digest 不匹配、Channel membership 撤销和 Session 撤销。

下一切片只为一个已知 Channel 建立 Web 历史、发送和发起 Thread，并接入既有 Decision / Ticket 链。真实 production build + HTTPS + Caddy + 浏览器复验通过前，不宣布 Golden Path 沟通入口完成，也不接入正式实时连接。
