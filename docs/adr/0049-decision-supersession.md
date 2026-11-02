# ADR-0049：Decision 最小替代与历史兼容

状态：已接受；D01～D07 已实施，分层验收与限制见[实施记录](../status/reviews/2026-11-02-decision-supersession.md)。

日期：2026-11-02

核对基线：`6f9e8b6`，schema 023 / Activity 11 / `project-collaboration-v4`。承接 [ADR-0048](0048-decision-lifecycle-and-rejection.md) 的 B 方向；本 ADR 增量替代其 accepted 终态不变、接受重试只返回 accepted、三状态 DTO 和 v4 不含替代历史的限制，不重写原 ADR 或旧事件。Ticket、Document、成员资格和 evidence 继续使用既有权限。

增量 Supersedes：ADR-0048 上述生命周期 / 重试 / 读取与上下文限制；本次确认已生效，其余拒绝合同继续有效。

## 背景与首片选择

最小拒绝已实现，但已采纳结论发生变化时，还不能从正式入口说明“哪条新结论替代了哪条旧结论”。推荐先完成替代：它直接延续讨论—决策—执行的来源链；复核另需独立评估记录、到期语义和后续动作，不与替代同时实现。

设计基线源码中的 superseded 状态和 supersedes 类型只是预留。migration 001 只要求 superseded 有 active 入向替代关系，没有同 Project、唯一前后继、防环及完整事件约束；正式读取和 v4 仍拒绝该状态。接受精确重试要求当前 accepted，Ticket 创建在 receipt 前检查 accepted，包校验同样要求来源当前 accepted，不能只增加按钮和状态枚举。

本片采用已明确接受的新 Decision 替代已接受的旧 Decision。它不代替新提案的接受确认，不把一条 proposed 自动接受，不编辑或取消原结论，不自动转移工单。

## 已确认决定

| 编号 | 推荐合同 | 影响 |
| --- | --- | --- |
| D01 业务范围 | 同 Workspace / 同 Project，两条不同的 accepted Decision；新 → 旧建立不可变 active supersedes，旧变 superseded，新保持 accepted；每条至多一个直接前驱和一个直接后继，允许连续链 | 不合并 / 分叉，不跨 Project，不撤销替代，不重开 rejected / superseded；复核、到期、通知继续独立设计 |
| D02 理由与权限 | 理由保存在该 EntityLink 的严格 metadata；当前 active 成员、active Project decider / admin，双方全部 evidence 当前可读，并明确确认 | 不增加新业务表或字段级 ACL；理由只有双方 Decision 都可读才返回，不能复制私密 evidence 正文 |
| D03 版本与原子性 | 新增 decision_revision；proposed 为 1，接受 / 拒绝为 2，替代同时使双方各 +1；请求绑定双方版本，按 ID 排序锁住双方 | 不改旧接受 / 拒绝请求与 digest，不开放内容编辑；理由、旧状态、双方版本、关系、receipt、事件、Outbox、Activity 原子提交 |
| D04 历史与精确重试 | 旧接受精确重试核对原接受事实后返回真实当前 accepted / superseded；已建 Ticket 的原请求在当前贡献权限下仍可重试，新 operation 不能从 superseded 创建 Ticket | 旧确认不是重新接受，已有 Ticket / Document 不改状态、不换来源；导出校验创建时有效的接受事实 |
| D05 正式入口 | 完整 DTO 增版本与第四状态；详情从新 accepted 发起，复用 Project 列表选择旧结论，双方详情对照后确认；双向替代关系展示理由、操作者和时间 | 增 supersede / retry_decision / retry_ticket_creation 能力，区分当前授权与首次业务资格；列表仅扩展状态，不新增筛选、候选 API 或通用关系编辑器 |
| D06 事件与可移植历史 | 一条 decision.superseded 事件以旧 Decision 为 primary，显式投影到双方；Activity 12，默认 v5，严格兼容 v1～v4 | 新 profile 保留理由、双方版本、链与原接受 / 执行历史；不降级丢弃替代链，不向所有相邻对象传播 Activity |
| D07 迁移与完成线 | migration 024，无新依赖；旧合法数据只回填版本，不改事实时间、事件或 receipt；完成 R01～R10 后才交付 | Go / Web / CLI / schema 配套停写升级；真实实例、提交 / push、发布及运行资源分别按授权处理 |

## 状态、关系和理由

例如 A 被 B 替代，再由 C 替代 B，关系方向是 `C supersedes B supersedes A`。A / B 为 superseded，C 为 accepted；B 的原接受主体和时间始终保留。新结论可以先关联已有执行上下文，但不能已经用作另一次替代的起点；旧结论可以带有它此前替代的前驱。

单条边必须满足：

- from / to 均为 decision，Workspace 与 governing Project 相同，不能自指；
- 首次写入时双方均 accepted，from 没有已有 outgoing supersedes，to 没有已有 incoming supersedes；
- 每个 from / to 各至多一条此类边，链无环；后续 from 自身变 superseded 不删除历史边；
- asserted + user，由人工命令产生；导入仍使用正式历史来源和 disabled 历史身份，不伪造目标成员刚执行的命令；
- 不提供 remove / redirect / metadata 编辑入口，数据库拒绝删除、移除或修改该事实。

理由复用 EntityLink metadata，精确形状为 `{"supersession_reason":"规范化纯文本"}`。其它关系保留原 metadata 合同，不能借此开放任意 JSON。使用既有 link.created_by、created_at、source_event_id 表示操作者、时间和事件，不在 Decision 增另一套前后继 ID 或操作者列。理由按 Go TrimSpace 规范化，原始与规范化 UTF-8 均不超过 32 KiB、无 NUL、非空，拒绝未配对 JSON surrogate；写入后不可变。

旧 question、outcome、rationale、proposer、deciders、decided_at、created_at 和全部 evidence 保留；rejection_reason 仍为 null。只更新旧 status、双方 decision_revision / updated_at，并增加替代关系。new 的 accepted 代表其结论仍可用于新 Ticket，不代表替代命令再次执行了一次接受。

## 版本与写入协议

decision_revision 为正整数，上限 2147483647。它表达已确认状态及替代关系的变化：

- proposed 创建为 1；旧合法 proposed 的 updated_at 可晚于 created_at，迁移和 v5 兼容保留原时间；proposed → accepted / rejected 为 2，旧 schema 1 事件分别隐含 1→2，无需补字段或重写事件；
- 一次替代使双方各 +1。新结论虽仍 accepted，outgoing 事实和 updated_at 已改变，旧确认必须过期；
- rejected 恒为 2；没有替代的 accepted 为 2，作为替代起点后为 3；superseded 根据是否原有前驱为 3 或 4，必须与完整历史重放一致；
- 不开放提案编辑、evidence 编辑、accepted 内容编辑或状态回转；未来这些能力仍须独立设计版本合同。

唯一新增写入为 `POST /api/v1/workspaces/{workspace_id}/decisions/{old_decision_id}/supersession`。请求示例：

```json
{
  "client_operation_id": "web:unique-operation",
  "replacement_decision_id": "dec_new",
  "base_decision_revision": 2,
  "base_replacement_revision": 2,
  "supersession_reason": "新的约束已有证据支持，采用新结论。",
  "confirmed": true
}
```

只接受这六个必填字段，严格拒绝重复 / 未知 / null 字段、非整数版本、额外 JSON 值、query 和超限 body（64 KiB）。operation 沿用 1..128 printable ASCII；版本须在正整数上限内，首次实际变更遇上限则 409。Session、精确 Origin、存储态 CSRF、Host / TLS 与错误 envelope 复用原合同；所有响应 no-store，非 POST 返回 405 / Allow。

401 为失效 Session；不可读、未知或跨 Workspace 任一端点为同形 404；只有双方均可读时才返回跨 Project、状态、已被替代、已使用的起点、版本或归档 409；缺角色 / evidence 为通用 403，不携带来源 ID、类型或数量。先复核准入、可发现性和 active / 角色，再判断业务竞争；内部查询错误不得转换成无权限或空候选。

新 receipt kind 为 decision.supersede，scope 为当前 actor / Workspace / old Decision / operation。canonical SHA-256 按固定字段顺序编码 replacement_decision_id、base_decision_revision、base_replacement_revision、trim 后 supersession_reason，不包含 confirmed、时间或 actor。result_type 为 entity-link，result_id 为该 link，result_revision 为 null；不能改用不同目标或理由冒充原重试。

首次与精确重试均返回 200，data 只表达不可变的原操作事实：superseded / replacement 两个 Decision ref、relation ref、规范化 supersession_reason、双方 from / to revision 与 occurred_at。它不冒充双方的当前快照；UI 随后重新读取两页当前状态。

每次精确重试先复核双方当前 Project 决策权、active 和全部 evidence；再匹配 receipt 并交叉核对原事件、link、metadata、主体、时间、双方版本事实和当前链未被篡改。后来 replacement 也被替代，仍可确认原操作，不重新连边；当前无权则拒绝。损坏原事实返回通用内部错误，不用“旧对象已经 superseded”推断原请求成功。

## 锁顺序、时间与已有执行对象

事务顺序为 admission → 当前账户 / membership → 不可变 Project 定位与双方可读性 → 同一 Project / 角色 → 两个 Decision 按 ID 升序 FOR UPDATE → 全部 evidence → 原 receipt → 双方版本 / 状态 / 链校验 → 写事实与投影。现有接受 / 拒绝仍锁单对象，Ticket 创建使用相同 Project 顺序和 Decision 共享锁，管理 / 撤权继续复用 admission 边界。事务中不调用外部服务。

新替代时间在持锁后由数据库取得，严格晚于双方 updated_at 和旧结论已有 Ticket 的 created_at 上界；微秒精度不足或系统时钟回退时取这些已存事实的最大时间再加一微秒。Decision、link、receipt 和事件的新事实共享该时间，原 decided_at 不改。检查已有 Ticket 上界时复用权威 implements 关系，不能按文本或标题猜测来源。

Ticket 创建的首次时间也改为在 Decision 共享锁取得后确定，晚于该 Decision 当前 updated_at，receipt / Ticket / 事件 / link 使用同一时间。这样创建先持锁则先形成合法执行来源；替代先持锁则阻止新创建，不依赖请求到达时间判断事实先后。旧合法 Ticket 时间不重写。

已有执行对象的合同：

- Ticket、状态 / 负责人 / 优先级、Document 版本 / 保存 / 恢复均保留原来源；不自动完成、取消、迁移到新 Decision 或改正文。现有 Ticket 继续按自身 Project 贡献权限推进。
- 新 operation 只允许从当前 accepted 创建 Ticket。superseded 的 create_ticket 为 false，提示新执行应检查替代结论。
- Ticket 创建先复核当前贡献权限，再核对原 receipt，最后对没有原结果的新请求检查 accepted。原操作若已成功，验证 Ticket / implements / 创建事件的原 actor、来源与时间后返回原对象，即使来源随后 superseded；不重建工单。
- 原 acceptance body、canonical digest 和 result_revision=null 保留。精确重试以原接受事件 / receipt / 未改的接受字段证明原成功，可返回真实当前 accepted 或 superseded DTO；首次接受必须仍为 accepted。Web 改为“原接受操作已确认，以当前状态为准”，不能显示旧 accepted 快照或修改新的 supersession。
- 提案 200 重试仍返回同一对象的真实当前状态，增加 superseded；提案首次 201 仍必须 proposed。拒绝重试和 rejected 终态不变。

## 公共读取与 Web

完整 Decision DTO 增必填 decision_revision，支持四状态。accepted / superseded 保留原接受字段且 rejection_reason=null；updated_at 不再必须等于 decided_at，必须由对应版本和替代历史解释，不能简单删除时间校验。rejected 保留原相等时间与版本 2。Project 列表仍只有 ref / title / status，增加“已被替代”，原 cursor 和稳定 ID 分页不改。

Decision Nexus View capabilities 精确扩展为 accept / reject / create_ticket / supersede / retry_decision / retry_ticket_creation 六个 boolean。同一读取快照计算：

- supersede 表示当前对象可作为新结论：accepted、无 outgoing supersedes、版本未达上限、active Project 当前 decider / admin 且自身全部 evidence 可读；它不保证用户稍后选择的旧结论也合格。
- retry_decision 表示当前仍有权核验本人的原决定请求：active Project 当前 decider / admin 且自身全部 evidence 可读，不以当前终态或是否已有 outgoing 限制。替代未知请求要分别重读双方此能力；不能因首次 supersede 已变 false 就丢弃原请求。
- retry_ticket_creation 表示当前仍可核验本人的原创建请求：active Project 当前有贡献能力，状态为 accepted / superseded；只有页面内已有未知原请求才显示重试入口，不允许据此生成新 operation。
- 旧 accept / reject / create_ticket 规则保留，superseded 不显示这些新写动作。能力不是授权凭据，POST 必须再次检查。

从新 accepted 详情发起“替代旧结论”，复用同 Project 决策列表的分页读取选择对象，四种状态均保留显示，只有 accepted 可选，不用客户端角色猜权限或悄悄过滤其它状态。选择后读取旧详情，对照双方 question / outcome / rationale、来源、当前版本和本次理由，再明确确认。旧 evidence 受限时不显示可执行确认；最终资格由服务端写入判定。改变对象、理由或读取代次后清除旧确认。

Decision relation 增 outgoing / incoming supersedes。可读该边的双方后，readable 形状才额外返回 supersession {reason, actor, occurred_at}；其它关系不附带该字段。入向不可读目标完全隐藏，出向不可读目标保持不带方向、关系名或目标的 restricted 占位；理由不能从 Activity、错误或附件通道泄漏。evidence 源与替代边分开校验，不能把 supersedes 错当作 derived-from。

结果未知仅保留原完整 payload / operation，可显式重试；原 acceptance、替代或 Ticket 创建未知请求不能因页面读到 superseded 或首次动作不再可用就推断自己失败。对应 retry 能力为 false、401 / 404 或切换对象时清空对应私有草稿；成功的决定不因 retry 能力为 true 而显示新的写动作。双方读取使用共同请求代次，页面恢复、取消、迟到响应继续使用既有保护，不增加浏览器持久化。

## 事件、Activity 与迁移

新 schema 1 decision.superseded 以旧 Decision 为 primary、当前用户为 actor、web 为 source；payload 精确包含 status=superseded、replacement Decision ref、relation EntityLink ref、from_revision / to_revision，以及 replacement_from_revision / replacement_to_revision。不复制理由或结论正文。

这是一项明确的双对象命令：同一个事件显式投影到旧 / 新两个 Decision Timeline，各自标明“被替代 / 替代了旧结论”、该对象前后版本，并将另一端点作为按当前权限投影的 subject。原事件只存一次，双方不补造重复领域事实。正常写入、重建与导入使用同一分支，Activity 升至 12；不传播到 Thread、Ticket、Document、通知或全部图邻居。

migration 024：

1. 预检原支持的三状态字段组合、没有任何预留 superseded / supersedes 历史；不支持的旧人工写入整体拒绝，不自动修复或删除。
2. 新增 decision_revision，旧 proposed 回填 1，accepted / rejected 回填 2；不改旧时间、事件或 receipt。收紧生命周期、metadata、相邻版本、同 Project 和来源事实约束。
3. 为 supersedes 的两端分别建立唯一约束，拒绝自指、环、remove、改写及没有精确事件 / 原生 receipt 的新边；superseded 恰有一个后继，双方版本由同一事件证明。导入只使用既有 context.history-recorded 来源验证，不伪造 web 事实。
4. 扩展新 command receipt、事件与导入 facts 白名单、Activity 12 和重建；备份仍保存 decisions / entity_links / receipt / 事件等权威表，理由随 immutable metadata 保存。

不编辑旧 migration，不在服务启动时迁移。023→024 停写并配套 Go / Web / CLI；旧工件拒绝新 schema。退回旧工件须以匹配 023 备份恢复全新空目标，恢复前说明备份后的写入损失；真实实例操作须独立授权。

## v5 上下文兼容

默认导出为 project-collaboration-v5，format_version 保持 1。Decision 增 decision_revision；Link 增必填 nullable supersession_reason，只有 supersedes 为非空，其 actor / time / endpoints / event / lineage 复用既有字段，不再复制第二套关系。History 增 decision.superseded 与严格双对象版本 facts。

- 严格读取 v1～v4 原字段 / 事件白名单，再内部补版本（proposed 1、旧终态 2）和 link 的 null 理由；不改原 manifest digest、补造事件或向旧 profile 塞新字段。仅 v5 可包含 superseded / supersedes / 新事件，不降级丢弃历史。
- 重放原 proposed → accepted / rejected，再按时间验证替代事件同时推进两端；新旧结论在当时均 accepted、版本相邻、边和 metadata 理由合法、原始 actor / time / source 一致。所有结论、边、事件必须闭合，拒绝分叉、双后继、环、缺端点 / 缺事实和最终状态 / updated_at 不符。
- Ticket 读取其 ticket.created 事实：来源在当时已 accepted，created_at 不早于 decided_at；若来源最终 superseded，Ticket 必须严格早于该来源的被替代时间。之后 Ticket 状态和 Document 版本可以继续发生，不因来源当前状态而整体拒绝合法历史。
- 导出包含双方、完整可读替代链和 evidence / 原执行来源；不足以构成合法闭包时明确拒绝导出，不忽略不可读端点或删除关系来制造成功。两轮独立导入 / 导出后仍须保留所有原始来源，导入 proposed / accepted 可按目标当前资格继续原生操作。
- 历史替代不产生目标成员刚替代的原生事件、源 command receipt 或旧分发 Outbox；理由、双对象 Activity 和后续原生继续替代均须可恢复与重建。

## 验收矩阵与实施顺序

本表为已确认的验收要求；实际分层结果、运行范围和未覆盖项集中见[实施记录](../status/reviews/2026-11-02-decision-supersession.md)，不能只凭本表推断已执行。

| 编号 | 必须覆盖的结果 |
| --- | --- |
| R01 正式主链 | 正式讨论产生 A / B，分别人工接受，再 B 替代 A；列表、双方详情、理由 / 主体 / 时间、关系与双对象 Timeline 一致；C 再替代 B 保留完整链 |
| R02 权限与隐私 | 当前角色、归档、暂停 / 移除 / disabled、双方 evidence、跨 Workspace / Project；缺任一权限不能写或重试；关系与事件不泄漏隐藏端点、理由或来源数量 |
| R03 确定性竞争 | 两个新结论争一个旧、一个新争两个旧、A / B 互相替代、链延伸、原生 Ticket 创建与替代、撤权与写入，分别固定双方先持锁；无死锁、无环、至多一次效果 |
| R04 重试兼容 | 同 operation 并发 / payload 改变 / COMMIT 丢确认；后来新结论也被替代后原替代仍可核验；原接受 / 提案 / 已建 Ticket 重试返回真实事实，新的 Ticket operation 冲突 |
| R05 原子与来源 | 双方更新、link / metadata、receipt、事件、Outbox、双方 Activity 任一步故障整单回滚；原生和导入事实不能互相伪造；旧接受字段 / 时间与已建执行对象不改 |
| R06 Transport / DTO | 严格请求 / Unicode / 字节与版本上限、Session / Origin / CSRF / 缓存头 / 方法、四状态 / 六能力组合、双方版本与已接受字段的时间解释；未知状态 / 损坏关系整页失败 |
| R07 迁移恢复 | 023→024 只回填版本、不改旧事实；旧预留异常拒绝迁移；Activity 重建、权威备份和匹配旧工件空目标恢复；新旧 readiness 精确拒绝错配 |
| R08 可移植历史 | 严格 v1～v5、原 digest、理由 / 双对象事件与版本、链与 Ticket 创建时语义；恶意缺事实 / 环 / 伪造身份 / 超限拒绝；两轮导入再导出及实际 CLI 继续原生替代 |
| R09 正式 Web | 普通成员从 Project 找到新旧结论并对照确认；旧页 superseded、原 Ticket / Document 仍可操作；双标签、网络未知、原接受 / Ticket 重试、焦点 / 撤权 / 页面恢复与迟到响应 |
| R10 交付边界 | Go / race / vet、真实 PostgreSQL、Web 类型 / 测试 / Lint / build、仓库检查；桌面 / 手机 / 键盘分别记录，真实 BFCache、独立用户、持续使用和容量仅按实际证据判断 |

实施顺序为迁移与原子事务 → 历史重试 / Ticket 时间与读取 → v5 闭包 / 重建 / 恢复 → 正式 Web → 分层验收。完成线仅为“当前有权成员能用已接受的新结论替代旧结论，完整保留原决策与执行事实，并能安全发现和迁移这段历史”。

## 替代方案与后果

| 方案 | 取舍 |
| --- | --- |
| 先做复核 / review_at 提醒 | 需要独立复核记录与触发模型，不能解决新结论替代来源；继续后置 |
| 接受新提案时同时替代旧结论 | 合并两种人工事实及权限 / 重试边界；首片采用先接受再明确替代 |
| 修改旧 outcome / 让旧 Ticket 指向新结论 | 会覆盖原确认与创建依据，不采用 |
| 任意图、多旧合一、跨 Project | 增加多边权限、锁顺序、闭包及脱敏复杂度；首片限单链，同 Project |
| 独立替代记录表或只把理由放事件 | 现有 immutable EntityLink 已承载端点 / actor / time / source；严格 metadata 足够表达本片，避免重复权威来源或在 Activity 复制正文 |
| 不改版本与旧重试 / 跳过 superseded 导出 | 会使旧确认覆盖新事实或损坏合法迁移，不能构成完整交付 |

代价是新增 Decision 版本、双对象原子事实与上下文 v5；收益是结论更新仍保留接受、替代和执行的不同时间与责任，且不引入复核调度或通用图编辑框架。

## 确认与运行授权

2026-11-02 所有者明确回复“确认，继续推进和实现”，确认 D01～D07 及下述隔离验收范围；据此实施本片。提交、推送与真实实例操作继续按独立授权处理。

本次已确认的隔离验收范围：复用已安装工具 / 缓存和已有固定镜像，新建本任务专属临时 PostgreSQL / 空测试数据库、loopback Go / HTTPS fixture 与独立 Chrome profile，仅使用合成数据，故障 / 备份恢复只作用于这些资源。运行会临时占用端口、CPU、内存和磁盘；缺少工具 / 镜像则报告，不安装或拉取，不改系统信任或个人浏览器 profile。结束后按任务资源 ID 停止并精确清理容器、卷、fixture、临时包 / profile，保留必要脱敏证据。

上述确认不授权 push、PR、tag / Release、发布、部署、真实实例升级、外部消息、系统配置或依赖变更；不能沿用拒绝切片已结束的资源授权。
