# ADR-0052：Workspace「我的工单」

状态：已接受

日期：2026-11-08

现有 Project 工单列表支持按本人责任、状态和优先级筛选。成员需要一个日常入口，在当前 Workspace 的多个 Project 中找到自己负责的待处理工单，并从现有 Ticket 详情继续执行和查看来源。2026-11-08 项目所有者确认推进这一范围。

## 决定

- 从工作台当前 Workspace 进入「我的工单」。列表仅包含当前负责人为登录用户、状态为 `open` 或 `in-progress`、所属 Project 为 `active` 且当前可读的 Ticket。可读性沿用 governing Project 的规则。受限 Project 要求显式成员关系，来源 Thread 独立授权。仍可读取工单的只读成员可以看到自己的分配记录。
- 服务端在一次 repeatable-read 事务中复核当前 Session 与 Workspace 准入，并在分页前应用全部条件。每页沿稳定 Ticket ID 升序读取。翻页时重新检查当前权限和匹配条件，刷新可重新读取首页，查看排序边界之前新匹配的工单。
- Web 显示标题、所属项目、状态与优先级，链接到既有 Ticket 详情。页面离开时清理列表并取消请求，页面恢复或重新聚焦时重读首页。列表状态与 Workspace 和当前用户绑定。

## HTTP 合同

`GET /api/v1/workspaces/{workspace_id}/my-tickets`

- 仅接受 `limit`（1～50，默认 25）与 `after`，参数各出现一次。负责人取自当前 Session。
- 响应为 `data.items[{ref,title,status,priority,project:{id,name}}]` 和可空的 `data.next_cursor`。列表响应仅包含上述字段。不可进入 Workspace 时沿用发现入口的 404，Session 失效为 401。响应使用 `private, no-store` 和 `Vary: Cookie`。
- 游标绑定 `my-tickets` 列表、Workspace、当前操作者和排他 Ticket ID 边界。其他作用域、列表或操作者的游标返回 400。
- 配套交付 Go 和 Web。当前读路径复用现有数据与索引。回退时使用匹配的旧 Go 和 Web 工件。

## 验收

用正式协作与工单命令建立样本，从同源 Session 入口验证跨项目分页、受限项目撤权、只读成员、改派、终态、归档与 Workspace 隔离。Web 验证工作台导航、状态与优先级展示、进入工单详情、返回页面重读和切换 Workspace 后丢弃迟到响应。
