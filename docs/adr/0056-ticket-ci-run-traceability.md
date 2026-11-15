# ADR-0056：工单与构建的可追溯关联

状态：已接受

日期：2026-11-15

相关合同为 [Ticket 与 Component](0034-ticket-component-relations.md)、[CI Run 读取](0007-component-scoped-ci-run-read.md)、[checkout 来源](0037-ci-run-repository-checkout-provenance.md)、[staging 记录](0031-session-scoped-staging-deployment-recording.md) 和 [单 Project 上下文包](0044-portable-project-context-package.md)。

## 背景

成员已经可以从消息或 Decision 创建工单，追溯来源、管理执行讨论并关联涉及的 Component。接收侧也已有可读的 Jenkins CI Run、受控 checkout 来源与外部 staging 终态记录。M4 还缺少工单与某次具体运行之间可由人核验的关系，以及在 Web 中沿这条关系追踪已有部署记录的入口。 [M4 路线图](../roadmap.md#m4工单与上下文关联) · [Golden Path](../golden-path.md)

同一个 Component 下可能有多个 Project 的事项，也可能每天产生多次运行。Ticket 的 `affects`、CI Run 所属 Component、Repository 的 `built-from` 和 Deployment 的 `deploys` 分别保存独立事实。本批增加单独的人工关联，准确记录操作者所选的那一次运行。

## 决定

| 事项 | 范围 |
| --- | --- |
| 正向关系 | 同 Workspace 的多对多 `ticket --tracks-build--> ci-run`。人工确认的含义是跟踪此工单时可参考这次运行。保留已接收的 Jenkins 成功、失败和取消终态，覆盖验证通过、失败诊断和取消调查。 |
| 关联资格 | 当前账户和 Workspace membership 有效，Ticket 所属 Project 为 active，操作者有 contributor、decider 或 admin 角色，且两端当前可读。适用于四种 Ticket 状态。已有 Component 关联可用于起始筛选，由用户明确选择构建。 |
| 解除资格 | 使用同一读写边界，指定当前 Ticket 下的精确 EntityLink ID。归档 Project 继续提供只读关联和历史。 |
| 读路径 | Ticket → CI Run，CI Run → 当前可读的 Ticket，CI Run → 已记录且当前可读的 staging Deployment。Deployment 详情沿原运行引用回到 CI Run。每一跳复用原对象授权。 |
| 证据 | Ticket 关联、Jenkins 构建终态、checkout 观察和外部 staging 终态各有独立来源与时间。工单状态与部署操作继续使用各自的显式命令。 |

### 权限与历史

- Ticket 读取以当前 governing Project 为准，也适用于 restricted Project。CI Run 读取复用所属 Component 的权限。Deployment 读取同时验证原 CI Run 与 Environment。所有入口使用当前身份和对象权限，旧 receipt 用于核验原操作，游标用于确认分页边界。Workspace owner 与 Jenkins source 沿各自的现有授权规则。记录部署需要单独取得目标 Environment 的显式授权。
- 每页先授权入口对象，再在同一数据库读取快照中逐项应用另一端的当前权限和筛选，最后占用页容量。直接读取缺失或不可发现的对象及关系返回相同的 404。Project 可读而缺少写权或已归档时返回 403。新操作遇到非终态运行、已有 active 关系或已移除的解除目标时返回 409。同一操作标识提交不同输入也返回 409。
- 当前关系和历史都对不可读另一端完全隐藏，连同占位、ref、标题、操作时间、计数和分页边界一起隐藏。CI Run 页的反向列表逐 Ticket 检查它所属的 Project。Ticket 页显示历史时，已解除的记录仍按两端当前权限读取。
- Ticket 详情附加独立的构建关联区域和可分页历史，CI Run 页附加关联工单与部署记录区域。保留既有 Nexus View 的严格 JSON 形状及 Ticket 两种创建来源。通用 Relations、默认 Timeline、搜索及通知继续使用各自现有入口，本批的新历史经专用读取公开。

### 写入、回执与恢复数据

- 复用 EntityLink。保存 `assertion=asserted`、`origin=user`、操作者与服务器时间。同一对至多一条 active 记录，使用数据库唯一约束和反向索引。解除写入 active → removed、操作者、时间和固定原因 `ticket-ci-run-unlinked`。同一对重新关联生成新的 `lnk_` ID。保留原记录与关联代次。
- 新增 `ticket.ci-run.link` 和 `ticket.ci-run.unlink`，回执目标是 Ticket，结果是精确关系 ID。提交 `confirmed:true` 和 `client_operation_id`。关联摘要纳入运行引用，解除摘要纳入精确 link ID。精确重试重新验证当前读写资格，再以原事件、关系代次和回执核验原成功事实。首次关联 201，关联重试及解除 200。已移除的关系仍可用于核验原请求。
- 事务按现有准入规则锁定账户、Workspace membership、Ticket 的 Project 与角色，再锁定 Ticket、CI Run 及其读取所需的 Component，最后锁定待解除的关系。并发的构建关联写入以同一 Ticket 串行决议，验证与既有权限、Jenkins 写入及 Activity 重建的取锁顺序。关系、receipt、事件、Outbox 与 Activity 同事务提交。
- schema 1 事件为 `ticket.ci-run-linked` 和 `ticket.ci-run-unlinked`。主要对象是 Ticket，事实包含精确关系引用、CI Run 引用及关系状态。以既有事件信封留存 Project、角色已验证的 Session 操作者、请求来源及时间。Activity 升至 17，正常写入与恢复后的重建共用投影器。关系动作保留 Ticket 当前字段和更新时间，Jenkins、checkout 与 Deployment 的事件单独保存。

### HTTP 与 Web

以下后缀使用 `/api/v1/workspaces/{workspace_id}`。复用 Session、Origin、CSRF、严格 JSON 和 `private, no-store`。GET 默认一页 25 条，最多 50 条。活跃列表按另一端稳定 ID 排序，历史按 `occurred_at DESC, event_id DESC` 排序。游标绑定 Workspace、操作者、入口对象、查询类型和排序边界。每页重新授权，游标只取自已返回记录。

| 入口 | 请求与结果 |
| --- | --- |
| `GET /components/{component_id}/ci-runs` | 供 Ticket 选择构建，读取受支持且可见的终态。候选摘要含运行 ref、状态、完成时间及 Component ref；展示名称使用现有角色可读取的 Component。 |
| `GET /tickets/{ticket_id}/ci-runs` | active 关联、运行摘要、精确 link ref 和每项 `can_unlink`，外层包含当前 `can_link`。 |
| `POST /tickets/{ticket_id}/ci-runs` | `{ci_run_id,client_operation_id,confirmed}`。返回 `{link_id,applied:true}`。 |
| `DELETE /tickets/{ticket_id}/ci-run-links/{link_id}` | `{client_operation_id,confirmed}`。返回 `{link_id,applied:true}`。 |
| `GET /tickets/{ticket_id}/ci-run-link-history` | 有权查看的关联和解除历史，含事件、操作者、时间、CI Run ref、link ref 和关系状态。 |
| `GET /ci-runs/{ci_run_id}/tickets` | 当前可读 Ticket 的 active 反向分页，摘要只含 Ticket ref、标题、状态与 Project ref。 |
| `GET /ci-runs/{ci_run_id}/deployments` | 该运行已有且可读的 staging 终态记录，含 Deployment ref、Environment 安全摘要、状态和完成时间。沿用已有跳转地址。 |

Ticket 入口先从已有 Component 目录选择组件，再分页选择具体运行，确认时显示运行 ref、所属组件、终态和完成时间。CI Run 页的反向入口用于读取和跳回工单，写动作在 Ticket 页完成。结果未知时保留原 payload 与 operation ID。Workspace、对象或读取权限变化时清空当前结果并取消在途读取，丢弃迟到响应。单独失去写权时关闭编辑，保留当前可读内容。

## 备份、取回与取舍

- **PostgreSQL 整库备份**保存 CI Run、Deployment、现有研发来源、关联各代次、事件、回执和 Outbox。恢复到独立空库后重建 Activity，并按新登录身份验证可见性与原回执。028 备份先由匹配工件恢复，再执行 migration 029。迁移只新增关系类型、约束、索引和回执规则并更新投影版本，旧工单与运行历史保持其原事实。
- **单 Project `.nexus`**沿用 `project-collaboration-v9` 及原 scope 文件。运行、部署、构建关联和对应历史沿现有研发上下文排除边界处理。取回后的 Ticket 保留分类、来源和执行讨论。迁移手册交代这项区别，面向操作者的范围说明使用固定文案。按样本反复导入和导出校验完整性、原 digest 及权限，检验包内没有运行引用、部署证据或悬空关联事件。
- 选用人工确认的 EntityLink，使关系能独立修正并重放。根据 Component、分支、commit 或工单编号自动建立关系，需要另行定义来源权威和确认流程。研发证据的跨实例迁移也需要另行定义运行、组件、仓库、环境的身份映射和授权边界。

## 实施顺序与验收

| 顺序 | 交付与验收 |
| --- | --- |
| 1. 关系与事件 | 接通约束、命令、双端授权、精确解除、原操作重试和投影。验证跨 Workspace、restricted Project、归档与撤权、并发关联与解除重连、故障回滚。 |
| 2. 分页与 Web | 通过正式入口从工单选取终态构建，在 CI Run 反查工单并进入已有 staging 记录。验证隐藏项先于分页过滤、状态和读取权限、旧 Nexus View 形状、取消在途读取、键盘和窄屏。 |
| 3. 兼容与恢复 | 以合成身份和受控 Jenkins 输入在隔离回环 HTTPS 操作。验证 028 工件恢复后升级至 029、029 整库恢复、Activity 重建、原回执与旧 v1～v9 包读取和 v9 往返。 |

项目所有者于 2026-11-15 确认本批的关系模型、写权限、公共 HTTP 接口、投影版本和备份合同。真人观察、真人实测及真实外部交付联调沿[当前执行顺序](../status/current.md#近期执行顺序)安排。
