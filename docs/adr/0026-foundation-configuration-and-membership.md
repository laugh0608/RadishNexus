# ADR-0026：基础对象创建与首批成员配置

状态：已接受（2026-09-14）

日期：2026-09-14

## 背景与源码基线

基线为 `d462bef`。当前 [bootstrap](../../server/cmd/nexus-bootstrap/main.go) 和 [identity Store](../../server/internal/platform/authn/postgres/store.go) 只建立用户、账户、Workspace 与 owner membership；不建立 Team、Project 或 Channel。[邀请实现](../../server/internal/platform/authn/postgres/identity.go) 只让用户加入 Workspace，不能获得 Project 写权限。[ADR-0025](0025-project-and-channel-discovery.md) 已开放发现入口，但空实例没有正式配置入口。

[现有 schema](../../server/db/migrations/001_golden_path_foundation.sql) 要求 Project 具有同 Workspace 的 `owner_team_id`；Team 只有基础元数据，没有 Team membership 表。[当前 resolver](../../server/internal/goldenpath/postgres/access.go) 用显式 Project role 决定写能力，restricted Channel 还要求显式 Channel membership。Workspace owner、Team 所有权和 Project admin 均不能替代窄权限。

本 ADR 补齐“owner 登录 → 建立责任 Team 与 Project → 创建 Channel → 邀请 → 配置成员 → 成员发现频道并发送 Message”。本轮交付首批配置，不宣称完整成员治理、账号恢复或 M1 退出。

## 已确认的决定与影响

| 决定 | 推荐范围 | 影响 |
| --- | --- | --- |
| 首个 Project 管理权限 | active Workspace owner 可创建 Project；必须显式确认本人作为初始 Project admin，与 Project 原子建立 | 这是新对象创建时的一次明确授权，不赋予 owner 对既有 restricted Project 的管理或读取权 |
| 首批 Project 角色管理 | Project admin 可为其他成员配置 `viewer / contributor / decider` 或移除角色；现有 `admin` 不可通过本批入口降级或移除，也不新增 admin 授予入口 | 避免首批配置同时引入管理员交接、最后管理员和私密频道管理权转移；完整管理权交接另立切片 |
| 成员选择与私密边界 | 向 owner、Project admin 开放有界的同 Workspace 活跃成员选择；restricted Channel 配置要求操作者同时为 Project admin 和该 Channel 显式成员 | 新增受控成员目录的可见范围；只展示稳定用户 ID 和展示名，不输出邮箱、身份绑定或无关角色 |
| 权限撤销 | 移除 Project 角色时同时清除该用户在此 Project 的 Channel / Thread 显式授权；移除 restricted Channel 成员时清除此频道内 Thread 显式授权 | 防止旧窄权限在重新加入后自动恢复；不删除业务内容，重新加入需重新明确授权 |
| 持久化与公共入口 | 新增受约束的配置 receipt、安全 Audit 和 forward-only migration，增加专用同源 API；复用既有 Go / PostgreSQL / React 基线 | 需要同步 readiness、备份恢复分类和 Go / Web 发布版本；不增加依赖 |

项目所有者已确认上述基础配置范围。另提出部署后首次访问创建首位管理员的要求，初始化入口的新增合同与权限范围单独补充；不因此引入绕过私密对象的超级权限。

## 对象与授权合同

### Team 与 Project 创建

- Team 首批只承担责任归属元数据。active owner 可以创建、列出同 Workspace Team；不建立组织层级、Team 权限继承、Team membership 管理或跨 Workspace 归属。
- Project 创建需要 active owner、同 Workspace 的已存在 Team，以及 `initial_admin_user_id` 精确等于当前用户；字段必须显式提交，不能缺省填入。Web 显示本人将成为该 Project 管理员的授权说明。
- 创建事务写入 Project 与初始 `admin` membership。创建者无权替其他人建立初始管理员，也不向既有 Project 自动添加自己。
- Project 的 `visibility` 为 `workspace / restricted`，初始 `status=active`；`key` 在 Workspace 内唯一。首批不提供修改 key、归属、visibility、归档或删除对象的入口。
- owner Team 不授予读取、贡献、决策或部署权限；Project 创建不同时建立 Component、Repository、Environment 或它们之间的 EntityLink。

### Project 角色配置

- 操作者必须是 active Workspace 成员、当前可读 Project 的显式 `admin`，Project 必须 active。只有 Workspace owner 身份不能配置既有 Project。
- 新增或变更角色的目标必须具有 active 账户和同 Workspace 的 active membership。首批允许目标角色 `viewer / contributor / decider`，不能提交 `admin`。
- 操作者不能修改自己的角色；目标当前为 `admin` 时拒绝任何本批角色写入。界面将 admin 展示为不可编辑，并说明管理权交接尚未开放。
- 移除可以清理本 Project 已有的非 admin 成员，包括已暂停或账户已禁用者；不存在的成员与已移除状态作无变化处理，不能借此查询其它 Workspace 的用户。只有已有授权记录可提供这类清理目标，不在候选目录中暴露全局账户状态。
- 移除 Project membership 同事务清理该用户在本 Project 下的 Channel / Thread memberships，不回传被清理的隐藏对象、数量或时间；不改变其他 Project、Workspace membership 或业务内容。
- `workspace` 可见 Project 移除角色后，active Workspace 成员仍有既有只读基线；界面明确说明“撤销项目角色不等于禁止读取公开项目”。restricted Project 才会失去直接读取与发现。
- `viewer` 不能发送 Message；`contributor / decider` 沿用现有能力。角色降为 viewer 会失去写能力，但不会被描述为撤销已有读取授权。

### Channel 创建与成员配置

- Channel 只能由 governing Project 的当前 admin 在 active Project 内创建；初始 `status=active`，visibility 为 `project / restricted`。
- `project` Channel 不建立无效的专用成员列表，读取继续按 Project 边界决定；拒绝该类 Channel 的成员增删请求。
- 创建 restricted Channel 必须显式提交去重后的 `member_user_ids`，其中包含操作者本人。其他初始成员必须是同 Project 的有效显式成员；每次创建最多 50 人，后续通过分页与逐人命令继续添加，不设置团队总人数或许可证席位限制。
- 管理 restricted Channel 必须同时满足 Project admin 与当前 Channel 显式 membership。未加入的 admin 与 owner 不能查询成员、添加自己或用已知 Channel ID 绕过不可发现性。
- 后续添加成员只从此 Project 的有效显式成员选择，不自动赋予 Project role；移除成员不改变 Project role。只读 Project 基线不是自动加入 restricted Channel 的凭据。
- 首批禁止通过 Channel 成员入口移除任何当前 Project admin，保留已有管理员的频道访问；频道管理权交接与管理员退出随独立切片处理。
- 移除非 admin Channel 成员时同事务清理该用户在此 Channel 内 Thread 的显式 membership。重新加入 Channel 不复活已清理的私密 Thread 授权。
- 不批量替换完整成员集合，避免旧页面覆盖其他人的修改；操作针对一个明确用户。Thread 创建时既有的窄权限合同保持不变。

### 归档、撤权与并发

- 已归档 Project / Channel 保持既有可读性，拒绝本批新增、角色变更和成员增删；针对归档对象的安全清理、解归档和紧急治理另行设计。
- 所有写入在业务事务内重新检查账户状态、Workspace membership、Project role、对象状态与所需窄权限；transport 的 Session 检查不能代替事务授权。
- 对同一 Project 的配置写入按 Project 行锁串行化；需要多账户或 membership 锁时按稳定 ID 顺序获取。实现前将锁顺序与现有邀请、Message、Decision 事务一并核对，并用真实数据库竞争测试排除死锁或过期授权成功。
- 撤权与业务写入按事务锁取得先后确定提交顺序：允许在撤权之前取得有效授权锁的写入先完成；撤权提交后的新请求和精确重试不能使用旧权限。不能承诺撤回已经交付到浏览器的信息。
- 本批不新增 Workspace 暂停、账户禁用、owner 转移或管理员应急恢复入口，因此不宣称解决这些动作引发的失管问题；也不凭 owner 或旧 receipt 恢复失管对象。

## 公共 API 与成员读取

以下路径统一以 `/api/v1/workspaces/{workspace_id}` 为前缀。均复用现有 Session、可信 Host / proxy、错误 envelope、`private, no-store` 与 `Vary: Cookie`；写请求使用精确 Origin 和现有双提交及存储态 CSRF 校验。

| 方法与后缀 | 请求 / 用途 | 权限 |
| --- | --- | --- |
| `GET /teams` | 选择责任 Team | active owner |
| `POST /teams` | `{client_operation_id, name}` | active owner |
| `GET /members` | Workspace 活跃成员选择 | active owner，或在此 Workspace 至少有一个 active Project 的有效 admin |
| `POST /projects` | `{client_operation_id, key, name, owner_team_id, visibility, initial_admin_user_id}` | active owner，显式本人初始 admin |
| `GET /projects/{project_id}/configuration` | Project 安全元数据与当前配置能力 | 当前可读 Project；仅返回调用者自身能力，不含成员或隐藏频道 |
| `GET /projects/{project_id}/members` | 分页角色列表及本 Project 候选成员 | 当前 Project admin |
| `PUT /projects/{project_id}/members/{user_id}` | `{client_operation_id, expected_role, role}` | 当前 Project admin；role 限三种非 admin 角色 |
| `DELETE /projects/{project_id}/members/{user_id}` | `{client_operation_id, expected_role}` | 当前 Project admin；删除角色与从属窄权限 |
| `POST /projects/{project_id}/channels` | `{client_operation_id, name, visibility, member_user_ids}` | 当前 Project admin |
| `GET /channels/{channel_id}/configuration` | Channel 安全元数据与当前配置能力 | 当前可读 Channel |
| `GET /channels/{channel_id}/members` | 分页 restricted Channel 成员 | 当前 Project admin 且为 Channel 成员 |
| `PUT /channels/{channel_id}/members/{user_id}` | `{client_operation_id, expected_member}` | 当前 Project admin 且为 Channel 成员 |
| `DELETE /channels/{channel_id}/members/{user_id}` | `{client_operation_id, expected_member}` | 当前 Project admin 且为 Channel 成员 |

### 字段、分页与响应

- 名称 trim 后为 1..120 个 Unicode 字符且最多 480 bytes，拒绝非法 UTF-8、NUL 与控制字符。Project key 使用 1..32 位小写 ASCII 字母、数字或中划线，首字符为字母；不静默改写大小写。新入口校验不修改或回填既有对象名称与 key。
- `member_user_ids`：project Channel 必须为空数组；restricted Channel 为 1..50 个不同的合法 `usr_` ID，必须显式包含操作者。数组顺序不影响 canonical digest，重复值拒绝。
- `expected_role` 必填，为 `null / viewer / contributor / decider / admin`，其中 null 表示预期没有 Project membership；`expected_member` 必填布尔值。新操作与当前值不一致返回 `409`，客户端刷新再让用户决定，不能静默覆盖。已有 receipt 的精确重试不重新执行旧状态变更。
- JSON 只接受表列字段，拒绝重复 key、未知字段、尾随 JSON 和不匹配类型；写请求拒绝 query，body 上限 32 KiB。operation ID 沿用现有 1..128 bytes printable ASCII 校验。
- GET 列表沿用 `limit` 默认 25、最大 50 与稳定 ID keyset 分页；cursor 有版本并绑定 Workspace、列表种类、Project / Channel，不能跨作用域复用。先按当前权限过滤再分页，无隐藏数量或总数。
- Workspace 候选成员仅返回 `{id, display_name}`；Project 成员返回 `{user:{id,display_name}, role, eligible}`；Channel 成员返回 `{user:{id,display_name}, eligible}`。`eligible` 只表示当前是否可用于授予该层权限，不输出账户禁用原因、邮箱、密码状态或其他 Workspace 信息；已有失效成员仍可作为清理目标展示给已授权管理者。
- Project 候选成员从其角色列表中选择 `eligible=true` 的条目；Workspace 候选成员从 `/members` 选择。展示名可重复，以稳定 ID 消歧，不要求维护者手工提供 ID。
- 配置 GET 的 `capabilities` 只描述当前调用者可以尝试的动作；页面据此展示入口，服务端每次命令独立授权。Project 返回 ref、key、name、visibility、status；Channel 返回 ref、governing Project ref、name、visibility、status；均不扩大既有发现 DTO。
- 创建成功返回 `201`，精确重试返回 `200`，`data` 包含受控的 `{id,name}` Team 或新对象配置 DTO；成员写入返回 `200`，`data` 仅包含目标用户 ID 与本次请求已处理标记 `applied:true`。该标记不代表当前成员状态，页面收到结果后重新读取当前列表。
- 未登录 `401`；不可发现的 Workspace / Project / Channel 一律 `404`；已可发现但没有配置能力为 `403`。无效目标候选统一 `404`，不区分不存在、跨 Workspace 或不满足候选条件；归档、前置状态变化、幂等内容冲突为 `409`。其余错误复用既有 HTTP 边界，不回显原始 SQL 或内部原因。

## 幂等、审计、事件与持久化

- 复用现有 command → Store → 同事务持久化模式。[collaboration receipt](../../server/db/migrations/007_collaboration_command_receipts.sql) 只允许 Decision / Ticket 命令并要求领域 EntityRef / event，因此不放宽它的约束来承接 Team 或权限写入。
- 新增 `workspace_configuration_receipts`，唯一范围为 `(workspace_id, actor_id, command_kind, scope_id, subject_id, client_operation_id)`；command_kind 固定为 `team.create`、`project.create`、`channel.create`、`project.member.set`、`project.member.remove`、`channel.member.add`、`channel.member.remove`。Team / Project 创建的 scope 为 Workspace，Channel 创建的 scope 为 Project，创建命令的 subject 使用固定空串；成员命令的 scope 为 Project / Channel，subject 为目标 user ID。数据库 CHECK 固定组合、ID 前缀、operation ID 与 SHA-256 格式，不提供通用执行入口。
- canonical digest 包含全部业务字段及 expected 条件，名称按校验后的 trim 值编码、成员数组排序，明确 null 与缺失不同。首次命令原子写入业务事实、receipt 与成功 Audit；相同 scope / operation / digest 返回原处理结果，不再次修改成员；digest 变化返回 `409`。
- receipt 保存 canonical digest、结果稳定 ID、Audit ID 与时间，禁止更新和删除。不保存原始请求、名称、邮箱、邀请码或完整 HTTP 响应；成员已经被后续命令撤销时，旧成功 receipt 不能重新授予权限。
- 所有重试先重查当前操作者权限；创建重试还须能读取原创建结果。成员目标在第一次命令后退出或被撤销，不影响有权操作者查询自己的原处理结果，但不得返回目标当前私有信息。归档和操作者失权后不返回旧成功结果。
- 新增 `workspace_configuration_audit`，仅记录成功配置：稳定 Audit ID、Workspace、操作者、动作、目标作用域与用户 / 对象 ID、角色或成员状态的 before / after、`changed`、时间与服务器 request ID。创建 Project / restricted Channel 时另以 `granted_user_ids` 记录初始授权用户，成员命令用 subject 与 before / after 表达变更。Audit 禁止更新和删除，receipt 用同 Workspace 外键关联它；动作与明细采用白名单校验，不保存任意请求 JSON。无变化命令记录 `changed=false`，精确重试不追加 Audit。Project / Channel 移除引发的授权清理，在同一 Audit 的受控明细中记录被移除的 Channel / Thread ID，不记录标题或正文，不进入普通响应。
- 成功 Audit 必须与业务事务同时提交；插入 Audit、receipt 或事件失败则全部回滚。权限拒绝、冲突、输入错误与数据库故障保留受控诊断及 request ID，不写“成功”记录。本批不声称具有持久化失败审计、Audit 查询 UI、保留策略或全产品安全审计能力；这些仍是试用 / 生产治理的独立缺口。
- `project.created`、`channel.created` 采用现有事件 envelope、`schema_version=1`、用户 actor、`source_kind=web` 与 request correlation，payload 仅 `{status:"active"}`；Channel 的 governing Project 留在既有事件上下文。与对象、初始授权、receipt、Audit 和 Outbox 同事务提交。
- 创建事件仅投影到其主要对象 Activity，safe facts 只含 status，不把受限频道创建投影到更宽的 Project，也不公开初始成员。正常更新与全量重建采用相同事件白名单和投影定义；不新增 Project / Channel Nexus View 页面，不承诺本批 UI 展示该 Timeline。
- Team 创建与成员变更记录安全 Audit，不伪造 Team EntityRef 或向普通 Activity 投影成员名单；暂不扩展 Team EntityLink 类型或向所有领域消费者广播权限细节。

## Web 最小路径

1. owner 在既有首页 Workspace 作用域打开基础配置，选择 / 创建责任 Team，填写 Project 并显式确认自己成为初始 admin。
2. 成功后刷新既有 Project 列表、选中新 Project，创建 project Channel 或显式选成员创建 restricted Channel。
3. 复用现有账户与邀请入口，由用户显式传递邀请；不自动发送邮件、外部消息或把邀请 token 放入 URL。
4. 新成员兑换邀请后，admin 刷新成员选择，明确授予 contributor；restricted Channel 还须明确加入该成员。选择 decider 时展示其可确认 Decision 的影响。
5. 新成员从首页发现可读 Project / Channel，使用现有 Message 入口发送消息；刷新仍可读取消息。撤销后检查首页、直接请求和既有 SSE 的当前权限边界。

页面保留加载、空态、错误、刷新、分页、键盘与焦点行为。切换 Workspace / Project 或失权立即清理对应目录和表单；迟到响应不能覆盖新作用域。模糊网络失败保留本次 operation ID 与 payload 供人工重试，改变请求内容或开始新动作才生成新 ID；不后台自动执行权限变更，不用旧 receipt 响应冒充最新成员状态。

## 替代方案

- bootstrap 预置默认 Team / Project / Channel：隐藏产品配置缺口，并把业务选择耦合到身份初始化，不采用。
- owner 自动成为所有 Project admin，或 admin 直接加入未知 restricted Channel：扩大既有权限，违反 ADR-0004 / ADR-0025，不采用。
- 一次交付完整 Team RBAC、管理员交接与应急恢复：增加权限继承和窄权限失管设计，超出本批普通成员协作目标；后续按独立合同推进。
- 只接受人工输入用户 / Team ID：无法由普通管理者独立完成配置，因此本批必须包含受控成员与 Team 选择入口。
- 用普通 Activity 或身份 Audit 直接存成员变更 JSON：前者读取边界过宽，后者没有配置目标与 before / after 合同，不采用；新增窄 Audit 表不构建通用审计平台。

## 迁移、验证与实施顺序

### 迁移与恢复

- 新增 migration 009，建立两张配置表及必需约束；不改写 001～008，不自动改现有管理员、成员或对象归属。
- receipt / Audit 均属于权威数据，加入备份 manifest 表分类、恢复快照与一致性验证；现有工具遇到未分类表会拒绝，不能绕过它。派生 Activity 仍按现有流程重建，Session 和未兑换邀请不随数据恢复。
- readiness 继续精确匹配 migration history；新 Go / Web / schema 按同版本发布。回退为升级前备份恢复到全新目标并运行旧版本；不提供 destructive down migration。
- 本提议不授权在真实实例执行 migration、启动服务或改远程状态；实施中的隔离数据库 / 浏览器环境另按具体目标与清理范围申请。无需安装或升级依赖。

### 实施切片

1. 同步领域 / 核心契约；实现配置 command、权限、不变量、migration、receipt、Audit、事件与恢复分类，完成定向单元和真实 PostgreSQL 验证。
2. 接入专用 HTTP 路由、严格 DTO、成员选择与配置读取；复用现有短请求 listener，避免现有 discovery 路由的 GET-only handler 截获新增 POST。
3. 接入现有首页与邀请路径，完成上述 owner → 普通成员的端到端操作，并同步服务说明和当前状态。

切片依次推进，不把后端通过记为 Web 闭环，也不把浏览器一次通过记为团队持续使用。

### 验证矩阵

| 层级 | 必须证明 |
| --- | --- |
| 单元 / HTTP | 字段白名单、canonical digest、初始 admin 显式确认、角色白名单、expected 条件、Host / Origin / CSRF、错误收敛、配置 DTO 最小化 |
| 真实 PostgreSQL | 从正式 bootstrap / command 起步；并发创建单胜者、key 冲突、幂等 payload 冲突、Audit / receipt / event 故障整体回滚、scope 外键与不可变约束 |
| 权限与并发 | owner 不穿透、非 admin 拒绝、受限频道 admin 未加入仍不可发现、撤权与发送 / 配置竞争、旧 receipt 不复权、成员清理不泄漏私密对象、跨 Workspace / 暂停 / 禁用拒绝 |
| 管理限制 | 不能新增 admin、不能降级或移除既有 admin、归档拒写；非 admin 清理后窄授权不复活；移除 workspace 可见 Project 角色仍保留只读基线 |
| Web | 空实例配置、既有邀请新账户 / 已登录账户兑换、重名成员选择、权限不足与错误恢复、迟到响应、双击 / 模糊失败重试、分页、手机与桌面布局 |
| 恢复 / 投影 | 新表进入备份恢复、历史 receipt / Audit 保留、正常创建事件与重建一致、未授权用户看不到成员审计、旧 schema readiness 拒绝 |
| 仓库检查 | 按范围运行 Go 格式 / test / vet、Web 检查及 `./scripts/check-repo.sh`；仅记录实际执行结果 |

合同审查本身不代表以上实现或测试已经完成。管理员交接、账号恢复、安全失败审计和生产升级窗口仍按路线图独立验收。

实施与实际验收范围见 [2026-09-14 记录](../status/reviews/2026-09-14-foundation-configuration.md)。上表为验收目标，不能据此推断所有组合均已执行。首次访问初始化另见 [ADR-0027](0027-first-visit-administrator-setup.md)。
