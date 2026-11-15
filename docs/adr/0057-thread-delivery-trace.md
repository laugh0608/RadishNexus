# ADR-0057：从原讨论追踪工单、构建与部署

状态：已接受

日期：2026-11-15

相关合同见 [协作对象反向关系](0022-transactional-activity-and-incoming-relations.md)、[消息转工单与执行讨论](0054-message-to-ticket-and-execution-discussion.md)、[工单与构建关联](0056-ticket-ci-run-traceability.md) 和 [staging 记录](0031-session-scoped-staging-deployment-recording.md)。

## 背景

Thread 已能反查派生 Decision，也有独立入口查看显式关联的执行工单。成员可以在 Ticket 和 CI Run 页面双向跳转，从 CI Run 查看已有的 staging 部署记录。Golden Path 要求成员回到原讨论时也能找到后续工单、构建与部署结果。 [当前缺口](../status/current.md#已确认缺口) · [Golden Path](../golden-path.md#演示故事)

Thread 页面增加只读的“执行与交付”区域。成员从工单开始展开构建，再按需展开部署记录。各级继续使用原对象的稳定地址、当前状态与权限。

## 决定

### 工单可达路径

| 来源 | 确定方式 |
| --- | --- |
| 从 Decision 建单 | Ticket 的创建来源为 `implements` Decision，且该 Decision 以 active `derived-from` 引用当前 Thread。按原创建来源追溯，已被替代的 Decision 仍可保留其原工单。 |
| 从 Thread 内消息建单 | Ticket 的创建来源为 `started-from` Message，且该 Message 的 `thread_id` 为当前 Thread、`channel_id` 与 Thread 的来源 Channel 一致。此路径只取 Thread 内回复。频道顶层消息建单已有独立的原消息入口。 |
| 执行讨论 | Ticket 和当前 Thread 有 active `relates-to`，沿 ADR-0054 的同 Project 和来源 Channel 约束。 |

- 三条路径均限当前 Workspace 和 Thread 所属 Project。复用现有 Thread、来源 Channel 和相关对象的读取合同。无来源 Channel 的历史 Thread 可按原读权限使用 Decision 路径。
- 每个可读 Ticket 只出现一行，按稳定 Ticket ID 去重后再分页。只展示实际可读的来源类型，按 `decision-origin`、`message-origin`、`execution-discussion` 固定顺序。来源标签只说明路径类型，详细来源经现有工单详情跳转。
- 解除执行讨论后从下一次读取移除此路径。如果该 Ticket 另有可读的创建来源路径，保留一行及其剩余来源类型。原 Decision 的替代链和同一 Channel 中的其他 Thread 各自沿原入口读取。

### 读取与权限

- 每个请求先验证当前 Session、Workspace membership、Thread 所属 Project、Thread 和具备来源 Channel 时的 Channel 读取权。判定任一路径时继续核验其对象及关系，包括 Decision、Message、Ticket 和共同 Project。无来源 Channel 的兼容读取沿现有 Thread 规则。
- 构建页在同一次读取快照内重新确认当前 Thread 仍能经至少一条路径发现该 Ticket，再分页读取该工单的 active `tracks-build` 关系。运行必须可读，按现有合同读取所属 Component 和 Jenkins 终态。
- 部署页重新检查上述路径、指定运行及该 Ticket 到该运行的 active 关联。只读该运行已有的 staging 记录，逐项检查 Deployment、CI Run 和 Environment 读取权。归档 Project 继续沿现有只读权限，构建终态与部署终态各自展示。
- 先授权、过滤和去重，再取 `limit + 1` 计算当前页与游标。不可读条目对外完全省略，直接请求缺失、不可发现或失去上游路径的嵌套对象统一返回 404。同一页使用同一数据库读取快照。跨页和刷新重新授权并读取当前事实。
- Thread 读取使用现有结构化关系和权威业务行。默认 Nexus View、Timeline、讨论关联管理及原有 Ticket 和 CI Run 端点继续沿其合同。关联与交付历史在相应对象详情读取。

### HTTP 与 Web

路径前缀为 `/api/v1/workspaces/{workspace_id}/threads/{thread_id}/delivery-tickets`。复用 Session 和 `private, no-store`，GET 仅接受 `limit` 与 `after`，默认一页 25 条，最多 50 条。三个层级各自按对象稳定 ID 升序。游标绑定 Workspace、操作者、查询类型、Thread、路径上的 Ticket 和 CI Run 以及可见边界。

| GET 后缀 | 返回 |
| --- | --- |
| 前缀本身 | 可读 Ticket 的 ref、标题、当前状态和有序 `origins` |
| `/{ticket_id}/ci-runs` | 可读关联构建的 ref、所属 Component 安全摘要、构建终态和完成时间 |
| `/{ticket_id}/ci-runs/{ci_run_id}/deployments` | 该构建已有且可读的 staging Deployment ref、Environment 安全摘要、部署终态和完成时间 |

每级统一返回 `items` 和 `next_cursor`。仅凭子对象可读性不能在 Thread 上下文中跨过失效的父级路径。来源标签、链接与摘要使用明确的响应白名单。构建继续跳转原 CI Run，部署继续跳转原 Deployment。工单、构建和部署的写入仍从各自的正式入口进行。

Web 首次读取工单页，成员展开某一行时再读取该层子页。每一级独立翻页，支持清晰的加载、空结果、读取失败和重试状态。切换 Workspace、Thread、父级或游标时取消对应请求并清掉旧数据。页面重读从工单首页重新核对可见链路。发现某条路径或关联已失效时清除相关子层并重读父层，忽略失效请求的迟到响应。

### 数据与性能边界

- 直接从既有事实派生当前视图。沿用 schema 029、Activity 17、整库备份和 `.nexus` v9。构建、部署和关联历史继续随整库备份恢复；v9 取回后的 Thread 由该包恢复的协作路径派生工单列表。
- 首层在数据库中合并三条受控路径，按 Ticket ID 聚合来源，再做有界分页。下层复用 ADR-0056 的权限和列表规则，增加父级可达性检查。用代表性合成数据记录查询计划、每页 SQL 次数及多来源过滤成本。实现使用现有 PostgreSQL 索引与依赖，若测量显示索引缺口，再说明所需迁移的影响。
- 保持按需展开各层。这样每次请求都有清晰的端点权限和分页边界，单张工单下的大量运行与部署记录可以独立浏览。跨对象事件汇总属于单独的 Activity 合同。

## 实施顺序与验收

| 顺序 | 验收 |
| --- | --- |
| 1. 服务端读取 | 三种来源与双路径去重，已替代 Decision、历史 Thread、归档对象、混合可见性、跨 Workspace／Project、游标重放、解除后的嵌套直达访问，并记录代表查询计划。 |
| 2. Thread 页面 | 经正式入口找到工单、展开构建和已有 staging 记录并跳转。验证各层独立分页、取消请求、撤权或解除后清理和重读，以及键盘与窄屏。 |
| 3. 合成环境与恢复 | 在隔离 PostgreSQL 和回环 HTTPS 验证 Session 权限及旧入口兼容。整库恢复后从原 Thread 按当前权限追溯，复核 v9 取回的协作路径和研发取回边界。 |

本批的合成数据与回环 HTTPS 验证见[技术验收](../status/reviews/2026-11-15-thread-delivery-trace.md)。真人观察、真人实测与真实 Jenkins 联调沿[当前执行顺序](../status/current.md#近期执行顺序)安排。
