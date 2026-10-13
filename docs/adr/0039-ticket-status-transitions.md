# ADR-0039：Ticket 最小状态流转

状态：提议（未接受、未实施）

日期：2026-10-13

关联：[ADR-0004](0004-project-scoped-collaboration-permissions.md)、[ADR-0019](0019-session-scoped-thread-decision-ticket-transport.md)、[ADR-0022](0022-transactional-activity-and-incoming-relations.md)、[ADR-0034](0034-ticket-component-relations.md)、[ADR-0038](0038-project-ticket-discovery.md)。若接受，本 ADR 增量替代 ADR-0019 / ADR-0038 的 Ticket 公共读取仅支持 open 的限制；不改变创建来源、Project 权限或交付事实的边界。

## 背景与源码基线

基线 `7ace9b4` 已交付 Project 工单只读发现。成员可以找到并打开 Ticket，但不能记录开始、完成、取消或重新打开。领域和数据库已预留 `open / in-progress / done / canceled`；这不等于已有状态写入合同。

源码核对发现需要一起处理的边界：

- [Ticket 与创建 service](../../server/internal/goldenpath/service.go)没有状态版本；[创建事务](../../server/internal/goldenpath/postgres/store.go)的精确重试读取原 Ticket 当前值，并不保存创建时的对象快照。
- [公共协作 DTO](../../server/internal/platform/httptransport/collaboration.go)、[Project 发现 DTO](../../server/internal/platform/httptransport/discovery.go)和 Web [协作 adapter](../../web/src/collaboration/api.ts) / [发现 adapter](../../web/src/workspace/project-tickets-api.ts)只接受 open。创建 handler 也无条件要求 open；工单流转后，旧创建重试必须能返回原对象的真实当前状态。
- [Component 反向列表](../../server/internal/platform/httptransport/ticket_component.go)复用 Ticket 校验，[Web summary](../../web/src/collaboration/ticket-component-api.ts)也只接受 open，不能遗漏此读取入口。
- [协作 receipt](../../server/internal/goldenpath/postgres/collaboration_receipts.go)已有 `result_revision`，目前用于 Document；命令 / target / result 的数据库约束须明确扩展，不能混用 Document revision 语义。
- [Activity projector](../../server/internal/goldenpath/postgres/activity.go)和公共 Timeline 只认识 Ticket 创建、组件关联 / 解除；当前投影版本为 6，新增状态事件必须同时进入正常写入与全量重建。

本轮只准备独立提案，不修改这些实现。真实 Jenkins 的 ADR-0035 / ADR-0037 B 继续暂缓。

## 提议决定

### 1. 四状态与允许转换

| 当前状态 | 用户含义 | 允许目标与动作 |
| --- | --- | --- |
| `open` | 待开始 | `in-progress` 开始处理；`done` 标记完成；`canceled` 取消工单 |
| `in-progress` | 处理中 | `open` 返回待开始；`done` 标记完成；`canceled` 取消工单 |
| `done` | 已完成 | `open` 重新打开 |
| `canceled` | 已取消 | `open` 重新打开 |

允许 open 直接完成，避免为已完成的小事项制造虚假的“处理中”历史。done 与 canceled 保持不同事实；两者不能直接互转，也不能直接进入 in-progress，须先显式重新打开。相同状态的新命令不是一次变更，返回 `409 conflict`，不创建空事件或成功 receipt。

done 表示用户将该执行事项标记完成，不表示 Decision 被再次确认、测试通过、代码合入、CI Run 成功或 Deployment 完成。取消不删除 Ticket 或来源，重新打开不恢复 / 重建任何旧关系。不得从 CI、组件共属、AI 摘要或定时任务自动修改状态。

不增加 assignee、priority、deadline、type、workflow 配置、完成比例或状态原因字段。状态变更保留 actor、时间、前后状态与版本；首片不引入自由文本说明及其额外权限 / 保留合同。

### 2. 当前权限与对象边界

写入仅允许当前 active Workspace 成员、Ticket 所属 active Project 的 contributor / decider / admin。viewer、只有公开 Project 只读基线的成员、创建者身份和 Workspace owner 均不能绕过 Project 写角色。读取继续按 governing Project 当前权限，归档 Project 保留历史读取。

状态变更不重新接受来源 Decision，也不要求能读其 restricted Thread evidence；Ticket 的执行状态不从来源引用继承权限。Component、Repository、Document 和 Deployment 的授权保持独立。

服务端在每次新命令和精确重试中先复核当前账户、Workspace membership、Project 可读性 / 写角色及 active 状态，再处理 receipt。失效 Session 为 `401`；对象未知、跨 Workspace 或失去读取权为同形 `404`；仍可读但没有写角色为 `403`；有写角色但 Project 已归档为 `409`。错误不返回内部角色、隐藏对象或原始数据库原因。撤权后 receipt 不提供旧成功结果，重新取得权限后仍可按原请求精确重试。

### 3. 独立状态版本与并发

为 Ticket 增加 `status_revision`，正整数，初始为 1；每次有效状态变化加 1，其他操作不改变它。采用 PostgreSQL integer，公共 JSON 为 1..2147483647 的整数；达到上限后新状态变化返回冲突且不写入，已成功操作的精确重试仍可确认。

状态写入要求 `base_status_revision`，不把 `updated_at`、旧 UI 状态、Activity ID 或 receipt 当成版本。单独比较旧状态不能识别 `open → in-progress → open` 后的旧请求；即使状态相同，旧版本也必须冲突。

事务顺序遵循现有配置 / 组件写入的锁定惯例：定位不可变 governing Project，复核并锁住账户、Workspace membership、Project / 角色，再锁定 Ticket 行。Ticket 使用排他行锁；读取当前状态与版本、claim receipt、验证转换、更新和写事件都在同一事务中完成。实施时核对与现有创建 / 关联 / 配置命令的锁顺序，不引入反向锁定。

不同 operation ID 基于同一版本并发修改，至多一个成功，其余 `409`；不能以“目标现在相同”将输家伪装成成功。新请求的 base 与当前版本不同、同状态或非法状态边返回 `409`；未知状态值、非整数 / 越界版本属于 `400 invalid`。冲突不消耗 operation ID，不保留 receipt 或事件。

只更新 `status / status_revision / updated_at`。状态事件时间在取得 Ticket 锁后确定，以数据库可保存的微秒精度严格晚于此前 `updated_at`，避免排队请求的取时顺序导致同一 Ticket 状态历史倒序；版本才是并发依据。关系变化不增加状态版本，也不改 Ticket 更新时间；来源、标题、创建者与创建时间不变。

### 4. 唯一状态命令与精确重试

拟新增唯一业务端点：

`POST /api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/status-transitions`

请求形状示例，不是当前可执行 API：

```json
{
  "client_operation_id": "browser-generated-printable-ascii",
  "base_status_revision": 1,
  "to_status": "in-progress",
  "confirmed": true
}
```

仅接受这四个字段；严格 JSON 拒绝未知 / 重复字段、null、多余值及任何 query，body 上限 8 KiB。`client_operation_id` 沿用 1..128 bytes、ASCII `!` 至 `~`；`confirmed` 必须为 true，表示用户显式提交此动作，不进入业务状态、事件或 digest。没有通用 PATCH、批量状态接口或客户端 actor / role / Project 输入。

沿用 HTTPS Host、TLS / trusted proxy、Session、精确 Origin、double-submit 与存储态 CSRF、服务端 Principal / correlation。成功与错误均 `private, no-store`、`Vary: Cookie`；未知路由为 404，不支持方法为 405 和精确 `Allow: POST`。错误沿用既有 envelope、413、415 和通用 500。

命令种类为 `ticket.status.change`；receipt scope 为既有 `(workspace_id, actor_id, command_kind, target_type=ticket, target_id, client_operation_id)`。canonical digest 只包含已校验的 base_status_revision 和 to_status，固定字段顺序、整数与枚举编码，不包含 confirmed、时间、身份或 request ID。

- 当前权限通过后，先匹配 receipt，再检查新命令的版本和转换。相同 scope / digest 的精确重试确认原操作；不同 digest 返回 409。
- 首次成功和精确重试均为 `200`，返回 `data: {ticket: {type,id}, applied: true, applied_status, applied_status_revision}`。后两项来自原不可变事件 / receipt，并严格交叉核验；不返回 receipt ID、事件 ID、digest 或 operation ID。
- 成功响应表示“该操作已应用”，不是当前 Ticket 快照。即使工单后来又变化，重试仍返回原 applied 结果，绝不再次改变状态或回退版本；随后通过现有 Nexus View 读取当前值。
- receipt 复用 `result_type=ticket / result_id=target_id / result_revision=原应用后的状态版本`，与原事件 ID 绑定。事件必须为同 Workspace / actor / Ticket / governing Project 的状态事件，事件版本和 applied 状态一致；损坏或缺失不默认成功。
- 响应丢失、网络 / 5xx 歧义时客户端保留相同 payload 与 operation ID，只允许显式精确重试；不能为了“试试看”换 ID 或改 base。刷新或离开页面不承诺保留待确认请求，重新进入先读权威状态，不自动重放。

### 5. 公共读取与创建重试兼容

所有现有读取入口同时接受四状态，使用同一封闭枚举和中文标签；未知状态仍整页 / 整对象失败，不静默过滤、跳过或伪造 open。

| 入口 | 拟调整 |
| --- | --- |
| Ticket Nexus View 的 `data.current` | 四状态，新增必填 `status_revision`；其余字段和来源关系不变 |
| Ticket Nexus View 的 `data.capabilities` | 新增仅 Ticket 使用的 `{change_status: boolean}`，与 Current 在同一读取事务按当前写角色、Project active 与版本上限计算；Thread / Decision 不新增该字段 |
| Project Ticket 列表 | 只扩展 status 枚举，仍为 ref / title / status；分页、归属和 cursor 不变，不增加版本、写能力、筛选或排序 |
| Component 反向 Ticket 列表 | summary 接受四状态，字段、权限与分页不变 |
| Decision → Ticket 创建响应 | Ticket 对象增加状态版本；首次 201 必须 open / 1，精确重试 200 返回原对象真实当前状态 / 版本 |
| 关系、Document 来源等 Ticket 引用 | 保留原 ref / title 形状与权限，不能因目标非 open 而拒绝或隐藏 |

Nexus View 的能力只决定是否展示操作，不是写入凭证；服务端最终重新授权。能力为 false 时仅显示状态和历史，归档项目明确提示只读；不暴露具体角色。可选目标来自本 ADR 的固定转换表，不引入通用工作流描述协议。

特别修正既有创建 handler 对所有响应都要求 open 的条件：新建与重试分别校验，重试仍返回同一 Ticket，不复制一份新工单，不伪造创建快照，也不把旧 ticket.created 事件改成新状态。创建 receipt 的旧 digest / result 语义保持不变。严格 Web adapter、Go DTO 和相关测试须成套更新。

### 6. 事件、Activity 与原子性

每次真实变化新增 `ticket.status-changed` schema 1。envelope 沿用 user actor、Ticket primary ref、governing Project scope、web source、服务端时间与 correlation；payload 严格为：

```json
{
  "from_status": "open",
  "to_status": "in-progress",
  "from_status_revision": 1,
  "to_status_revision": 2
}
```

四状态、合法转换与相邻版本必须校验；不复制标题、正文、来源、内部权限、operation ID 或 digest。Ticket 更新、receipt、不可变领域事件、Outbox 和 Activity 同事务提交；任一步失败全部回滚，普通状态动作不新增第二套 Audit 或状态历史表。

Activity 投影版本拟从 6 升至 7，正常写入与显式重建使用同一 projector。新事件只投影到 Ticket，无 subject，安全事实仅为上述前后状态与版本。公共 Timeline 为此事件增加严格分支，保留 ID、actor、occurred_at、空 subjects 和上述四个字段；旧 ticket.created 继续记录 open，组件事件继续使用 relation_state，均不能被当前状态覆盖。前端显示“待开始 → 处理中”等文字及状态版本，以明确重新打开的独立轮次。

不把状态事件扩散到 Decision、Thread、Component 或 Document Timeline，不建立通知、搜索投影或 Ticket ↔ CI Run 关系。历史事实按当前 Ticket 权限读取，来源对象权限不扩大；不从 Activity 反向更新权威 Ticket。

### 7. Web 操作与恢复

只在既有 Ticket 详情增加状态区；Project / Component 列表继续只读。详情展示当前状态及允许的动作，提交前显示具体目标；完成、取消、重新打开使用显式二次确认，开始处理 / 返回待开始使用清晰动作按钮。确认过程不等同 Decision acceptance 或交付审批。

- 无写能力时不显示可提交表单；提交中禁止重复点击，不乐观修改 Current 或 Timeline。成功后重新读取完整 Nexus View，显示原动作已应用与实际当前状态。
- 409 后明确提示内容已变化，清理旧确认并重读；用户查看最新状态后才能以新 ID / 新 base 再作选择，不自动覆盖他人结果。
- 网络结果未知时保留原动作的精确重试按钮；不因后续读取的状态恰好等于目标就认定本操作成功，不把待重试请求改成最新版本。失去读取权或 Session 后清空对象与动作；失去写权时保留可读内容但关闭状态操作，恢复权限也不自动提交。
- 显式刷新、focus / 可见性恢复及浏览器后退恢复时复核对象、能力与版本；取消旧请求、清理旧确认，迟到结果不能恢复旧作用域。待确认网络请求与刷新读取分别管理，仍有读写权时原 payload 可供精确重试。
- 返回 Project 列表按 ADR-0038 重新读取首页；状态变化不影响稳定 ID 分页位置，不强加更新时间排序。
- 桌面、手机、长标题、键盘焦点与状态文本均验收；不依赖颜色表达状态，不增加浏览器持久存储、前端状态库或依赖。

### 8. 迁移、恢复与交付

拟以当前 015 为基线新增 forward-only migration 016（实施前核对编号）：增加 Ticket status_revision 与约束，扩展 receipt 的 command / target / result_revision 约束及其原事件一致性校验，升级 Activity 版本；不修改已发布 migration。

旧实例所有正常 Ticket 应为 open。迁移前必须检查；发现非 open 的既存 Ticket 则拒绝迁移并报告需独立修复的异常，不默认补版本或虚构状态历史。正常旧 Ticket 以 open / 1 作为版本起点，不生成伪变更事件；未来新 Ticket 创建也为 open / 1，旧 ticket.created schema 1 保持原样。

Ticket、协作 receipt 与 domain_events 已在权威备份范围。恢复必须保留状态版本、原 applied receipt 和完整前后历史，不从 Activity 推断；全新目标恢复后显式重建 Activity 7，精确重试不能再次应用。Session 继续按既有恢复边界处理，不随备份变成可用凭证。

Go、schema 和 Web 配套升级；旧二进制按精确 schema readiness 拒绝新 schema，旧 Web 对新字段 / 状态的拒绝不能用 fallback 掩盖。升级时安排停写和客户端重新加载，应用 016 后不可直接回退旧二进制；恢复升级前备份或采用独立 forward repair，说明备份点之后数据取舍。没有跨版本写兼容窗口，不自动迁移 / 重建真实实例。

## 替代方案与取舍

| 方案 | 不采用的原因 |
| --- | --- |
| 只提交目标状态，最后写入覆盖 | 丢失并发修改与用户确认时的上下文 |
| 只提交 expected_status，或比较 updated_at | 无法可靠识别重新打开后的旧请求；时间戳还混合展示与数据库精度问题 |
| 只支持 open → done，不允许重新打开 | 无法完整表达取消与返工，绕开了已预留四状态的关键失败边界 |
| done / canceled 任意互转 | 混淆完成与取消；重新打开为新一轮显式决策保留清晰历史 |
| 用一次通用 PATCH 引入完整工作流与负责人 | 扩大字段、权限、验证与 UI 范围，首片收益不足 |
| 用 CI 成功自动完成 Ticket | CI / Deployment 与执行事项的领域语义独立，真实交付关系尚未冻结 |

代价是一个窄版本字段、一条新命令及事件、一次 migration 和读取合同的同步升级。收益是普通成员可以安全记录执行进度、区分取消与完成，并在并发、撤权和响应丢失时保留可信历史。

## 未来验收范围与退出判据

以下是接受后必须执行的验收，不构成本轮执行证据：

1. **状态与权限**：覆盖完整允许 / 拒绝矩阵、同状态、未知状态、各角色、owner / 创建者不穿透、公开只读基线、restricted 失权、暂停 / 移除 Workspace 成员、归档及私密 evidence 独立性；人工 done 不产生 CI Run / Deployment。
2. **真实 PostgreSQL 原子性**：不同操作同 base 并发只有一条成功、相同操作并发去重、回到相同状态后的旧版本冲突、版本上限；事务注入失败时状态 / 版本 / receipt / 事件 / Outbox / Activity 一并回滚，账户 / membership 撤回与状态命令的锁定边界可复验。
3. **重试与安全**：响应丢失后精确重试、同 ID 改目标或 base、后续多次转换后的旧 receipt、撤权后重试；匿名 / 失效 Session、Host / proxy / Origin / CSRF、严格 JSON / 大小 / query、缓存头、错误收敛以及损坏 receipt / 事件拒绝。
4. **读取一致性**：详情、Project 列表、Component 反向列表、创建精确重试均能回读四状态；首次创建固定 open / 1，流转后重复创建仍返回原 Ticket；旧创建与组件事件保留原事实，正常 Activity 与重建结果一致，无隐藏或部分成功。
5. **升级与恢复**：015 正常升级、非 open 基线显式拒绝、新 schema readiness、混合旧创建 / 新状态 / 原 receipt 的备份与空目标恢复；重建后所有历史和幂等结果保持一致。没有实际执行则明确记为未验收。
6. **Web 自动化与隔离 HTTPS 浏览器**：正式创建链和普通 contributor，从 Project 列表进入详情，开始 / 完成 / 取消 / 重新打开；双标签页冲突、响应丢失的精确重试、刷新 / 后退 / 焦点恢复、撤回读写权、迟到结果、手机 / 键盘与长标题。再沿来源、组件和文档回归，不能用直接 SQL 修改状态代替产品主流程。
7. **工程与文档**：定向 Go、真实 PostgreSQL、备份恢复、Web 和 `./scripts/check-repo.sh`；同步领域模型、公共合同、server / Web 说明、当前状态与实施记录，仅报告实际执行证据。

退出结论限定为“当前权限下可显式流转四状态，版本冲突、精确重试与历史可追溯，现有读取和恢复合同一致”。不代表完整工单系统、真实团队持续使用或完整 Golden Path 通过。

## 本轮授权与待确认范围

所有者本轮只要求提交 ADR-0038 既有实现，并梳理此独立提案；本 ADR、索引和当前状态是全部新增文档范围。四状态转换表、写权限、版本 / 重试合同、公共字段与迁移均为建议，待所有者确认后才实施。当前正式实现仍只读展示 open。

本轮不修改代码、schema、依赖、已接受领域合同或远程状态，不启动验收服务；不自动提交本提案，不 push、发布或部署。真实 Jenkins B 继续暂缓，后续 Decision 只读发现及试用准备仍按当前顺位独立推进。
