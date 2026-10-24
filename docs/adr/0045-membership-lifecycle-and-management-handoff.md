# ADR-0045：成员生命周期与管理交接

状态：已接受并实施；2026-10-24 所有者确认 D01～D08，隔离验证见[实施记录](../status/reviews/2026-10-24-membership-lifecycle.md)

日期：2026-10-24

核对基线：`5cd7946`，schema 019。所有者已明确授权按开发依赖连续完成实现和验证，范围内内部批次不重复确认。

部分替代：[ADR-0026](0026-foundation-configuration-and-membership.md) 关于 admin 不可变更、管理员不可退出私密频道及 Thread 成员管理未开放的限制；扩展 [ADR-0042](0042-workspace-owner-handoff.md) 的成员代次、邀请和本人结果查询合同。其他现行合同保持有效，实施时同步对应真相源。

## 阶段目标与已有事实

本阶段承接 [M1 Web 平台基础](../roadmap.md) 的成员、角色与管理入口。交付标准是：管理者与成员可以通过正式页面完成增补管理员、责任交接、临时暂停、恢复与正式退出；历史内容保留，私密内容不因组织角色改变而扩大可见范围，人员变动不靠直接改库处理。

| 源码与现行合同 | 已有能力 / 限制 | 本阶段处理 |
| --- | --- | --- |
| `goldenpath/postgres/configuration.go`、ADR-0026 | 普通 Project 角色可配置；admin 不可新增、降级或移除；restricted Channel 要求 admin 且已加入 | 开放有约束的管理员管理和交接，保留私密读取前提 |
| `goldenpath/postgres/messaging.go`、`access.go` | restricted Thread 创建时仅授予创建者 membership；读取同时检查 Project、来源 Channel 与 Thread | 增加显式成员 / 管理责任操作，不改来源或正文 |
| `authn/postgres/store.go`、`identity.go` | Session 跨 Workspace；membership 仅 active / suspended；邀请不能恢复 suspended 成员 | 扩展 Workspace 生命周期，不把全局账号禁用或全账户退出混入成员管理 |
| ADR-0042、migration 018 | owner 主动交接、role_revision、邀请绑定创建者角色任期 | 保留 owner 交接；新增成员生命周期代次，防止暂停 / 恢复后的旧请求和邀请复活 |
| `httptransport/channel_events.go` | SSE 每次输出重新授权，空闲时 15 秒 heartbeat 复核 | 接入新的成员代次 / 会话限制，保留可测试的撤权时间边界 |
| ADR-0032 | Deployment 显式授权有独立记录与代次 | 暂停 / 退出时撤销当前授权；恢复成员不自动恢复部署能力 |
| ADR-0044、migration 019 | 历史作者 disabled / suspended，禁止认证或激活；导入不携带 ACL | 历史作者不进入成员恢复、接任或邀请候选；导入负责人取得目标管理责任 |

不在本阶段增加全局账户禁用、无权限管理员强制接管、Team RBAC、SCIM、组织层级、生产部署审批、消息删除、审计平台或数据保留清理服务。文件、通知、Attention Item 等 M1 其余范围仍按路线图推进。

## 已确认的关键决策

所有者已确认下表 D01～D08 推荐方案，包括隔离验证资源、故障注入与精确清理。

| 编号 | 推荐决策 | 用户可感知的影响与替代方案 |
| --- | --- | --- |
| D01 生命周期 | 区分临时暂停与正式退出：暂停保留尚未撤销的协作授权但全部失效；恢复需 owner 明确确认，会恢复仍存在的协作授权。正式退出清除协作授权，再加入从普通成员开始。两者均撤销 Deployment 授权，恢复后须重新授予 | 暂停适合临时停用，退出适合离职。替代方案是暂停也清空全部授权，安全规则更统一，但恢复需逐个重新配置且容易丢失管理责任 |
| D02 Project 管理员 | 支持多个 admin、增补、降级、移除与主动交接；现有 admin 近期认证并明确确认即可操作，不引入接收审批状态机。交接后原管理员角色必须显式选择 | 可以增设备份管理员；任一有效 admin 可管理同级管理员，受最后有效管理员和私密责任保护。替代方案是只允许单次交接，人员治理能力较窄 |
| D03 私密对象管理 | 为 restricted Channel / Thread 增加局部 `manager / member` 身份；当前可读该对象的 Project admin 仍可管理。局部 manager 可以管理本对象成员，不因此获得 Project admin 或业务写权限 | 可由实际参与者维护私密协作，不必先升为项目管理员；任何管理角色都不能绕过父级 / 本对象读取边界 |
| D04 停权与失管 | 正常交接 / 降级 / 移除 / 主动退出不得留下无有效管理者的对象；安全暂停优先停止访问，允许暂时无有效 Project / 私密对象管理者，不自动转交内容。最后一个有效 Workspace owner 仍不可被暂停或退出 | 紧急停权不会被隐藏的待交接事项阻塞；无可用且有权管理的人时，须恢复原负责人的安全身份与成员资格后交接，不提供 owner 越权兜底 |
| D05 恢复、会话与再次加入 | 暂停后旧 Session 在本 Workspace 永久失效，恢复后需重新登录；其他 Workspace 不受影响。正式退出后的重新加入必须使用新签发、绑定本人及退出代次的邀请，旧邀请不恢复成员 | 用户在其他 Workspace 可继续工作；无法用暂停前的会话、旧邀请、旧操作重新获得被撤销的能力 |
| D06 可见性与审计 | owner 管理 Workspace 成员状态；Project admin / 局部 manager 只管理其作用域。审计按当前管理权限及对象读取权限过滤，owner 不获得隐藏 Project / Channel / Thread 的名称、ID、数量或明细 | 管理目录包含本 Workspace 人员状态与稳定身份，不展示邮箱、全局账户状态或私密内容；本人只可查询自己的固定操作摘要 |
| D07 交付与兼容性 | 一次完整阶段交付数据库约束、API、Web、审计、邀请兼容、导入 / 备份恢复及验证；Go / Web / schema 配套停写升级，无滚动兼容窗口 | 推荐使用现有依赖；分批实施但不把未装配的 API 或页面单独当作阶段完成；具体迁移与遗留数据处理见下文 |
| D08 验证资源 | 确认设计后允许本轮新建专属 PostgreSQL 容器 / 网络、回环 HTTPS 与隔离浏览器，使用合成数据进行并发、取消、进程 / 连接中断和提交确认丢失测试；退出精确清理本轮资源 | 仅使用已安装镜像、依赖与工具缓存，不更改系统信任、不碰真实实例、不下载升级依赖、不写远程状态；缺失资源再说明具体需要 |

暂停后若保留 owner / admin / 局部 manager 身份，恢复会重新使这些尚存管理身份有效，确认页必须明确展示操作者有权知晓的角色，并用固定政策说明隐藏作用域；恢复绝不等同于只读开放。

D01 是临时暂停与正式退出的核心取舍；“恢复”页面必须直接写明将恢复仍存在的协作权限，不能使用含糊的“启用”文案隐藏影响。D03 新增局部管理授权，D04 明确安全暂停的可用性代价，已连同其余决策获确认。

## 角色与权限矩阵

所有能力均以 active 账户、有效 Session、active Workspace membership 及当前代次为前提；下表不代表永久权限。

| 能力 | Workspace owner | Project admin | 私密对象 manager | 普通成员 |
| --- | --- | --- | --- | --- |
| 查看 Workspace 管理目录、暂停 / 恢复 / 移除他人 | 本 Workspace，受 owner 保护 | 否 | 否 | 否 |
| 修改 Project 角色、管理 admin | 仅同时为该 Project admin 时 | 是 | 否 | 否 |
| 管理 restricted Channel / Thread 成员及 manager | 仅同时满足右侧条件时 | 必须已经可读目标 | 仅自身对象，且父级 / 目标可读 | 否 |
| 主动退出 Workspace / Project / 私密对象 | 本人，先处理 owner / 管理责任 | 本人，先交接责任 | 本人，先交接责任 | 本人 |
| 查看管理审计 | Workspace 成员操作；对象明细仍需该对象管理权与读取权 | 本 Project；私密对象需额外可读 | 本对象 | 仅本人固定操作摘要 |
| 业务写入 / 确认 Decision / 记录 Deployment | 沿用各领域权限 | 沿用各领域权限 | 不因 manager 身份新增 | 沿用各领域权限 |

Project admin 与局部 manager 都可以授予其职责内的管理能力，不能授予 Workspace owner。Workspace owner 仍只通过 ADR-0042 的主动交接流转；本阶段不增加任意提升 owner 的入口。

### 有效管理者

- Project：active 账户、active Workspace membership、该 Project 当前 admin。
- restricted Channel：满足当前 Project 读取、具备显式 Project role、Channel membership，且为本 Channel manager 或当前 Project admin。
- restricted Thread：满足 Project、来源 Channel 与 Thread 的当前读取，具备显式 Project role，且为本 Thread manager 或当前 Project admin。
- suspended / removed、disabled、导入历史身份、失去父级访问者都不计入有效人数。归档不消除管理责任。
- 局部 manager 可以具有 viewer 角色；其身份只允许管理本对象的成员，不授予消息写入、Decision acceptance 或其他业务能力。

Project 提升 admin 不自动加入任何私密对象；Channel manager 不自动读取其中 restricted Thread。提升局部 manager 的接任者必须已经是本对象有效成员；新增内容访问与授予管理责任是两个明确动作。

## 成员生命周期

### 状态及操作

membership 保留稳定 `(workspace_id, user_id)`，增加 `removed` 状态，不物理删除作者或历史引用。

| 原状态 | 动作 | 新状态 | 权限处理 |
| --- | --- | --- | --- |
| active | owner 暂停他人 | suspended | 协作角色 / 窄授权留存但不可用；撤销 active Deployment 授权；本 Workspace 旧 Session / 准入代次失效 |
| suspended | owner 明确恢复 | active | 只恢复仍存在的协作授权；不重建已删授权或 Deployment 授权；目标须重新登录 |
| active / suspended | owner 正式移除非 owner | removed | 先满足交接条件，再清除 Project / Channel / Thread 授权与局部管理角色，撤销 Deployment 授权，保留历史 |
| active | 本人主动退出 | removed | 与正式移除相同；owner 先完成 ADR-0042 交接 |
| removed | 本人兑换专用新邀请 | active / member | 保留原 user ID / 作者归属，创建新准入代次；不带回旧 Project / 私密对象 / Deployment 授权 |

没有 `removed → active` 的直接“恢复”按钮；普通邀请、登录、账号密码恢复或同名账户不能实现这条转换。暂停本人用退出流程替代；不提供没有清晰恢复责任人的“自我暂停”。

### owner 与管理责任

- owner 可暂停另一 owner，但事务提交后必须至少有一位 active 账户、active membership 的 owner。自己不能暂停自己；正式移除 / 退出 owner 必须先通过既有主动交接降为 member。
- 对最后有效 owner 的保护覆盖本阶段命令及现有 owner 交接；不声称能阻止部署者直接 SQL、外部身份运维或数据库损坏。
- 安全暂停不等待 Project / 私密对象交接，因此可能让其暂时无有效管理者。普通业务读取 / 写入仍按其他成员各自权限运行；暂停不会给这些成员新增管理权，也不删除内容。
- 正常退出 / 移除会检查所有会受影响的管理责任，即使对象归档。任何剩余责任不满足保护条件时，整个退出事务拒绝，不能半移除。操作者看不到的对象不返回 ID、名称、类型、数量或逐项阻塞原因；对外统一为“成员状态或交接条件已变化，请重新核对”。
- 成员本人可在仍有效时查看自己有权访问的管理责任清单并先交接。owner 不通过这份清单获得私密对象目录。
- 现有有权管理者可在暂停期间撤销保留的角色 / membership；恢复只能启用剩余记录，禁止从历史 Audit 重放已撤销授权。
- 无有效管理者时，已可读目标的 Project admin 可通过同一受控成员管理命令补充局部 manager。无人具备这一现有权限时停止自动处置，不能凭 owner 身份添加自己或指定新人读取私密内容。

### 会话、邀请与高风险授权

新增成员生命周期版本，角色任期与生命周期版本各司其职。暂停、恢复、移除和重新加入均推进生命周期版本；版本上限拒绝，不回绕。普通业务请求从服务器验证的 Session 获得本 Workspace 当前准入代次，在事务授权点复核，客户端不得自报一个代次取得能力。

暂停和恢复更新 Workspace 作用域的 Session 有效起点。旧 Session 在其他 Workspace 继续有效，但不能重新进入这个 Workspace；恢复后通过正常登录建立新 Session。新登录时处于 suspended 的 Workspace 不可用。会话列表 / Workspace 选择需区分“全局 Session 失效”与“此 Workspace 需重新认证”，不能为了简化实现将用户从全部 Workspace 登出。

已签发邀请继续绑定创建者 owner 任期，并新增生命周期绑定。创建者暂停、恢复、退出或 owner 任期变化后，旧邀请保持无效。suspended 目标不能通过任何邀请恢复；removed 目标只接受绑定其 user ID、当前退出代次、有效创建者任期的新邀请，且需以该既有账户登录兑换。邀请不绑定姓名匹配、不重建账户、不传递旧角色；生成与传递仍由用户显式操作，不发送邮件。

暂停 / 移除将本 Workspace 的 active Environment Deployment 授权按既有撤销合同关闭，保留代次与历史 Deployment 引用；即便暂停恢复，也必须由当前有权 owner 重新授予新的授权。其他 Workspace 的会话、成员与 Deployment 授权不改变。

## Project 管理员与私密对象交接

### Project admin

- 新增 admin 必须选择同 Project 已有的有效显式成员，不能直接从未知 ID 或跨 Workspace 用户提升。管理员增补和普通角色变更通过同一正式成员列表发现候选。
- 新增 / 降级 / 移除 admin、本人交接和本人退出均要求近期认证、精确 expected 版本与明确确认。允许多 admin，不引入唯一“项目所有者”。
- 主动交接原子完成接任者升为 admin 与操作者变更为显式选定的 viewer / contributor / decider 或退出 Project。其他 admin 不变，不同时转移 Workspace owner、Team、Environment 或私密内容访问。
- 降级 / 移除他人 admin 由当前另一 admin 执行；不能借普通角色接口绕过同级管理与最后有效管理者检查。
- 仅降级 Project 角色时，保留既有 Channel / Thread membership 和显式局部 manager；失去的只是由 Project admin 派生的管理能力。确认页明确这不等于退出私密协作。
- 退出 / 移除 Project role 时清理该用户在此 Project 的全部窄授权与局部管理身份，先核对剩余有效管理者。workspace 可见项目的只读基线仍保留，不能把“退出项目角色”宣传成“完全禁止读取”。

### restricted Channel / Thread

- 新建私密对象时在确认信息中明确创建者将成为其初始局部 manager；Channel 创建仍要求 Project admin，Thread 创建仍要求相应协作写权限。creator 来源不变，管理身份单独保存。
- manager 可添加 / 移除普通成员、增补 manager、交接或退出；目标必须具备有效 Project 显式 role，Thread 候选还必须能读来源 Channel。加入 Thread 不补授来源 Channel 权限。
- 候选目录仅返回本作用域可选用户的 ID、展示名和必要角色，不公开邮箱或全局账户状态。只有获得本对象管理能力者可读取此目录。
- 管理责任转交只在已有有效成员之间操作，不将整个成员名单或父对象授权复制给接任者；同一明确确认内原子变更双方局部角色。
- 移除 Channel 成员同事务清理该用户在其中所有 Thread 的 membership / 局部管理身份，跨对象最后管理者检查必须覆盖这些 Thread。移除 Thread 成员仅影响该 Thread，不改变作者、来源关系、消息或 Decision。
- 最后有效管理者可以通过原子交接退出，不能直接删除自己。操作计划发生变化必须重读，不后台选择替代接任者。
- project 可见 Channel / Thread 不新增无意义的专用成员名单，继续使用 Project / 来源 Channel 权限。

### 归档与遗留异常

归档对象不接受新业务写入。为完成安全治理，允许其有权管理者撤销成员、交接 / 修复管理责任；后者只可授予已具备相应内容访问的人，不新增普通内容受众，不解归档。批量 Workspace 暂停 / 退出仍覆盖归档对象。

迁移前已无有效管理者的异常数据不能靠自动选任意 owner 修复。保留已有业务读取，拒绝新增会使情况恶化的管理操作；已可读目标的 Project admin 可进行局部责任修复，否则由部署者先按现有身份恢复流程恢复原负责人的合法身份，再通过正式页面治理。运维修复单列，不作为本阶段网页管理能力的默认路径。

## 公共命令、计划与结果

沿用 `/api/v1/workspaces/{workspace_id}`、Session / CSRF / Origin、精确 Host、严格 JSON 和 `private, no-store`；新增能力装配到现有成员 / 配置模块，不建立通用权限执行器。

| 入口类别 | 作用与授权 |
| --- | --- |
| Workspace 管理成员列表 | owner 分页查询 active / suspended / removed；与现有只供选人的 `/members` 分开 DTO，后者仍只返回合格候选 |
| 成员 suspend / resume / remove 与本人 leave | 专用命令，不提供任意 PATCH status；除本人 leave 外要求当前 owner，所有敏感动作近期认证 |
| Project 管理员与私密成员命令 | 扩展既有精确角色操作；补专用 admin handoff、局部 manager handoff 与 Thread 成员路由 |
| 操作预览 | 只读生成绑定 actor、目标、动作、预期代次及策略的计划；可见对象正常展示，隐藏影响不返回明细 / 数量 |
| 操作结果 | 独立查询本人 operation 的 applied / not_observed 与固定摘要；不把响应丢失当作失败回滚，不重放权限 |
| 本人退出结果 | 退出后仍能以有效全局 Session 读取本人固定历史结果，不重新授权访问 Workspace；单独严格路由，不放宽普通 Workspace 认证 |
| 管理审计 | 当前管理权与对象权限先过滤再分页，不公开隐藏总数；本人操作结果不是通用 Audit 查询权 |

### 已实现的路由与严格 DTO

令 `W=/api/v1/workspaces/{workspace_id}`，管理基础路径 `B` 为：

| 作用域 | B | 动作枚举 |
| --- | --- | --- |
| Workspace | `W/management` | `suspend / resume / remove / leave / reinvite` |
| Project | `W/projects/{project_id}/management` | `add / role / remove / handoff / leave` |
| Channel | `W/channels/{channel_id}/management` | `add / role / remove / handoff / leave` |
| Thread | `W/threads/{thread_id}/management` | `add / role / remove / handoff / leave` |

- `GET B/members`、`GET B/candidates`、`GET B/audit`：只接受 `limit` 与 opaque `after`，默认 25、上限 50；cursor 绑定 actor、Workspace、作用域及视图，先过滤后分页。`GET W/management/responsibilities` 仅返回本人当前可读的管理责任，亦分页。
- `POST B/{action}/preview`：严格且必填 `target_user_id`、`role`、`actor_after_role`；不适用字段必须为 `""`。scope、action、actor 均由路由 / Session 确定。预览是受 CSRF / Origin 保护的只读 POST。
- `POST B/{action}`：上述三字段加 `client_operation_id`（UUID）、`plan_digest`（64 位小写十六进制）、`confirmed: true`。无客户端可指定的 lifecycle 或 Session 代次。apply 重算计划摘要；预期版本包含在已确认计划内，不另外接收可篡改的 `expected_*` 字段。
- Project `add` 只可选 `viewer / contributor / decider`，已有成员通过 `role` 可升为 `admin`；`handoff` 的 `actor_after_role` 可为前三种角色或空串（退出）。Channel / Thread 的 `add` 不带角色，`role` 为 `member / manager`，`handoff` 后原管理者可为 `member` 或退出。`leave` 目标必须为本人，`role / remove / suspend / handoff` 不允许借目标字段修改本人。
- 计划响应含 proposal、`actor_id / target_name / before / after / actor_role / target_workspace_role / target_status`、`scope_revision / actor_lifecycle_revision / target_lifecycle_revision / actor_role_revision / target_role_revision / parent_project_revision / parent_channel_revision`、固定 `policy` 与 `digest`。版本均用十进制字符串，计划不包含隐藏下属的聚合版本或数量。
- 成员页 `data` 为 `items / next_cursor / can_manage / self_role / scope_revision`；成员只含 `id / display_name / status / role / lifecycle_revision / eligible`。`eligible` 表示当前作用域可选任资格，不披露全局账户状态。审计项仅含 `id / actor_id / target_user_id / action / before / after / occurred_at`。
- `GET /api/v1/auth/management-results/{workspace_id}/{client_operation_id}` 使用有效全局 Session，不要求当前 Workspace membership；只查询本人。结果为 `not_observed` 或含已知 `action / scope_id / target_user_id / occurred_at` 的 `applied`，不返回当前权限、内部 receipt 或审计清理明细。首次 reinvite 成功另带一次 `invitation: {token, expires_at}`；精确重试 / 查询不返回 token。
- 原 Project / Channel 成员 PUT / DELETE 路由返回 `410 Gone`，调用者必须转到计划与确认流程；原成员只读及对象创建入口保留。Go、Web、schema 成套更新，无旧写协议兼容窗口。

普通成功使用 `data` 包裹并返回 200。精确 JSON 拒绝缺字段、重复字段、未知字段和 null。管理错误继承现有错误 envelope；`result_unconfirmed` 为 503，保留原 operation 查询，不推断回滚。

所有高影响操作使用明确的操作 ID、规范化请求摘要、expected 状态 / 版本和确认的计划摘要。计划不预留对象、不锁定未来状态，apply 重新验证全部条件。计划摘要不得编码不可读对象 ID / 数量；隐藏影响通过事务内策略检查，不能通过摘要变化暴露秘密拓扑。

角色或状态 A→B→A 后，旧 expected 版本仍失效。Project 角色及各窄权限作用域分别采用单调权限代次，删除 / 重建 membership 不能令旧请求重新有效。Project 公开的角色版本不因隐藏 Channel / Thread 成员变动而递增，计划不包含反映隐藏拓扑变化的聚合版本。精确 receipt 重试只返回原已应用事实；新操作仍重新授权。本人因本次交接 / 退出失权的结果查询例外只包含 operation、动作、本人已知目标、时间与 applied，不读取对方当前状态或授权明细。

普通读取 / 列表无权与不存在同形 404；可读但无管理能力为 403；状态、版本、管理责任、计划或幂等冲突为通用 409；会话失效与需近期认证复用现有错误合同。错误、日志、操作计划与浏览器恢复不得泄漏隐藏对象信息。

## 事务、并发、审计和安全边界

当前调用链存在不同锁顺序：Message / Thread 读取可先锁对象再锁 membership，配置先锁账户 / membership 再锁 Project，owner 交接先处理双方身份。因此不能仅在新命令中增加 membership 排他锁后遍历 Project，否则会产生锁环。

已有 schema migration 协调锁统一排在 Workspace 协调锁之前，禁止新生命周期事务持有 Workspace 锁再等待排在前面的 schema 锁。在所有相关事务的业务行锁之前采用统一 Workspace 权限变更协调：普通业务与只读配置取共享协调锁；Workspace 生命周期、owner 交接与成员 / 基础配置写入取排他协调锁。源码核对发现既有普通读取先 Channel / Thread 后 Project，而配置相反，新增权限代次触发器也会写入对象行；因此本阶段统一在 Workspace 内串行权限写入，避免只调整新命令而保留反向锁环。代价是同一 Workspace 的配置写入会短暂等待在途业务事务，按 G21 实测；不同 Workspace 不串行。它是单库事务的并发边界，不是第二套授权规则。账户级身份操作独立，涉及邀请的事务先由受控 token 查得 Workspace、取得协调锁、再锁账户和邀请并重新验证；禁止先锁账户再等待同 Workspace 排他操作。

### 协调入口清单

| 入口 | Workspace 协调方式与复权 |
| --- | --- |
| Message / Thread / Decision / Ticket / Document、关系、Nexus View、发现、CI checkout 与 Deployment 读取 / 业务写入 | `beginWorkspaceAdmission` 共享锁后复查账户、Session 与 membership，再执行各对象权限；SSE 使用同一短事务读取 |
| Team / Project / Channel、Component / Environment / Repository 配置，显式 Deployment 授权 | 配置写入排他锁，读取共享锁；旧内部成员删除亦检查下属管理责任 |
| 管理预览 / 成员 / 审计 / 责任列表与 apply | `managementTransaction`，读取共享、apply 排他；会话摘要、生命周期、近期认证在等待后重新验证 |
| owner 交接 | schema 共享 → Workspace 排他 → 既有排序身份 / Session / membership 行锁 |
| 邀请签发、兑换与 OIDC 邀请完成数据库事务 | 先由 token 得到 Workspace，再取协调锁、锁身份与邀请、重查签发者角色 / 生命周期和目标退出代次；无 provider 网络等待持锁 |
| 上下文 export / prepare / apply / result | 保留原 import 独占边界，schema 锁优先，再取得对应 Workspace 协调和当前准入；导入不传 ACL |
| 全局 Session 登录 / logout、密码恢复 | 独立账户级顺序，不持账户锁再请求 Workspace 锁；Workspace 业务持 Session 共享锁复查 revoked / expires |
| migration / restore | migration schema 排他协调；restore 仅空目标与停写匹配工件；不在业务 Workspace 锁内等待 migration |

所有协调锁使用 5 秒等待预算。重复读事务在等待协调锁期间形成的旧快照，仍通过 membership 行锁的并发更新检测拒绝，不能将锁前快照当作当前权限。零 membership revision 只留给可信进程内 fixture / operator，公开 Session adapter 必须携带正代次与 Session ID。不为尚未实现的搜索 / 通知声称完成适配。

- 协调锁与身份 / membership / 对象锁必须有等待预算；超时保留真实错误，不自动重试高影响写入。不同 Workspace 不因一个暂停操作整体串行。
- 撤权之前已取得有效授权和共享协调锁的事务可先完成；撤权提交后的新请求与迟到旧准入代次必须拒绝。无法撤回已经交付给用户的数据。
- SSE 不在整个连接生命周期持锁，每次输出在短事务重新验证；空闲连接继续 15 秒 heartbeat 复核。浏览器在授权失败、Workspace 切换和页面恢复时清空对应内容及表单，丢弃迟到响应；无业务推送的页面不承诺毫秒级远程清屏。
- 角色 / membership / Deployment 撤销、成功 Audit 与 receipt 原子提交，任一失败整单回滚；COMMIT 后确认丢失返回未确认并提供独立结果查询。
- 不把权限管理记录广播到普通 Activity / SSE，不包含正文、邮箱、凭据或任意备注。复用配置 Audit 的受控事实，新增命令形状必须有数据库约束；涉及隐藏对象的清理明细只存受保护审计，不进入 Workspace owner 的普通响应。
- 失败保留受控错误与 request ID，本阶段不宣称具备全产品持久化失败审计和审计保留 / 清理能力。

## 数据、升级、备份与导入

正式 migration 020 承载全部本阶段数据变化，001～019 保持不变。

权威数据增量包括 Workspace `removed` 状态、生命周期版本与 Session 有效起点、Project / 窄权限代次、局部 manager 身份、绑定退出代次的再准入邀请，以及新命令的 Audit / receipt。优先扩展既有 membership 和受约束配置记录，不建立通用 policy engine。

迁移规则：

1. 既有 active / suspended、Workspace owner、Project role 与内容可见范围保持；有效基线初始化新代次，不把历史 suspended 成员自动激活。
2. 已有 restricted Channel 的 Project admin 且已加入者继续保留现行管理能力；不自动让 owner 加入或提升普通成员。
3. 既有 restricted Thread 的原创建者若仍是有效可读成员，可建立初始局部 manager；这是本阶段已确认的新增管理授权，不修改 creator。否则不任意选人，按遗留异常处理。
4. ADR-0044 已导入对象的历史作者不得成为 manager；当前仍有权的导入负责人按批次 / 目标显式授权建立初始局部管理责任。已撤销的负责人不能因 migration 重新获得权限。
5. 原邀请补齐创建者生命周期代次；已失效邀请不复活。已退出成员无旧数据，因此只由新合同产生其再准入材料。
6. 所有作者 / decider / 修订者 / 管理历史外键仍保留，不删除 membership 行，不改写既有 Decision、Ticket、Document、EntityLink、Activity 或导入来源事实。

Go、Web 和 schema 需要同版本停写升级，readiness 拒绝不匹配；迁移失败回滚当前 migration，不用删 migration history 降级。已提交的新版本业务数据不能由旧二进制直接读取，回退沿 ADR-0043 的匹配旧工件 / 升级前备份空目标恢复，可能放弃升级后写入，需要独立明确。

新权威列 / 表及 Audit / receipt 随备份恢复，Session 与未兑换邀请仍不恢复；恢复可能带回备份时仍有效的成员或角色，重新开放前须核对人员变动，不能只证明 schema readiness。恢复后检查旧会话无效、历史作者不可激活、管理责任与权限记录一致。

`.nexus` 的 profile 不加入角色、成员名单或管理授权。导入仍只授予当前负责人目标 Project admin 与包内私密对象的初始管理 / 访问；源 manager 不映射成目标管理者。再次导出历史来源保留，协作包格式不因局部管理列自动升级；若实际需要改变公开格式，须重新说明并确认，不静默扩字段。

## Web 交付

- Workspace 管理页：成员状态分页、暂停 / 恢复 / 移除、owner 交接链接、新邀请与再准入；不把普通选人器变成完整身份目录。
- Project 成员页：普通角色与 admin 管理、本人交接 / 退出、当前可见管理责任；解释 workspace 可见项目的只读基线。
- Channel / Thread 成员页：当前受众、局部 manager、候选选择、增补 / 交接 / 退出；父级访问不满足时不给出自动补权按钮。
- 变更确认：列明操作者、目标、动作、原 / 新角色、保留和撤销的能力、不可恢复项。暂停恢复明确写出“恢复尚未撤销的协作权限，部署权限需另行授予”；隐藏影响使用固定政策说明。
- 模糊失败保留原 operation 与已确认请求，提供查询结果 / 原操作重试；不自动生成新操作。交接后即时重读当前能力，退出后回到可访问 Workspace 或账户页。
- 管理页面支持桌面 / 手机、键盘 / 焦点、空态、分页、冲突、重认证、多标签页与后退恢复；不把鉴权错误包装成空列表或成功提示。

## 实施批次与阶段验收

确认 D01～D08 后在同一阶段内连续推进，不逐个内部批次重复索取相同授权；只有新增产品决策、依赖、真实环境或超出资源范围时另行说明。

| 批次 | 交付重点 | 必须满足的检查 |
| --- | --- | --- |
| A | 接受合同、补精确命令 DTO、权限矩阵 / 锁入口清单、migration 与领域规则 | 升级合法 / 异常基线、数据库约束、历史身份与旧邀请不复活 |
| B | 成员生命周期、角色 / 局部管理、计划与 receipt、审计读取、统一事务授权 | 正负权限、并发、最后管理者、隐藏信息、重复请求与确认丢失 |
| C | 正式 Web 管理页面与跨作用域状态处理 | 管理者与普通成员从登录到交接 / 暂停 / 恢复 / 退出 / 再加入，桌面与手机 |
| D | 升级恢复、导入互操作、代表性规模及故障验证 | 正式数据链、实际数据库 / HTTPS / 浏览器，资源清理与精确证据 |

验收矩阵：

| 编号 | 场景与判据 |
| --- | --- |
| G01 | 正式 bootstrap / 邀请建立多人工作区，增补 admin、交接、降级和原管理员退出；全过程不直接 SQL 造最终权限 |
| G02 | 唯一 / 多 Project admin、Workspace owner 的保护；两个并发退出不能留下无有效管理者 |
| G03 | 提升 admin 不泄漏 / 授予私密内容；局部 manager 不得修改 Project role、确认无权 Decision 或记录 Deployment |
| G04 | private Channel / Thread 候选受父级限制；加入 Thread 不能补权 Channel；creator 历史与管理身份分离 |
| G05 | 交接者和接任者暂停、失去角色、失去父级权限或代次变化后，旧计划 / 旧页面拒绝 |
| G06 | Project / Channel 移除触发所有下属窄授权清理；同时保护下属 Thread 管理责任，不返回隐藏明细 |
| G07 | active→suspended→active：暂停立即阻断新请求，恢复只启用仍存在的协作授权；暂停期间已撤销记录和 Deployment 授权不恢复 |
| G08 | 安全暂停最后 Project / 私密管理者可成功且不转交内容；最后有效 Workspace owner 暂停拒绝 |
| G09 | 正式移除 / 自己退出保留作者和正文，撤销所有工作区对象授权；再次加入从 member 与空显式授权开始 |
| G10 | 旧 / 通用邀请不能恢复 suspended / removed；专用邀请绑定本人、退出代次、创建者任期且一次兑换 |
| G11 | 同账户多个 Workspace：本范围旧 Session 永久失效，其他 Workspace 正常；恢复后的旧页面 / 请求不获得新代次 |
| G12 | 暂停与 owner 交接、邀请兑换、局部角色更新 / Deployment、密码恢复、logout 的真实屏障竞争，无稳定锁环或过期成功 |
| G13 | 已持授权事务与撤权的前后顺序明确；SSE 每次输出 / heartbeat 拒绝，恢复后旧连接不自动重新开放 |
| G14 | 归档对象安全撤权 / 交接有效但不能新增普通受众或业务写入；遗留失管不自动升级 owner |
| G15 | 管理审计按当前权限过滤后分页；隐藏 ID、数量、类型、标题不进入 owner 响应、计划摘要或错误；本人结果例外不扩大访问 |
| G16 | 退出成功但 HTTP 确认丢失后，本人全局 Session 可查询固定成功摘要；无关用户、不同 payload、旧 operation 不重放 |
| G17 | SQL / Audit / receipt 故障、取消、backend / 进程中断整体回滚；实际 COMMIT 确认丢失可通过结果查明 |
| G18 | 019→新 schema、空目标备份恢复、旧工件恢复后显式升级；核对生命周期代次、管理责任、权限和旧 Session |
| G19 | ADR-0044 导入 / 再导出，不传递源 ACL；导入历史身份不能选任、激活、邀请或被成员清理误删 |
| G20 | 正式 Web 双身份、双标签、390px 手机、键盘、刷新 / 后退与重新认证；不依赖手输稳定 ID 或手改数据库 |
| G21 | 代表性成员 / Project / 私密对象数量下，记录计划、apply、权限查询与恢复耗时、锁等待；结果受限不静默截断为成功 |
| G22 | 不改动真实实例；测试容器 / 网络 / HTTPS / 浏览器精确清理；自动化、Agent 浏览器与独立人工持续使用分开记录 |

P05 / P06 / P10 / P13 / P20 等 ADR-0044 剩余补验列为独立验收清单，不用本阶段新增功能覆盖其未执行事实。ADR-0043 独立操作者、原生中文 IME 和真实 Jenkins 的暂缓继续有效。

## 替代方案与代价

| 方案 | 判断 |
| --- | --- |
| 暂停直接清空所有协作授权 | 可选，恢复更保守但重新配置和管理责任恢复成本较高；推荐通过明确恢复动作保留尚未撤销的协作关系 |
| owner 可接管所有私密对象 | 不采用；会把组织管理权变成内容超级权限，破坏现行边界 |
| 所有退出都先阻止，等全部管理责任交接 | 适用于计划退出；不适合安全暂停，可能让应停权账户继续访问 |
| 只有 Project admin 能管理任何私密对象 | 可保持旧 Channel 规则，但无法让普通参与者管理自己创建的私密 Thread，且把局部职责和项目级权力绑定 |
| 接任者确认 / 审批 / 到期队列 | 本阶段不采用，需引入通知与待处理状态；沿用近期认证、已加入候选与明确确认的主动交接 |
| Workspace 暂停时撤销该用户全部 Session | 不采用，会影响其他 Workspace；承担一次 Workspace 会话 / 请求代次改造以保持正确边界 |

主要成本是成员状态与代次、局部管理身份、跨模块事务授权顺序和管理 Web 的同步变化。它们共同服务于人员变动这一稳定业务边界，不以通用权限框架或额外管理层替代具体规则。领域模型、核心合同、API、备份分类和自动化已同步；本阶段证据及其限度见实施记录。
