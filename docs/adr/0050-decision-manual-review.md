# ADR-0050：Decision 最小人工复核

状态：已接受。2026-11-08 所有者确认 D01～D07，已实施并通过正式 HTTPS Chrome 验收。 [分层验证与待复验范围](../status/reviews/2026-11-08-decision-manual-review.md)

日期：2026-11-08

核对基线：`d266dd8`，schema 024 / Activity 12 / `project-collaboration-v5`。本提案承接 [ADR-0048](0048-decision-lifecycle-and-rejection.md) 的 C 方向，沿用 [ADR-0049](0049-decision-supersession.md) 的四状态、单链替代和原操作历史。

## 背景

已接受结论在新约束或证据出现后，团队需要记录“在当时掌握这些材料的情况下，该结论是否仍适用”。例如原讨论新增了兼容性证据，决策者在 Decision 详情确认继续沿用原结论，或记录需要另行提出替代方案。之后打开原 Decision、Ticket 或 Document 的成员仍可循原来源追溯当时的决定和后续复核。

- 替代事务在双方 Decision 锁内校验版本，用数据库时间排列替代与已有 Ticket。现有授权要求写入者可读全部 `derived-from` Thread，且私密 Thread 的权限独立于 Project。 [替代事务](../../server/internal/goldenpath/postgres/decision_supersession.go) · [现有授权](../../server/internal/goldenpath/postgres/access.go)
- Nexus View 的 Decision 能力来自同一服务端读取快照，Timeline 当前全量读取。Activity 有正常事务投影和独立重建路径。事件和 receipt 分别保留领域事实与原命令标识，可用 `event_id` 找到原结果。 [Nexus View](../../server/internal/goldenpath/postgres/nexus_view.go) · [Activity](../../server/internal/goldenpath/postgres/activity.go) · [receipt](../../server/db/migrations/007_collaboration_command_receipts.sql)
- 上下文包已有版本化 `history.jsonl`。`review_at` 在 Decision 表和包中已有可空字段，迁移 024 禁止修改已有值，当前正式 Web 没有设置或消费该字段的入口。 [上下文包](../../server/internal/contextpackage/types.go) · [迁移 024](../../server/db/migrations/024_decision_supersession.sql)

## 已确认决定

| 编号 | 合同 | 影响 |
| --- | --- | --- |
| D01 对象与结果 | 从当前 `accepted` Decision 手动记录一次已完成的复核。结果为 `still_applicable`（仍适用）或 `reconsider`（需重新评估） | 原状态、结论、责任、时间和执行来源保持原值。需要改变结论时，成员另行创建、接受新提案，再显式替代 |
| D02 证据与权限 | 使用原 `derived-from` 证据，可附一条不同的同 Project Thread；按当前权限保护整条复核 | 当前 `decider` / `admin` 具备全部证据读取权才可记录或重试；读取历史按 Decision 和该条证据的当前可读性，开放给符合条件的普通读者 |
| D03 权威事实与版本 | 追加一条不可变 `decision.reviewed` 事件，以事件 ID 标识复核，绑定当时的 `decision_revision` | 多次复核各自保留，Decision 版本和更新时间保持原值；复核与替代在现有 Decision 锁内确定顺序 |
| D04 协议与原请求 | Decision 下开放复核读写，明确确认后提交；复用 collaboration receipt | 响应和重试返回同一原复核事实；接受、拒绝、替代与创建 Ticket 的原请求和 digest 保持原合同 |
| D05 Web 与 Activity | Decision 详情提供记录入口和分页的复核历史；使用现有事务内 Activity 投影与重建 | 支持 `superseded` 后回查历史，普通 Timeline 保留既有事实范围。复核说明和证据受独立读取检查保护 |
| D06 自部署与迁移 | 提议 schema 025 / Activity 13 / `project-collaboration-v6`，Go / Web / CLI 配套交付 | 旧 v1～v5 严格读取；完整往返须保留复核、证据和历史身份，恢复时保留原 receipt |
| D07 触发范围 | 首片仅支持人工记录，`review_at` 保持现有可空数据语义，计划复核另议 | 到期提醒、调度、自动确认、状态回转、复核任务分派和跨 Project 证据留待后续切片 |

## 业务事实与证据

一次记录包含目标 Decision、当时版本、人工触发、复核说明、结论、操作者、服务端时间和证据。说明用于记录复核依据与判断，要求 TrimSpace 后非空，原始及规范化 UTF-8 均最多 4 KiB，禁止 NUL 和未配对 JSON surrogate。说明以纯文本渲染，表单提示用引用指向私密材料。`reconsider` 记录评估意见，后续采用新结论须完成独立的提案、接受和替代命令。当前结论仍具备既有使用资格，直至按 ADR-0049 被替代。

原证据沿用 Decision 的 `derived-from` EntityLink。补充证据用单个 `thread` ref 保存在本次复核，作用域仅限本记录。它不得重复现有来源，不能改变 Decision 原有证据、授权和替代资格。首片从现有 Channel / Thread 入口复制同源 canonical Thread 链接，表单用既有 Thread 详情读取预览。只提交结构化引用，服务端不访问用户提供的 URL。用户可以在原讨论追加消息后直接复核，也可以引用新讨论。

自 2026-11-23 起，表单也可从该决策所属 Project 的可读频道分页选择讨论，频道中的发现范围沿用 [ADR-0061](0061-channel-thread-discovery.md)。已是原证据的讨论在列表中标明并禁选。选中讨论或粘贴链接均通过 Thread 详情核对当前读权和 Project 归属，再由成员确认。页内重新核对遇到撤权时清理草稿并重读复核历史。提交仍由原命令在事务内检查证据。[双账号验证](../status/reviews/2026-11-23-decision-review-evidence-picker.md)

## 写入、权限与并发

在 `/api/v1/workspaces/{workspace_id}/decisions/{decision_id}/reviews` 新增 `POST`。请求字段全部必填，其中 `additional_evidence` 可显式为 `null`。

```json
{
  "client_operation_id": "web:unique-operation",
  "base_decision_revision": 2,
  "conclusion": "still_applicable",
  "reason": "新兼容性测试已覆盖既定约束，继续采用当前方案。",
  "additional_evidence": {"type": "thread", "id": "thr_follow_up"},
  "confirmed": true
}
```

复用同源 Session、Origin、CSRF、Host / TLS、错误 envelope、`private, no-store` 和 `Vary: Cookie`。POST 沿用 64 KiB body 上限、1～128 printable ASCII operation ID、正整数版本约束和严格 JSON 检查；拒绝重复字段、未知字段、额外 JSON 值和 query。失效 Session 为 401；目标 Decision 未知、不可发现或不在 Workspace 为同形 404。Decision 可读后，Project 归档为 409，缺决策权或任一证据不可读为通用 403，隐藏的补充证据与未知证据同形。补充证据可读后才报告 Project 不符或重复引用的 409。错误和日志均不携带隐藏证据标识、标题、数量或说明正文。

写入使用现有 admission、当前账户和 Workspace membership、Project / 角色、Decision `FOR UPDATE`、原证据与补充证据、receipt、首次业务资格、原子写入的顺序。读取补充证据复用正式 Thread 授权，含 origin Channel 及 restricted Thread 成员规则。现有权限管理与写命令在同一 admission 边界确定先后。首次写入要求仍为 `accepted` 且版本等于 `base_decision_revision`，否则返回 409 并由 Web 重新读取。复核只追加事件，彼此独立的复核可以记录同一版本；同一 operation 只产生一次效果。

在持锁后取数据库时间，复核时间须严格晚于当前 Decision 的 `updated_at`、上一条复核和本次证据的创建时间。比较时包含导入复核的原始事实时间。替代时间在 ADR-0049 现有下界之外，还须晚于双方最后一次复核；时钟回退或微秒精度冲突时沿用最大事实时间加一微秒。复核先取得锁则可记录旧版本，随后的合法替代保留该复核；替代先取得锁则过时的首次复核冲突，包括替代链新端仍为 `accepted` 但已升版本的情形。

receipt kind 为 `decision.review`，scope 沿用 actor / Workspace / kind / Decision / operation。规范化摘要按固定顺序绑定 `base_decision_revision`、`conclusion`、TrimSpace 后的 `reason` 和 nullable `additional_evidence`；`confirmed` 仍须为 true。receipt 的 target 和 result 均为该 Decision，`result_revision=null`，用已有 `event_id` 指向本次复核。首次成功为 201，精确重试为 200，响应均返回不可变复核记录；同 operation 变更内容为 409。

精确重试先重新授权，再用 receipt 核对原事件的 Workspace、Project、Decision、操作者、来源、时间和规范化 payload。核对成功后，即使目标已 `superseded` 或版本已变，也可取回原事实。原事件缺失或不符返回内部错误。Decision、Ticket 和 Document 的已有写入与精确重试继续按各自合同处理。

## 记录、Activity 与正式入口

唯一权威记录为 schema 1 `decision.reviewed`，primary 为 Decision，actor 为实际用户，source 为 `web`。事件 payload 精确包含 `decision_revision`、`trigger`（固定 `manual`）、`conclusion`、`reason` 和 nullable `additional_evidence`。原证据由既有来源关系确定。复用不可变领域事件和既有 receipt 约束。migration 增补命令白名单及新事实一致性约束，校验原生事件与 receipt 一一对应、同一 Decision / Project / 操作者 / 时间，以及补充 Thread 存在于该 Project。导入由 `context.history-recorded` 的来源事实及已校验历史证明。无需新增业务表或通用 EntityRef 类型。

同一事务写入事件、receipt、Outbox 和 Activity；用同一事件转换处理正常写入、导入和重建。Activity 13 只投影到目标 Decision，`safe_facts` 仅含复核版本，说明、结论和证据引用由受控历史读取提供。投影、Outbox 状态、通知、搜索和插件均不公开复核 payload；首片的 Outbox 只交付既有 Activity projector。通用 Timeline 跳过该事件类型，Decision 页面在相邻的“复核历史”区块使用该投影分页，并按 `event_id` 读取权威事实。

同一路径开放 `GET`，仅接受 `limit` 和 `after`，默认 20、最多 50。按 `(occurred_at, event_id)` 倒序使用 keyset 分页，cursor 绑定 Workspace、Decision 和当前主体，只由最后一条已返回的可见记录生成。每页在同一读取快照中检查 Decision 以及各条原证据和补充证据的当前可读性；先过滤，再按 `limit + 1` 条可见记录决定下一页。没有权限的历史不占页长，不返回占位、计数、actor、时间、结论、说明或证据引用。服务端可按索引分批扫描候选以复用 Thread 权限检查；扫描错误向上返回，不能截断成空页。归档和 `superseded` 保留可读历史。返回 `event_id`、Decision ref、复核版本、触发、结论、说明、actor、时间及经过检查的来源引用；导入历史使用已有导入标识和历史身份。

Decision Nexus View 增 `capabilities.review`。条件为 `accepted`，且满足既有 `retry_decision` 所要求的 active Project、决策权和原 evidence 可读性；提交时再复查选择的证据。结果未知时，页面保留原 payload / operation 在当前页面内供显式重试；旧版本或首次能力变为 false 不推断原提交失败。切换对象或失去现有 `retry_decision` 能力时清除草稿；补充证据撤权、401 / 404 或重读失败时清除相应的私密状态。页面恢复沿用既有代次和取消机制，重读能力与历史首页并丢弃迟到响应，不新增浏览器持久化。`superseded` 页面可以显示已有复核和受权限过滤的替代链。

## 替代方案与取舍

| 方案 | 首片取舍 |
| --- | --- |
| 人工提交独立复核事实 | 建议采用。用现有版本、事件、授权和来源就能完成一个正式场景，并保留同一版本的多次评估 |
| 增加 `reviewing` 或在 Decision 上只保存最近一次复核 | 会把评估和原结论生效状态耦合，或覆盖前一次评估依据；保留四状态与完整独立历史 |
| 同时开放 `review_at` 编辑、到期和提醒 | 还需明确时间变更、提醒主体、撤权、补发和恢复行为；首片先验证人工记录能否解决追溯问题 |
| 增加独立复核表和通用关系类型 | 单一不可变记录可由现有事件保存，补充证据是记录内受控引用。若后续需要开放复核本身的引用、草稿或分派，再单独设计其身份与关系 |

## 迁移、恢复与实施顺序

- migration 025 前向扩展 receipt 和事件一致性约束，保持既有 Decision 状态、版本、时间、`review_at`、事件和 receipt。Activity 按既有投影版本切换与重建流程从 12 升到 13；升级后原事件继续可读，正常新写入在响应成功前完成投影。Go / Web / CLI 与 schema 配套停写升级，旧工件拒绝新 schema。退回旧工件时，用匹配 024 的备份恢复空目标，并说明备份后写入的损失。
- v6 延用 `format_version: 1` 和 `history.jsonl`，只增加 `decision.reviewed` 的严格类型和事实形状。v1～v5 按原白名单和原 manifest digest 校验，保留原字段转换。v6 不向旧 profile 塞入新记录，亦不静默删掉复核以生成旧包。对当前可读的单 Project 闭包保留 Decision 的全部复核、原身份和来源。任一必需证据不可读或缺失时，沿用通用闭包失败，不返回隐藏引用或裁剪出表面成功的历史。
- 历史校验按事实时间在现有接受与替代重放中验证复核当时为 `accepted` 且版本匹配，证据在当时存在，原 evidence 完整，补充 Thread 在同一 Project。复核不推进 Decision 版本或 `updated_at`，拒绝在被替代后补入的复核和无法证明版本的记录。导入映射复核的 Decision、事件、作者与 Thread 引用，使用既有 `context.history-recorded` 与 lineage；不可登录历史身份继续保留，目标不继承源 receipt 或历史 Outbox。整库备份则保存原 receipt 和事件，匹配版本恢复后可重建 Activity 并继续精确重试。
- 依次实施 PostgreSQL 写入与权限、事件与历史兼容、正式 HTTP / Web，再按下表分层取证。同步领域模型、决策基线、核心契约和实际工件入口说明。隔离运行资源与真实实例操作按仓库规则分别授权。

## 验收矩阵

以下为实施验收计划。

| 编号 | 层次 | 通过条件 |
| --- | --- | --- |
| R01 主链 | 正式 HTTPS Web | 从 Project 找到原 Decision，追加原讨论证据或引用另一可读 Thread，记录“仍适用”；另记“需重新评估”并走既有新提案与替代。刷新双方页面能区分原决定、原复核与替代，原 Ticket / Document 仍可追溯及正常维护 |
| R02 权限 | 真实 PostgreSQL、HTTP、Web | 覆盖只读成员、无决策权、归档、账号暂停 / 撤权和 restricted Channel / Thread；首次与重试权限均重新判断。读者失去任一证据权后，该条历史从分页消失，下一页标记只基于可见记录，无占位或隐藏字段；Decision 的既有可见性照常 |
| R03 并发 | 真实 PostgreSQL | 分别固定复核、替代、撤权先取得锁；覆盖替代两端和时钟回退。过期首次提交冲突；复核先成功则替代后仍保留原版本记录。不同 operation 可独立复核同一版本，同 operation 并发只提交一条 |
| R04 重试与回滚 | 真实 PostgreSQL、HTTP | 覆盖 COMMIT 后丢响应、原 payload 重试、内容变化、替代后核验、撤权后拒绝；在事件 / receipt / Outbox / 投影关键点注入失败，全部回滚。旧接受、拒绝、替代及已建 Ticket 重试仍返回既有结果 |
| R05 协议与分页 | 定向 HTTP / Web 测试、真实 PostgreSQL | 严格 JSON、Unicode、限制、Session / Origin / CSRF、方法与缓存头，拒绝外站和错 Workspace 的 Thread 链接。覆盖隐藏记录跨越候选批次、相同时间排序、cursor 错作用域、迟到响应和恢复后重读；后续读取故障保留明确错误 |
| R06 Activity | 真实 PostgreSQL、HTTP、Web | 正常写入后无需手工重建即可读取；导入投影、重建与正常投影一致。旧 Timeline 完整，复核历史有序可翻页；其它对象和消费者无复核内容或提示 |
| R07 自部署历史 | 真实 PostgreSQL、CLI、匹配版本恢复 | 024→025 保留旧事实与 `review_at`；v1～v5 严格读入，v6 两轮独立往返保留复核、证据、时间、身份与来源并可继续新写。伪造版本、缺证据、被替代后补录及无权导出均拒绝；恢复后可重建并核验原 receipt |
| R08 独立操作 | 独立成员操作与后续试用观察 | 有权成员从正常导航独立完成记录与追溯；观察是否能解释结论为何沿用或后来改变，以及需要维护者介入的步骤。试用准入和真实团队持续使用独立记录 |
