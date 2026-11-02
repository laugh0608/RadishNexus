# ADR-0048：Decision 生命周期与最小拒绝切片

状态：已接受；2026-11-02 所有者确认 D01～D07 及文末隔离验收范围；A 已实施，证据与限制见[实施记录](../status/reviews/2026-11-02-decision-rejection.md)。

日期：2026-11-02

核对基线：`08b3359`，schema 022 / Activity 10 / `project-collaboration-v3`。承接 [Decision 领域语义](../domain-model.md#decision)、[ADR-0004](0004-project-scoped-collaboration-permissions.md)、[ADR-0019](0019-session-scoped-thread-decision-ticket-transport.md)、[ADR-0022](0022-transactional-activity-and-incoming-relations.md)、[ADR-0040](0040-project-decision-discovery.md)与 [ADR-0044](0044-portable-project-context-package.md)。接受后增量替代 ADR-0019 / ADR-0040 的 Decision 公共读取仅支持 proposed / accepted，以及现行上下文包不支持 rejected 的限制；旧接受命令与来源授权合同继续成立。

## 背景与源码判断

讨论、Decision 提案 / 接受、Project 决策发现和后续 Ticket / Document 已有正式切片。成员尚不能记录“这个提案没有被采纳”，容易把否决留在正文，或误把取消工单当成撤销决策。

源码核对发现：

- [migration 001](../../server/db/migrations/001_golden_path_foundation.sql)已预留 rejected / superseded、rejection_reason / review_at 和替代关系约束，但这不是公共功能的交付证据。
- [接受与创建事务](../../server/internal/goldenpath/postgres/store.go)对同一 Decision 分别使用排他锁与共享锁；接受前要求 active Project、decider / admin 及全部 evidence 当前可读。接受 receipt 的 digest 只有规范化 outcome / rationale，没有状态版本。
- [协作 DTO](../../server/internal/platform/httptransport/collaboration.go)、[发现 DTO](../../server/internal/platform/httptransport/discovery.go)、Web [协作 adapter](../../web/src/collaboration/api.ts)和[列表 adapter](../../web/src/workspace/project-decisions-api.ts)只承认 proposed / accepted；详情 UI 对非 proposed 的分支直接显示接受结果与创建 Ticket，不能直接放行第三种状态。
- [提案重试](../../server/internal/platform/httptransport/collaboration.go)已区分首次 201 与重试 200，后者读取同一 Decision 的当前状态；新增拒绝后须继续成立。
- [上下文包类型](../../server/internal/contextpackage/types.go)没有 rejection_reason；[校验器](../../server/internal/contextpackage/validate.go)只允许 proposed / accepted，并逐项核对来源、确认主体、时间、历史数量及 Ticket 的 accepted 来源。新增状态必须同步导出、导入和二次导入。
- [Activity](../../server/internal/goldenpath/postgres/activity.go)只认识 decision.proposed / decision.accepted；普通写入、重建、导入来源和公共 Timeline 须共同支持拒绝事实。

以上是静态源码判断，未运行新功能、数据库或浏览器验收。

## 生命周期设计与切片边界

下表说明三类动作的产品含义。A 是本次请求确认的完整实现范围；B / C 仅提供后续设计约束，仍需独立 ADR 冻结存储、协议、并发与迁移后才可实现。

| 动作 | 触发与理由 | 来源和历史保留 | 本次边界 |
| --- | --- | --- | --- |
| A 拒绝提案 | 人工判断某条 proposed 不予采纳，明确填写 rejection_reason | 保留问题、提议人、原 evidence，记录实际拒绝人和决定时间；该 Decision 保持 rejected | 实现 proposed → rejected；与现有 proposed → accepted 竞争，终态均不重新打开 |
| B 替代已确认结论 | 需求、约束或证据变化，需要一条新的 accepted Decision 替代旧 accepted | 新 → 旧的 supersedes EntityLink 与旧状态变更原子提交；保留旧 outcome / rationale / deciders / decided_at，另记替代操作者、时间及理由来源 | 首个后续方案建议限同 Workspace / Project、一新一旧；须防自指、循环和旧结论被并发重复替代。替代理由的权威保存、版本和事件尚待独立冻结 |
| C 复核 | 人工发起、到达 review_at 或发现新证据，重新评估结论是否适用 | 复核记录须有发起依据、操作者、时间、证据和结论；维持原结论也要留下复核事实，改变结论则走新提案 / 替代 | 复核是独立评估事实，不增加 reviewing 状态；到期不自动撤销 accepted、不自动确认新结论。复核记录模型、调度与通知未纳入 A |

B / C 的写入均应复核当前决策权限与所涉 evidence；替代双方分别授权，关系不授予权限。不可读的前后结论及证据继续按当前目标权限隐藏，不能通过链路、计数或到期提示泄漏。

accepted 不能通过拒绝改成 rejected。rejected 不能被接受、编辑理由或重新打开；如需重新考虑，从原讨论提出新 Decision，原拒绝记录保留。A 不新增提案内容 / evidence 编辑、替代、复核、批量决策、待我决策、通知或自由工作流。

## 已确认决定

| 编号 | 推荐方案 | 影响与取舍 |
| --- | --- | --- |
| D01 状态与业务边界 | A 只增加 proposed → rejected；accepted / rejected 在本片均为终态，保持原问题与来源 | 不把拒绝当删除或取消工单；替代与复核按上表保留设计约束，不一并实施 |
| D02 当前权限 | 拒绝与接受相同：当前 active Workspace 成员、active Project 的 decider / admin，并可读全部现有 evidence；必须明确 confirmed=true | 提议人、contributor、Workspace owner 身份均不单独授予拒绝权；现有 Decision / Ticket / Document 读取权限不变 |
| D03 并发与重试 | 接受 / 拒绝在同一 Decision 排他锁下检查 proposed；保留旧接受 body、digest 和 receipt，不新增 Decision 状态版本 | 无编辑 / 重开 / 回转时，状态与不可变内容足以拒绝旧确认；后续放开这些动作必须先冻结版本合同 |
| D04 写入与理由 | 新增唯一 POST rejection 子资源；理由必填，trim 后保存为纯文本；receipt、事件、Outbox、Activity 同事务 | 拒绝理由是 Decision 可读内容，不是私密 evidence 的正文备份；Activity 不复制理由 |
| D05 正式读取与操作 | 所有正式 Decision DTO 支持三个状态，新增 nullable rejection_reason；Nexus View 增加服务端计算的接受 / 拒绝 / 创建 Ticket 能力 | Project 列表只扩展状态，原字段、ID 排序、cursor 和无筛选合同不变；Go / Web 必须成套升级 |
| D06 历史兼容 | 新导出默认 project-collaboration-v4，严格读取 v1 / v2 / v3 / v4，旧包的 Decision 理由转换为 null | 不向旧 profile 塞新字段 / 事件，不降级丢弃拒绝历史；receipt 仍只进入权威整库备份 |
| D07 迁移与完成线 | 计划新增 migration 023、Activity 11；不新增依赖；完成定向工程、真实 PostgreSQL、恢复 / 往返与正式 Web 验收 | 真实实例升级和隔离长驻资源分别授权；不以本片通过宣布完整 Golden Path、团队持续使用或 M1 退出 |

## A：拒绝写入与权限合同

唯一新增写入端点：

`POST /api/v1/workspaces/{workspace_id}/decisions/{decision_id}/rejection`

请求示例仅用于协议评审：

```json
{
  "client_operation_id": "browser-generated-printable-ascii",
  "rejection_reason": "当前方案无法满足已确认的约束，暂不采纳。",
  "confirmed": true
}
```

只接受以上三个必填字段；拒绝未知 / 重复字段、null、额外 JSON 值及任何 query。operation ID 沿用 1..128 bytes printable ASCII `!`～`~`；理由必须为合法 UTF-8、无 NUL，原始值和 trim 后值均不超过 32 KiB，trim 后非空；body 上限 64 KiB。confirmed 必须为 true，只表达本次人工提交，不进入领域表、digest 或事件。客户端不能提交 actor、role、Project、时间或 import 来源。

HTTPS Host、TLS / trusted proxy、Session、精确 Origin、双提交及存储态 CSRF、服务端 Principal / correlation、错误 envelope 与缓存头沿用 ADR-0019。新增端点的错误 / 成功均为 `private, no-store`、`Vary: Cookie`；其它方法返回 405 与 `Allow: POST`。

每个新请求与精确重试均先复核当前准入、Project 可读性 / active / 决策角色和全部 evidence。Session 失效为 401；对象未知、跨 Workspace 或不可发现为同形 404；可读但缺决策角色或 evidence 不全可读为通用 403；可读 Project 已归档为 409。沿用接受动作的先 active、再角色、再 evidence 判定顺序；不返回不可读 evidence 的 ID、类型、数量或失败详情。A 沿用正式 derived-from → Thread 来源 profile，不新增 evidence 类型；空 evidence 或无法识别的来源不得形成成功，内部错误不能当成无权或空集合。

拒绝只更新 status、rejection_reason、decider_ids、decided_at、updated_at。新原生 rejected 的 deciders 精确为本次当前用户一人，outcome / rationale 保持 null；保留 question、proposer、created_at 与全部原来源关系。理由在详情以纯文本显示，不能经自动摘要代填为已确认事实。

理由继承 Decision 的 Project 可见性，不建立字段级私密权限。UI 应提示填写可向该 Decision 读者公开的理由，使用结构化 evidence 指向原材料，不复制私密来源正文；这不改变现有“能读 Decision 不等于能读 evidence”的边界。

## A：事务、旧确认与精确重试

接受与拒绝共享同一对象锁及授权顺序。实施时将相关路径统一为：Workspace admission → 不可变 governing Project 定位 → 当前账户 / membership、Project / 角色授权 → Decision 排他锁 → evidence 授权 → receipt → 新状态校验 → 业务与投影提交。Decision → Ticket 创建也按同一 Project 在 Decision 共享锁之前授权，避免与管理命令形成反向锁定；其贡献角色与 accepted 来源规则不改变。权限管理与这些写入沿用同一 admission 边界串行化，普通 GET 不取得 Decision 排他锁。

本片的并发依据是锁内的 proposed，成立前提是问题 / evidence 无编辑入口、proposed 无恢复入口，accepted / rejected 无回转入口。不同 operation 的接受 / 拒绝、双接受或双拒绝至多一次成功，其余 409，即使两次理由相同也不能冒充同一成功。已拒绝后送达的旧接受，以及已接受后送达的旧拒绝均冲突；失败不留下 receipt、事件或半更新。

不增加 base_status_revision，也不修改旧 decision.accept digest。若未来加入任何可能让用户所见内容变化或状态回转的动作，必须先补齐版本 / 所见内容绑定和旧确认兼容；不能沿用本片前提直接开放。

新 command kind 为 `decision.reject`，receipt scope 沿用 `(workspace_id, actor_id, command_kind, target_type=decision, target_id, client_operation_id)`。canonical SHA-256 输入精确为单字段 JSON `{"rejection_reason":"trim 后理由"}`，不包含 confirmed、身份、时间或 request ID。result_type=decision、result_id=target_id、result_revision=null。

- 当前授权通过后，先匹配 receipt 再检查新状态。相同 scope / digest 返回原成功对象；scope 相同但理由改变返回 409，不覆盖旧事实。
- 首次与精确重试均返回 200，`data` 为下节定义的完整 rejected Decision DTO。重试交叉核对原 receipt、事件、Decision ref、Project、actor、时间、当前 rejected 状态与理由；缺失或损坏返回通用 500，不能仅因对象已 rejected 就判定成功。
- accepted / rejected 在 A 不再变化，因此读取当前终态可确认原操作。旧 acceptance 重试继续沿原 digest 返回原 accepted 对象，不会再次接受；换 operation 的同值请求仍冲突。
- 旧提案 receipt 仍返回同一 Decision 的真实当前状态：首次 201 必须 proposed，200 可为 proposed / accepted / rejected；服务端与 Web 均需验证，不能创建副本或伪造 proposed 快照。
- receipt 不出现在 DTO、事件、Activity 或上下文包；权威整库备份保留 receipt。网络 / 5xx 结果不明时，页面只允许以原 payload / operation 显式重试，不自动换 ID、改理由或改发接受。

首次接受 / 拒绝的决定时间均在取得 Decision 锁后取数据库时间，按微秒精度严格晚于此前 updated_at；decided_at、updated_at、receipt created_at 和新事件 occurred_at 相同。旧事件 / receipt / 时间不重写，精确重试不取代原成功时间。

## A：公共读取、能力与 Web

所有完整 Decision DTO（Nexus View Current、提案响应、acceptance / rejection 响应）增加必填 nullable `rejection_reason`，状态扩展为 proposed / accepted / rejected。其它原字段保留。

| 状态 | outcome / rationale | rejection_reason | deciders / decided_at |
| --- | --- | --- | --- |
| proposed | null / null | null | 空数组 / null |
| accepted | 沿原接受合同必填 | null | 沿原合同保留；A 的原生接受由一人执行 |
| rejected | null / null | 非空纯文本 | 一位实际拒绝人 / 决定时间 |

完整读取继续以 Decision 自身当前 Project 权限为准，不要求全部 evidence 可读；来源单独过滤，只有写决策要求 evidence 完整可读。非法状态 / 字段组合使整对象或整页失败；superseded 仍未开放，不映射为 accepted，不静默过滤。

Decision Nexus View 在 Current 的同一读取快照新增必填 `capabilities: {accept: boolean, reject: boolean, create_ticket: boolean}`，与 Ticket 现有 capabilities 分支按对象类型区分：

- accept / reject 为 true 仅当 proposed、active Project、当前 decider / admin 且全部 evidence 可读；无法读取 evidence 时两者为 false，保留可读 Decision 和 restricted 占位。内部查询错误不得伪装成 false。
- create_ticket 为 true 仅当 accepted、active Project、当前有贡献能力；不额外要求来源 Thread evidence 可读，不扩大或缩小既有 Ticket 创建权限。
- 能力只控制操作展示，不是授权凭据。Thread 不增加该字段；其它对象按既有合同返回，跨类型能力组合必须被 adapter 拒绝。

Project Decision 列表仍为 `ref / title / status`，只扩展 rejected 与“已拒绝”文字；保留稳定 ID 升序、原 cursor、不返回总数、无状态筛选。Thread 的反向 Decision 关系、Ticket / Document 来源和其它仅引用 Decision 的摘要维持 ref / title 及当前权限，不复制拒绝理由。

Web 详情按三状态分别渲染：proposed 按能力显示接受或拒绝动作；拒绝填写理由、展示问题 / 来源及本次理由后明确确认；accepted 保留原结论及有权时的创建 Ticket；rejected 显示理由、拒绝人、时间和可读来源。无权时只展示可读事实，不能把“待确认”等同“待我决策”。提交期间接受 / 拒绝互斥，改动作或改理由即清除旧确认。

409 清理旧确认并重读权威状态，不能把接受的未决请求自动转换为拒绝。结果未知保留原请求供显式重试；成功后重读当前 Nexus View。页面切换、Workspace 切换、焦点 / 可见性恢复、pagehide / persisted 恢复、失权清空、取消与迟到响应保护复用已有交互模式。输入、错误与状态有文字和语义标签，覆盖键盘与窄屏。

## A：事件、迁移与上下文兼容

每次首次拒绝新增 schema 1 `decision.rejected`，payload 严格为 `{"status":"rejected"}`，Decision 为 primary、governing Project 为 scope、当前用户为 actor、web 为 source。理由保存在终态 Decision，不复制到事件、Activity、错误或日志。正常写入的 Decision、receipt、不可变事件、Outbox、Activity 同事务提交；投影失败全部回滚。

新事件只投影到 Decision Timeline，无 subject，安全事实只为 rejected；不扩散到 Thread / Ticket / Document，不生成通知，不影响 Ticket、Document、CI Run 或 Deployment。Activity 计划从 10 升至 11，正常写入与全量重建使用同一新分支，旧 proposed / accepted 事件保留原 schema 和事实。

计划新增 forward-only migration 023；沿用现有 rejection_reason 列，不新增 Decision 状态版本或独立历史表。迁移须：

1. 核对旧 Decision 均符合旧正式 proposed / accepted 合同且无 rejection_reason；出现 rejected / superseded 或非法字段组合时拒绝迁移并明确诊断，不转换或删除数据、不伪造事件。
2. 增加 / 收紧三个正式状态的字段不变量，扩展 decision.reject receipt 的 kind / target / result 约束，并用 deferred 校验关联原生拒绝事件、actor、Project、时间与结果 ref。旧接受 receipt 及 digest 不改写。
3. 约束原生 decision.rejected 事件有精确拒绝 receipt，状态、决定主体 / 时间与其一致；导入历史只走既有 context.history-recorded 分支，不能伪造原生 web 事实或补造源 receipt。扩展导入事件白名单、facts 校验及 Activity 版本，不弱化旧检查。

新 profile 为 `project-collaboration-v4`，format_version 继续为 1；在 v3 基础上只新增 Decision 的必填 nullable rejection_reason，以及 decision.rejected 历史事件。Ticket 的状态 / 负责人 / 优先级合同、Document 连续版本、来源链与无凭据 / 无授权闭包不变。

- v1 / v2 / v3 先按各自精确字段与事件白名单校验，再将 Decision 理由转换为 null；旧 Ticket 的既有转换保留。旧 profile 不得带 rejected、新理由字段或拒绝事件；原 manifest / 输入 digest 不变，转换不制造历史。
- v4 Decision 必须恰有一条 proposed 创建事件，终态再恰有一条 accepted 或 rejected，二者互斥。校验原来源、proposer、decider、decided_at、updated_at 和全局时间次序；理由与状态匹配、决定时间不早于创建时间。新原生接受 / 拒绝按上述锁内取时严格递增；旧合法 accepted 可能与创建时间相等，升级后导出 v4 仍应保留并接受，不能因换 profile 追加时间差要求。旧包仍按各自原时间合同验证。
- Ticket 的 implements 来源必须是 accepted 且确认时间不晚于 Ticket 创建；rejected 不允许有源自它的 Ticket / 下游 Document 执行闭包。新状态不能通过导入伪造已接受来源；已有合法 accepted → Ticket → Document 完整保留。
- 导入后 rejected 与历史理由保持原值，主体仍是不可登录的历史身份；不会使目标成员刚执行拒绝，不分发历史 Outbox。导入 proposed 后的新原生拒绝必须由当前有权成员显式执行。二次导出 / 导入与 Activity 重建保持拒绝来源链。
- 新服务读取 v1～v4，新导出默认 v4；旧服务拒绝 v4，不提供丢弃拒绝历史的降级导出。Go / Web / CLI / schema 配套升级，精确 readiness 不放宽、不宣称滚动兼容。

实际升级前按 [ADR-0043](0043-single-host-upgrade-failure-recovery.md)保留匹配旧工件的备份；失败时 forward repair，或将匹配旧备份恢复到空目标。新拒绝写入后的数据不能直接交给旧应用；恢复旧备份会丢失备份后的写入，须另行核对与确认。真实实例操作不由本设计授权。

## 验收矩阵与实施顺序

本表定义验收要求；实际执行结果与限制见[实施记录](../status/reviews/2026-11-02-decision-rejection.md)。运行资源与真实环境的授权单独说明。

| 编号 | 必须覆盖的结果 | 主要证据层 |
| --- | --- | --- |
| R01 正式主链 | 正式账户 / Message → Thread → proposed；拒绝后从 Project 找回，详情、来源和 Timeline 一致；原 accepted → Ticket → Document 仍可用 | PostgreSQL + HTTP + Web |
| R02 决策权限 | viewer / contributor / owner 无隐式拒绝权；decider / admin 须读全部 evidence；只读 Decision 不因缺 evidence 消失；公开 / restricted、跨 Workspace、归档、停用 / 暂停 / 退出均按合同拒绝 | PostgreSQL + HTTP |
| R03 来源隔离 | 私密 Channel / Thread 撤权使接受 / 拒绝能力清空、写入和重试被拒；关系 / Timeline / 能力 / 错误无隐藏 ID、类型、数量；Ticket 写权限保持独立 | PostgreSQL + Web |
| R04 竞争与旧确认 | 双拒绝、双接受、接受与拒绝两种先后、同 operation 并发、撤权与写入均有确定锁顺序；至多一项真实决定，其余冲突；终态不能互转或重开 | 确定性 PostgreSQL 并发 |
| R05 精确重试 | 原 / 新 acceptance、rejection 首次与同 payload 重试；变化理由冲突；提案 200 返回同一 rejected；丢响应不重复事件；损坏 receipt / event 不伪造成功 | Go + PostgreSQL + HTTP |
| R06 Transport | 三字段严格解析、缺失 / 重复 / 未知 / null / 空白 / 非法 UTF-8 / NUL / 超限理由、confirmed、query、CSRF / Origin / Session、405 / Allow / 缓存头；三状态 DTO、能力与状态组合、未知状态整页失败 | HTTP + Web adapter |
| R07 原子性 | 更新、receipt、事件、Outbox、Activity 任一步故障整单回滚；接受 / 拒绝时间在锁内确定；原生 / 导入事件不能互相伪造 | PostgreSQL 故障注入 |
| R08 升级恢复 | 022→023；旧行、旧事件和旧 receipt 不改；非法旧状态拒绝迁移；投影重建一致；权威备份恢复保留理由 / receipt，旧工件空目标恢复与新 schema readiness 按合同拒绝错配 | 真实 migration + 备份恢复 |
| R09 包兼容 | 严格 v1～v4、旧包夹带新字段 / 事件拒绝、v4 双终态 / 缺历史 / 理由不匹配 / 时间与作者错配拒绝；accepted 执行来源、rejected 无执行来源、导入 proposed 后原生拒绝及两轮往返不丢来源 | 解析 + PostgreSQL + CLI |
| R10 正式交互 | 普通成员经 Project 进入；三状态与能力正确，拒绝理由 / 确认、双标签冲突、网络未知、页面恢复、迟到响应、撤权清空、桌面 / 手机 / 键盘分别记录 | Web 自动化 + 隔离浏览器 |

实施依次完成数据 / 事务与历史约束、HTTP / 所有读取与 Activity、v4 迁移往返、正式 Web 及定向验证；各层使用同一合同，不把内部拒绝 service 单独报告为功能已交付。按实际风险执行 Go / race / vet、真实 PostgreSQL、Web 类型 / 测试 / Lint / build 和仓库检查；独立操作者、真实团队持续使用与生产容量仍单列。

完成线仅为“有权成员能明确拒绝待确认提案，并从正式入口追溯理由、主体、时间和来源；接受路径、权限、重试与可移植历史保持一致”。

## 替代方案与后果

| 方案 | 判断 |
| --- | --- |
| 拒绝就是删除提案、取消 Ticket 或覆盖 accepted | 丢失未采纳结论或混淆执行事实，不采用 |
| 提议人或 contributor 可拒绝本人提案 | 更接近撤回而非决策；当前角色已有 decider / admin，新增撤回语义留待真实需求 |
| A 同时上线替代、复核和重新打开 | 需要新历史记录、并发版本、替代链与包格式，超出本片；按 B / C 独立冻结 |
| A 先增加版本再改旧接受请求 | 当前无内容修改和状态回转，无真实 ABA；会增加旧 digest / receipt 兼容成本。本片用锁内状态检查，未来放开前先补版本合同 |
| 只扩展数据库 / 按钮，导出时跳过 rejected | 正式列表会失败或历史丢失，不能构成完整切片 |

代价是正式 DTO / 能力与上下文 profile 的配套升级，以及拒绝权限、导入历史和恢复的新验证面；收益是保留“为什么没有采纳”的一等事实，且不干扰已建执行对象。

## 确认与授权边界

2026-11-02 所有者明确回复“确认，继续推进”，确认 D01～D07 并授权实施 A 及下述隔离验收。实施同步领域模型、决策基线、核心契约与实际实现说明；B / C 继续保持待设计。

确认实施 A 不自动包含提交、push、PR、真实实例升级、依赖安装、新增长驻服务 / 容器 / 桌面浏览器进程或系统配置变更。

本次已明确授权的隔离验收范围如下：复用现有测试脚本、已安装工具 / 缓存与已有固定镜像，创建本任务专属的临时 PostgreSQL 容器 / 空测试数据库、仅绑定 loopback 的 Go / HTTPS fixture 和独立 Chrome profile；只使用合成数据，并将并发、回滚、备份 / 恢复及导入故障限制在这些资源内。会临时占用本机端口、CPU、内存与磁盘；缺少工具 / 镜像时报告，不自动安装或拉取，不改系统证书信任或个人浏览器 profile。结束后按本次记录的资源 ID 停止并精确清理新建容器、卷、fixture 进程、临时包和浏览器 profile，保留必要脱敏证据；既有容器 / 数据库 / 真实实例不在操作或清理范围。
