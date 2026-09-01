# 消息实时收发实验

状态：可丢弃技术实验，不是生产服务

日期：2026-09-01

## 目的

本实验为 [ADR-0017](../../docs/adr/0017-channel-message-boundary-and-single-process-realtime.md) 提供最小可执行证据，验证单进程内的消息重复提交、实时增量、断线重连、权限撤销和慢消费者边界。它不实现正式数据库、公共 API 或聊天产品。

实验沿用仓库 Go 1.25 最低版本，刻意只使用标准库 HTTP command + Server-Sent Events（SSE），不新增依赖。这个组合便于单独检验游标和授权收敛，不代表把 M2 的版本化 WebSocket 目标改成 SSE，也不承诺多副本协议。

## 已验证语义

- 相同 `(channel_id, author_id, client_operation_id)` 与相同正文只创建一条 Message，并返回同一 `msg_` ID；并发重试也只有一个创建者；
- 相同幂等键携带不同正文时返回冲突，不用后到请求覆盖已接受内容；
- `Last-Event-ID` 可以在同一进程 generation 和有界回放窗口内补发断线期间的事件；
- generation 改变、游标过期或消费者落后于回放窗口时只发送不含业务数据的 `resync-required`，客户端必须重新读取权威数据；
- 权限变化有独立通知通道。即使频道没有新消息，已撤销读权限的订阅也会收到不含业务数据的 `access-revoked` 并关闭；重连继续按当前权限返回不可发现；
- 发布只发送合并通知，不等待订阅者读取。慢消费者不会阻塞写入，落后过多时转入权威重同步；
- Message ID 在当前实验进程内跨 Channel 唯一，频道游标只在当前进程、当前 Channel 内有序，两者不混用；
- 实时 Message 投影不返回 `client_operation_id` 或内部游标；SSE cursor 只出现在 `id` 字段，不进入权威业务对象。

## 实验 HTTP 形状

- `POST /channels/{channel_id}/messages` 接收 `client_operation_id` 与 `body`；
- `GET /channels/{channel_id}/events` 返回 SSE，使用 `Last-Event-ID` 请求回放；
- `X-Experiment-User` 只是在测试中注入 principal 的假边界，绝不是身份协议。

正式切片必须改用已有 Session、Workspace membership、CSRF、精确 origin、公共错误映射和 PostgreSQL 事务。初次订阅只从 `ready` 游标开始；客户端必须先读取 canonical Message 列表。`resync-required` 后也必须重新读取 canonical 数据，不能把内存回放当作权威消息存储。

## 运行验证

从仓库根运行：

```bash
./scripts/check-messaging-realtime.sh
```

入口执行 `go test -race ./...`、`go vet ./...` 与 `go mod tidy -diff`，默认不访问网络。HTTP 测试使用 `httptest` 的本机随机端口，不连接外部服务。

## 已知限制与删除条件

- 全部消息、幂等记录、游标和回放窗口都只存在内存；进程重启必然要求 canonical 重同步；
- access gate 只是已有 Workspace + Project + Channel / Thread 权限的可控替身；正式 command 必须在 PostgreSQL 事务接受点重新验证权限；
- 实验没有 Session Cookie、CSRF、Origin 校验、限流、审计、持久化 Outbox、正式 ID 生成器、HTML 安全渲染或客户端状态机；
- 仓库正式 server 的全局写超时当前不适合长连接。接入正式 SSE 或 WebSocket 前必须单独冻结 listener timeout、heartbeat、反向代理 flush 和优雅关闭合同；
- 实验不是第二套 server，不能被 production build、Compose 或 Web App 引用。

当正式 PostgreSQL + application service + transport 测试覆盖相同失败路径后，应删除本目录，而不是继续把产品能力堆进实验。
