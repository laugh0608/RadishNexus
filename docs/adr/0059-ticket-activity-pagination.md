# ADR-0059：Ticket Activity 历史分页

状态：已接受

日期：2026-11-23

## 背景

成员从原讨论进入工单后，需要查到最近的执行进展，也能继续翻到建单时的来源。本批实施前，Ticket Web 详情已经分开读取关系，但即使用了 `relations=summary`，Go 仍按时间升序取回该工单全部通用 Activity，逐项解析 subject 后才在 HTTP 层按 `include` 筛选。历史增长会增加每次打开工单的读取量。[数据库读取](../../server/internal/goldenpath/postgres/nexus_view.go) · [详情与事件校验](../../server/internal/platform/httptransport/collaboration.go) · [关系分页合同](0058-nexus-view-relation-pagination.md)

## 决定

### Ticket 事件与可见性

每次读取先完成 Session、Workspace 准入及 Ticket 所属 Project 的当前读权检查。可读的归档 Project 继续提供历史。以同一个 PostgreSQL repeatable-read 事务读取事件和 subject，沿用现有公开事件 DTO，包括严格字段校验、事件发生时的分类事实及已有的 `imported` 标记。[Activity 权限合同](../architecture/core-contracts.md#activity-投影)

| Ticket 事件（均带 `ticket.` 前缀） | 显示条件与内容 |
| --- | --- |
| `created` | 保留 Decision 和 Message 两种来源。来源已存在但不可读时沿用纯 `{"visibility":"restricted"}` subject。Message 来源工单要求 `execution_context`，缺少时详情和独立分页均沿用原 409，即便所读页没有建单事件。 |
| `status-changed`、`assignee-changed`、`priority-changed` | 沿用每种变更的前后值和版本。 |
| `component-linked`、`component-unlinked` | 展示关系建立或解除。可读 Component 返回受控引用及标题，受限 subject 沿原占位形状。 |
| `due-date-changed`、`classification-changed` | 分别要求 `due_date`、`classification`。分类变更展示操作时的标签名称。 |
| `discussion-linked`、`discussion-unlinked` | 要求 `execution_context`。按现有 Thread 与来源 Channel 规则重新核验，即使关联已解除也重查。讨论缺失或当前不可读时整条事件省略，包括事件 ID、时间、操作者及页边界。 |

`ticket.ci-run-linked` 与 `ticket.ci-run-unlinked` 继续由 `/tickets/{ticket_id}/ci-run-link-history` 公开。执行讨论的回复正文仍从原讨论读取。[构建关联历史](0056-ticket-ci-run-traceability.md) · [执行讨论](0054-message-to-ticket-and-execution-discussion.md)

其他对象各有自己的事件范围。Thread 协作详情的通用 Timeline 为空。Decision 通用历史包含提案、接受、拒绝、替代，人工复核沿专用 reviews 入口。Document 使用创建和修订，CI Run 与 Deployment 各用自己的 recorded 事件。投影器还处理 Project、Channel、Environment 和 Repository 的创建、Component 创建及 Repository 关联或解除、Ticket 标签目录的创建、改名和归档。这些类型均在本批 Ticket 路由白名单之外。[投影器](../../server/internal/goldenpath/postgres/activity.go) · [复核读取](0050-decision-manual-review.md)

### HTTP 与游标

以下 GET 路径相对于 `/api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/nexus-view`。沿用同源 Session、现有准入错误和 `private, no-store`。

| 请求 | 响应及数据库读取 |
| --- | --- |
| 详情，不带 `timeline` 参数 | 保留原 DTO、全量升序时间线、来源基数校验、`relations=summary` 及已有 `include` 组合。 |
| 详情，带 `timeline=separate` | 显式协商独立历史，响应省略 `data.timeline`，数据库跳过通用历史查询。仍校验 Current、来源、权限和能力。可独立使用或叠加原 `relations=summary` 与 `include`。只有 Ticket 接受此参数。 |
| `/timeline` | 返回 `data.items` 和可空的 `data.next_cursor`。接受可选的 `limit`（默认 25，范围 1～50）、`after` 和 `include`。详情与分页复用既有七种非空 `include` 子集及固定顺序 `due_date,execution_context,classification`，省略时使用原默认事件集合。 |

分页按 `(occurred_at DESC, event_id DESC)` 做排他 keyset，首屏展示最新可读进展。先按事件白名单、所选字段和当前权限过滤，再取 `limit + 1` 条。仅当存在下一条可读事件时，用本页最后一个返回事件生成游标。末页返回 `null`，空页返回空数组，无总数。仅增删不可读讨论时，可读页内容及游标应一致。

游标编码沿现有分页惯例，绑定格式版本、查询种类、Workspace、操作者、Ticket、规范化 include 集合和已展示的边界事件 ID。服务端在当前可读事件集中解析该 ID 的排序键。拒绝未知或重复参数、空值、非法 limit、损坏或作用域不符的游标（400）。未登录返回 401，Ticket 缺失与不可发现使用相同的 404。Workspace 准入失败沿既有语义。Ticket 仍可读而边界事件缺失、失去可读性或已不属于可返回集合，统一返回 409，客户端从首页重新读取。单页使用一致快照，跨页按当时权限与事实读取，新发生的事件在刷新首页时出现。

### Web 与查询成本

正式 Ticket 页在详情 GET 上同时指定 `relations=summary` 和 `timeline=separate`，并在详情 GET 与历史 GET 上使用相同的完整 include 集合。关系分页继续使用 ADR-0058 的入口。Activity 区显示当前页，提供查阅更早事件、返回首页与重试。切页时替换列表。对象或 Workspace 切换、显式刷新、重新进入、权限复查及成功写入后的详情刷新都清理旧页与游标，取消在途请求，忽略迟到响应。翻页遇到边界冲突时清页并重读首页一次，首页仍冲突则显示错误。认证、准入或 Ticket 失权走既有页面处理并清空受保护内容。普通分页读取失败仅在 Activity 区提供重试。旧服务端拒绝新合同须显示错误。

数据库在 LIMIT 前按集合完成事件筛选与讨论可读性判断，复用或对照正式 Thread / Channel 权限判断，以有界查询投影本页 subject。基线 schema 030 已有 `activity_items_target_timeline`，键依次为 Workspace、对象类型和 ID、投影版本、事件时间和 ID，可测试反向扫描。实施时在代表数据上记录 `EXPLAIN (ANALYZE, BUFFERS)` 和每页 SQL 调用数，分别增加本工单历史、隐藏讨论及同 Workspace 无关工单的数量。索引迁移由实测结果另行决定。[已有索引](../../server/db/migrations/002_activity_projection.sql)

## 替代方案与影响

直接截短旧详情会让已有客户端把第一页当成全部历史。在 Web 端截取仍需服务端读取全部事件。单独请求历史多一次 HTTP 往返，换取独立翻页与重试，也让数据库可以跳过首屏用不到的历史。本批只增加 Ticket 读协议，沿用现有实体、写命令、权限模型、Activity 17 与上下文包 v9。回退时可用匹配的旧 Web / Go 工件读取原详情，无业务数据转换。

## 验收场景

| 场景 | 可复验结果 |
| --- | --- |
| 同时刻与页边界 | 用两种建单来源准备多页历史，含同一时间的多事件、恰好一页和仅最后一条可读的情形。权限和数据固定时，串联新页再反序，与相同 include 下的旧时间线 DTO 逐项一致。 |
| 权限混排 | 将一整页以上的私密讨论放在可读事件之前、之间和之后。无权成员仍能取得完整可读页，末尾仅剩隐藏事件时无下一页。比较无隐藏事件的对照组。受限建单来源仍保留原占位。 |
| 撤权与并发 | 取得第一页后撤回边界讨论读权，下一页统一 409。撤回非边界讨论后继续翻页并跳过它。撤回 Ticket 读权后清空详情。跨账号、Ticket、Workspace 或 include 的游标被拒绝。页间新增较新事件后重读首页可见。 |
| 旧调用与专用历史 | 覆盖无参数、七种 include、两种关系模式、Message 来源 409 与非法组合。验证旧详情完整，精简详情实际跳过时间线查询。构建关联只出现在有权访问的专用历史中。 |
| 正式 Web | 从原讨论进入工单，使用键盘翻页、刷新并切换工单，检查窄屏、请求失败、401/404/409 和迟到响应。用合成账号串起讨论、Decision、工单历史、关联构建和已有 staging 结果。[有限故事范围](../status/current.md#接下来的事项) |
| 恢复与导入 | 在独立空目标恢复 schema 030 备份，重建 Activity 后用当前身份对照历史、顺序与权限。v9 导入后按映射身份及事件核对可携带历史和 imported 标记，构建关联仍按既有排除范围处理。[上下文迁移手册](../../deploy/context-package.md) |

项目所有者于 2026-11-23 确认上述接口与权限范围。实施与测量见[技术验收记录](../status/reviews/2026-11-23-ticket-activity-pagination.md)。
