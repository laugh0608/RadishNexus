# ADR-0054：消息转工单与执行讨论

状态：已接受

日期：2026-11-15

部分替代 [ADR-0019](0019-session-scoped-thread-decision-ticket-transport.md) 中 Ticket 只从 Decision 创建的限制，扩展 [ADR-0053](0053-ticket-due-date.md) 的 Ticket Nexus View 可选读取形状。

## 背景

成员已经能在频道发消息、从消息发起 Thread、从已接受的 Decision 创建 Ticket，并在工单中管理状态、负责人、优先级和截止日期。频道中的日常执行事项也需要进入工单。成员推进工单时需要继续讨论，并能从讨论回到工单。两项能力属于 M4。 [产品定义](../product-definition.md#从讨论到执行) · [产品路线图](../roadmap.md#m4工单与上下文关联)

现有 Ticket 读取和 v1～v7 上下文包都要求创建历史对应一条 `implements` Decision 关系。Channel 的消息契约已支持通过 `thread_id` 发送回复。Thread 页面需要补齐按 Thread 读取回复和继续讨论的入口。 [消息契约](0018-session-scoped-channel-message-transport.md) · [Ticket 读取实现](../../server/internal/platform/httptransport/collaboration.go) · [上下文包校验](../../server/internal/contextpackage/validate.go)

## 决定

### 创建来源与执行讨论

| 用户动作 | 后端事实 |
| --- | --- |
| 从已接受的 Decision 创建工单 | 沿 ADR-0019 及 Decision 生命周期契约。创建来源是唯一且不可变的 `Ticket --implements--> Decision`。 |
| 从频道中的一条消息创建工单 | 操作者填写面向 Project 成员的标题并明确确认。创建来源是唯一且不可变的 `Ticket --started-from--> Message`。来源可以是频道顶层消息或符合下述阅读范围的 Thread 回复。 |
| 将现有 Thread 关联为工单的执行讨论 | 在同一 Project 内显式关联已有来源 Channel 的 Thread，保存为 `Ticket --relates-to--> Thread`。Ticket 与 Thread 可以多对多关联，同一对端点最多一条 active 关系。解除指向精确的 EntityLink，保留移除者、时间和事件。重新关联追加新 EntityLink。 |

- 每张通过正式命令新建的 Ticket 恰好有一种创建来源。关联、解除和重开均保留创建来源。消息来源始终指向具体 Message，包括从 Thread 回复创建的情况。执行讨论须显式关联。两类新增关系均记录为 `asserted`、`user`，以对应创建或关联事件作为 `source_event_id`。
- 新建 Ticket 处于 `open`，沿既有状态、负责人、优先级和截止日期的初始值及版本。来源 Message 保留正文、作者和时间的权威记录。创建响应、Ticket、关系元数据与事件仅携带各自所需字段，标题由操作者填写。
- Channel 与 Thread 回复的消息菜单提供“创建工单”和“查看由此创建的工单”。工单详情显示来源并跳转到原消息所在 Channel 或 Thread。目标页按稳定 ID 通过新增的单条消息读取显示被定位消息，因此较早的来源也能直接打开。
- Thread 页面从所属 Project 的可读工单分页选择待关联工单，无须手工输入 ID。工单详情分页列出讨论，可以打开 Thread 或解除关联。新讨论沿现有“从消息发起 Thread”入口创建，Thread 页面补齐回复列表与发送。Thread 页同时提供关联工单入口和每条回复的来源工单入口。
- 工单详情也可发起同一关联。工单侧 `can_link` 允许时，成员从本项目可读频道分页选择讨论。已归档频道标明并禁选，当前工单关联页已经显示的讨论标为已关联。选中后按讨论侧读取 `can_link` 和关联首页，核对后方可确认。其他页或并发新增的重复关系由原 POST 在事务中裁决。解除仍使用精确的 EntityLink ID。[频道讨论列表](0061-channel-thread-discovery.md) · [双账号验证](../status/reviews/2026-11-23-ticket-discussion-picker.md)
- 讨论关联写入 EntityLink 和自己的历史事实。Ticket 状态、各字段版本及 `updated_at` 继续由各自的工单动作维护。讨论回复沿 Channel 的 Message 记录和实时契约，工单 Activity 只展示关联的建立和解除。

### 权限与可见性

| 动作 | 当前权限与条件 |
| --- | --- |
| 从消息创建 Ticket | active Workspace、active Project、active Channel，操作者是 governing Project 的 contributor、decider 或 admin。服务端核对路径中的 Channel 与 Message 一致，重查来源 Message 的当前读取权。Channel 必须是 `project` 可见性，回复还须来自同 Channel 的 `project` Thread。Ticket 继承该 Project 的可读范围。 |
| 从工单打开创建来源，从消息发现工单 | 分别检查两端当前可读。消息回复沿其 Channel 与 Thread 的完整读取约束。消息端只列可读的直接创建结果，工单端无法读取来源时只显示通用受限占位。 |
| 关联已有 Thread | active Workspace、active Project、active 来源 Channel，当前操作者有 Ticket 的 Project 贡献权，同时能读取 Ticket、Thread 和来源 Channel。允许操作者关联其可读的 restricted Channel 或 Thread 中的讨论。跨 Workspace 或 Project 的两端不可发现。 |
| 解除 Thread 关联 | active Workspace、active Project，当前操作者有 Ticket 的 Project 贡献权且能读取两端。允许解除来源 Channel 已归档的可读关系。完成或取消的 Ticket 仍可维护讨论关系。 |
| 阅读、回复讨论 | 读取复用 Thread 和来源 Channel 的现有权限，回复经原 Channel Message 写入入口重新检查当前贡献权、Channel 状态与 Thread 读取权。 |

- 已有权限来自成员生命周期、对象和角色。Workspace owner 与 Project admin 读取 restricted Channel 或 Thread 时，同样适用该对象的显式成员条件。关系命令保持两端对象的 ACL。
- 任何一端失权时，讨论关系从双向列表、计数、游标和新增讨论 Activity 中隐藏，也不产生关联占位。不可读、不存在及跨作用域的端点统一返回 `404`。可读对象缺少 Project 写权或 Project 已归档时返回 `403`。可读的 restricted 来源或已归档 Channel 收到建单请求时返回 `409 conflict`，Web 按服务端能力隐藏该动作。
- 已归档 Project 的对象沿现有规则可读，写入失败。已归档 Channel 保留合法来源及回复记录的读取；创建工单、新增讨论关联和发送回复均要求 Channel active。
- 写入和精确重试均重新检查对应动作的授权及不可变作用域。首次解除要求关系仍 active，精确重试核验原移除事实。并发撤权走既有准入和权限行锁。旧 receipt、前端缓存及 URL 都不能提供过期授权。

### HTTP、事务与历史

下表路径前缀为 `/api/v1/workspaces/{workspace_id}`。写入使用现有 Session、Origin、CSRF、严格 JSON、`client_operation_id` 和 `confirmed: true`。读取使用 `private, no-store`，拒绝未声明和重复参数。

| 方法与路径 | 形状及作用 |
| --- | --- |
| `GET /channels/{channel_id}/messages/{message_id}` | 按 ID 读取该 Channel 的一条消息，复用现有 Message DTO。服务端重查 Channel、Message 和可选 Thread 的读取权，路径中的 Channel 必须与 Message 一致。 |
| `POST /channels/{channel_id}/messages/{message_id}/tickets` | 写入包含 `title`，沿现有 Ticket 标题校验和归一化。首次 201，精确重试 200。返回 Ticket 摘要和 `source_message` 结构化引用，另附跳转所需的 Channel 与可空 Thread 引用。 |
| `GET /channels/{channel_id}/messages/{message_id}/tickets` | 分页查该 Message 直接创建的工单。返回 `items`、`next_cursor` 和 `capabilities.can_create`。Channel 和 Thread 页面在用户打开该入口时读取。 |
| `GET /threads/{thread_id}/messages` | 分页返回该 Thread 的回复，复用 Channel Message 的 DTO、`before` 与顺序语义，游标额外绑定 Thread。来源消息继续从 Thread 的来源引用读取。发送沿既有 `POST /channels/{channel_id}/messages`。 |
| `GET /tickets/{ticket_id}/discussions` | 分页返回 active 讨论关联，含 EntityLink ID、Thread 安全摘要、来源 Channel 引用和当前 `can_unlink`。 |
| `GET /threads/{thread_id}/tickets` | 分页返回 active 讨论所关联的 Ticket 摘要，含 EntityLink ID、当前 `can_unlink` 和选择目标所需的 `capabilities.can_link`。 |
| `POST /tickets/{ticket_id}/discussions` | 写入包含 `thread_id`。首次 201，精确重试 200，返回原 EntityLink ref 和应用确认。同一对端点的不同请求竞争只产生一条 active 关系。 |
| `DELETE /tickets/{ticket_id}/discussion-links/{link_id}` | 解除精确的 active 关系。首次和精确重试均为 200，返回原解除确认。新关系使用独立 ID，迟到的旧解除不影响它。 |

- 关系分页使用稳定目标 ID，默认 25、最大 50。先过滤两端当前权限，再填充页和生成游标。游标绑定 Workspace、操作者、查询类型与端点。Ticket 摘要为 ref、title、status 和 Project ref。Thread 回复沿既有时间和 Message ID 顺序。均沿现有分页的非快照语义。
- 现有 Decision 创建 Ticket 的 v1 命令和响应继续使用 `source_decision`。Ticket Nexus View 支持单值 `include=execution_context`，组合使用 `include=due_date,execution_context`。原 `include=due_date` 仍独立有效。显式请求执行上下文时，来源按 Decision 或 Message 形状严格解码，新增讨论历史也通过此选项展示。Message 来源使用 `started-from`；来源可读时返回消息及导航引用，受限时返回通用占位。讨论列表走专用分页。
- Decision 来源的 Ticket 在省略执行上下文选项时沿旧详情响应。旧列表仍按原权限返回工单行。旧详情请求若命中当前可读的 Message 来源工单，返回 `409 conflict` 与升级客户端的提示。新 Go 与 Web 随同部署，直接工单链接由配套 Web 使用新选项读取。
- Web 在切换 Workspace、Ticket、Thread 或重新核对权限时清除旧结果并取消读取，忽略迟到响应。写响应未知时保留原操作 ID 和原输入，登录失效或失去读取权时清空页面命令状态。
- 从消息创建工单使用独立的 `ticket.create-from-message` receipt，以 Workspace、操作者、来源 Message 和操作 ID 定作用域。同一消息可以通过不同的显式操作发起多张工单。讨论关联和解除各用独立命令类型，分别绑定 Ticket 与 Thread 或精确 EntityLink，按 canonical payload digest 核验重试。相同 ID 更改输入、重复 active 关系或针对已解除关系的新解除请求返回 `409`。重试后 Web 重读当前关联和能力。
- 业务行、EntityLink、receipt、事件、Outbox 和 Activity 在同一事务提交。Decision 来源继续使用 `ticket.created` schema 1。Message 来源采用同事件 schema 2，payload 仅含 `status: open` 与 `message` ref。新增 `ticket.discussion-linked` / `ticket.discussion-unlinked` schema 1，payload 含 Thread 和 EntityLink ref。讨论事件以 Ticket 为主要对象，Activity 读取时重查 Thread 与 Channel，关联解除后仍按当前对象权限核验可见历史。
- migration 027 注册关系、约束新增来源与事件和 receipt 的一致性，并为 active 讨论和 Message 反向发现提供索引。Activity 升至 15，正常写入、导入及重建复用同一事件转换。合法旧记录原样保留，既有状态、分配、优先级和截止日期的更新及恢复沿现有合同。

## 上下文包、备份与兼容

- 默认导出 `project-collaboration-v8`，继续使用 `format_version: 1`。沿用 v7 的 Ticket 和 EntityLink 字段。按 Ticket 创建事件和来源关系确定来源类型，分别校验唯一 active `implements` 或 `started-from`、来源先于或等于 Ticket 创建时间、同 Project 和事件对应关系。Decision 来源继续按创建当时的 accepted 事实校验。Message 来源核对 Message、Channel、可选 Thread 的完整链和可见性。
- v8 保存可读的 active 与 removed 讨论关系及其完整建立、解除记录。校验两端、原始 actor 和时间、同对端点的 active 唯一性以及解除对应的关系代次。讨论事实按关系时间重放，原 Ticket 字段历史仍独立重放并核对最终值及 `updated_at`。讨论事件仅由同一组关系及其历史引用，便于按整组应用导出权限。
- 创建来源属于必需依赖。任一必需依赖不可读时，导出沿现有规则整体拒绝，对外隐去对象细节。额外执行讨论属于可选引用，导出者无法读取任一端时，同步排除该关系的 active 或 removed 记录、配对事件以及仅由它们引用的历史身份与映射。包中可读 Message 正文只出现在原 Message 对象。
- v1～v7 继续按各自原始字段、事件版本和关系白名单验包。合法旧 Ticket 可由原 `implements` 推导内部来源类型，保留源 manifest digest、既有历史和时间。旧 profile 明确拒绝 Message 来源与讨论关联的新事实。旧工件遇到 v8 整包拒绝。
- 导入沿既有空目标策略映射 Message、Thread、Ticket、关系、事件和历史作者。双向引用与历史来源在两轮导出导入后仍可解析。历史作者保持不可登录，目标权限来自目标实例当前授权。原命令 receipt 只随整库备份恢复，导入的历史沿既有规则保留原事实。
- 新 Go、Web、CLI、schema 与 Activity 作为同一批工件交付。026→027 沿单宿主停写升级流程。026 备份先用匹配工件恢复到独立空目标，再显式升级。027 整库恢复验证两种来源、讨论关系、原回执精确重试及 Activity 重建。 [上下文取回](../../deploy/context-package.md) · [升级恢复](0043-single-host-upgrade-failure-recovery.md)

## 取舍

创建工单时明确显示标题和 Project 可读范围。让来源消息和派生工单共用该范围，成员可以在收到工单后直接追溯来源。已有工单则可以关联 restricted 讨论，有权成员在原讨论继续工作。Channel、Thread 和 Message 承载正文与权限，EntityLink 保留引用和历史。

## 开发顺序与验收

| 顺序 | 交付内容与验证 |
| --- | --- |
| 1. 消息创建与来源 | 建立事件、来源不变量和写入 receipt。Go 与 Web 接通消息创建、直接来源反向分页和原消息定位。验证顶层消息和回复、旧 Decision 创建、不同与相同操作 ID、响应丢失、写入回滚及旧 HTTP 形状。 |
| 2. 关联与继续讨论 | 接通 Thread 工单选择器、双向分页、明确确认、精确解除、回复历史和既有发送入口。验证跨 Project 拒绝、归档、两端撤权清空、私密关系从列表及 Activity 隐去、同对端点竞争和迟到解除。 |
| 3. 迁移、恢复和整体验证 | 完成 v8、v1～v7 兼容、双轮导入、旧 schema 停写升级和独立空目标恢复。在隔离合成场景，从正常频道入口完成建单、原消息定位、继续讨论、解除与重连、重新登录后双向发现与恢复后回执核验。 |

范围包含数据库、权限、公共 HTTP 与包 profile。项目所有者于 2026-11-15 确认按表中顺序完成同一开发批次。工单关联具体构建沿 checkout 采集证据的后续顺序推进。真人独立观察、R08 和真人实测沿项目开发完成后的既定安排。 [交付链依赖](0037-ci-run-repository-checkout-provenance.md#首个实现切片与退出判据) · [当前执行顺序](../status/current.md#近期执行顺序)
