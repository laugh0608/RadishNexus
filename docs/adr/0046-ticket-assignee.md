# ADR-0046：Ticket 最小负责人

状态：已接受并实施；2026-10-28 所有者确认 D01～D07，验收范围与限制见[实施记录](../status/reviews/2026-10-28-ticket-assignee.md)

日期：2026-10-28

核对基线：`5d53350`，schema 020。承接[当前状态](../status/current.md)的执行管理接续；[ADR-0039](0039-ticket-status-transitions.md)已建立四状态，[ADR-0045](0045-membership-lifecycle-and-management-handoff.md)已建立成员生命周期与管理责任。本 ADR 只解决“这件工单由谁负责”，不改变成员管理角色或给予负责人额外权限。

## 背景与首片范围

成员已能从 Project 找回 Ticket、查看 Decision 来源、改变状态并继续 Document，但无法在正式页面明确执行责任。先补单一可选负责人，可以直接改善讨论到执行的闭环；优先级、筛选、看板、多负责人、工作量和通知继续独立切片。

本片在现有 Ticket 详情提供设置、改派、清空及“由我负责”动作，Project Ticket 列表显示同一负责人摘要。不新建任务中心、不增加独立人员目录，也不要求负责人重新复制 Decision 内容。负责人不必读取来源私密 Thread，仍只按 Ticket 的 governing Project 授权。

## 已确认决策

| 编号 | 推荐方案 | 影响与替代方案 |
| --- | --- | --- |
| D01 责任模型 | 一个 Ticket 最多一个负责人，允许未分配；旧工单与新建工单初始未分配，创建者不默认承担执行责任 | 多负责人或 Team 分配暂不进入；不从创建者、Decision decider、Project admin 自动推断 |
| D02 候选 | 同 Workspace 的当前有效普通身份，active 账户 / membership，当前 Project 为 contributor / decider / admin；排除导入历史身份与 viewer | 选择具备执行写权的人；不要求可读私密来源。Workspace owner 本身不构成候选资格 |
| D03 操作者与状态 | 当前有 Ticket 写权的 contributor / decider / admin 可以设置、改派或清空；仅 open / in-progress 接受新分配，done / canceled 保留结项责任，需先显式重新打开再修改 | 与现有状态写入权限一致，不增加“只有负责人能改状态”的新限制。替代方案为仅 admin 分配，但会增加维护者干预 |
| D04 暂停 / 退出 / 撤权 | 保留已记录负责人及历史；当前不符合 D02 时显示“当前不可执行”，允许其他有权成员改派 / 清空；不阻止安全暂停或成员退出 | 责任记录不授予权限。恢复成员或重新加入并显式重授项目写权后，可执行性重新计算，原责任记录仍在；不会重新授予任何权限。替代方案为失权时原子清空，需要扩展全部生命周期 / 角色撤销事务及逐工单事件，另有更大的影响范围 |
| D05 并发与重试 | 独立 assignment_revision；提交绑定所见负责人版本、状态版本及设置目标的当前成员 / 项目管理版本；使用既有协作 receipt 和原子事件 / Activity | A→B→A 也拒绝旧请求；网络结果未知时只精确重试原操作。目标版本变化必须重读，不能沿用旧确认 |
| D06 身份展示 | 只在当前可读 Ticket / Project 内展示负责人最小身份与可执行性；候选列表只给有分配能力者，稳定 ID 分页 | 不暴露邮箱、全局停用原因、其他 Workspace 或私密管理职责；Activity 保留稳定身份，展示名按当前允许的最小身份策略解析 |
| D07 导入与兼容 | 新 profile `project-collaboration-v2` 明确携带历史负责人和分配历史；目标不自动绑定可登录成员，显示“历史负责人，待重新分配”；保留 v1 读取并映射为未分配 | 不给 v1 偷添字段，不将历史作者激活，不用同名 / 同邮箱匹配；首次目标分配必须由当前有权成员明确选择。具体 v2 字段、升级与旧包兼容见下文和实施记录 |

D04 的推荐有意把责任记录与可执行资格分开：退出不会删除历史责任，重新加入也不会从工单获得旧权限。它仍会影响用户对“恢复后是否继续负责”的预期，所有者已确认采用保留责任记录的方案，恢复后的可执行性按当前权限重新计算。

## 数据与权限合同

Ticket 增加可空的 `assignee_user_id` 与从 1 开始的 `assignment_revision`。`status_revision` 仍只随状态动作变化，不能共用以免不相关写入互相覆盖。负责人外键绑定同 Workspace 的历史身份；可执行性按当前授权计算，不持久化一份易漂移的资格布尔值。

旧 Ticket 初始 `null / 1`，不生成虚构分配事件。正式分配不改变 Project role、Channel / Thread membership、Workspace owner、Deployment 授权、Decision 状态或 Ticket 状态；不新建 EntityLink 表示人员指派，人员责任属于 Ticket 自身字段。

读取区分：

- 未分配：负责人为空；
- 当前负责人：最小身份摘要和 `eligible`，只表明能否在当前 Project 执行；
- 历史导入负责人：不可登录的历史身份，`eligible=false`，明确提示需重新选择目标成员。

候选查询复用现有 Project / Workspace 授权逻辑，不直接给普通 contributor 开放管理目录。在 Ticket 作用域添加最小候选入口，返回 ID、展示名、分页 cursor 和用于提交的版本材料；无权与不存在继续同形。不得通过候选总数、过滤原因或错误明细泄漏不可读对象。

归档 Project 只读，不接受分配。当前负责人变化不会成为读取准入条件；未分配也不阻止当前四状态流转。本片不引入自动分配、接单审批、抢单队列或全局“我的待办”。

## 写入、历史与失败边界

复用既有 Ticket 短请求与协作 receipt，新增 `ticket.assignee.set` 命令（显式 null 表示清空），不新建通用执行工作流。精确字段、端点与 receipt 约束在实现前随 D01～D07 一起冻结。

新操作必须按统一顺序锁定并验证：操作者 Session / Workspace 准入、Project 当前写权、Ticket 当前状态与两个版本、目标候选资格及所见版本。目标在读取候选后暂停、退出、降为 viewer 或删除再重建 Project role 时，旧确认均不可提交。空目标清空无需查询被移除成员的私密信息。

锁顺序必须复用 ADR-0045 与现有状态事务；同时覆盖“分配先发生，随后停权后显示不可执行”与“停权先发生，旧分配拒绝”。不能为了避免死锁跳过目标成员当前权限或版本复核。

真实变化同事务更新 Ticket、assignment_revision、更新时间、不可变 `ticket.assignee-changed` 事件、receipt、Outbox 与 Activity；事件记录前后稳定身份及相邻分配版本，不复制邮箱、姓名、正文或授权快照。只投影到当前 Ticket，不广播给不可读来源或自动发通知。

同一负责人重复设置、未分配时再次清空返回冲突，不伪造变化。精确重试先复核操作者当前准入及 Ticket 写权，再返回原 applied 事实；不因目前 Ticket 已完成或目标已经暂停而执行第二次分配，随后单独读取实际当前状态。失去当前权限仍拒绝，不通过 receipt 恢复权限。

Web 在结果未知时保留原 operation 与 payload；409 后清理旧确认并重读，不自动覆盖其他人的分配。撤权、Workspace 切换、页面恢复及迟到响应保护复用现有 Ticket 生命周期；“由我负责”与普通选择使用同一服务端校验。

## 导出、导入与升级影响

D07 是与本次 ADR-0044 补验直接相关的实施前置条件。现有 v1 的 Ticket DTO 与历史事件白名单无法表达负责人，不可直接输出新字段或丢弃新事件后继续声称无损导出。

v2 显式加入 Ticket 历史负责人、assignment_revision 与分配事件；历史负责人也进入包内作者闭包，遵守原有无凭据 / 无授权规则。目标保留其不可登录历史身份与来源，不转交给导入负责人；后续正式重新分配创建新的目标原生事件。v1 的旧 Ticket 明确转换为未分配 / 1，不能凭作者字段猜测负责人。

新服务读取 v1 / v2，旧服务拒绝 v2；新导出默认 v2，不提供静默降级。具体 v2 DTO、事件白名单、历史连续性与跨两次导入映射已有契约测试。本轮按已确认方向同步实现 profile、migration 与正式 API。

实施采用新的 forward-only migration、Activity 投影版本、Go / Web 配套工件及完整备份分类；不修改已发布 migration，不假定滚动兼容。旧工件恢复仍回到匹配旧 schema 的空目标，再显式升级。负责人数据与历史进入权威备份，恢复后可执行资格仍以恢复实例的当前身份和权限为准。

## 验收与退出条件

| 范围 | 必须验证 |
| --- | --- |
| 正式主链 | Message → Thread → Decision → Ticket → 选择负责人 → 开始 / 完成 → 重新打开 / 改派；列表与详情一致 |
| 候选与权限 | 普通 contributor 可发现候选；viewer、无 Project role 的 owner、跨 Workspace、暂停 / 退出 / disabled / 历史身份不可选；私密来源不扩权 |
| 生命周期 | 暂停、恢复、正式退出、重新加入、项目降权与再授权；当前资格变化、原责任与历史符合 D04，原 Session / 旧确认不复活 |
| 并发与恢复 | 双标签改派、状态同时完成、目标停权竞态、A→B→A、精确重试与 COMMIT 确认丢失；事件 / receipt / Activity 故障全部回滚 |
| 历史与迁移 | 未分配、设置、改派、清空、历史身份；v1 / v2 包、二次导入、后续原生分配、投影重建、升级与旧工件空目标恢复 |
| 正式 Web | 桌面 / 手机 / 键盘、分页、空候选、长名、冲突、网络未知、刷新 / 后退 / Workspace 切换与迟到响应 |

退出结论限定为“当前有权成员能明确执行责任，人员变化不授予权限，分配历史与迁移保持可追溯”。不表示完整执行管理、通知系统、真实团队持续使用或 M1 退出。

## 替代方案与确认边界

- 只让本人认领：写入较简单，但不能完成团队任务分配；保留为产品选择，不与推荐方案混用。
- 负责人失权时自动清空：状态更直接，但须修改生命周期和撤权事务、处理隐藏工单的批量历史与失败回滚；本 ADR 采用保留记录并明确不可执行。
- 暂不处理导出兼容：会让已有数据退出路径失效，不采用。
- 一次建立看板、优先级、筛选和通知：会横向扩大范围，当前不采用。

2026-10-28 所有者明确要求“提交工作区更改，确认，继续推进下一步”。既有补验与提案已提交为 `211f050`；D01～D07 已获确认并授权实施。新增运行资源仍须说明本轮范围，提交 / push / 真实实例迁移不从实施授权自动推导。

## 本轮接口与格式细化

- Ticket 责任读取与写入使用 `GET / POST /api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/assignee`；候选使用同路径下 `/candidates`，分页参数沿用 `limit / after`，cursor 绑定 Workspace、操作者和 Ticket，按用户稳定 ID 排序。
- 读取返回 `assignee`（null 或 `id / display_name / eligible / imported`）、`assignment_revision`、`status_revision`、`can_assign`。Project 工单列表增加同一 `assignee` 摘要，既有 Ticket 创建与状态响应保持原职责。
- 写入必填 `client_operation_id / base_assignment_revision / base_status_revision / assignee_user_id / target_lifecycle_revision / project_management_revision / confirmed`。清空时 `assignee_user_id=null`、`target_lifecycle_revision="0"`；其余版本由读取 / 候选页提供。membership 与 Project bigint 版本采用十进制字符串。原操作精确重试不重新校验目标资格，但仍复核操作者当前 Ticket 写权；新动作必须重查全部目标版本。
- 事件 `ticket.assignee-changed` schema 1 的 payload 为 `assignment: {from_user_id, to_user_id, from_revision, to_revision}`，两个人员字段为可空稳定 ID；空值表示未分配。旧创建事件不补写分配历史。
- 新 profile `project-collaboration-v2` 保持 `format_version: 1`，Ticket 增加必填 `assignee_user_id`（可空）和 `assignment_revision`。v1 必须严格使用旧三字段形状，在校验后的内部数据中转换为 null / 1；v1 不接受分配事件或额外新字段。v2 校验从 null / 1 开始的连续分配历史，最终负责人 / 版本与 Ticket 一致，updated_at 等于状态与分配两类历史的最后时间。
- 分配历史涉及的前后身份均进入作者闭包，跨实例逐项重映射；在目标内仍为 disabled 历史身份。计划 digest 继续绑定原包 manifest；不将转换后的内部形状冒充原包摘要。
