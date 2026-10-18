# ADR-0041：自部署本地账户受控恢复

状态：已接受并实施（2026-10-18）

日期：2026-10-18

延续：[ADR-0012](0012-local-identity-and-session-foundation.md)、[ADR-0023](0023-local-account-and-radish-oidc-login.md)、[ADR-0027](0027-first-visit-administrator-setup.md) 的身份边界；补充尚未开放的本地密码恢复，不替代首次初始化、成员治理或备份恢复合同。

## 背景与源码基线

基线为 `5ddcc1c`。Project 工单发现、Ticket 最小状态流转和 Project 决策发现已经完成，近期进入试用准备。[路线图](../roadmap.md#试用与生产边界)要求试用前具备账号恢复及明确的运维支持路径。当前只有初始化、登录、邀请与旧账户邮箱映射；唯一 owner 遗忘密码时，不能重新 bootstrap，也不能让维护者手工修改数据库成为产品恢复入口。

最初提案以该基线核对源码与文档。下表保留实施前的事实及约束；本轮所有者随后授权实现与隔离验收，实施结果见后文。

| 核对项 | 源码 / 正式合同 | 对本片的约束 |
| --- | --- | --- |
| 账户与本地凭证 | [migration 008](../../server/db/migrations/008_federated_identity.sql)、[身份 service](../../server/internal/platform/authn/service.go) | `user_accounts` 与 `local_credentials` 分离；账户与凭证各有 active / disabled，邮箱不可改，password hash 变化要求更晚的 `password_changed_at` |
| 认证算法 | [password.go](../../server/internal/platform/authn/password.go)、ADR-0012 | 复用当前 Argon2id、随机 salt、密码长度和严格 verifier parser，不新造算法、依赖或默认密码 |
| Session | [Session Store](../../server/internal/platform/authn/postgres/store.go)、[migration 005](../../server/db/migrations/005_local_identity_and_sessions.sql) | Session 绑定全实例 user，不绑定 Workspace；resolver 重查账户与当前 membership；只能单向撤销，不能改身份、有效期或复活旧行 |
| 成功 / 失败登录并发 | `CreateSession` 先锁 `user_accounts` 并以读到的 password hash 复核；`RecordFailedLogin` 当前只按 user / 状态 / 锁定时间更新 | 成功登录已有防旧凭证落库基础；失败计数缺少凭证版本条件，恢复前开始的迟到失败不能污染新凭证 |
| 外部身份与准入 | [identity Store](../../server/internal/platform/authn/postgres/identity.go)、[OIDC Store](../../server/internal/platform/authn/postgres/oidc.go)、ADR-0023 | 解绑 / 绑定按账户行串行化；邀请不是密码恢复。真实 OIDC 仍未装配，不能借此片接入 provider |
| 当前身份审计 | migration 008 的 `identity_audit`、`recordAudit` | 实际只有 id、封闭 action、非空 actor_id、可空 workspace_id、occurred_at；没有 operator / target / 恢复操作记录，也没有独立 result 字段。现有记录不能冒充部署者恢复审计 |
| 初始化与旧映射 | [setup](../../server/internal/platform/authn/setup.go)、[bootstrap CLI](../../server/cmd/nexus-bootstrap/main.go)、[identity-migrate CLI](../../server/cmd/nexus-identity-migrate/main.go) | 已有账户永不重开初始化；旧邮箱映射不重置密码、不授予角色；恢复另设明确命令 |
| 备份分类 | [backuprestore.go](../../server/internal/backuprestore/backuprestore.go)、[ADR-0010](0010-verified-postgresql-backup-and-restore.md) | 账户、凭证、外部映射和 identity_audit 是权威备份数据；Session、未兑换邀请、OIDC 事务只有 schema，不恢复数据 |
| schema / 装配 | [readiness](../../server/db/readiness.go)、[ADR-0024](0024-read-only-schema-readiness.md)、[Compose](../../deploy/README.md) | 精确 migration identity；现有 operations 工件没有恢复账号命令。DB 权限分工、生产升级和当前版本全套 Compose 演练不能沿用旧证据宣称通过 |

## 决定

### 1. 首片目标与信任边界

提供显式的一次性运维命令 `nexus-account-recover`，恢复**已有、active、已映射邮箱的本地密码凭证**。操作者拥有被授权的部署环境与目标数据库运维访问，通过独立渠道核实账户归属和恢复请求；软件不把知道邮箱、user ID、旧 Cookie 或 Workspace owner 身份当作恢复授权。

该入口的信任根是宿主 / 容器操作权和数据库连接凭据，不是新的产品角色。部署者通过重设密码能以目标用户身份登录，因此它是受控的账号接管能力，不能宣称只是无权限影响的工具。必须限制运维入口、明确人工核验责任并保留操作记录；应用无法防御已经能任意改数据库的恶意部署者。

| 主体 / 目标 | 本片行为 |
| --- | --- |
| 持有目标实例运维授权的部署者 | 可对单一、已核实的 eligible 本地账户执行恢复；须确认跨 Workspace 的影响 |
| Workspace owner / Project admin / 普通用户，仅有产品 Session | 无恢复他人密码的 HTTP / Web 能力；不能用单工作区权限重置全实例账户 |
| active 账户 + active 本地凭证，因失败次数暂时锁定 | 可恢复密码，同时清除该凭证的临时锁定和失败计数 |
| `user_accounts` 或 `local_credentials` 为 disabled | 拒绝；不自动解除禁用或提供 force 选项 |
| OIDC-only、未映射邮箱的 legacy 账户、仅有业务 user、目标不存在 | 拒绝；不补建凭证、猜测邮箱、合并账户或重新邀请代替恢复 |
| 具有多个 Workspace membership 的同一 user | 密码变化及旧 Session 撤销对全账户生效；各 Workspace / Project / Channel / Thread / Environment 授权保持原样 |
| 唯一 owner 丢失密码 | 与其他 eligible 账户使用相同恢复路径；不新建 owner、不迁移角色、不重新初始化 |

本片不做邮箱找回、SMTP / 邮箱验证、改邮箱、账户激活、用户自助修改密码、强制首次改密、恢复码、管理员交接、MFA、OIDC 接入或完整失陷响应。已禁用或没有本地凭证的最后 owner 不由本片解禁或补建，须另行设计身份治理流程。

### 2. 本地 CLI 合同与目标核对

下列合同已由独立 CLI 实现。三个子命令都要求 `--request-stdin`，一次处理一个账户，不接受批量、密码参数或密码环境变量。没有 HTTP route、恢复 URL、常驻恢复服务或自动启动行为。

| 子命令 | 严格 JSON 输入字段 | 输出与副作用 |
| --- | --- | --- |
| `inspect` | `email` | 按既有规范化规则精确查找本地凭证；仅返回 `user_id`、`account_status`、`credential_status`、`password_changed_at`、`eligible`。只读；不回显邮箱、角色、Workspace 列表或 verifier |
| `apply` | `user_id`、`expected_email`、`expected_password_changed_at`、`operation_id`、`operator_ref`、`reason_code`、`new_password`、`confirmed` | 要求 `confirmed=true`；成功只返回 `result=applied`、`user_id`、`operation_id`、`audit_id`、新的 `password_changed_at`。只在确认事务提交后报告成功，不创建 Session |
| `status` | `user_id`、`operation_id` | 只读查询该账户的恢复操作记录；返回 `applied` 及记录中的安全字段，或 `not_observed`。不接收密码，不验证当前密码，不重新撤销会话 |

共同输入规则：单个 UTF-8 JSON object、上限 8 KiB；拒绝未知 / 重复 key、缺字段、null、错误类型、尾随数据和不规范标识。稳定 `usr_` ID 使用现有规则；`operation_id` 为调用者预先生成并保存的 canonical UUID v4，只用于操作关联，绝不是恢复凭据。`operator_ref` 为 1–64 个 ASCII 字母、数字、点、下划线或连字符，指向受控运维记录中的操作者；禁止填姓名、邮箱、凭据或自由文本。`reason_code` 首片固定为 `lost_local_password`。

`expected_email` 经现有规范化后必须与目标凭证精确相同；`expected_password_changed_at` 必须来自本次 inspect，使用固定六位微秒的 UTC RFC3339（例如 `2026-10-18T00:00:00.123456Z`），保留数据库微秒精度，并在写事务中比较原值。不把时间版本当作授权令牌，也不以展示名消歧。inspect 只表示当时的候选状态，apply 必须再次检查。

密码沿用 15–128 个 Unicode code point、最多 1024 bytes、有效 UTF-8 的既有规则，不裁剪、截断或自动规范化；用既有 verifier 检查新密码不同于当前密码，否则拒绝。当前 verifier 损坏 / 算法不支持时失败关闭，不借恢复绕过数据损坏。使用新随机 salt，直接复用既有 password service；不保存原密码、新密码或用于重试的额外密码 digest / verifier。

操作者须先核对部署目录、Compose project 与数据库连接目标，再从受控核验渠道取得当前邮箱并 inspect，人工确认返回的稳定 user ID。数据库连接复用 `runtimeconfig.DatabaseURL` 及文件 Secret；不新增 DSN 参数、不降低已有 TLS 要求。仓库没有实例唯一身份核验协议，user ID 在备份副本中可能相同，不能声称 inspect 自动证明“连到了正确实例”。本片不引入实例注册系统。

输入可由无回显的终端辅助程序输送到 stdin；不得用含真实值的 shell 命令、命令历史、普通日志或截图传递密码。目标用户应在受控终端输入最终密码；没有“临时密码自动过期 / 强制改密”能力，运维者代设和传递密码的责任必须明示，不伪称用户已独占凭证。

### 3. 事务、版本与并发

所有子命令在访问身份表前复用精确 schema readiness，缺失、较新、漂移均拒绝，不自动 migrate。部署升级与恢复操作不得并行；readiness 是观察，不是升级锁。命令总预算 2 分钟，写事务锁等待上限 5 秒，超时失败而不静默循环重试。

语法与 schema 检查后先查询 operation_id 是否已有记录；若已记录，直接拒绝重复应用。未记录只是当时的观察，不能代替事务内再次检查和数据库唯一约束。KDF / 新旧密码比较可在事务前对候选快照执行，避免持锁计算。

apply 的唯一写事务按以下顺序执行：

1. 锁定目标 `user_accounts` 行，再锁 `local_credentials` 行；与现有登录、绑定 / 解绑的账户锁顺序一致。重查账户 / 凭证 active、已映射邮箱、预期邮箱与密码版本、操作 ID 未使用。
2. 确认用于 KDF / 新旧密码比较的同一凭证版本及 hash 仍成立。候选过期则返回冲突，不能自动重读并覆盖。
3. 使用数据库时间更新 password hash 与 `password_changed_at`，后者必须严格晚于旧值；时钟未前进或倒退时返回 `clock_not_advanced` 并回滚，不用调用方时间覆盖。清零 `failed_login_count`、清空 `locked_until`；保留邮箱、created_at、凭证 / 账户状态和 `last_authenticated_at`，恢复本身不是认证成功。
4. 撤销目标 user **全部尚未 revoked 的 Session**，包含不同 Workspace、设备和认证方式产生的会话；按既有约束使用不早于各 Session created_at 的 revoked_at，不删除、不改有效期、不重新签发。
5. 在同一事务插入一条不可变的恢复成功审计 / 操作记录；唯一 operation_id、凭证变更与 Session 撤销共同提交。任一步失败全部回滚，不能先改密码再补审计。

密码恢复不修改 user ID、展示名、业务作者、membership、Project 角色、私密授权、Environment 授权、外部身份映射、业务事件、Outbox 或 Activity。没有登录后自动获得新权限的旁路。

并发不变量：

- 两个恢复使用同一预期版本，最多一个应用；后者必须显式重新 inspect / 确认，不能“最后写入者胜”。
- 成功登录若先取得账户锁，其新 Session 必须被随后的恢复撤销；若恢复先提交，使用旧 hash 的 `CreateSession` 必须拒绝。无论 HTTP 响应先后，恢复前凭证不得在恢复之后留下可用 Session。
- `RecordFailedLogin` 必须接收并匹配本次校验读到的凭证版本 / hash，必要时按同一账户→凭证锁序重查；过期快照只能产生原登录失败，不能增加新凭证失败次数或重新锁定它。这是本片必须修正的现有内部边界，不改公共登录错误合同。
- 账户 / 凭证禁用、外部绑定 / 解绑与恢复按账户锁及真实当前状态裁决；无论执行顺序，恢复不能解除禁用或补回绑定。新增恢复代码不得只锁凭证而绕过账户锁。

### 4. Session、在途请求与其他登录能力

恢复提交后，新开始的 Session / CSRF 解析必须拒绝旧 token，浏览器下一次请求按既有 401 路径清理并重新登录。SSE 沿[现有 handler](../../server/internal/platform/httptransport/channel_events.go)的发送复核和 15 秒 heartbeat 复核关闭旧订阅，不增加第二套推送机制。

这不追溯撤回已读取的数据，也不保证在事务提交时瞬间取消所有已通过认证的在途业务请求；不能把“撤销 Session”描述成全实例事务终止。要求无在途操作时，部署者先摘流、停止并等待应用退出，再执行恢复并重启；命令不自动停止服务。并发测试仍必须验证上述认证不变量，不能只靠停服掩盖竞态。

保留原 external binding；新的有效外部认证仍可签发 Session，正在绑定的旧 Session 则须在 callback 复核时失败。未兑换邀请仍按原期限和创建者当前 owner 状态有效，恢复不批量删除邀请或修改成员。恢复不是外部 IdP 登出、邀请撤销或完整账号失陷处置；疑似泄露场景须单独摘流与调查这些能力，不能仅凭本命令宣布风险已消除。真实 OIDC 接入仍暂缓，已有身份 Store 的回归不等于 provider 已联调。

### 5. 审计及提交结果不明确

新增 forward-only migration 017，扩展既有 `identity_audit`，不另造平行安全事件表：

| 数据 | 建议规则 |
| --- | --- |
| `action` | 新增 `local.password.recovered`；只表示成功应用 |
| `actor_kind` / `actor_id` / `operator_ref` | 既有行保持 `actor_kind=user` 与原 actor_id，operator_ref 为 null；恢复行为 `actor_kind=operator`、actor_id 为 null、operator_ref 必填。不得把目标用户填为操作者 |
| `target_user_id` | 恢复行为必填，引用既有用户；旧动作保持 null，不推断或伪造旧 target |
| `operation_id` / `reason_code` | 恢复行为必填；operation_id 数据库唯一，reason_code 固定；旧动作保持 null |
| `previous_password_changed_at` / `applied_password_changed_at` | 恢复行为保存版本前后值，后值严格递增；不保存新旧密码 / hash / digest |
| `workspace_id` / `occurred_at` | 恢复 workspace_id 必须为 null，因为是全账户动作；occurred_at 使用本次数据库变更时间 |

数据库约束须区分两种 actor 形状；恢复记录拒绝 UPDATE / DELETE，以及把其他动作改写成恢复记录。既有 action 和写入调用继续兼容。operator_ref 是运维者声明的追溯标识，不是通过 Nexus Session 认证的人员证明；真实人员归属需结合受控宿主 / 运维日志，不能据此宣称完整不可抵赖审计或完成数据库最小权限分工。

成功行同时承担该 operation_id 的提交证据，不保存第二份密码 verifier 来提供请求重放。明确采用以下语义：

- 已成功使用的 operation_id 再次 apply，一律返回 `operation_already_recorded`，不修改任何状态，也不因为 ID 相同就声称本次传入密码已应用。跨 user 使用同一 ID 同样拒绝，不返回其他目标细节。
- 模糊提交 / stdout 写出失败后，操作者保留原 operation_id，运行 status。`applied` 只证明该操作的历史应用，不保证密码仍是当前版本或账户仍可登录；后续已发生的新恢复不被旧操作覆盖。
- `not_observed` 只表示当前数据库查询快照没有该已提交记录，在途事务仍可能提交；不能据此直接换新 ID 重置。待原进程结束 / 诊断连接后，可以用同一 ID 和原预期版本重新尝试：原操作若已提交则拒绝重复，若确实回滚才有机会应用。
- 重试输入的新密码仍由操作者负责。由于没有保存原请求密码指纹，不宣称“相同 ID 精确重放了同一密码”；需要确认密码本身时走正式登录，不能通过 status 验证密码。

拒绝、输入错误、写入前连接失败和已确定回滚不写成功行，输出固定脱敏分类及非零退出状态。已经发起提交后连接中断或 COMMIT 未获确认时输出“结果未确认”，不能一律归类为未应用。已提交但 stdout 写出失败也返回非零并指引 status，不撤销或重做事务。不得通过独立补写事务制造与实际提交脱节的成功审计。失败过程由运维侧保存脱敏退出结果；本片不宣称持久化所有失败尝试。

错误输出不包含邮箱、密码、verifier、stdin、Cookie、DSN、Secret 路径、SQL 参数或 PostgreSQL Detail；沿用私有数据库错误包装保留内部 cause，CLI 只输出安全分类。成功与失败测试都要捕获 stdout / stderr / 日志验证，不能只检查返回码。

### 6. 迁移、备份与回退

实施需新 migration 扩展审计约束和不可变恢复记录、同步 upgrade / readiness 测试与操作工件。不能编辑既有 migration 005 / 008；不新增依赖，不调整密码算法或 Session 有效期，不把新命令装入常驻 server 镜像。operations 工件新增显式、默认不运行的恢复命令，复用文件 Secret 和数据库网络；部署数据库角色拆分仍属独立工作，不把 Compose 当前共用凭据解释成最小权限完成。

`identity_audit` 已归类为权威备份数据，新恢复记录随其保留；不改变现有表分类，不新增恢复 token、一次性能力或额外密码副本。Session、邀请和 OIDC 事务继续只恢复 schema；外部映射及本地凭证仍是受控运维备份数据，不因此进入未来 `.nexus` 导出。

必须验证两条不同链：

1. **恢复操作后备份 → 同版本全新空目标**：新密码有效，旧密码无效，稳定 user / 权限 / 业务引用与恢复审计一致；全部旧 Session、邀请和 OIDC 事务数据为空，bootstrap 仍关闭。status 可确认被备份的历史操作，但不能创建 Session 或触发恢复。
2. **恢复操作前备份 → 旧工件恢复空目标 → 显式升级**：备份会恢复当时的旧 verifier，也可能没有后来的恢复审计。密码恢复不会让历史备份自动失效；恢复目标须保持隔离，操作者重新核对并执行新的凭证恢复后才开放访问。禁止用旧二进制写新 schema，也不能把旧 operation_id 跨实例 / 跨备份当全局真相。

保留原受控备份用于故障取证，不原地降级、修改旧 verifier 或删除备份冒充回滚。恢复失败按既有规则停止并使用新的空目标；不自动覆盖生产实例。更改密码后再生成新的受控备份是运维步骤，需要单独明确存储、保留与执行范围，不由命令自动触发。

## 替代方案与取舍

| 方案 | 本片取舍 |
| --- | --- |
| 邮箱自助找回 / 恢复链接 | 邮箱未验证、投递尚未建立；当前不具备证明账户归属的合同，暂不采用 |
| Workspace owner 重置成员密码 | 账户可跨工作区，单 Workspace 管理权不足以控制全局身份，不采用 |
| 重开 setup、创建第二个 owner 或再次兑换邀请 | 会改变主体 / 授权或绕过唯一初始化，不采用 |
| 直接 SQL 更新 hash | 缺少版本、并发、Session 撤销与原子审计，不作为受支持流程 |
| 只吊销 Session / 只更换密码 | 前者不阻止旧密码重新登录，后者保留旧会话；两者必须在同一事务完成 |
| 恢复码、临时密码、强制改密、多因素恢复 | 引入新的秘密生命周期和用户流程；不是此片的最小范围 |
| 存储请求密码 digest / 第二份 verifier 做精确重放 | 增加凭据材料和备份面；选用版本冲突、历史操作查询及明确的重试语义 |

收益是不用云或邮件服务也有正式恢复路径，保留历史业务身份并处理模糊提交；代价是需要可信部署者、人工归属核验、新审计 migration 和跨认证路径的并发验证。该设计不能替代管理员交接、账号激活、全套安全 Audit、运维权限最小化或失陷响应。

## 验收矩阵

以下为实施验收要求；本轮逐项证据及证据层限制见[实施记录](../status/reviews/2026-10-18-controlled-account-recovery.md)。夹具须经正式 bootstrap / 邀请建立身份，通过正式登录创建 Session；只在禁用、损坏或故障专项中直接准备异常数据库状态，并明确标注用途。

| 编号 | 场景 | 必须成立的结果 | 证据层 |
| --- | --- | --- | --- |
| P01 | 普通账户、唯一 owner 丢失本地密码 | 新密码正式登录成功、旧密码失败；原 user、角色、历史作者不变；setup 仍关闭 | Service + PostgreSQL + CLI + HTTPS 浏览器 |
| P02 | 同一 user 在多个 Workspace，含 suspended membership 与 restricted 授权 | 所有旧 Session 失效；原 membership 状态 / 角色和窄授权完整不变，不新增可访问对象 | PostgreSQL + HTTP |
| P03 | Workspace owner / Project admin / member 只有 Session；猜测恢复 URL；DB 连接无权或只读 | 没有恢复 HTTP / Web 路由；角色不能代替数据库运维授权；未获 DB 写权限不能应用、无半完成 | HTTP 装配 + CLI 权限环境 |
| P04 | user account disabled、credential disabled、暂时 locked | 前两者拒绝且无写入；临时锁定可随成功恢复清除 | PostgreSQL |
| P05 | 目标不存在、无本地凭证、OIDC-only、legacy 未映射、user / email 不匹配 | 全部拒绝；不创建凭证、改邮箱、合并或授予角色 | Service + PostgreSQL + CLI |
| P06 | 操作连接错误实例 / 相同 ID 的备份副本 | 操作说明要求部署目标核对；不得宣称 ID / inspect 是实例认证 | 运维说明审查 + 双实例流程 |
| I01 | 空 / 超长 / 非法 UTF-8 输入、重复 / 未知字段、尾随 JSON、参数 / 环境密码 | 解析失败，无数据库写入、无输入回显；不接受未声明输入渠道 | CLI 单元测试 |
| I02 | 密码边界、中文、空白、旧密码复用、损坏 verifier、随机源失败 | 沿用既有规则、不改变原文；相同密码及损坏 / 随机失败拒绝且无副作用 | Service + CLI + PostgreSQL |
| I03 | 未 confirmed、无效 ID / UUID / operator_ref / reason、错误时间精度 | 拒绝；不自动确认、规范化到另一个目标或猜测版本 | CLI + Service |
| T01 | 完整成功事务 | 一次更新密码版本、清临时锁定、撤销所有非 revoked Session、插一条正确 operator 审计；无 Session 签发 | PostgreSQL 快照 |
| T02 | hash 后、更新后、撤销后、审计插入失败、提交前取消 | 已确定回滚时所有权威行与原快照一致，无半完成；语义 / 约束不放宽 | PostgreSQL 故障注入 |
| T03 | 两个恢复同一预期版本，不同 / 相同 operation_id | 最多一个应用；其余冲突 / 已记录，不覆盖胜者、不重复审计 | PostgreSQL barrier 并发 |
| T04 | 已提交的 operation_id 用于另一个 user 或改变输入 | 拒绝再次应用，数据库唯一约束兜底；不泄露其他目标、不伪称本次密码已用 | PostgreSQL + CLI |
| T05 | 旧密码登录验完后暂停，与恢复交错；登录先取得锁 | 恢复先则旧 hash 无法建 Session；登录先则该 Session 被恢复撤销 | PostgreSQL barrier + HTTP |
| T06 | 旧密码失败计数延迟到恢复后落库；恢复后新失败 | 旧快照不影响新计数 / 锁定；新凭证的失败照常计数和限流 | Service + PostgreSQL barrier |
| T07 | 禁用、绑定 / 解绑与恢复交错 | 无解除禁用、丢失绑定检查或遗漏旧 Session；锁超时明确失败，死锁受害事务完整回滚 | PostgreSQL + 现有身份回归 |
| T08 | 数据库时钟不晚于旧版本；Session created_at 晚于恢复时刻 | 前者整体失败；后者 revoked_at 满足既有约束且不可再认证，不修改 Session 身份 / expiry | PostgreSQL |
| S01 | 多浏览器 / 设备旧 Cookie、CSRF，恢复后读写和重登 | 新请求 401；UI 清理旧上下文；新密码重新登录仍使用当前授权 | HTTP + HTTPS 浏览器 |
| S02 | 已连接 SSE，恢复后无消息 / 有新消息 | heartbeat / 发送复核关闭；不复活旧连接，不声称追溯撤回已发送数据 | HTTP 流集成 |
| S03 | 外部 binding、旧会话发起的绑定事务、已发邀请 | binding / 邀请保持原合同；旧 Session 绑定不能完成；新的合法外部认证不被描述为已禁止 | 身份 Store 回归；不代替真实 OIDC |
| A01 | operator 成功行与旧身份审计迁移 | actor / target 分离，恢复 action / 字段 / 版本约束正确；旧行语义不改，不伪造 user actor | migration + PostgreSQL |
| A02 | 修改 / 删除恢复记录、改写旧 action 为恢复 | 数据库拒绝；备份恢复正常 INSERT 不受阻 | PostgreSQL + 恢复 |
| A03 | stdout 丢失、COMMIT 确认丢失、原事务仍在途 | 不伪报未应用；status 分清 applied / not_observed；原 ID 重试无重复或覆盖，新 ID 不自动生成 | CLI + 数据库故障注入 |
| A04 | 操作后又恢复一次，再查旧 operation_id | 返回原历史事实；不声称旧密码仍有效、不覆盖当前版本、不重撤新 Session | PostgreSQL + CLI |
| A05 | 成功、拒绝、SQL 约束错、连接 / Secret 文件错、调试日志 | stdout / stderr / 日志 / 审计无密码、邮箱、verifier、DSN、SQL Detail；成功只在事务确认后输出 | CLI 捕获 + 敏感标记断言 |
| B01 | 016 升级到新审计 migration | 旧身份数据和审计保持；旧工件 readiness 拒绝新 schema；新命令拒绝旧 / 漂移 schema | 升级 + readiness |
| B02 | 恢复操作后备份与同版本空目标恢复 | 新密码 / 审计 / 业务身份保留，旧密码拒绝，Session / 邀请 / OIDC 事务为空，setup 关闭 | 双实例备份恢复 + 正式登录 |
| B03 | 操作前备份恢复再升级 | 明确旧 verifier 可回来；隔离目标经新核验 / 新操作恢复后才开放，不能复用原实例结果推断成功 | 双实例回退演练 |
| B04 | 非空目标、manifest 损坏、未知 relation / schema 不匹配 | 沿用恢复停止线，无覆盖、忽略 checksum 或放宽表分类 | 备份恢复回归 |
| R01 | 非目标账户与所有业务表前后快照 | 无角色、成员、授权、业务事实、Activity 或 Outbox 变化；非目标身份保持 | PostgreSQL 快照 |
| R02 | 当前 operations 工件装配与正式 HTTPS 链 | 在隔离 Compose 中以 stdin / Secret 完成 inspect→apply→status→登录；常驻 app 不多出恢复入口 | Compose + 浏览器 |
| R03 | 失败退出与清理 | 未确认结果可诊断；隔离容器 / 卷 / 证书 / 临时输入按事先范围清理；不触及真实账户 | 操作记录 |

实施门禁为 `./scripts/check-server.sh`、`./scripts/check-server-postgres.sh`、`./scripts/check-server-backup-restore.sh`、`./scripts/check-repo.sh`；Web 401 / 登录恢复交互按风险运行 `./scripts/check-web.sh`，并完成新 operations 工件与正式 HTTPS 验收。升级、模糊提交及竞态必须使用真实 PostgreSQL，不能以 mock 代替。本轮所有者明确授权隔离资源启动、验收与清理；后续生产或其他实例操作仍须独立授权。

实施退出结论限定为“受支持的自部署本地账户可经受控运维入口恢复，原身份和权限保留，旧会话失效，提交结果可查，升级及空目标恢复可复验”。不能据此宣布管理员交接、全部失陷处置或外部团队试用条件已经全部满足。

## 实施结果与当前停止线

2026-10-18 所有者已确认本 ADR 范围并授权实现，交付 `authn` / PostgreSQL Store、新 CLI、migration 017、operations 装配、失败登录并发修正及测试。Go、真实 PostgreSQL、Web、双实例备份恢复、当前 Compose / HTTPS、浏览器与旧工件恢复升级链已有[逐项证据](../status/reviews/2026-10-18-controlled-account-recovery.md)。server / deploy README、架构与当前状态已同步；所有者随后授权核对后本地提交，未 push、未操作真实账户或生产实例。

迁移、应用和 operations 必须配套交付；不能将本轮隔离验证解读为生产部署已完成。下一步按当前状态分别设计管理员交接、试用支持范围 / 评估授权与升级失败恢复；真实 Jenkins B、原生中文 IME 和 Radish OIDC 继续暂缓。本片不授权这些后续实现或外部操作。
