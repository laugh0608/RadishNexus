# ADR-0053：Ticket 最小截止日期

状态：已接受

日期：2026-11-15

## 背景

Project 工单列表、状态看板和「我的工单」已经能找到待处理事项并进入详情。截止日期让成员在这些入口直接看到约定的完成日期，并能追溯何时设置、修改和清除。此项属于 M4。 [产品路线图](../roadmap.md#m4工单与上下文关联) · [Ticket 领域](../domain-model.md#ticket)

现有负责人、状态和优先级都有独立版本。优先级已实现正式 Session 写入、事务内历史和同一次请求重试，适合作为写入边界。Project 工单列表和看板共用同一发现接口与工单行，个人工单复用同一套当前权限语义。三个既有读取形状，包括 Ticket Timeline，均有严格 Web 解码。Project 上下文包 v6 也按 profile 严格检查对象字段、事件和终态。 [优先级合同](0047-ticket-priority-and-filtering.md) · [我的工单合同](0052-workspace-my-tickets.md) · [Project 工单读取](../../web/src/workspace/project-tickets-api.ts) · [个人工单读取](../../web/src/workspace/my-tickets-api.ts) · [上下文包校验](../../server/internal/contextpackage/files.go)

## 决策范围

| 项目 | 方案 |
| --- | --- |
| 日期语义 | 可选的日历日期，使用 PostgreSQL `DATE` 和 API `YYYY-MM-DD` 字符串或 `null`。范围为 `0001-01-01` 至 `9999-12-31` 的有效公历日期。纯日期按同一个年月日显示，跨浏览器或服务器时区保持一致。 |
| 用户入口 | 在 Ticket 详情设置、修改和清除。Project 列表、四状态看板及「我的工单」展示日期或“未设置”。已完成、已取消的工单保留终态日期，重新打开后才可修改。归档 Project 保持只读。 |
| 权限 | 读取沿 governing Project 的当前 Ticket 权限。写入沿优先级动作边界，要求 active Workspace、active Project 及 contributor、decider 或 admin 角色，且 Ticket 处于 `open` 或 `in-progress`。负责人和 Workspace owner 仍按各自实际 Project 权限操作。 |
| 首片展示 | 保留现有列表排序、筛选、页码及个人工单选集。首片只表达已约定日期。逾期判断、时区截止时点、提醒、Attention Item、日期筛选与排序分别留作后续需求。 |

## 存储和写入

- 在 Ticket 增加可空 `due_date` 与独立 `due_date_revision`。新增 migration 026 将旧行及新建 Ticket 初始化为 `NULL / 1`，保留旧 `updated_at`、状态、责任、优先级及既有历史。
- 详情使用 `GET / POST /api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/due-date`，两者均拒绝查询参数。GET 返回当前 `due_date`、`due_date_revision`、`status_revision` 和 `can_set_due_date`。POST 严格接收 `client_operation_id`、`base_due_date_revision`、`base_status_revision`、`due_date` 和 `confirmed: true`。显式 `null` 表示清除，省略、空字符串、时间戳和非法日期返回 400。
- POST 复用 Session、Origin、CSRF、准入事务和 Ticket 行锁。首次提交比较所见日期及状态版本，同值、版本冲突、达到版本上限、终态及归档沿既有动作错误语义处理。负责人和优先级变化保持各自版本。完成或取消与日期修改按 Ticket 锁串行。
- 同一事务更新日期、版本和单调递增的 `updated_at`，写 `ticket.due-date.set` receipt、`ticket.due-date-changed` 事件、Outbox 与 Activity。事件包含前后日期及相邻版本，清除保留显式空值。首次成功和精确重试返回 HTTP 200，`data` 包含 `ticket` ref、`applied: true`、`applied_due_date` 和 `applied_due_date_revision`。精确重试复核当前权限并读回原结果，后续日期变化或终态流转保留原操作事实。界面随后重读当前值。相同操作 ID 更换内容返回冲突。 [既有优先级写入](../../server/internal/goldenpath/postgres/ticket_priority.go)

## 读取和 Activity 兼容

- 为 Project 工单与「我的工单」的现有 GET 增加单一可选参数 `include=due_date`。明确请求时，每个 Ticket 行都返回 `due_date`，空值为显式 `null`。默认请求沿现有精确响应形状。参数只影响展示字段，日期由现有分页查询同批读取，沿用现有权限和游标作用域。
- Ticket `nexus-view` 的 GET 支持同一显式参数。请求时，Ticket Timeline 展示截止日期变更，沿当前权限投影可公开的前后日期与版本。省略参数时返回原 Timeline 事件集合。其余对象的 Nexus View 继续各自的查询合同。
- 新 Web 在这三处显式请求并严格解析新形状。旧 Web 的默认请求继续兼容。新 Web 与对应服务端工件配套发布。使用本项目既有的 `private, no-store` 和 Session 作用域。本 ADR 扩展 ADR-0019 的 Ticket 查询选项、ADR-0047 的 Project 工单读取及 ADR-0052 的个人工单读取，范围限上述显式参数。 [协作 Transport](0019-session-scoped-thread-decision-ticket-transport.md) · [发现 Transport](../../server/internal/platform/httptransport/discovery.go)
- 现有 Project 列表和看板共用工单行，日期只需一处行展示。详情沿现有确认、未知结果重试、失权清空和请求取消模式；在 Web 层以日期字符串保留年月日。 [项目工单视图](../../web/src/workspace/ProjectTicketsPage.tsx)

## 迁移、历史和恢复

- Activity 投影升至 14。已有投影随迁移保持可读，正常写入、导入和重建共用事件转换。新事件限 Ticket；普通读取按上述默认／显式形状输出。备份保存权威 Ticket、事件和 receipt，恢复时重建 Activity。 [Activity 重建](../../server/internal/goldenpath/postgres/activity.go) · [备份恢复契约](0010-verified-postgresql-backup-and-restore.md)
- 默认导出 `project-collaboration-v7`，保留 `format_version: 1`。v7 保存当前日期、独立版本和变更历史。按事件发生次序从空值 / 1 重放，核对前后日期、相邻版本、事件时的工单状态、最终字段和更新时间。保留导入来源及历史身份，导入后由当前有权成员继续原生写入。
- v1～v6 先按各自原字段、事件白名单和 manifest 校验，再归一化为无截止日期 / 1。v6 继续保留替代和复核能力，v7 另增截止日期。旧工件继续按旧 profile 拒绝未知包。新 CLI 与服务端配套交付，源 receipt 仍仅随整库备份恢复。 [上下文包格式及兼容](../../deploy/context-package.md)
- 按现有单宿主停机升级流程保留匹配的旧工件及预升级备份，验证 025 → 026 和独立空目标恢复。旧候选的 025 恢复记录用于其原工件组合，新切片用匹配的 schema / Activity / CLI 取得恢复及精确重试证据。 [单宿主恢复](0043-single-host-upgrade-failure-recovery.md)

## 取舍

日历日期能表达“某天完成”的首片需求，同时避免在尚无工作区时区合同的情况下解释逾期。精确截止时刻需要单独确定时区、日界和提醒语义。通过显式读取参数在现有列表和 Timeline 传送新形状，可以保留旧客户端精确解码，并避免对工单逐条查询。持久日期和变更历史随上下文包一起交付，保证取回后的项目仍能解释日期。

## 验证和确认

| 验收面 | 通过条件 |
| --- | --- |
| 正式入口与日期 | 在合成场景中从已有工单入口设置、修改及清除，检查详情、Project 列表、看板、个人工单和 Timeline。跨时区显示相同日期，验证闰日、非法输入、终态只读和重开。 |
| 权限、并发与回滚 | 验证无权拒绝和撤权清空、归档及终态规则、同版本竞争、状态往返、独立字段变动、精确重试。检查事务失败时日期、事件、receipt、Outbox 和投影一致回滚。 |
| HTTP 兼容 | 验证默认与显式形状、`null` 的准确含义、未知和重复参数、严格解码，以及既有筛选、稳定排序、游标权限和请求取消。 |
| 迁移、备份和取回 | 旧行保持原状态时间及历史。实际 PostgreSQL 验证新增 schema、Activity 重建及空目标恢复，核对日期与原请求回执。v1～v6 验证各自原合同，v7 验证损坏历史拒绝及两轮往返。 |

项目所有者于 2026-11-15 确认数据库、Session v1 读取与写入合同及上下文包 profile 的上述变更范围。
