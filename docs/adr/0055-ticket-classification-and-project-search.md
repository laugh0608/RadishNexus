# ADR-0055：工单分类与项目内查找

状态：已接受

日期：2026-11-15

扩展 [ADR-0047](0047-ticket-priority-and-filtering.md) 的 Project 工单筛选、[ADR-0052](0052-workspace-my-tickets.md) 的个人列表展示，以及 [ADR-0019](0019-session-scoped-thread-decision-ticket-transport.md) 和 [ADR-0054](0054-message-to-ticket-and-execution-discussion.md) 的建单输入。

## 背景

Project 列表和看板共用工单查询，按状态、负责人和优先级查找。消息和 Decision 都能建单，工单也能关联讨论。随工单增多，成员需要区分需求、任务和缺陷，用项目自己的标签整理工作，并按标题找回工单。本批将分类、查找、展示及恢复一起交付。 [M4 路线图](../roadmap.md#m4工单与上下文关联) · [工单产品定义](../product-definition.md#工单)

现有列表按稳定 Ticket ID 分页，游标绑定 Project、操作者和筛选组合。上下文包 v8 严格验证两类创建来源及 Ticket 字段历史，两条建单路径分别保留自己的事件和回执。 [列表查询](../../server/internal/goldenpath/postgres/discovery.go) · [建单与来源](0054-message-to-ticket-and-execution-discussion.md) · [上下文迁移手册](../../deploy/context-package.md)

## 决定

| 事项 | 合同 |
| --- | --- |
| 工单类型 | 固定 `requirement`（需求）、`task`（任务）、`defect`（缺陷）和 `unset`（未分类）。升级旧工单、导入旧包和省略分类的建单命令使用未分类，标签集合为空。Web 创建时可选择分类，详情可归为未分类。 |
| 标签目录 | 每个标签属于一个 Project，使用稳定 `ticket-label / lbl_` 引用。Project admin 可以创建、改名和归档。目录名在同 Project 内唯一，归档名称仍保留。改名后的当前名称在所有关联工单中统一显示。 |
| 归档标签 | 原绑定继续显示当前名称和“已归档”标记，可按该标签筛选。关联选择器只提供有效标签。整理工单时可保留或移除已有归档标签，移除后仅有效标签可以再被选中。 |
| 分类写入 | 当前 active Project 的 contributor、decider 或 admin 可整理四种状态的工单，包括已完成和已取消的工单。分类有独立版本，与状态流转、负责人、优先级和截止日期各自管理。 |
| 组合筛选 | 类型、标签、标题与现有状态、负责人和优先级条件按 AND 组合。选择多个标签时，工单必须同时具有全部所选标签。Project 列表和看板共用后台规则。 |

### 数据、权限与写入

- migration 028 增加 Ticket 的 `ticket_type`、`classification_revision`，以及 Project 标签目录和 Ticket 标签绑定表。分类版本从 1 开始。历史 Ticket 初始化不补造事件或修改 `updated_at`。目录保存名称、状态、目录版本和审计所需的作者及时间。绑定由稳定标签 ID 表示，数据库用 Workspace 和 Project 复合外键保证工单与标签同属一个 Project，并限制同一绑定只出现一次。
- 标签名去除首尾空白后校验为单行 UTF-8，长度 1～40 个 Unicode 字符。Project 内唯一键对 ASCII 字母折叠大小写，其余字符按原值比较。显示保留用户输入的大小写。用同一规则校验创建、改名、导入和重建。标签归档沿 active → archived，目录管理不删除历史行。
- 已准入的只读 Project 成员可读取目录、工单分类及历史。项目归档后目录和工单分类可读。Workspace owner 的管理身份沿现有 Project 角色规则判断。分类字段、标签及其历史路径采用同一读取边界。标签 ID 和工单引用不授予权限。
- 修改分类提交完整目标集合 `{ticket_type, label_ids}`、所见 `base_classification_revision`、`client_operation_id` 和 `confirmed:true`。按稳定 ID 规范化集合，重复 ID、未知枚举及无效格式返回 400；错误响应对不存在和其它 Project 的标签使用同一结果。相同状态的新请求、过期版本及试图新增归档标签返回 409。新工单的标签须在提交时有效。
- 事务沿 Workspace 准入、Project 授权、Ticket 排他锁和标签行的稳定 ID 顺序获取锁。标签目录写入锁定 Project 和该标签，归档及改名不逐张修改工单。若归档先提交，新分配冲突；若分配先提交，归档后该原绑定仍保留。标签改名绑定目录版本，工单分类绑定稳定标签 ID 和分类版本。分类变更和状态变更采用同一 Ticket 锁确定工单历史顺序。
- 写动作复用 Session、Origin、CSRF、严格 JSON 和命令回执。相同操作 ID 和规范输入先复核当前权限，再核验原回执。分类精确重试按原成功事实返回，界面重新读取当前值。它可以在标签后来改名或归档后核验已经提交的分配。目录写入留下可追溯的配置审计。
- 分类变更、绑定、receipt、不可变事件、Outbox 及 Ticket Activity 同事务提交。新增 `ticket.classification-changed` schema 1，记录前后类型、标签 ID 集合和相邻分类版本，对本次新分配的标签记录当时已生效的目录版本。标签创建、改名、归档各有 schema 1 事件，并通过目录事件重放名称和状态。历史显示操作时的标签名称，当前列表显示当前名称。分类变更推进 Ticket `updated_at`，目录写入只推进自己的版本。Activity 升至 16。

### 建单、详情与 HTTP 兼容

以下路径均在 `/api/v1/workspaces/{workspace_id}` 下。分类和目录读写使用现有响应包装及私有缓存策略。

| 场景 | 入口与形状 |
| --- | --- |
| 从消息或 Decision 建单 | 两条现有 POST 在原字段基础上接受可选 `classification:{ticket_type,label_ids}`。默认空分类与省略输入等价，沿原 digest。显式有效分类随原建单事务保存，建单回执的规范 digest 纳入分类。创建响应沿两条路径的原形状。 |
| Ticket 详情 | `GET/POST /tickets/{ticket_id}/classification`。读取返回当前类型、标签摘要、分类版本和 `can_set`。写入返还原操作已应用的版本，随后重读。 |
| 标签目录 | `GET/POST /projects/{project_id}/ticket-labels`。GET 沿稳定 ID 分页，支持 active、archived 或全部，默认 active。POST 显式确认并创建。 |
| 改名和归档 | `POST /projects/{project_id}/ticket-labels/{label_id}/rename` 与 `POST /projects/{project_id}/ticket-labels/{label_id}/archive`。显式确认并绑定所见目录版本。 |

- 创建仍使用 Decision 来源 schema 1 或 Message 来源 schema 2 的 `ticket.created`。显式初始分类在同事务追加 `ticket.classification-changed`，从未分类、无标签及版本 1 进到版本 2，以创建事件作因果来源，由建单回执确认整次操作。任一步失败整次回滚。旧重试可沿原请求取回原工单。
- Project 工单列表和「我的工单」通过 `include=classification` 增加必填 `classification:{ticket_type,labels}`。各标签摘要包括稳定引用、当前名称和 active／archived 状态。与截止日期一起读时使用 `include=due_date,classification`。旧读请求继续使用原形状。状态看板按现有四列调用同一 Project 工单列表。「我的工单」维持按本人责任及当前可读 Project 查找待处理工单。
- Ticket Nexus View 通过 `classification` include 项显示分类历史。组合值按 `due_date,execution_context,classification` 的顺序取所需子集。原两项可继续单独或组合使用。独立分类详情入口提供当前值和写权限。
- Web 两条建单路径共用分类输入；Ticket 详情中确认类型和标签变化。只读界面展示当前类型和标签。查询条件变化清空各列游标并从首页读取，Workspace 或权限变化时清空旧结果并取消在途请求。写响应未知时沿用原操作标识和输入。

### 项目内查询

| Query | 语义 |
| --- | --- |
| `type` | 按一个已定义类型筛选，包含 `unset`。 |
| `labels` | 单个参数接受逗号分隔的不同标签 ID，服务端按稳定 ID 排序后使用全匹配。已归档标签有效。 |
| `q` | 去除首尾空白后长度 1～80 个 Unicode 字符，以参数化的字面子串查找 Ticket 标题。ASCII 字母忽略大小写，其余字符按原值比较，`%` 和 `_` 按普通字符查询。 |

- 沿现有 discovery 参数大小和页大小限制。未知键、重复参数和空查询值返回 400。标签 ID 必须属于当前已授权的 Project，否则按同一无效条件错误处理。
- 在一次授权快照内，以参数化 SQL 在 LIMIT 前应用所有匹配条件。继续按稳定 ID 升序分页。服务端把规范筛选组合的摘要、Workspace、Project、操作者、查询类型和分页边界绑定到新版游标。含新筛选时使用 v3，原筛选场景继续接受 v2。展示选项只决定返回字段。列表和看板维持各自页边界，刷新后可重读之前排序边界里的新匹配项。
- 后端先用现有 Project 作用域索引和新增标签绑定索引支撑查询。标题查询与多标签组合用代表性数据测量 PostgreSQL 查询计划、分页和查询次数，测量结果作为本批技术证据。

## 取舍与影响

固定类型便于在看板和列表使用共同语言，Project 标签由各项目按自己的工作内容维护。保留“未分类”让既有工单可按需逐步整理。归档保留已有标签，适合回看完成工作和追溯历史。标题检索沿用现有 PostgreSQL 和 Project 查询边界，分类相关查询与配置在同一版本交付。

## 迁移与验证

- 默认导出 `project-collaboration-v9`，继续使用 `format_version: 1`。v9 保存目录对象和目录历史、工单当前类型与标签集合、分类版本及变更。按每条历史验证引用、标签管理版本、分配时状态、工单最终分类和时间。导入映射标签 ID 及历史作者，源权限和原生命令 receipt 沿既有迁移策略。导入后的 Project 可继续改名、归档和分配。
- v1～v8 先按各自原字段、事件和对象白名单校验，再归一化为未分类、标签列表为空、分类版本 1，保留原 manifest digest 和两类 Ticket 创建历史。旧工件按各自 profile 验包。Go、Web、CLI、schema 028、Activity 16 和 v9 配套交付。
- 027 备份由匹配工件恢复到独立空目标后再迁移至 028。028 备份保留分类、标签、目录与工单历史、回执及成功审计。恢复后重建 Activity，复核分类命令原回执和各视图查询。

| 顺序 | 交付与验收 |
| --- | --- |
| 1. 类型和标签管理 | 接通目录、两条建单路径及 Ticket 分类详情。验证旧建单回执、事务回滚、只读成员、归档 Project、跨 Project 标签、并发改名和归档、完成工单重新分类与原操作重试。 |
| 2. 列表和看板查找 | 在 Project 中按标题、类型和多个标签组合查找，分页查看并进入详情；看板各列独立翻页，「我的工单」展示分类。验证全匹配、归档标签筛选、字面查询和筛选切换、撤权及迟到响应。 |
| 3. 取回和恢复 | 验证 v1～v8 严格读取、v9 双轮取回、027→028 与新旧工件匹配、空目标恢复、Activity 重建和原回执核验。用合成身份从正常 Web 入口走完建单、分类、查找、归档再筛选及数据恢复。 |

本批涉及数据库模型、写入权限、公共 HTTP 接口和上下文包 profile，于 2026-11-15 获项目所有者确认。真人观察与实测仍沿项目开发完成后的安排。 [当前执行顺序](../status/current.md#近期执行顺序)
