# ADR-0020：Session 作用域下的单进程 Message 实时增量

状态：已接受

日期：2026-09-03

## 背景

ADR-0017 已用可丢弃实验验证 Message command 幂等、单进程有界回放、慢消费者隔离和权限撤销；ADR-0018 随后建立正式 PostgreSQL command、canonical history 与 Session 作用域短请求。canonical Web 页面和 Thread → Decision → Ticket 协作闭环也已通过真实浏览器验收。当前缺口不是新的写入协议，而是把已经验证的单向 Message 增量安全地放回唯一正式 Go server 和固定 Caddy 入口。

正式 server 对普通请求统一设置 15 秒 `WriteTimeout`，Caddy 还设置 20 秒 response header timeout。直接把实验 handler 挂入该 listener 会让空闲连接被全局写超时截断，也没有冻结 heartbeat、连接上限、代理 flush、优雅关闭或公共事件结构。实验还用测试 Header 注入身份并缓存正文，不能成为正式入口。

M0.5 仍是单 Go 进程、单 Caddy、单 PostgreSQL 实例。当前只需要已知 Channel 的服务端单向增量；presence、typing、客户端 command 帧、多副本 fan-out 和跨实例恢复仍没有真实需求。因此本 ADR 在不新增依赖、不改变写入短请求、不接入 Web UI 的前提下，冻结一个可由 canonical history 恢复的正式 SSE 边界。

## 决定

### 路由与认证

新增正式路由：

- `GET /api/v1/workspaces/{workspace_id}/channels/{channel_id}/events`

它与 Channel Message 短请求共用唯一 HTTPS origin、精确 Host、可信代理、Secure Session Cookie、当前 active Workspace membership 和 application `Principal`。GET 不要求 CSRF，但不接受用户、Workspace、role、cursor 或权限 Header，不开放 credentialed CORS，也不接受 query 参数。未知子路径保持 `not_found`，其它方法返回 `method_not_allowed` 和 `Allow: GET`。

建立连接前先验证 Session、路径与当前 Channel 读取权限。建立后在每条业务事件发送前和每次 heartbeat 都重新解析同一个 Session token，并重新验证 Workspace membership 与 Channel 权限；Message 投影还按当前 Thread 权限读取。建立前的认证、授权和输入错误继续使用正式 JSON error envelope。响应已经成为 `text/event-stream` 后，不再拼接 JSON error；权限失效发送最小控制事件并关闭，未知内部失败只记录 request ID 后关闭。

### 公共事件与最小化

事件流固定为 UTF-8，响应设置：

- `Content-Type: text/event-stream; charset=utf-8`；
- `Cache-Control: private, no-store`；
- `Vary: Cookie`；
- `X-Content-Type-Options: nosniff`。

版本 1 只定义四个事件名：

| `event` | `id` | `data` |
| --- | --- | --- |
| `ready` | 当前不透明实时 cursor | `{}` |
| `message.created` | 该通知位置的不透明实时 cursor | 与短请求相同的 `{"data": MessageDTO}` envelope |
| `resync-required` / `access-revoked` | 无 | `{}` |

heartbeat 使用 SSE comment，不携带 `id` 或业务数据。Message DTO 继续只含 Message / Channel / nullable Thread ref、author、正文和创建时间；不含 `client_operation_id`、Workspace / Project role、membership、Session / CSRF、领域事件、Outbox 或内部 cursor 字段。进程内 hub 只保存 Workspace ID、Channel ID、Message ID、递增 position 和随机 process generation，不缓存正文、作者、幂等键或权限结果。

不能读取某个 restricted Thread reply、但仍能读取 Channel 的订阅者不会收到该 Message；连接内部位置仍前进。这样后续可见事件可以继续到达，且被隐藏的 Thread ID、Message ID、正文、作者和时间都不会泄漏。若 Channel 或 Session 已不可用，则发送 `access-revoked` 后关闭。

### Cursor、回放与 canonical resync

SSE `Last-Event-ID` 是唯一重连输入，最多 512 bytes，只允许单值。cursor 是未经 padding 的 URL-safe Base64 canonical JSON，内部含版本、随机 process generation、请求 Workspace + Channel 的 scope digest 和 Channel 内 position；它不是 EntityID、数据库 offset、授权能力或长期排序承诺。客户端不得构造或依赖内部字段。

- 没有 `Last-Event-ID` 时，订阅从当前 position 开始并立即收到 `ready`；客户端应以该连接为增量边界读取 canonical history。
- 相同 process generation 且仍在回放窗口内时，从 cursor 后按 position 回放。
- generation 不同、非规范编码、未来 position、cursor 已落后于窗口，或慢消费者在连接期间落后于窗口时，发送不含业务数据的 `resync-required` 后关闭。
- 进程重启、通知丢失和窗口溢出都只影响增量延迟；客户端必须重新读取 canonical Message history。不得把 hub replay 当作权威消息存储。

每个 Channel 最多保留最近 1024 个 Message ID 通知。发布只追加一个 ID 并向订阅者发送合并 wake signal，不等待订阅者读取或加载正文；慢消费者不能反压已经提交的 Message command。只有 application Store 已成功提交且 `CreateMessageResult.Created=true` 时才发布通知；完全相同的幂等 retry 不重复发布。PostgreSQL 中既有 `realtime-dispatcher` Outbox 意图保留给以后可靠 worker 或多实例 fan-out，本轮不把进程内通知伪装成 durable delivery。

### Listener、heartbeat、资源和关闭

仍使用唯一正式 `http.Server`。通过 Go `ResponseController` 只对已经认证、授权且即将进入 SSE 的响应禁用全局 15 秒 `WriteTimeout`；每次事件或 heartbeat 写入前设置 5 秒写 deadline，flush 后再清除。heartbeat 间隔固定为 15 秒，因此正常空闲流会在 Caddy 20 秒 response header timeout 前先写出 `ready`，之后也有有界权限复核和断链检测。

M0.5 使用硬上限而非容量承诺：每进程最多 256 条 SSE、每个用户最多 4 条、每个 Channel 最多 64 条。超过任一上限在写出 SSE header 前返回 `429 rate_limited`。计数只存在内存，不进入业务数据或 Session。进程收到关闭信号时先关闭 hub 并唤醒所有订阅，再执行现有最长 10 秒的 HTTP graceful shutdown；订阅不得让 `Server.Shutdown` 等到客户端自行断开。

固定版本 Caddy 能识别 `text/event-stream` 并即时 flush，因此本轮不为全部反向代理响应强制关闭缓冲。Compose 门禁必须通过真实 Caddy HTTPS 证明 `ready` 和 `message.created` 在连接保持打开时到达，而不是只验证直连 `httptest`。如果以后更换代理或配置，必须重新验证首事件、heartbeat、断线和缓冲行为。

### 权限变化唤醒边界

正式 hub 提供 Channel access changed 的进程内 wake 信号，供以后受治理的 membership command 在事务提交后通知相关订阅；wake 本身不携带用户或权限结果，handler 仍以数据库当前事实重新授权。当前尚无公共 membership mutation，因此真实数据库外部变更最迟由 15 秒 heartbeat 收敛；测试同时覆盖显式 wake 的即时收敛和无 wake 的 heartbeat 收敛。多进程部署前必须以 Outbox 或等价可靠机制传播权限变化，不能依赖单进程内存通知。

## 未采用的方案

### 现在接入 WebSocket

当前只有服务端单向 Message 增量。WebSocket 会同时引入升级请求 Origin、帧协议、双向 command、ping/pong、背压、依赖选择和多实例 fan-out，却不会替代 canonical history 或当前权限复核。M2 出现 presence、typing 或真正双向协作控制时再以版本化 WebSocket 协议替换 transport；本 ADR 的最小化、授权、回放失败和 resync 语义继续成立。

### 用短轮询作为默认实时入口

轮询能恢复权威历史，但会把固定 polling interval 和持续数据库列表查询变成默认负载，也不能及时验证空闲连接的撤权关闭。canonical GET 仍是明确 resync 手段，不建立隐藏 polling fallback。

### 直接消费 Outbox 或引入 Redis / broker

Outbox worker 能缩小提交后进程崩溃造成的通知空窗，broker 能做跨实例 fan-out，但当前正式拓扑只有一个应用进程。现在增加常驻 worker、claim/ack、重试与清理或新的基础设施会扩大恢复和运维合同。既有 Outbox 继续保存投递意图；出现多进程或经测量的可靠延迟需求后再单独决策。

### 在 hub 中缓存完整 Message DTO 或权限快照

这样可以减少回放数据库读取，但会形成第二份正文和过期权限缓存，扩大 restricted reply 与撤权泄漏面。正式实现只缓存 Message ID，每次发送按当前权限加载 canonical projection。

## 后果

正面影响：

- 正式 server 第一次具备与 Session、PostgreSQL 权限和 canonical Message DTO 一致的实时增量入口；
- 普通写请求、业务事务和 Web 页面不需要迁移到长连接，模糊写入仍由既有幂等合同恢复；
- 进程内状态严格最小化，权限撤销、restricted Thread 和重启都 fail closed 到 canonical resync；
- 单进程 slow consumer、资源上限、15 秒全局写超时、Caddy flush 和 graceful shutdown 有明确可测试合同；
- 不新增依赖、Redis、broker、第二个 server 或 WebSocket 兼容负担。

成本与限制：

- 每条业务事件和 heartbeat 都会访问 Session / Workspace / Channel 权限；没有测量证据前不缓存；
- 提交成功到进程内通知之间存在非 durable 空窗，进程重启也清空 replay；canonical history 必须始终可用；
- 1024 replay、256 / 4 / 64 连接数是 M0.5 防护上限，不代表生产容量或多实例配额；
- restricted Thread reply 被跳过后，只有隐藏事件的客户端可能在重连时重复权限判断，窗口溢出时必须全量 resync；
- 该入口不提供未读、通知、presence、typing、编辑、删除、附件、移动端后台推送或跨实例实时性。

## 迁移与验证

本 ADR 自身不新增 migration、第三方依赖或 Web 接入。接受证据包括：

1. hub 单元与竞态测试：canonical cursor、在线增量、断线回放、generation / future / stale cursor、慢消费者、三层连接上限、access wake 和 shutdown；
2. application / PostgreSQL 测试：只在首次事务提交后通知，精确 retry 不重复通知，Channel 与 restricted Thread 当前权限过滤；
3. HTTP 测试：正式 Session、Host / proxy、路径和无 query 合同，`ready` / `message.created` DTO，`Last-Event-ID`、15 秒 heartbeat 授权、控制事件最小化、每次写 deadline 与关闭；
4. 真实 Compose + Caddy HTTPS：连接保持打开时及时收到 `ready`，短请求创建 Message 后及时收到 `message.created`，Go server 与 PostgreSQL 仍不发布宿主端口；
5. `go test -race`、PostgreSQL integration、Compose 和仓库门禁全部通过。

上述证据已于 2026-09-03 全部通过：正式 Go server 的全量 `go test -race`、真实 PostgreSQL + Session integration、固定 Caddy 2.11.4 的全新 Compose HTTPS `ready` / `message.created` 流式验证，以及仓库门禁均成功。可丢弃实时实验及其独立 CI job 已删除，竞态覆盖并入正式 Go Server 门禁。

同日的后续独立切片已把该合同接入 canonical Channel Web：客户端先等待 `ready`，再读取 canonical history 并合并期间缓冲的增量；原生 EventSource 负责携带 `Last-Event-ID` 自动重连，`resync-required` 建立新边界并全量重读，`access-revoked` 清空正文和草稿。真实 PostgreSQL + production build + 固定 Caddy HTTPS + 内置浏览器已经证明跨账号增量、Caddy 重启后恢复、restricted Thread reply 不泄漏、heartbeat 撤权、登出重登和 390px 布局。Web Storage 仍只由代码和自动化复核，未读取浏览器存储；写 command、canonical history 与本 ADR 的服务端事件合同均未改变。

## 规范依据

- [WHATWG Server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html) 定义事件流、`Last-Event-ID` 和浏览器重连语义；
- [Go `net/http`](https://pkg.go.dev/net/http) 与 [`ResponseController`](https://go.dev/src/net/http/responsecontroller.go) 定义 flush 和按响应控制 write deadline 的能力；
- [Caddy `reverse_proxy`](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy) 定义 SSE streaming 与 flush 行为。
