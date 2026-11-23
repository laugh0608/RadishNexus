# ADR-0060：从频道原消息找回讨论

状态：已接受

日期：2026-11-23

## 背景

成员刷新或重新进入 Channel 后，仍需要从原消息回到已有讨论。实施前，频道页只记住本次创建成功的 Thread。消息历史中的 `thread` 表示回复所属的讨论。一条回复还可以作为另一个讨论的来源。数据库以不可变的 `Thread started-from Message` 关系记录来源，每条消息最多发起一个 Thread。[消息与来源关系](0017-channel-message-boundary-and-single-process-realtime.md) · [现有短请求](0018-session-scoped-channel-message-transport.md)

## 决定

在现有 `/api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages/{message_id}/threads` 路径增加 Session 只读 `GET`。成员在单条消息上选择「查看讨论」时读取，复用频道短请求的 Session、Workspace 准入、缓存和错误合同。该读取不接受查询参数。

| 当前读取条件 | 响应 |
| --- | --- |
| 路径内的源消息和它发起的 Thread 都可读 | `200`，`data.thread` 仅含 `ref`（`type=thread`、`id`）和 `title` |
| 源消息可读，尚未发起讨论，或讨论当前不可读 | 同一 `200` 和 `{"data":{"thread":null}}` |
| 源消息不属于该路径，或当前不可读（含回复所属的私密 Thread 不可读） | `404 not_found` |

每次查询在同一个 PostgreSQL 快照内检查来源和目标。沿用既有 Project、Channel、Thread 的读权限，允许有读权的 viewer 查看；归档后仍能读取历史。Web 只用返回的可读引用进入既有 Thread 详情。重新进入频道、会话变化或重新校验权限时丢弃旧结果。

## 替代方案

向每条历史消息附加子讨论会让普通历史加载承担额外权限查询，也需要同步调整实时恢复合同。复用创建命令探测会尝试写入，重复创建的冲突响应也无法给出当前可打开的引用。按消息显式读取可以沿用现有唯一来源索引。

## 迁移与验证

沿用 migration 006 的来源关系和索引，适用于已有消息。原 POST、历史响应及 SSE 的形状保持兼容。数据库验证覆盖 viewer、私密来源与私密目标、归档和撤权。Web 验证使用合成账号从原消息创建讨论，刷新、离开并重新进入频道，再按当前读权打开讨论；另一个账号检查空结果和撤权后的页面。
