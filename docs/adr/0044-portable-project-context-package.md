# ADR-0044：Project 结构化上下文导出与新实例导入

状态：已接受；2026-10-24 D01～D06 首片已实现并完成隔离往返 / 浏览器验证，部分边界与容量证据仍待补齐

日期：2026-10-24

基线：`ece637b`，schema 018。所有者已明确暂缓 ADR-0043 的独立人工复验，并要求继续下一阶段；本 ADR 承接 M1 的结构化导出 / 全新实例导入，暂缓不表示 ADR-0043 全矩阵通过，也不授予外部试用权。

## 背景与现有合同

[路线图](../roadmap.md)将版本化结构化导出和全新实例导入列为独立退出条件。[总体架构](../architecture/overview.md#可移植上下文包)规划 manifest、结构化对象与 EntityLink、可读 Markdown、校验和、遗漏说明和导入报告；[ADR-0010](0010-verified-postgresql-backup-and-restore.md)的 PostgreSQL 整库备份仍承担运维恢复，包含密码 verifier 和内部授权历史，不能直接作为可移植格式。

所有者已确认 D01～D06，实施包含 migration 019、版本化格式、受认证 API / CLI 和 Web 来源展示。源码核对确认以下约束：

| 既有边界 | 当前实现 | 对导入的影响 |
| --- | --- | --- |
| 引用与权限 | [核心合同](../architecture/core-contracts.md)、`goldenpath/postgres/access.go`、`relations.go` | Workspace owner 不自动读取 restricted Project / Channel / Thread；导出不得提升权限或从反向关系泄漏对象 |
| Message / Document 作者 | migration 006 / 010 外键指向 Workspace membership | 历史作者不能自动变成可登录的目标成员，也不能全改成导入负责人 |
| messaging Thread 来源 | migration 006 要求同 Channel 恰好一条 active `started-from` Message | 过滤数据时必须检查依赖闭包，不能用无来源 Thread 冒充完整迁移 |
| Document 历史 | migration 010 要求连续 revision、真实 source event、同 Project 的来源关系 | 只导出当前正文不足以支持历史恢复；导入分支需保留版本与来源约束 |
| Ticket 状态 | migration 016 的事件 / receipt 双向约束要求原 web 命令形状 | 不得把旧 client operation 直接带到新实例，也不能为通过约束伪造新 web 命令 |
| Activity / Outbox | 原子投影、当前权限过滤；事件与投递状态分离 | 导入历史须可重建，但不能重新发送通知、Webhook 或执行历史命令 |
| 稳定引用 | 实例内 ID 不复用；导入冲突需显式映射 | 相同字符串不证明同一对象或同一个人；新实例也不能按邮箱 / 展示名自动认领 |

## 已确认首片

采用下表的首片边界，冻结相应新格式、导入来源模型和目标授权行为；所有者随后明确选择“采用推荐首片”，确认本表范围及影响；新增运行资源和远端操作仍按各自授权边界执行。

| 决策 | 推荐首片 | 影响与未覆盖范围 |
| --- | --- | --- |
| D01 数据范围 | 单个 Project 中导出者当前可读的 Channel、Message、Thread、Decision、Ticket、Document 全部版本，以及闭包内 EntityLink / 历史 | 是可读范围的协作包，不是整个 Workspace 或整个 Project 的无遗漏副本；CI / Deployment、附件等另立 profile |
| D02 目标 | 同版本新实例完成正常 bootstrap 后的空业务 Workspace；只允许预先建立身份和成员，不允许已有 Project / Channel / 协作 / 研发对象 | 使用明确目标标识和空业务检查；不是空数据库 restore，也不合并已有项目 |
| D03 初始权限 | 导入生成 restricted Project；只给目标导入负责人显式 admin，并仅在包内私密 Channel / Thread 中授予其初始访问 | 明确收窄 / 重建目标授权；不自动复制 Workspace owner、源成员角色或私密成员名单。其他成员后续经正式配置授予；不宣称原 ACL 等价迁移 |
| D04 作者身份 | 保留源作者引用与来源证据；默认建立不可登录、暂停 membership 的历史身份；首片不自动绑定已有账户 | 历史作者与当前权限分离；不带邮箱、密码或 OIDC subject，不因同名建立身份映射。身份认领另立切片 |
| D05 身份与事实 | 所有目标对象 / 事件使用新 ID，持久保存逐项映射；目标展示“来自导入”，历史事实保留来源时间与作者 | accepted Decision 与已完成 Ticket 是包声明的历史快照，不表示目标用户刚审批或执行；导入者承担接纳快照的明确决定 |
| D06 入口 | 首片受控 Go CLI 完成 prepare / inspect / apply / result；目标 Web 支持来源标识和继续协作验证 | 暂不做网页文件上传、后台导入队列或云端中转；源导出仍受真实用户权限约束，不接受任意 `--actor-id` 冒充登录 |

首片成功只证明本 profile 的独立往返，不能直接关闭包含研发上下文、文件、更多授权和目标环境的全部 M1 退出条件。`.nexus` 不设席位限制，不依赖官方云；本 ADR 不改变许可证或试用授权。

## 数据包合同

首个 profile 为 `project-collaboration-v1`，格式标识 `radishnexus.context`、`format_version: 1`；首片采用新建的 `*.nexus/` 目录，文件为 UTF-8 JSON / JSONL 和 Markdown，暂不压缩。正式字段由 `server/internal/contextpackage` 的白名单 DTO 定义，测试构造合成样例；不以数据库列名或内部表快照作为公开格式。

| 文件 / 内容 | 规则 |
| --- | --- |
| `manifest.json` | 格式与 profile、随机 package ID、源版本说明、源 Workspace / Project 引用、UTC 快照时间、范围说明、文件路径 / 字节数 / 行数 / SHA-256、明确排除的能力类别 |
| `objects.jsonl` | 各类型版本化记录；保存业务状态、归属和包内引用，字段白名单，不接受未知对象类型 / 枚举 / 字段 |
| `actors.jsonl` | 包内历史主体 key 与源不透明主体引用；仅含解释作者所需材料，默认无邮箱、外部账户身份或源成员管理信息 |
| `links.jsonl` | 两端都在闭包内且可读的类型化关系，首片仅支持 asserted、user / import 来源的 started-from、derived-from、implements 与 Ticket→Document relates-to；保留来源历史引用及已支持移除状态，其他断言 / 来源拒绝 |
| `history.jsonl` | 支持类型的最小不可变历史事实、版本、时间、主体和因果引用；不复制任意 JSON payload、业务正文或原始 webhook |
| `revisions.jsonl` | 每个 Document 的连续 revision、标题、作者、来源事件、恢复来源和正文文件引用 |
| `documents/<package-key>/<revision>.md` | 所有可读且支持版本的原始 Markdown；标题、revision、来源和 hash 在对象记录中，不维护第二份正文真相 |
| `scope.json` | 固定说明 profile 排除类别，以及可安全公开的裁剪说明；不列不可读对象的 ID / 类型 / 标题 / 数量或隐藏关系 |

安全与一致性要求：

- 包内引用使用 `(package_id, type, source_id)` 命名空间；不将其解释成目标实例现有 EntityRef，不凭 source ID 自动去重或认人。
- 校验和证明文件一致性，不证明来源身份或历史真实性；不声称具有签名、可信证明或端到端加密。包只能由获授权人员在受控存储中转移。
- 固定文件路径白名单；拒绝路径穿越、绝对路径、链接、设备文件、重复条目、重复 JSON key、未知版本、非法 UTF-8 和未列文件。先读取为任务自有的有界材料并校验，再生成计划；apply 不重新信任可被替换的原路径。
- 首片输入限制：总输入 64 MiB、最多 10,000 个业务对象 / 历史作者、50,000 条历史 / 关系 / 文档版本记录、1 MiB manifest，单 JSONL 行最多 1 MiB，Document 单版本沿用现有 256 KiB 限制。超限明确拒绝，不静默截断；这些是输入防护限制，不构成生产容量承诺。
- 源实例信息不含 DSN、主机私网地址或本机路径。文件名、日志和错误不携带标题、正文或用户邮箱。
- 凭据表、Session、邀请、OIDC 事务、密码 verifier、恢复记录、Secrets、Jenkins source / delivery receipt、外部写授权、源 client operation / receipt、Outbox 投递状态、setup 材料及数据库 DDL 均不进入包。
- 配置 Audit 与账号恢复 Audit 不作为普通项目历史导出；本片另记真实导出 / 导入行为的审计，不把源管理记录重新执行为目标动作。
- 字段白名单不保证正文没有用户手工粘贴的秘密或个人信息。首片不实现自动语义脱敏，不静默改写正文；包交付前由数据负责人审阅，实际材料不进入仓库 fixture 或公开诊断。

## 源权限与依赖闭包

源方要求当前 active Workspace member 且为目标 Project admin；这是额外的批量导出准入，不替代对每个对象、历史和关系两端的读取检查。Workspace owner 本身不能越过 Project 和私密内容权限。

受控 CLI 负责文件落地；源数据由同源、已认证应用入口按当前 Session / CSRF 与近期认证提供。认证沿用现有身份合同，CLI 凭据使用当前 OS 用户所有、无组 / 其他用户权限的普通单链接 JSON 文件；不得为省事生成永久 export token 或允许管理员指定另一个用户 ID。

从可读对象集合构造闭包：

1. Channel、Message、Thread、Decision、Ticket、Document 逐对象解析当前权限，使用正式授权策略；不另写 owner-allows-all 或独立 ACL 引擎。
2. `started-from`、Decision evidence、Ticket implements、Document 创建来源 / revision source 等维持对象不变量的依赖必须在同一支持范围内且可读；否则包准备失败。对源用户只返回不含隐藏对象信息的失败类别和 request ID，不列缺失目标。
3. 非必要、可读的跨范围关系（如 Ticket→Component）不进入此 profile；scope 固定声明该类能力未迁移。不可读关系直接隐藏，不能把关系数或目标类型写进“遗漏报告”。
4. 已有读取合同允许安全泛化的受限占位，不能据此生成缺失实体。导入不创建假 Message、Thread、Decision 或伪造批准来源以补齐外键。
5. 源内单一一致快照产生包；正式发布包前再次核对当前主体 / 对象权限，期间发生撤权则丢弃临时结果。必须与既有配置写入锁建立经测试的顺序，明确权限判定的线性化点；不能只在开始时检查一次，也不要求事后撤回已经完成交付的副本。
6. 不提供可遍历隐藏资源的总数或特定失败原因；相同对外错误须覆盖“无权 / 不存在 / 无法安全构造闭包”，详细诊断仅保留非敏感阶段信息。

需要 Team 归属时，只迁移当前成员允许读取的最小责任元数据；不携带 Team 人员名单或权限继承。无法在现有授权中证明元数据可导出时停止，不从数据库存在推导可读。

## 目标、作者与授权

目标先由正常首次初始化建立导入负责人。导入必须明确指定目标 Workspace，要求其当前 active owner、近期认证和显式确认；目标数据库 schema readiness 必须与实现匹配。导入不开放 setup、不覆盖现有成员、不改密码或现有 owner。

本片允许目标已有本地账号与成员，要求业务对象为空；已有 Project、Channel、协作对象、研发实体、关系或业务历史均拒绝。身份 / 初始化审计不被删除，目标空业务条件由精确 allowlist 定义并在提交事务内重查。不能只按 Project 行数判断空目标。

- 新 Project 强制 `restricted`，源 visibility 保存为来源信息；目标负责人被明确授予该新项目 admin。目标方其他既有成员默认不能发现导入内容。
- 对包内 restricted Channel / Thread 仅为目标负责人建立显式访问；这是本次导入确认中的新授权，不能描述为原成员权限恢复。原 visibility 和权限模型类别保留为来源，原 ACL 不导入。
- 历史主体使用新内部 ID，`user_accounts` 为 disabled、Workspace membership 为 suspended，不建本地凭据 / 外部身份 / Session；不持有 owner 或任何业务写权。角色字段的存储默认值不代表 active 权限。
- 原作者 / decider / 编辑者与当前操作者分别记录；Web 显示历史导入来源，不能把源作者当作目标可登录成员。源作者显示材料不得扩大当前查询能披露的身份范围。
- 后续用户通过现有邀请和授权加入；既有账号即便邮箱、名称或 ID 字符串相同，也不自动绑定历史主体。历史身份认领和授权模板不在首片。
- 导入负责人若不应获得包内全部内容，必须换用合适负责人或重新准备较窄数据包；不能在未知授权下开放目标实例。

这会有意改变源和目标的初始成员权限，必须在 prepare 报告中逐类说明并确认。往返验收应比较“明确批准的目标权限计划”，不能要求所有源 ACL 相等，也不能以成员空白为由省略权限负向验证。

## 历史、幂等与导入来源

migration 019 增加 `context_import_batches`、`context_import_mappings`、`context_import_history` 与 `context_export_audit`；对象与正文仍写各自领域表，来源表不保存第二份业务正文。

- 目标对象与事件使用新 ID；新 ID 和源引用映射与数据同事务落库。所有结构化引用按已知字段映射，不做字符串全局替换。Markdown 原文里的 `entity://` 或外部 URL 保持文本，不自动变成授权关系或可执行请求；报告明确文本链接可能仍指向旧上下文。
- 当前业务状态保留，但被标明是从包接纳的历史快照。accepted Decision 不触发目标审批，Ticket done 不证明目标 CI / Deployment，导入不自动推进任何新工作流。
- 目标本次导入事件的 actor 是真实导入负责人，source 为 import，时间为真实导入时间；源作者、发生时间和原事件类型保存在受约束的历史来源记录。源声明不能冒充目标用户刚刚产生的原生 web 事件。
- Document 连续版本、Ticket 状态演进、来源 EntityLink 和历史时间线仍需可验证 / 可重建。新增导入来源分支必须有独立约束，并使现有正常写入的检查保持原强度；不能 `DISABLE TRIGGER`、改 `session_replication_role`、把 source 改成 web、补造原命令 receipt 或开放普通 API 任填 import 来源。
- 源 request / client operation ID 不进入目标幂等空间；导入以独立 operation ID、完整包 digest、目标 identity、权限计划和格式版本绑定。相同操作精确重试返回同一结果，任何输入变化冲突。
- 同一包在同一目标不能以新 operation ID 再造一份副本；已有成功批次用于结果查询。原生业务命令也不能命中源实例旧 receipt。
- 导入历史只进入获批准的历史投影；不创建待分发历史 Outbox，不发通知、不调用 provider、不抓取包内 URL、不执行插件或 Markdown 内指令。未来消费者不能把历史导入当作待执行外部任务。
- 再导出已导入对象必须保留有界来源链，不能“洗掉”import 来源后变成原生确认；具体最大层数和超限拒绝纳入格式测试。

## prepare / apply / result 与失败边界

1. **prepare**：只读校验包版本、字段、完整性、依赖、历史序列、资源上限；核对明确目标与空业务状态，生成 ID 映射和目标权限计划。不写目标数据库，也不预留一半业务对象。
2. **inspect**：输出可审阅的对象 / 版本计数、明确排除类别、来源可信性限制、作者处理、权限变化、目标标识和计划 digest；必要完整映射保存在受控报告，不打印正文或凭据。
3. **apply**：要求确认精确 digest 与目标，重新验证当前身份 / 空业务状态 / schema；目标维护时停止其他写入口。并发 import 使用同一数据库锁，外部 writer 不能绕过空目标与权限重查。
4. 在一个业务事务中写入映射、历史、对象、关系、当前投影、导入审计和成功 receipt。任何导入或 Activity 失败整单回滚；不能出现数据已开放而来源 / 投影尚未完成。
5. **result**：提交响应丢失时，通过独立读取 operation ID / package digest 查询实际结果，证明提交或仍未观测；不得根据 CLI 退出码判断已回滚。未观测不等于原操作从未发生，重试前需检查运行中事务与锁。
6. 验收通过后再由负责人开放目标；失败不自动删除目标、源或包。清理仅针对明确授权的临时工件，完成前不覆盖输出目录。

物理 schema migration 仍由既有显式 runner 执行，不藏在导入命令中。导入数据失败的回滚不等于 schema 降级；包格式版本与内部 schema 版本分别维护。首片只承诺经验证的当前版本双方，不自动接受未知旧包 / 更高版本。

## 实施切片与验收

| 切片 | 交付 | 完成判据 |
| --- | --- | --- |
| A 合同与只读 prepare | 明确字段 / profile / 限额、严格解析与计划；确认 source 导出授权路径 | 恶意 / 不完整包和非空目标不写库，计划能展示作者与权限变化 |
| B 导出与事务导入 | 复用正式权限，最小 migration / 来源模型 / 映射 / 幂等 / 投影 | 真实源到独立空业务目标往返；提交前后故障、并发与结果查询证明原子性 |
| C 正式用户验收 | Web 导入来源标识、普通用户受控加入、读写 / 来源 / 版本检查 | 登录、发现、关系跳转、历史、后续协作和负向权限通过；独立人工持续使用单列 |

A 的 prepare 或纯解析器不单独称为结构化迁移完成，B / C 不能靠直接写最终业务 fixture 替代正式源数据链。

| 编号 | 必测场景 |
| --- | --- |
| P01 | 从正式 bootstrap、邀请、Message→Thread→Decision→Ticket→Document 建源，包含 accepted / proposed、Ticket 状态变化和文档多版 |
| P02 | 普通成员、非 admin owner、已停用 / 撤权主体不能绕过导出准入和逐对象读取 |
| P03 | private Channel / Thread、不可读 Decision evidence、受限反向关系：无内容 / ID / 类型 / 数量泄漏；必要闭包缺失明确拒绝 |
| P04 | 导出中并发写入得到一致快照；并发撤权遵循已冻结线性化点，不交付未授权包 |
| P05 | 包字段、未知类型 / schema / profile、重复 JSON key / 记录、错误 digest / 字节数 / 行数、超限、截断均拒绝 |
| P06 | 路径穿越、符号 / 硬链接、特殊文件、被替换输入、未列文件被拒绝；不访问网络 URL |
| P07 | 新目标已 migration / bootstrap，仅身份数据可接受；已有业务 / 映射冲突 / 错 Workspace / 错实例拒绝且不修改原数据 |
| P08 | 源到目标所有引用映射一致，不按相同 ID / 邮箱 / 名称合并；历史主体不能登录或获得角色能力 |
| P09 | 默认 restricted 与负责人显式授权；目标其他 owner / member 不自动看到私密内容；后续正常邀请 / 授权有效 |
| P10 | accepted Decision、Ticket 各状态、Document 连续历史 / 恢复版本、移除关系与来源时间不丢失且明确标注 import |
| P11 | 正常 web 写入继续受原约束；伪造 import 事件 / 关系 / 来源、假 receipt、历史缺号不能绕过校验 |
| P12 | 同 operation 精确重试、不同 payload / 计划冲突、同包新 operation、两个并发 apply 不产生重复实体 |
| P13 | 提交前 SQL 错误、进程 / 连接取消、Activity 失败整体回滚；源与目标原身份保持 |
| P14 | 实际 COMMIT 后丢失确认，result 查明已提交，同操作重试不重放 |
| P15 | Activity 全量重建与导入时投影一致；无历史外部投递 / 通知 / provider 调用；源 receipt 在目标无效 |
| P16 | 原始正文含 URL / entity URI / HTML 等仍按原 Markdown 限制处理，不自动 fetch / 执行；文本链接限制可见 |
| P17 | 凭据、verifier、Session、邀请码、OIDC、源授权 / 管理审计与 Outbox 不入包；诊断不打印正文或身份材料 |
| P18 | 再导出 / 再导入保留 import 来源链与版本；未知 / 超深来源明确拒绝，不变成原生事实 |
| P19 | 正式 Web 桌面 / 手机登录、发现、来源标识、关系 / 版本读取、继续发消息 / 改状态 / 保存文档、撤权后刷新与后退 |
| P20 | 记录包与目标规模、prepare / apply / projection / 验证耗时；只清理专属资源，保留脱敏报告；人工操作与容量限制据实记录 |

2026-10-24 所有者已单独授权本轮专属临时 PostgreSQL 容器、回环 HTTPS / Chrome、SQL / 取消 / COMMIT 确认丢失故障与精确清理；仅使用已有依赖和镜像，不操作真实实例、系统信任或远程状态。P01～P20 仅按实际验证结果记录，独立人工持续使用保持单列。

## 替代方案与代价

| 方案 | 判断 |
| --- | --- |
| 全 Workspace 一次迁移所有研发、插件、身份与授权 | 当前来源 / 外部授权 / 多权限边界过多，先保留后续 profile，不把它混入首片 |
| 直接导出内部表 JSON 并还原 | 会耦合 schema、receipt 和凭据，不能形成清晰的可移植合同 |
| 只导出 Markdown 或当前对象内容 | 可以离线阅读，但不能保留 Decision 来源、Document 历史、Ticket 状态与 EntityLink，不满足本片目标 |
| 导入到任意已有 Workspace 并自动按姓名 / 邮箱合并 | 会改变身份、权限和幂等，首片拒绝；以后需独立冲突与认领合同 |
| 保留历史但默认只向负责人开放 | 推荐；代价是其他用户需要重新邀请 / 授权，必须明示权限变化 |

本片使用标准库与现有依赖实现服务端 migration、包 DTO、导出鉴权入口、CLI、导入来源、Activity 与 Web 展示。运行步骤见[上下文迁移手册](../../deploy/context-package.md)。本轮独立往返、后续协作、故障与备份恢复证据见[实施记录](../status/reviews/2026-10-24-portable-context-package.md)；其中明确区分通过、部分覆盖与未执行，不宣称完整 M1 或生产容量已验收。
