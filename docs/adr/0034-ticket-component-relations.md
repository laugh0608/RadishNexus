# ADR-0034：Ticket 与 Component 的人工关联

状态：已接受

日期：2026-10-06

关联：[ADR-0019](0019-session-scoped-thread-decision-ticket-transport.md)、[ADR-0022](0022-transactional-activity-and-incoming-relations.md)、[ADR-0028](0028-minimal-markdown-document.md)、[ADR-0033](0033-repository-mapping-and-component-relations.md)。所有者已确认本提案并授权实施；完成状态与验证证据见当前状态。

## 背景与目标

基线 `f878090` 已接通 Repository 映射与 Component 关联。[Golden Path](../golden-path.md) 第 5 步仍缺少普通成员把 Ticket 关联到软件组件的入口；已有 Ticket → Document、Component → Repository 与 CI Run 所属 Component，不能自动推导 Ticket 已被某次构建或部署实现。

本切片补齐人工确认的 `Ticket --affects--> Component`，让成员从执行事项进入组件，并从组件反查当前有权读取的事项。它表达“该事项涉及此组件”，不证明代码已提交、测试已通过、构建已包含变更或部署已完成。

源码核对发现：Ticket Nexus View 的来源合同要求唯一 `implements` Decision，Timeline 当前只接受 `ticket.created`；Web 对关系类型、时间线事件及跳转目标也有严格白名单。不能只插入 EntityLink 后放宽未知类型检查，必须逐项扩展合同并保留原来源约束。

## 决定范围

| 项目 | 决定 |
| --- | --- |
| 关系 | 同 Workspace、多对多 `ticket --affects--> component`；不向 Ticket 添加 Component ID 数组 |
| 写权限 | Ticket 所属 active Project 的 contributor / decider / admin；同时要求当前账户、Workspace membership 与两端可读 |
| 读取 | Ticket 沿现有 Project 权限；Component 继续 Workspace 成员共享读取，反向 Ticket 必须逐项授权 |
| 修正 | 显式解除精确 link ID；重新关联创建新记录，旧 receipt 不改写当前关系 |
| 公共入口 | Ticket 关联区、组件详情反向列表、稳定组件详情地址；复用现有配置内容与工作台 |
| 存储与事件 | migration 014 扩展关系、协作 receipt 与索引；两类关系事件、Activity v5；不新增依赖 |

本批不做 Ticket 看板、状态流转、指派、通用关系编辑器、Component 私密权限或生命周期管理，不做 Ticket ↔ CI Run / Deployment、Repository commit 来源、Jenkins 持续采集、跨对象 Timeline 聚合。持久终态采集继续独立设计。

## 领域语义与权限

- 同一 Ticket 可涉及多个 Component，同一 Component 可关联不同 Project 的多个 Ticket。Component 不因此成为 Project 的子对象，也不获得 Project membership。
- Workspace owner、Component owner Team 和 Repository 管理资格均不替代 Ticket 的 Project 写权限。viewer 与仅有 Workspace 默认读取权限的用户只读；插件不进入本批 Session 命令。
- 新关联允许 `planned / active / deprecated` Component，拒绝 `retired`；这覆盖计划和维护事项。解除允许任意 Component lifecycle。Project 必须 active 才能新增或解除，归档后保留只读历史；不另开 owner 绕过归档的清理权。
- Ticket 当前正式状态仅 `open`，本切片不扩展状态枚举。关联不修改 Ticket 的 Current 字段、来源 Decision 或内容更新时间，关系变化由独立事件表达。
- 关联只要求 Ticket 与 Component 当前可读，不重新要求 Ticket 上游 restricted Thread 可读；沿既有 Ticket 权限，不让引用成为穿透上游证据的方式。
- 直接访问不可发现的 Workspace、Ticket、Component 或关系返回 404；对象可读但 Project 无写权限或已归档返回 403；Component retired 新关联、重复 active 关系或幂等输入冲突返回 409。错误不泄漏另一端标题或内部状态。

## EntityLink、精确解除与幂等

新增唯一方向 `ticket / affects / component`。关系采用 `assertion=asserted`、`origin=user`、当前用户与服务器时间，`origin_ref=null`、`metadata={}`。直接命令与自身事件同事务，`source_event_id=null`；事件携带精确 link ID 和另一端引用，不伪造先前来源事件。

partial unique 保证同 Workspace 同对至多一条 active；建立反向 active 索引。解除核对 link ID 的 Workspace、Ticket、Component 和方向，再写 active → removed、当前移除者与固定原因 `ticket-component-unlinked`。禁止删除、修改创建来源或复活 removed；再次关联生成新 `lnk_` ID。已有来源 Decision 和 Document 关系不受影响。

新增 `ticket.component.link` / `ticket.component.unlink`，复用 `collaboration_command_receipts`：target 固定为路径 Ticket；result 为精确 `entity-link`，`result_revision=null`；解除的 link ID 进入 canonical digest，关联的 Component ID 进入 digest，均包含 `confirmed=true`。沿用 actor / Workspace / command / target / operation 作用域，不改旧命令摘要。

首次关联返回 201、原请求重试返回 200；解除首次与重试均为 200。响应为 `data:{link_id,applied:true}`，表示原请求已处理，不表示关系当前仍 active。相同 operation 改 payload 为 409；不同 operation 遇到已有 active 关系或已移除关系为 409，不记录无变化成功。旧关联重试不得复活关系，旧解除重试不得移除重新建立的新关系。

receipt 返回前重新检查当前账户、membership、Project 写权限、两端可读性及该命令的生命周期限制。新关联和关联重试均拒绝 retired Component；解除重试允许该组件退休。禁止返回旧 receipt 绕过撤权或归档。

## 事务与来源记录

关系、receipt、领域事件、Outbox 和正常 Activity 同一事务提交；任一位置失败整单回滚。使用已有协作命令的 receipt、事件与不可变 EntityLink provenance 保留操作来源，不把普通 Project 协作动作塞进 Workspace 配置 Audit，也不宣称 Activity 取代安全审计。

写入顺序拟为：账户 → Workspace membership → governing Project → Project role → Ticket → Component → EntityLink → receipt / 事件 / 投影。先只读定位不可变 governing Project 和解除目标，再按顺序锁定并复核。Ticket 不发生内容更新，避免无意义排他锁；Component 锁串行化同组件关联及生命周期冲突，精确解除锁定旧关系。实施时核对现有 Ticket 查询、Project 成员撤权、Repository 配置和 Activity 重建的锁序，禁止用无限重试掩盖死锁。

新增 schema_version=1 事件 `ticket.component-linked` / `ticket.component-unlinked`：primary entity 为 Ticket，governing Project 为 Ticket 的 Project；payload 仅 `{component: EntityRef,link_id,state}`，state 为 active / removed；source 为 web，actor 为当前用户，correlation 为服务端 request ID。不复制 Ticket / Component 标题、Project 私密信息或 operation ID。

Activity v5 正常更新与重建共用映射，只投影关系状态与受控 Component subject。事件只出现在 Ticket 自身 Timeline，不把私密 Ticket 活动投放到 Workspace 共享的 Component 时间线。新 Timeline 项采用关系状态字段 `relation_state`，不得借用 Ticket `status=open` 或改变旧事件 DTO；旧三类协作事件仍保持原字段，Web 用事件类型区分解析。

## 读取、分页与公共接口

Session API 前缀为 `/api/v1/workspaces/{workspace_id}`，复用同源、CSRF、严格 JSON、no-store 与当前身份边界。写请求上限 8 KiB，`client_operation_id` 沿用 1..128 bytes printable ASCII；必须显式 `confirmed=true`，拒绝未知字段，不接收自由文本备注。

| 方法与后缀 | 用途与输入 |
| --- | --- |
| `GET /tickets/{ticket_id}/components` | active 关联分页与 Ticket 当前 `can_link` 能力；每项包含 link ID、Component 安全摘要和 `can_unlink` |
| `POST /tickets/{ticket_id}/components` | `client_operation_id,component_id,confirmed`，显式确认两端 |
| `DELETE /tickets/{ticket_id}/component-links/{link_id}` | `client_operation_id,confirmed`，精确解除 |
| `GET /components/{component_id}/tickets` | 只返回当前可读 Ticket 的 active 反向关联，含 link ID 与可否解除 |

Component 候选继续复用既有 `GET /components`，显示 name / key / lifecycle，retired 不可选；不再建候选对象目录。Component 摘要沿已有安全 DTO；Ticket 摘要仅 ref、title、status、Project ref，不能携带上游 Thread、Decision 或 Project 标题快照。能力只供 UI 展示，提交时重新授权。

两个关联列表采用 `{items,next_cursor}`；Ticket 侧额外有 `capabilities:{can_link}`，每项的 `can_unlink` 均来自 Ticket 当前 Project 权限。默认 25、最大 50，按另一端稳定 ID 升序，游标绑定 Workspace / 当前端点 / 列表类型，拒绝跨作用域和未知字段；不返回总数。反向扫描先授权再占用页配额，继续扫描直到得到一页可读目标或结束；next cursor 只能来自已返回对象，不能编码不可读 Ticket。跨请求不承诺数据库快照。

核心 Relations 的精确白名单增加 Ticket 出向 affects 与 Component 入向 affects。原 Ticket 来源分离器排除新关联后，仍验证恰好一条原 `implements` 来源；不改旧 restricted evidence 合同。核心 Nexus Relations 保持现有全量语义，不暗中截断；专用列表提供分页和精确解除 ID，二者读取同一关系事实。

不可读反向 Ticket 完全隐藏，不返回占位、ID、标题、数量或时间；Ticket 出向不可读 Component 沿既有纯 restricted 占位规则，专用关联列表则不提供该目标或管理入口。直接路径、列表、核心 Relations 与 Timeline 必须一致复用当前权限。任何读不能依赖浏览器角色或旧 receipt。

## Web 与范围切换

Ticket 页面增加“涉及组件”区，从已有分页 Component 目录选择并明确确认。关联成功刷新关系和 Ticket Timeline；解除提示仅解除该关系、保留两端与历史。保留 loading、empty、error、冲突、双击防护和输入校验，未知写结果冻结原 payload / operation，仅显式重试。

增加可直接访问的组件详情页 `/workspaces/{workspace_id}/components/{component_id}`，复用已存在的 Component 配置详情和 Repository 关联内容，并加入可读 Ticket 分页列表；不建立完整 Component Nexus View。首页组件入口与 Ticket 关联都使用这一地址，避免依赖上一个页面的内存选择状态。组件页返回的 Ticket 使用已有 canonical Ticket 路径。

失去当前端点权限时清空详情、关系与操作表单；仅失去写权限时保留可读内容并关闭管理动作。Workspace / 对象切换丢弃迟到响应，未知请求也不带入新作用域。列表刷新不得把待确认的旧 link ID 改为重连的新 ID。桌面、手机、键盘及浏览器后退 / 重新进入均纳入验收。

## 迁移、恢复与验证

migration 014 只新增该关系类型、来源约束、active 唯一 / 反向索引，扩展协作 receipt 白名单、target / result / revision 约束和精确结果校验，保留旧命令全部组合。来源校验限制 result 指向同 Workspace、同 Ticket 与命令的确切 EntityLink；旧证据不改写。Activity 从 v4 升至 v5，既有投影仅推进版本，不补造关系或事件。

没有新权威业务表；EntityLink、receipt、事件与 Outbox 已在备份范围，仍须验证恢复后 active / removed 历史、重新关联与旧请求重放。014 升级、全新安装、只读 readiness、旧 013 二进制拒绝新 schema 以及新旧投影一致性分别验收。Go / Web / schema 配套交付；回退使用升级前备份恢复到全新目标，不删除新关系伪造兼容。

验收矩阵：

- 正式 Message → Thread → Decision → Ticket、正式 Component 创建，再通过 HTTP / Web 关联；不靠 SQL 生成被验收的 Ticket / Component / EntityLink。
- contributor / decider / admin、viewer、无 Project 角色 owner、跨 Workspace、restricted / workspace Project；关联、反向列表与 Timeline 不扩大权限。
- Project 归档、成员撤权、账户禁用与写入的两种串行结果；撤权后新请求及原 receipt 重试均重新授权。
- 多对多、不同 Project 的 Ticket 混排分页、大段隐藏 Ticket 不形成计数 / cursor 泄漏，关系移除 / 重连后双向刷新。
- 双 actor 竞争、精确重试、changed payload、已 removed 精确解除、未知响应恢复；receipt / 关系 / 事件 / Outbox / 投影故障整体回滚。
- 旧 Ticket Source Decision、Document Relations、旧 Timeline DTO 不回归；正常投影、重建、014 升级和备份恢复一致。
- 桌面 / 手机、键盘确认、稳定地址、浏览器后退、失权清空和迟到响应丢弃。真实 Jenkins 与原生中文 IME 不纳入本切片结论。

## 替代方案与后果

- 只给 Ticket 加 `component_id`：无法表达多个组件、解除历史或独立来源，未采用。
- 复用 Repository 的 Workspace owner 配置权：普通 Project contributor 无法完成自己的执行上下文，且 owner 会越过 Project 边界，未采用。
- 直接做 Ticket ↔ CI Run / Deployment：需要确认具体代码和交付来源，Component 共属不能证明 Ticket 已交付，留到独立合同。
- 一次开放通用 EntityLink API、完整 Component 门户或持续采集 worker：扩大权限和运维范围，本次不采用。

代价是新增两类业务命令、专用分页与组件稳定页面，以及旧 Ticket DTO 的明确扩展。收益是补上执行事项与软件资产之间可逆、有来源、遵循 Project 权限的实际入口。

确认后实施 Go、PostgreSQL、HTTP、React、测试与对应真相源；本提案不授权业务迁移、依赖安装、长期服务、提交、push 或外部系统操作。隔离数据库和 HTTPS 验收沿当前任务已有授权及实际范围执行，范围变化时另行说明。
