# ADR-0047：Ticket 最小优先级与项目内筛选

状态：已接受；2026-10-28 所有者确认 D01～D07；已实施，验收范围见[记录](../status/reviews/2026-10-28-ticket-priority.md)

日期：2026-10-28

核对基线：`ec1caf3`，schema 021 / Activity 9 / `project-collaboration-v2`。承接 [ADR-0038](0038-project-ticket-discovery.md)、[ADR-0039](0039-ticket-status-transitions.md)与 [ADR-0046](0046-ticket-assignee.md)。本决策仅增量替代 ADR-0038 对 Ticket 列表“不筛选”的限制；稳定 ID 排序、当前权限和页面恢复原则继续成立。所有者已确认本 ADR 的数据与公共协议影响，授权按下述范围实施。

## 背景与源码判断

成员已经能从 Project 找回工单、分配负责人、改变状态和继续 Document。下一项实际缺口是表达处理优先级，并在当前 Project 找出例如“由我负责、尚未完成、高优先级”的工单。首片沿用现有列表与详情，不建立跨 Project 任务中心。

核对发现四个必须共同处理的边界：

- [列表查询](../../server/internal/goldenpath/postgres/discovery.go)在单次 repeatable-read 事务中授权，使用 `id > after_id` 与 `limit + 1`；筛选必须进入 SQL，再截取分页，不能在浏览器对一页数据过滤。
- [HTTP discovery](../../server/internal/platform/httptransport/discovery.go)只有严格的 `limit / after` query；现有 Ticket cursor 不含筛选条件或操作者。加入“由我负责”后，必须绑定当前身份与查询条件。
- [Web 列表](../../web/src/workspace/ProjectTicketsPage.tsx)已有作用域、焦点恢复、旧请求取消和首页重读；应延续同一生命周期，不建立持久化结果缓存。其 adapter 严格检查字段，新字段要求 Go / Web 成套交付。
- [上下文包校验](../../server/internal/contextpackage/validate.go)严格校验 Ticket 字段、状态与分配历史，并核对最终值及更新时间；优先级字段和事件不能直接塞入 v2 或在导出时丢弃。

## 已确认决策

| 编号 | 推荐方案 | 影响与取舍 |
| --- | --- | --- |
| D01 优先级 | 固定 `unset / low / normal / high / urgent`，显示“未设置 / 低 / 普通 / 高 / 紧急”；旧工单与新建工单默认 unset | 不自动推断普通或紧急，不引入 P0 含义、事故严重性、SLA、自定义级别或自动执行顺序 |
| D02 谁能修改 | 当前 active Project 的 contributor / decider / admin，在 open / in-progress 工单上显式修改；终态保留结项优先级，需重新打开后修改 | 与负责人动作边界一致；不增加只有负责人或 admin 才能修改的限制，优先级不授予权限 |
| D03 版本与历史 | 独立 `priority_revision`，绑定所见优先级与状态版本；沿用 receipt、原子事件 / Outbox / Activity 与精确重试 | 负责人变化不导致无关优先级冲突；状态 A→B→A 和优先级 A→B→A 均能识别旧确认 |
| D04 筛选范围 | 只在当前 Project 提供状态、负责人、优先级三组筛选，组间 AND；各组单选 | 状态可选全部 / 未结束 / 四状态之一；负责人只选全部 / 由我负责 / 未分配；优先级选全部或一个级别。暂不增加人员目录、指定他人、标签、全文搜索或保存视图 |
| D05 默认与分页 | 默认全部工单，维持稳定 ID 升序；筛选变化回首页；新 cursor 绑定查询与操作者 | 不默认隐藏终态。首片不按优先级或更新时间排序，避免可变排序键导致跨页重复；筛选跨页仍不是冻结快照，见下文 |
| D06 Web 状态 | 筛选与 cursor 栈仅保存在当前页面内存；同页刷新保留筛选并回首页，重新进入 / 整页重载重置为全部 | 不新增 URL query、浏览器持久化或可分享筛选视图；界面明确当前条件和重置入口 |
| D07 历史迁移 | 新导出默认 `project-collaboration-v3`，严格读取 v1 / v2 / v3；旧包转换为 unset / 1 | 不修改旧 profile、不静默降级；新增 forward-only migration 和 Activity 投影版本，配套验证升级、恢复、二次导入 |

以上 D01～D07 是一个可交付切片的推荐范围。D05 若改为优先级排序，应先独立冻结复合排序键与并发分页语义；不能在实现时静默更换。

## 优先级数据与写入合同

Ticket 增加非空 `priority` 与 `priority_revision`：前者为 D01 封闭枚举，后者为从 1 开始的 PostgreSQL integer。升级旧行与正式创建均为 `unset / 1`，不生成虚构历史、不改变旧 `updated_at` 或旧创建事件。unset 表示尚未表达优先级；normal 是一次明确选择，两者不能混同。

读取和写入沿用负责人入口的形态：

`GET / POST /api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/priority`

- GET 返回 `data: {priority, priority_revision, status_revision, can_set_priority}`，一次读取按当前 Ticket 权限和 Project / Ticket 状态计算能力；版本上限也使新写入不可用。归档仍可读，优先级不影响 Ticket 原有可见性。
- POST 严格必填 `client_operation_id / base_priority_revision / base_status_revision / priority / confirmed`；confirmed 必须为 true，版本为 1..2147483647 整数。拒绝 query、额外或重复 JSON 字段、null、未知枚举，沿用 8 KiB body 上限及现有 HTTPS / Session / Origin / CSRF 合同。
- 新命令为 `ticket.priority.set`。receipt 使用既有操作者 / Workspace / Ticket / command / operation scope；digest 只含两个 base 版本与目标 priority。不同 payload 复用 operation 返回 409。
- POST 首次成功或精确重试返回 `data: {ticket: {type,id}, applied: true, applied_priority, applied_priority_revision}`。这是原操作事实；Web 随后重读当前对象，不能用 applied 值覆盖可能更新的状态。
- 新请求复用 Workspace admission / Project 授权 / Ticket 排他锁顺序，先检查当前操作者准入和写权，再匹配 receipt。无权、归档、Session 失效与未知对象的错误边界沿用状态 / 负责人命令；receipt 不恢复旧权限。
- 对新操作，base 状态或优先级版本变化、同值设置、终态、版本到达上限均冲突，不留下 receipt 或事件。精确重试在当前授权通过后确认原成功事实，不因后来完成工单或优先级已再次改变而重新执行。
- 有效变化同事务更新 priority、priority_revision、updated_at，并写不可变事件、receipt、Outbox、Activity。时间在 Ticket 锁内确定，使用数据库微秒精度，严格晚于此前 updated_at；状态、分配与优先级共享时间次序，各自版本独立。
- 新事件 `ticket.priority-changed` schema 1，payload 为 `priority: {from, to, from_revision, to_revision}`，不复制正文或权限。Activity 只面向当前可读 Ticket，支持正常写入和投影重建，不自动通知或影响 CI / Deployment。

Project 工单列表在现有 `ref / title / status / assignee` 上增加必填 `priority`，不增加写能力或版本材料。详情使用上述专用 GET；创建响应、Component 反向 Ticket 摘要和其它仅引用 Ticket 的入口维持既有字段职责，不强制所有摘要承担优先级展示。所有新增展示入口使用同一封闭枚举，非法值使整对象 / 整页失败，不伪造 unset。

Web 详情显示当前值，选择目标后明确展示前后值并二次确认；409 清理旧确认并重读，结果未知仅精确重试原 payload。Workspace / Ticket 切换、恢复、失权及迟到响应保护复用现有动作模式。其他成员更改负责人不隐式重写优先级；完成工单与优先级竞争按实际锁顺序串行化。

## 筛选、游标与权限合同

继续使用唯一 Project 工单列表端点，不新增全局查询入口：

`GET /api/v1/workspaces/{workspace_id}/projects/{project_id}/tickets`

| Query | 允许值 | 省略含义 |
| --- | --- | --- |
| `limit` / `after` | 沿用 1～50、默认 25 与 opaque cursor | 首页 |
| `status` | `active / open / in-progress / done / canceled` | 全部状态；active 精确等于 open 或 in-progress |
| `assignee` | `me / unassigned` | 所有责任记录 |
| `priority` | `unset / low / normal / high / urgent` | 全部优先级 |

每个参数只能出现一次；未知键、重复、空值、未知枚举及额外排序参数均返回 400。省略与合法值是唯一两种形式，不接受 `all`、多值列表或客户端传入 user ID。沿用 2048 bytes query 上限。

`me` 仅由服务端当前 Principal 解析为用户 ID；它表达记录中的负责人是本人，不暗示当前具备执行资格。不附加 active 状态或 eligible 条件：筛选 done / me 可以查看本人结项记录，失去写权但仍可读时也能查看本人责任记录。unassigned 仅为负责人 SQL NULL，不把已退出、已停用或历史导入负责人误归为未分配；imported 负责人仍在全部结果中。所有筛选仍先服从路径 Project 当前权限。

SQL 在同一次授权快照中应用全部条件、`id > after_id`、`ORDER BY id` 与 `LIMIT limit+1`；不先取一页再过滤，不用前端角色、当前候选集合或来源 Thread 权限代替 Ticket 权限。空结果明确为“当前条件下没有可显示的工单”，不可读仍是 404，无总数、隐藏计数或全局筛选选项统计。

Ticket 列表使用 cursor v2，规范字段为 `v / workspace_id / project_id / kind / actor_id / status / assignee / priority / after_id`。kind 固定 ticket，三项筛选省略时在 cursor 中规范化为空字符串；所有字段必填，拒绝未知字段、重复字段与非规范编码。绑定解析后的当前操作者与同一规范筛选，不绑定页大小；合法换页可以改变 limit。查询条件或身份与 cursor 不符返回 400，重新授权仍独立执行；cursor 不是授权凭据。

不改动其它 discovery / 候选 / 反向关系 cursor。旧 Ticket v1 cursor 明确失效，客户端重新从首页读取；不把缺少筛选条件的旧 cursor 默认为新条件。Go / Web 成套升级时丢弃旧内存分页，服务端不会用兼容 fallback 隐藏协议差异。

固定集合下分页应完整且无重复。跨页并发改变状态、负责人或优先级，会让记录加入或退出匹配集合；已越过 ID 的新匹配项需要回首页读取，不能保证一次遍历包含并发期间的所有匹配项。稳定 ID 不移动，故不因修改优先级而把已读项重新排入后页。UI 明示稳定标识排序，并提供“刷新当前筛选”与“重置筛选”。

## Web 生命周期

在现有 Project 工单列表加入三个有标签的单选控件。条件变化立即取消请求、清理旧列表和 cursor 栈，从首页重新读取 Project 与筛选结果，使用同一请求代次；切换条件后的迟到响应不能恢复旧行或旧项目名称。筛选控制值可保留，不保留未重新授权的业务数据。

同页手动刷新、焦点 / 可见性恢复和 persisted 页面恢复保留当前条件并回首页重读；普通文档导航后重新挂载、整页重载或切换 Workspace / Project 使用默认全部条件。pagehide 清空数据并取消请求；返回页面必须重新授权，不恢复旧结果快照。详情返回列表仍使用既有 canonical 链接，不新增任意 return URL。精确 BFCache 命中与恢复分支测试应区分记录。

列表展示状态、负责人和优先级文字，不仅依赖颜色。只读成员可使用全部筛选，但不会由此获得候选目录或写动作；归档 Project 标记只读。窄屏、键盘、空态、错误重试和清空筛选均属于首片交付。

## 历史、导入导出与部署影响

新增 migration 022，Activity 投影从 9 升至 10，扩展正式事件、receipt 约束与投影重建。旧 migration 不改写，不引入依赖。查询索引先复用现有 Workspace / Project / ID 索引，以代表性筛选选择率的 EXPLAIN 和测量决定是否在新 migration 增加索引，不预先穷举三组条件的组合索引。

新 profile 为 `project-collaboration-v3`，仍使用 `format_version: 1`。在 v2 Ticket 五字段基础上增加必填 priority、priority_revision，并允许优先级事件；保持作者、关系、Document revision 与无凭据 / 无授权的既有闭包。优先级是工单历史事实，导入后原值保留，不需要映射为目标实例自定义配置。

- v1 / v2 必须先按各自精确旧字段和事件白名单校验，再转为内部 unset / 1；v1 同时继续映射未分配 / 1。不能允许旧 profile 携带新字段或事件，不能把内部转换结果当成源 manifest digest。
- v3 从 open / 1、未分配 / 1、unset / 1 开始分别重放三种历史；每种前后版本相邻，from 值匹配、禁止同值事件，优先级 / 分配事件发生时必须为非终态。验证全局时间次序与 Ticket 最终三个状态值 / 版本及 updated_at；旧包转换不制造优先级事件或改写时间。
- 导入的历史 actor 保持不可登录，事件来源与目标原生新事件区分；后续修改优先级必须当前有权成员显式操作。二次导入必须保留历史链，不能只迁移最终标签。
- 新服务读取 v1 / v2 / v3，新导出默认 v3；旧服务拒绝 v3，不提供丢弃优先级的降级导出。过滤条件、cursor 和 receipt 不进入项目上下文包；receipt 仍进入权威整库备份。
- Go / Web / CLI / schema 成套交付，精确 readiness 不放宽，不宣称滚动兼容。升级前保留匹配旧工件的备份；失败恢复到匹配旧 schema 的全新目标，或 forward repair，不删除 migration history。真实实例升级另行授权。

## 验收与退出条件

实际执行层次与剩余限制见[实施记录](../status/reviews/2026-10-28-ticket-priority.md)，以下是合同矩阵，不将单项自动化等同全部浏览器或生产环境验收。

| 范围 | 必需证据 |
| --- | --- |
| 主链 | 正式 Message → Thread → Decision → Ticket，默认 unset，设置 / 修改 / 清除优先级，列表与详情一致；开始 / 完成 / 重开保持边界 |
| 权限与历史 | viewer、公开 Project 无写角色成员、restricted 无成员 owner、跨 Workspace、归档、暂停 / 退出 / disabled；私密来源不扩权；只读 me 不隐式附加资格 |
| 原子与并发 | 同版本双写、优先级和状态 A→B→A、完成与优先级双方先发生、撤权与写入、独立负责人变更、版本上限、未知结果精确重试；event / receipt / Activity 故障回滚 |
| 筛选与分页 | 三组单选及 AND 组合、active 两状态、未设置与普通区分、me / unassigned / 历史负责人区分、0 / 25 / 26 / 多页、limit 1 / 50、固定集合无重漏；跨页变化与撤权重新校验 |
| Transport | 严格 body / query / DTO，缺失 / 重复 / 未知字段，外来身份 / 条件 / Project / Workspace / kind cursor、旧 v1 cursor、伪造边界、错误与缓存头 |
| 迁移与恢复 | 021→新 schema、旧行默认无虚构事件、投影重建、备份恢复、严格 v1 / v2 / v3、缺失字段与损坏历史、二次导入及导入后的新原生优先级动作 |
| 正式 Web | 普通成员从 Project 筛选进入详情；桌面 / 手机 / 键盘、切换条件、翻页、重置、重载 / 后退 / 恢复、失权清空、迟到响应、双标签冲突与网络未知 |
| 工程 | 定向 Go / race / vet、真实 PostgreSQL、Web 检查、仓库检查及代表性 SQL 计划；不以构建或单次性能测试替代真实运行证据 |

退出结论仅为“成员能明确工单优先级，并在当前 Project 按状态、本人责任和优先级找回执行事项；权限、历史和退出迁移保持一致”。不表示完整看板、可配置工作流、通知、生产容量、团队持续使用或 M1 退出。

## 替代方案与确认边界

- 所有旧工单默认 normal：会把尚未表达的优先级伪装成已确认判断，推荐 unset。
- 优先级可在终态修改：支持结项后整理，但改变结项事实预期；推荐先显式重开，与负责人一致。
- 首片增加按优先级排序、指定任意成员和 URL 保存视图：各自有实际价值，但会增加可变排序、人员发现与导航恢复合同，本片先按 D04～D06 范围交付。
- 只做前端筛选或不升级导出 profile：会漏掉后页结果或丢失历史，不采用。

2026-10-28 所有者明确回复“确认，继续推进”，D01～D07 已获确认并授权实施；该确认不自动包含提交、push、PR、依赖安装或真实实例升级。隔离运行资源按当次已明确的目标与清理范围执行。
