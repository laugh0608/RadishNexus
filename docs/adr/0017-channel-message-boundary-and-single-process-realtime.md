# ADR-0017：Channel / Message 边界与单进程实时收发实验

状态：已接受

日期：2026-09-01

## 背景

Golden Path 的上游入口要求成员先在 Project Channel 发送 Message，再从真实讨论进入 Thread → Decision → Ticket。正式服务已经具备 Workspace membership、Project / restricted Thread 权限、EntityLink、不可变领域事件、Outbox、Activity 和 Session transport，但还没有 Channel / Message 对象，也没有验证断线重连、重复发送、权限撤销和慢消费者。

总体架构仍把版本化 WebSocket 作为 M2 消息与协作事件的目标协议，但 M0.5 当前只需要冻结不会因传输实现变化而失效的业务语义。直接引入 WebSocket 依赖、集群 broker、Redis 或完整聊天 UI 会把协议、部署和产品交互同时铺开；只做轮询又无法暴露空闲订阅在权限撤销后的收敛问题。

因此本轮先建立一个不接入正式 server、可整体删除的 Go 标准库 HTTP command + Server-Sent Events（SSE）实验。实验用于证明游标、回放、幂等和当前权限语义，不把 SSE 追认为长期公共协议。

## 决定

### Channel 最小对象

M0.5 新增稳定实体类型 `channel` 与 `chn_` ID 前缀。Channel 最小字段为：

| 字段 | 约束 |
| --- | --- |
| `id` | 类型内全实例唯一、不可复用的服务端 ID |
| `workspace_id` | 创建后不可变 |
| `governing_project_id` | 同一 Workspace 的 Project，创建后不可变 |
| `name` | Project 内用户可见名称；不能代替稳定 ID |
| `visibility` | `project / restricted` |
| `status` | `active / archived` |
| `created_by / created_at / updated_at` | 复用既有共同字段语义 |

`project` Channel 复用 governing Project 的读取边界；`restricted` Channel 在能读取 Project 之外，还要求显式 Channel membership。Project `admin` 不自动穿透 restricted Channel。归档 Channel 保留历史读取与关系，不再接受发送或创建新 Thread。

本轮不建立私聊、群聊 Conversation、Channel 分类、主题、排序、置顶、未读、通知、搜索、附件、表情或 @ 提醒。

### Message 最小对象与幂等

M0.5 新增稳定实体类型 `message` 与 `msg_` ID 前缀。Message 是权威、不可变的原始讨论记录，最小字段为：

| 字段 | 约束 |
| --- | --- |
| `id` | 类型内全实例唯一、不可复用的服务端 ID |
| `workspace_id` | 与 Channel 相同且创建后不可变 |
| `channel_id` | 必填，指向同一 Workspace Channel |
| `thread_id` | 可空；回复时指向同一 Channel 中可读的 Thread |
| `author_id` | 发出 command 的当前 active 用户，不信任正文或客户端声明 |
| `body` | 原样保存的非空 UTF-8 文本，禁止 NUL，M0.5 上限 16 KiB |
| `client_operation_id` | 作者客户端生成的不透明 ASCII 幂等键，最长 128 bytes |
| `created_at` | 服务端接受 command 的权威时间 |

幂等唯一范围固定为 `(workspace_id, channel_id, author_id, client_operation_id)`：

- 首次接受时创建一个 Message；并发或后续相同请求返回同一 Message；
- 相同幂等键只有在正文逐 byte 相同时才是重试，正文变化必须冲突；
- `client_operation_id` 不是 EntityID，不进入 EntityRef、Activity、领域事件 payload 或其他读者可见的实时投影；
- 每次重试仍先按当前权限授权，历史成功不授予已撤销主体读取或发送能力；
- M0.5 不支持编辑、撤回、删除、富文本变换或服务端静默裁剪。未来新增这些动作必须使用独立 command 和历史语义，不能覆盖原始记录。

### Message、Thread 与 Decision 的来源链

Thread 继续是 ADR-0004 已冻结的独立 `thread` / `thr_` 对象，不退化为 Message 行号或只存在于客户端的分组。由 Message 发起的 Thread 增加不可变 `origin_channel_id` 授权属性；该类 Thread 必须与 Channel、Message 共享 Workspace 和 governing Project。

创建动作在一个事务中完成：

1. 当前主体必须能读取并向源 Channel 发送；
2. 创建 Thread；restricted Channel 只能创建 restricted Thread，project Channel 可以创建 project 或 restricted Thread；
3. 注册并创建 `thread --started-from--> message` 的 asserted + user EntityLink；
4. 写入 `thread.started` 领域事件与 Outbox 投递意图；
5. 不把源 Message 正文复制到 Thread 标题、事件、Activity 或关系 metadata。

Thread 的读取继续要求既有 Workspace + governing Project + Thread visibility；对有 `origin_channel_id` 的 Thread 还必须能读取当前 Channel。显式 Thread membership 只能进一步收窄，不能绕过 Channel。回复 Message 同时要求当前 Channel 和 Thread 可读，以及 Project contributor 或更高写能力。创建时加入 restricted Thread 的主体必须当时能读取 Channel；之后 Channel 权限撤销立即让该主体失去 Thread 和回复读取，即使旧 Thread membership 仍存在。

Source Message 本身仍按 Channel 可读；`started-from` 引用不会授予 Thread 权限。不能读取 Thread 的 Channel 读者在允许显示关系的对象页最多得到既有通用 restricted 占位，不能获得 Thread 类型、ID、标题、参与者或时间。Decision 继续从 Thread 建立既有 `derived-from` evidence，Ticket 继续 `implements` Decision，由此形成不复制正文的 Message → Thread → Decision → Ticket 来源链。

### 领域事件、Outbox、Activity 与实时投影

正式 Message command 必须在同一 PostgreSQL 事务写入 Message、`message.created` 不可变领域事件和 Outbox 投递意图。事件使用 Message 作为 `primary_entity`，保留 governing Project context，payload 只包含重建所需的 Channel / 可选 Thread 引用；禁止正文和 `client_operation_id`。Thread 创建同理原子写入 Thread、`started-from` EntityLink、`thread.started` 与 Outbox。

`message.created` 在 M0.5 不投影为全局 Activity，避免把每条聊天复制成噪声和长期正文缓存。Message 列表直接读取权威 Message 表。`thread.started` 可以在对象级 Timeline 中只保存安全引用，并在查询时复用 Channel + Thread 当前权限；任何 Activity、关系、搜索或通知都不能比权威对象更宽。

实时 payload 是提交后的、按当前主体授权生成的临时 Message DTO，可以含正文；它不是领域事件、Outbox 或 Activity 的序列化副本，也不是权威存储。不得在数据库事务提交前发送。丢失实时通知只影响延迟，客户端总能通过 canonical Message 查询恢复。

### 单进程实时实验合同

本 ADR 当时建立的可丢弃实验采用以下合同；该实现已在 [ADR-0020](0020-session-scoped-single-process-message-realtime.md) 的正式切片覆盖相同失败路径后删除：

- `POST /channels/{channel_id}/messages` 验证 command 幂等；
- `GET /channels/{channel_id}/events` 以 SSE 发送 `message.created` 增量；
- cursor 由不透明 process generation 与 Channel 内递增 position 组成，只是当前进程的临时恢复点，不是 Message ID、数据库 offset 或公共排序承诺；
- 客户端先读取 canonical Message 列表，再从 `ready` cursor 接收增量；断线时使用 SSE `Last-Event-ID` 在有界窗口内回放；
- generation 改变、cursor 过期、未来 cursor 或消费者落后于窗口时 fail closed，发送不含业务数据的 `resync-required` 后关闭；客户端必须重新读取 canonical 数据；
- 发布路径只写权威状态并发送合并通知，不等待每个订阅者。慢消费者超过回放窗口时重同步，不能反压或阻塞 Message command；
- 初次连接、回放、每次发送前和 heartbeat 都复核当前读取权限；Channel 权限变化另有唤醒信号，使空闲连接也能发送不含业务数据的 `access-revoked` 后关闭；重连继续按当前权限不可发现；
- 实验的 `X-Experiment-User` 只用于测试注入 principal，绝不进入正式认证合同。

正式接入若暂时复用 SSE，必须使用已有 Session、Workspace membership、CSRF、精确 same-origin、公共错误映射和限流边界。当前 server 的统一 15 秒写超时不适合长期响应，不能直接挂入现有 listener；必须先给长连接冻结独立 timeout、heartbeat、Caddy flush、连接上限、优雅关闭和可观测性。M2 进入双向 presence、typing 或协作控制前仍以版本化 WebSocket 为目标，并重新验证 Origin、背压、多副本 fan-out 与恢复协议。

## 未采用的方案

### 现在直接实现 WebSocket

WebSocket 能提供双向通道，也是总体架构的长期方向，但当前 Go 标准库没有要复用的 server 实现。此时新增依赖并同时冻结 handshake、帧协议、heartbeat、背压和客户端状态机会让 M0.5 的业务语义与 M2 transport 设计耦合。当前先用标准 SSE 的重连字段暴露相同的游标和授权失败路径，之后可以保留语义并替换传输。

### 只使用短轮询

短轮询能读取权威状态，但无法直接证明空闲连接如何因权限变化及时收敛，也会把 polling interval 混入交互延迟和负载判断。它仍可作为客户端显式降级或重同步手段，不作为本实验主路径。

### 立即引入 Redis、PostgreSQL LISTEN/NOTIFY 或消息中间件

M0.5 只有单个 Go 进程，现有 Outbox 已承担可靠异步投递意图。现在增加独立实时基础设施不会证明新的产品语义，只会提前扩大部署、恢复和故障面。多副本出现真实需求时，再以 Outbox、持久游标和实例间 fan-out 证据选择基础设施。

### 把 Message 正文放进领域事件或 Activity

这样回放和展示更方便，但会产生第二份长期正文、扩大权限撤销和数据保留面，并违反核心事件契约。实时 DTO 可以在发送时授权后加载正文；事件和 Activity 只保留安全引用。

## 后果

正面影响：

- Channel、Message 和 messaging-origin Thread 第一次拥有可供 migration 与 application service 直接实现的最小字段、权限和来源合同；
- Message 重试、实时 cursor 和 EntityID 职责分离，网络重放不会复制业务对象；
- 权限撤销能在没有新消息时收敛，关系、Activity、实时推送和 canonical query 继续依赖同一当前权限；
- Message → Thread → Decision → Ticket 保留结构化来源，不复制全文；
- 单进程实验零新增依赖，可在正式切片覆盖后整体删除，不形成第二套 server。

成本与风险：

- SSE 只验证服务端单向增量，不能证明 M2 WebSocket 的双向控制、二进制帧、多副本 fan-out 或移动网络行为；
- process-local 有界 replay 在重启和过度落后时必然重同步，canonical Message query 必须高效且稳定；
- messaging-origin Thread 新增 Channel 读取层，正式查询和关系投影必须避免通过旧 membership 或缓存绕过；
- 当前 16 KiB 正文和不可变 Message 只服务 M0.5，编辑、删除、保留策略与合规导出仍需后续设计；
- 长连接需要独立资源上限、timeout、代理和关闭合同，不能沿用普通 request 的默认值。

## 迁移与验证

本 ADR 不新增正式 migration、公共 handler 或 Web UI。当前实验已经实际执行 `go test -race ./...` 与 `go vet ./...`，覆盖：

1. 顺序和 64 路并发相同重试只创建一个 Message，变化 payload 冲突，作者或 Channel 不同不会误去重；
2. SSE 在线增量和 `Last-Event-ID` 断线回放；
3. 回放窗口过期后的 `resync-required`；
4. 空闲订阅权限撤销后的无业务数据关闭，以及撤销后重连不可发现；
5. 不读取通知的慢订阅者不会阻塞 1,000 次发布；
6. Message ID 跨 Channel 保持类型内唯一，实时投影不返回幂等键或内部 cursor。

下一正式纵向切片应按以下顺序交付：

1. migration 注册 `channel` / `message`、`started-from`，建立 Channel、membership、Message 幂等约束与 Thread origin Channel 属性；
2. application service 在真实 PostgreSQL 事务中重新验证当前权限，原子创建 Message / Thread、EntityLink、领域事件和 Outbox；
3. canonical Message query 与 Session 作用域 command transport；需要实时入口时先解决独立 listener / proxy 合同；
4. Web 只实现单 Channel 的列表、发送和从 Message 发起 Thread，贯通既有 Decision / Ticket，不扩展完整聊天功能；
5. 用真实 PostgreSQL、HTTPS、Caddy 和浏览器复验重试、重连、权限撤销、restricted 占位、CSRF、缓存与失败语义。

正式切片覆盖这些失败路径后删除实验；该删除条件已于 2026-09-03 由 ADR-0020、正式 server 竞态 / PostgreSQL integration 与 Compose + Caddy HTTPS 验证满足。若未来采用 WebSocket、持久 cursor 或多副本 broker，只替换传输和 fan-out，不得放宽本 ADR 的幂等、来源、权限、正文最小化或 canonical resync 语义。

## 规范依据

- [WHATWG Server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html) 定义 `Last-Event-ID`、自动重连和事件流格式；
- [RFC 6455](https://www.rfc-editor.org/rfc/rfc6455) 与 [WHATWG WebSockets Living Standard](https://websockets.spec.whatwg.org/) 作为后续双向协议和浏览器安全边界依据；
- [Go `net/http` 文档](https://pkg.go.dev/net/http) 定义 `Flusher` 与请求 Context 的连接生命周期；
- [Caddy `reverse_proxy` 文档](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy) 说明 `text/event-stream` 的流式 flush 行为。
