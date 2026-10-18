# ADR-0042：Workspace owner 主动交接

状态：已接受并实施（2026-10-18）

日期：2026-10-18

补充：[ADR-0023](0023-local-account-and-radish-oidc-login.md) 的邀请有效性与 [ADR-0026](0026-foundation-configuration-and-membership.md) 的配置审计、重试合同。仅本交接命令允许已降为 member 的原操作者读取自己的历史成功结果；其他配置命令继续按当前管理权限检查。Project admin / 私密 Channel 管理权交接仍未开放，不替代其停止线。

## 背景与源码核对

基线 `333c4ce`，schema 017。已有 [ADR-0041](0041-controlled-local-account-recovery.md) 恢复原账户密码，但不会把 Workspace 管理责任交给另一人。本片解决“现任 owner 仍能登录，希望把本工作区管理责任交给已有成员”；失联、禁用或无 owner 的应急接管另行设计。

| 已有事实 | 源码 / 合同 | 对本片的约束 |
| --- | --- | --- |
| membership 独立保存 active / suspended 与 owner / member；没有唯一 owner 约束 | [领域模型](../domain-model.md#workspace)、[身份 Store](../../server/internal/platform/authn/postgres/store.go) | 不假设每个 Workspace 恰好一个 owner，不把多 owner 实例迁成单 owner |
| Session 不固定角色，每次解析读取当前 membership | [Session resolver](../../server/internal/platform/authn/postgres/store.go) | 交接无需更换稳定用户或批量撤销 Session；旧角色缓存不构成授权 |
| 配置写入先锁账户、membership，再锁 Workspace / Project / Environment | [基础配置](../../server/internal/goldenpath/postgres/configuration.go)、[双主体授权](../../server/internal/goldenpath/postgres/delivery_configuration.go)、[Repository 配置](../../server/internal/goldenpath/postgres/repository_configuration.go) | 不能新增 Workspace → membership 的反向锁顺序；交接不获取业务对象锁 |
| 邀请创建锁操作者账户；兑换先锁邀请，再复核创建者 active 账户和 owner membership | [identity.go](../../server/internal/platform/authn/postgres/identity.go) | 交接与创建 / 兑换必须线性化；不能持 membership 锁后再批量删除邀请，否则会与兑换形成反向等待 |
| 邀请只检查创建者当前角色，没有 owner 任期标识 | [ADR-0023](0023-local-account-and-radish-oidc-login.md)、[identity.go](../../server/internal/platform/authn/postgres/identity.go)、[OIDC Store](../../server/internal/platform/authn/postgres/oidc.go) | 原 owner 再次取得 owner 后，尚未过期的旧邀请可能恢复有效；本片必须显式解决 |
| 配置 receipt 的唯一键包含 subject，读取前会检查当前管理权限 | [migration 009](../../server/db/migrations/009_workspace_configuration.sql)、[Configure](../../server/internal/goldenpath/postgres/configuration.go) | 直接照搬会挡住已成功降级者的确认，也不能阻止同 operation ID 换接收人 |
| 身份审计与 Workspace 配置审计分开，配置审计 / receipt 不可变 | [migration 017](../../server/db/migrations/017_controlled_account_recovery.sql)、[migration 009](../../server/db/migrations/009_workspace_configuration.sql)、[migration 012](../../server/db/migrations/012_component_environment_configuration.sql) | 本片是工作区权限配置，复用配置审计，不冒充全局 operator 恢复或普通 Activity |
| membership、配置审计 / receipt 保存数据；Session、邀请与 OIDC 事务只恢复 schema | [备份分类](../../server/internal/backuprestore/backuprestore.go) | 交接历史与角色代次须保留；恢复不能复活邀请或登录态 |

以上记录设计时的基线事实。2026-10-18 所有者确认后，已按下述合同实现 migration 018、Go、邀请代次检查与 Web；验收证据见[实施记录](../status/reviews/2026-10-18-workspace-owner-handoff.md)。

## 推荐范围与影响

| 决定 | 推荐合同 | 影响 |
| --- | --- | --- |
| 操作者 | active 账户、同 Workspace active owner、有效且最近 10 分钟内认证的 Session | 复用近期认证语义；数据库 / 宿主运维权不能作为此 Web 命令的替代身份 |
| 接收人 | 另一个已有 active 账户、同 Workspace active member；不能是自己或已有 owner | 不创建账户，不通过邮箱猜测主体，不启用 suspended / disabled，不减少其他 owner |
| 成功变化 | 接收人 member → owner；操作者 owner → member；两项在一个事务内完成 | 是交接，不是新增备用 owner 或单独降级；owner 数量在本命令内保持不变 |
| 用户确认 | 操作者选择稳定 ID 对应的成员并明确确认，不建立接收人审批流程 | 上线前由双方线下确认接任意愿；active 状态不证明对方当前在线或记得密码 |
| 权限范围 | 只更改两个 Workspace role 及各自 role_revision | Project / Channel / Thread、Environment 显式授权、Team 责任归属、业务作者、外部身份和密码不变 |
| 邀请 | 邀请绑定创建者当时的 role_revision；交接使原任期邀请永久失效 | 原 owner 再次接任也不能复活旧邀请；其他 owner 及其他 Workspace 的邀请不受影响 |
| 结果确认 | 当前 active 成员可按自己的 operation ID 查询历史事实；已记录精确重试只返回历史结果 | 仅新增窄范围读取例外，不恢复 owner、不允许重放写入、不开放审计列表 |
| 持久化 | migration 018 扩展 membership、邀请、配置 audit / receipt 及约束 | Go / Web / schema 配套升级；无新依赖，业务实例迁移仍需独立授权 |

Workspace owner 可以创建 Team / Project / Component / Environment / Repository、管理邀请及显式 Environment 授权，但并不自动获得 restricted Project / Channel 的访问权或记录 Deployment 的权限。接收人获得这些既有 owner 能力；原操作者失去它们，但已有 Project admin 和已授予的 Deployment 记录权仍保留。这不是“原管理员退出全部权限”，界面必须解释。

不提供 owner 增删通用编辑器、批量角色变更、Workspace 退出 / 暂停、全局账户禁用、Project admin 转移、私密频道接管、强制接收、运维应急接管或完整离职流程。不重开 setup，不复用密码恢复 CLI 修改授权。

## 状态、代次与最后 owner 保护

### 交接前后状态

设操作者 A、接收人 B，角色代次分别为 a、b：

| 主体 | 必需前置状态 | 唯一成功后置状态 |
| --- | --- | --- |
| A | account active；本 Workspace membership active / owner / a | membership active / member / a+1 |
| B | account active；本 Workspace membership active / member / b | membership active / owner / b+1 |
| 其他主体 | 不参与本次交接 | 角色、代次及所有权限不变 |

`workspace_memberships.role_revision` 使用正 bigint，初始值 1，只在 role 实际变化时递增 1；本片不改 membership status。数据库约束 / trigger 应拒绝角色变化而代次不增、代次倒退或无角色变化却改代次；溢出拒绝，不能回绕或重置。HTTP 以无前导零的十进制字符串传输，范围 1..9223372036854775807，避免 JavaScript 数值精度丢失。

请求必须携带所见的 a、b；role 的预期值固定为 owner / member，不由客户端指定任意目标角色。A→B→A 后，即使角色回到原值，旧页面的代次也已过期，不能成为一次新的交接。账户密码恢复不改 role_revision。

### 保护的准确含义

本片不提供“只降级自己”命令。必须在持有 A、B 当前账户与 membership 锁时确认 B 能接任，提升 B、降级 A、递增代次、审计和 receipt 全部原子提交；任何一步失败全部回滚。外部读取不能看见无 owner 的中间状态。唯一 owner 可以通过此命令交接；多个 owner 的实例只替换操作者本人，不合并、不降级其余 owner。

最后 owner 保护来自“有效 owner 换成另一个有效 owner”这一不可拆分操作，不来自事务外先查数量。两个 owner 同时交给同一 member 只能有一个成功；交给不同 member 可以分别成功，数量保持。交接不会修复已有零 owner、全体 owner 被禁用或 suspended 的异常实例，不能用目标 ID 强行取得管理权。

该保证限定于本命令的合法事务和当前认证 / 配置入口，不声称数据库已有覆盖任意 SQL、宿主管理员或未来账户禁用接口的全局最后 owner 约束。未来开放 membership 暂停 / 删除或账户禁用时，必须另行设计跨工作区最后有效 owner 保护；不能沿用本片验收宣称已覆盖。

## 邀请合同

邀请表新增 `creator_role_revision`，创建时在既有 owner 检查和锁内明确写入当前代次。创建者账户 active、membership active / owner、代次相同，以及原有期限、单次使用条件必须同时成立，才能兑换。

- A 交接后，A 在该 Workspace 旧任期发出的所有未兑换邀请立即不可再兑换；保留原行等待既有到期清理，不逐条改写或删除邀请，不回传失效数量、token 或被邀请人。
- B 的新邀请绑定 B 的新代次。A 将来重新成为 owner，只能创建新代次邀请；旧代次不再匹配。其他 owner 的代次和邀请不变。
- 创建邀请先取得 A 的有效 owner 锁并提交，则邀请可以先创建，但交接提交后失效；交接先完成则 A 的新邀请创建被拒绝。
- 兑换先取得创建者 membership 的共享锁并验证，允许先完成准入；交接等待该事务结束。交接先提交，则后续本地兑换和 OIDC 准入复核都拒绝。已经成功加入的成员不因邀请者交接而被移除。
- OIDC start 的检查不能替代 callback 的复核；start 先通过、callback 在交接后到达时仍拒绝准入。已有 external binding 的正常登录不以 owner 身份为前提；不受交接影响。此处只要求 Store 合同测试，不宣称恢复真实 provider 联调。
- 本片只保证交接导致的旧任期邀请不复活；没有新增暂停 / 解暂停入口，不能把现有 status 复核描述成独立的永久邀请撤销功能。

升级时为所有既有 membership 设置 role_revision=1；只为仍未兑换且创建者当前 active / owner 的邀请绑定该代次，原 token digest、期限与创建事实保持；其他旧邀请使用不可匹配的哨兵值 0，永久不可用。列允许 0 用于旧无效行，正式新邀请必须明确写入正代次，禁止缺值默认成当前代次。迁移不恢复已过期 / 已消费邀请，也不延长期限。

## 同事务授权、锁顺序与失败

交接必须走独立的配置命令分支，不能把新角色加入通用成员编辑下拉框，也不能仅把 `Configure` 的 owner 检查移到 receipt 之后。HTTP 先执行现有 Session / Origin / CSRF 校验，事务内再复核可信的 Session 身份；客户端不能提交 actor ID 或 Session digest。

新写入的锁顺序为：

1. 按 user ID 固定顺序对 A、B 的 `user_accounts` 获取 `FOR SHARE`，完整取得两个账户锁后再进入下一层；不能先调用只锁 A 为 `FOR UPDATE` 的 `sessionAccount` 再补锁 B。
2. 对发起 Session 行获取 `FOR SHARE`，复核属于 A、未撤销、未过期及创建时间不在未来。锁后的服务器当前时间用于有效期和近期认证判定，不能沿用排队前的时间延长 10 分钟窗口。
3. 按相同 user ID 顺序对本 Workspace 的两个 membership 获取 `FOR UPDATE`，不先共享锁再升级。检查当前 membership；查重成功记录后，只有首次写入才检查 owner / member、近期认证及预期代次。
4. 最后锁本 Workspace 行 `FOR UPDATE`，与现有对象创建 / Repository 配置保持 account → membership → Workspace 顺序；重新查重，首次写入在全部等待结束后以当前时间再次检查 Session 到期及近期认证边界，再完成双角色更新、审计与 receipt。此后不再获取额外账户、邀请、Project、Channel 或 Environment 行锁。

可先读 receipt 作历史结果快路径，但它不是授权：快路径必须独立锁 A 的账户、Session、当前 membership 并复核有效性，再读取本人 receipt。首次未查到后等待其他事务提交的请求，必须在 membership 锁取得后再次查重；不能因 A 已降为 member 就把此前成功当作失败，也不能因 B 后来改变状态而拒绝合法的历史结果读取。历史分支不依赖当前 B 的可发现性，不回传 B 的当前信息。

账户使用共享锁，使已有邀请兑换的“接收账户独占锁 → 邀请 → 创建者账户共享锁 → membership 共享锁”可先完成；交接不锁邀请行，避免构造反向等待。现有 owner 配置先持 membership 共享锁时允许先完成；交接先获得排他锁并提交后，新配置及其精确重试按新角色拒绝。已有显式 Project / Environment 权限继续按原合同工作。

与 ADR-0041 恢复、外部身份解绑、账户状态修改的交错由账户锁排序；与 logout 的交错由 Session 行锁排序。先获得有效授权锁的事务可以完成，之后的请求读取新状态；交接不瞬间取消所有在途请求或已经返回的内容，不为所有旧业务命令新增近期认证或 Session 锁。

首次写入使用 5 秒数据库锁等待上限及现有 HTTP 请求 context；取消、锁超时、deadlock、更新行数不符、审计 / receipt 插入失败均完整回滚，不自动换 operation ID 或隐藏重跑。普通 PostgreSQL COMMIT 拒绝是确定失败；确认丢失 / 连接终止无法证明结果，必须按下节确认。真实数据库验收必须证明上述锁顺序与现有入口相容；若发现稳定可重现的锁环，应修正顺序再交付，不能靠吞掉 deadlock 宣称验收通过。

## 操作身份、重试与历史读取

`client_operation_id` 对本命令限定为小写 UUID v4。作用域为 `(workspace_id, actor_id, command_kind='workspace.owner.transfer', client_operation_id)`，与接收人无关；相同 ID 更换目标或预期代次必须冲突。通过配置 receipt 的新增部分唯一索引落实，不改变其他 command_kind 的原唯一键。

canonical digest 包含命令版本、Workspace、服务端确定的 actor、target、双方预期 role_revision 和 `confirmed=true`；不包含 Cookie、CSRF、Session、显示名或客户端时间。一个成功只生成一条不可变配置 Audit 和一条 receipt。

| 情形 | 明确行为 |
| --- | --- |
| 首次有效操作 | 原子完成，返回 `applied` 历史结果 |
| 相同 actor / Workspace / ID / digest 已成功 | 当前账户和该 Workspace membership 仍 active、Session 有效，即可返回原历史结果；不要求仍 owner 或仍在近期认证窗口，不再次更新角色、邀请代次、审计或 receipt |
| 已成功 ID 携带不同目标 / 版本 | `409 conflict`，即使原操作者已降为 member 也不能替换旧请求 |
| 未见 receipt 的新写入 | 仍须当前 owner、近期认证、双方有效及预期版本相符；member 不能借“重试”发起新交接 |
| 账户禁用、Session 失效、membership 已暂停 / 移除 | 拒绝结果读取及重试，不以旧 receipt 绕过当前认证与工作区成员资格 |
| 其他用户或其他 Workspace 查询同 ID | 仅查询调用者自身作用域，未见记录返回 `not_observed`，不泄漏他人的存在或事实 |
| 交接后再次交接或角色回转 | 原 receipt 仍只代表当时成功，不表示接收人现在仍 owner；旧请求永不复权 |
| COMMIT 确认 / HTTP 响应丢失，超时、5xx 或客户端中断 | 结果可能未确认；保留原请求与 ID，主动查询，不自动创建新 ID |

状态查询的 `not_observed` 只是当前快照未见成功记录，不能断言已回滚，不能排除仍在途提交。等待原请求结束 / 诊断后，可用完全相同的请求重试；服务端再次查重与当前授权共同防止双重执行。若仍不具备确认条件，展示“结果尚未确认”，不得把按钮失败当作“交接未发生”。

历史读取是本命令显式限定的例外：原操作者只能读自己的操作，不提供 owner 可查看所有人的审计 API，不向普通成员开放候选列表。对 401 / 403 后的新登录，仅允许同一稳定用户在重新获得有效成员资格后查本人结果；不因更换浏览器账户继承原操作。

## HTTP 与最小 Web 交互

以下为已实现的新接口，统一前缀 `/api/v1/workspaces/{workspace_id}`，复用同源 HTTPS、错误 envelope、可信代理、`private, no-store`、`Vary: Cookie` 与现有请求限制。除候选列表外拒绝 query；POST 严格 JSON，上限 8 KiB，拒绝重复 / 未知 / null / 缺失字段、错误类型与尾随内容。

| 方法 / 后缀 | 请求或返回 | 权限 |
| --- | --- | --- |
| `GET /ownership` | `self:{id,role,role_revision}`、`can_transfer`、`recent_authentication`；只描述本人当前状态 | 当前 active 账户、Session 和该 Workspace active membership；`can_transfer` 表示 owner 能力，近期认证不足需另行登录 |
| `GET /owner-transfer-candidates` | `items:[{id,display_name,role_revision}]`、`next_cursor`；不含邮箱 / 凭证 / 全局状态 | 当前 owner；仅本 Workspace active 账户的 active member，排除本人和已有 owner |
| `POST /owner-transfers` | `{client_operation_id,target_user_id,expected_actor_role_revision,expected_target_role_revision,confirmed}` | 新写入按完整交接授权；已成功精确重试按上节的窄例外 |
| `GET /owner-transfers/{client_operation_id}` | 本人操作的 `applied` 或 `not_observed` | 当前有效账户 / Session / active membership；无需仍为 owner，无列表入口 |

候选沿用默认 25、最大 50 的稳定 user ID keyset 分页，先过滤再分页，无隐藏总数；cursor 有版本并绑定 Workspace、调用者及列表种类。候选新 DTO 不扩散到现有 `/members` 的 Project admin 可见范围。候选页面的代次只是提交前置条件，不能代替事务验证。

所有成功响应使用现有 `data` envelope。首次 POST 和历史重试均返回 `200`，`data` 为 `{result:'applied',client_operation_id,workspace_id,actor_user_id,target_user_id,audit_id,occurred_at,actor_role_revision,target_role_revision}`，两个 revision 是该次成功后的历史版本；查询未见记录为 `{result:'not_observed',client_operation_id,workspace_id}`。不返回邮箱、未兑换邀请数量、完整成员名单或后续角色状态。客户端必须重新读取 `/ownership` 与 Session 当前 Workspace 信息判断现在能力，不能从 `applied` 推导当前角色。

错误保持既有含义：无有效 Session 为 `401 unauthenticated`；Workspace Session resolver 内部的无 active membership `403` 经现有公共配置入口转换为 `404 not_found`，本命令沿用该不可发现性规则；非 owner 首次操作 `403 forbidden`，非近期认证 `403 recent_authentication_required`。目标不存在、跨 Workspace、disabled 或 suspended 统一 `404 not_found`，无具体原因；自己作为目标是 `400 invalid`，当前同 Workspace 的已有 owner 或预期代次变化为 `409 conflict`。固定角色转换、计数保护或版本溢出冲突不能降级为成功 no-op。COMMIT 确认不明使用本命令专用 `503 result_unconfirmed`，保留 request ID；不回显 SQL、内部错误或请求体，普通 5xx / 网络错误也必须允许结果查询。

Web 在当前 Workspace 管理区域提供交接入口，选人显示展示名与稳定 ID，要求显式确认：“你将成为普通成员，对方将成为 Workspace owner；你本工作区此前未兑换的邀请将失效；Project 和其他显式权限不会自动转移。”首片不发送邀请、邮件或外部通知。

提交前检查近期认证提示，必要时走现有重新登录；重登后重新选择接收人，刷新双方版本并再次确认，不持久化原选择。提交中禁止重复发起；响应丢失时保留原 ID / 请求，提供“查询结果”和“用原请求重试”，不自动轮询、不自动换 ID。待确认记录只在当前页面内存保存，提供复制不含秘密的 operation ID；刷新或重登后支持粘贴 ID 查询本人结果，不把密码 / Cookie / 邀请 token 或原请求存入 Web Storage。只剩 ID 而原请求已丢失时仅查询，不从当前版本拼造“原请求”；查询未观察到结果或失败时保留该 ID，并阻止本页面发起新交接，直到对应历史成功得到确认。清空输入或查询另一笔成功不能解除其他未确认操作的保护。

成功后刷新 Session 与本 Workspace 能力，清除旧 owner 专属候选、管理数据及尚未交付的邀请 token；保留仍有权访问的项目内容。其他标签页在激活、`pageshow`、显式刷新或下一次操作时重新读取；不承诺管理 UI 瞬间跨标签同步，也不新增实时 owner 事件。401 清除登录态，403 清除失权管理数据；历史结果面板在仍为 active member 时保持可用。桌面、手机、键盘确认与错误焦点恢复纳入验收。

## 审计、迁移与恢复

复用 `workspace_configuration_audit` / `workspace_configuration_receipts`，不新增平行 audit 系统，不把权限变化投影到普通 Activity / Outbox，也不改 ADR-0041 的 `identity_audit`。

- 新 command_kind 固定 `workspace.owner.transfer`；`scope_id=workspace_id`，`subject_id=result_id=target_user_id`，actor 来自 Session，actor ≠ subject，`changed=true`。
- 现有 `before_state / after_state` 记录接收人 `member / owner`；新增可空字段 `actor_before_role / actor_after_role` 固定 `owner / member`，以及 `actor_before_role_revision / actor_after_role_revision / subject_before_role_revision / subject_after_role_revision` 四个 bigint，均正且后一值恰好前一值 +1。
- 仅本 kind 允许上述六个字段非空；其他 kind 必须全部为 null，既有行语义不变。交接的 `granted_user_ids / removed_channel_ids / removed_thread_ids` 为空、`authorization_id` 为空，不把 Workspace role 伪装成 Project 或 Environment 授权。保留实际服务器 request ID 和时间。
- Audit / receipt 沿用不可变 trigger、Workspace 外键与受控形状检查；receipt 指向同 Workspace 的该次 Audit，canonical digest 与操作作用域精确匹配。不写邮箱、密码、Cookie、CSRF、Session digest、邀请 token 或完整请求。
- 两个 role / revision 更新、Audit、receipt 同事务；首次 INSERT 失败或提交失败不留单边升降级、半条证据或新成功状态。拒绝与失败不伪造成功 Audit；完整失败审计平台仍不在本片。

forward-only migration 018 已落实本合同。增加 role_revision、邀请创建代次、Audit 形状和本命令 receipt 部分唯一索引；不重写历史 Audit / receipt，不猜测历史 owner 变更。不加“只能一个 owner”的唯一约束，不以迁移擅自提升成员或修复失管实例。上线前检查现有异常数据并报告，不能静默清洗。

迁移需与新 Go / Web 匹配，readiness 拒绝旧 / 漂移 schema；不允许运行旧工件写入新角色合同，不在交接过程中执行迁移。升级前停写并备份；回退使用匹配旧工件将旧备份恢复至隔离空目标，不能执行 destructive down migration。

同版本备份恢复须保留角色、两个代次、Audit、receipt 和不可变约束；Session、未兑换邀请、OIDC 事务继续只恢复 schema。恢复后重新登录可查本人历史结果，setup 仍关闭。旧 017 备份恢复再升级可能恢复交接前的 owner 和历史结果，必须由部署者重新核对真实管理责任，不能拿原实例 operation ID 或当前 owner 印象当作恢复副本的事实，也不自动重放交接。

## 替代方案与取舍

| 方案 | 结论 |
| --- | --- |
| 先授予 B owner，再另一次请求降低 A | 两次请求留下半完成，且顺势开放通用 owner 管理；本片使用原子替换 |
| 只检查 owner 数量再降级 | 事务外计数无法保护并发，也不表达接收者确认条件，不采用 |
| owner 交接同时移交所有 Project / 私密对象 | 会绕过窄权限并扩张管理责任，不采用 |
| 邀请只依赖当前 owner 角色 | 交回原 owner 后旧邀请可能复活；用创建代次限定原任期 |
| 交接事务内批量删除所有旧邀请 | 与当前邀请行 → owner membership 的锁顺序相反；代次检查无需锁邀请集合，也不误伤其他 owner |
| 成功后仍要求原 owner 身份才能重试 | 降级是正常结果，会使响应丢失不可自助确认；仅本人历史操作提供读取例外 |
| 所有配置 receipt 都允许原操作者失权后读取 | 扩大敏感配置可见性；例外严格限制为此命令的最小历史事实 |
| 接收人确认、待处理邀请、到期与取消状态机 | 可作为后续责任审批需求，本片采用当前 owner 近期认证与明确确认的一步主动交接 |
| 复用全局密码恢复入口强制转 owner | 身份恢复与工作区授权不同，不采用 |

成本是一次 schema 变更、邀请创建 / 兑换的合同补充，以及一条需要携带可信 Session 上下文的配置命令。该范围解决 owner 主动移交，不代表完整管理员离职、全部最后 owner 治理或外部团队试用条件已经满足。

## 验收矩阵

下表定义验收要求；36 项已按相应证据层执行，逐项结果与边界见[实施记录](../status/reviews/2026-10-18-workspace-owner-handoff.md)。普通身份从正式 bootstrap / 邀请建立，至少一条完整链由普通成员独立登录；多 owner、暂停、禁用、故障注入等只使用明确标注的隔离异常夹具。

| 编号 | 场景 | 必须观察到的结果 | 证据层 |
| --- | --- | --- | --- |
| P01 | 唯一 owner A 向正式邀请成员 B 交接 | A member、B owner、双方代次 +1；新旧登录会话读取正确角色，setup 不重开 | Service + PostgreSQL + HTTPS 浏览器 |
| P02 | 多 owner，A 向普通成员交接 | 其余 owner 及代次不变，有效 owner 数量保持；不要求全局唯一 owner | PostgreSQL |
| P03 | member / Project admin / 非成员 / 跨 Workspace 调用 | 不能发起新交接或读候选；对象不可发现性和错误分类不泄漏目标状态 | Service + HTTP |
| P04 | 自己、已有 owner、不存在、跨 Workspace、suspended、disabled 目标 | 按合同拒绝；双方角色 / revision、Audit / receipt 全部不变 | PostgreSQL + HTTP |
| P05 | 失效 / 撤销 / 未来 Session、近期认证刚好 10 分钟及超界、锁等待跨界 | 有效期与近期认证按锁后时间复核，越界拒绝；历史读取不要求近期认证 | Service + PostgreSQL |
| P06 | 双方跨多个 Workspace、受限 Project / Channel、显式 Deployment 权限 | 只有目标 Workspace 两个 role / revision 改变；全表快照证明业务、窄权限、密码和绑定不变 | PostgreSQL + HTTP |
| P07 | 原 owner 降级后的 owner 专属命令及旧配置 receipt 重试 | 创建 / 授权等仍按当前 owner 检查拒绝；原 Project admin 和显式记录权继续正常，无通用读取例外 | PostgreSQL + HTTP |
| V01 | 严格 JSON、false / 缺失确认、UUID、稳定 ID、版本字符串边界 | 拒绝未知 / 重复 / null / 超限 / 错类型 / 尾随输入；bigint 不丢精度、溢出无写入 | 单元 + HTTP |
| V02 | 候选 25＋2 分页、同名、跨作用域 cursor、分页中降级 | 先权限过滤再分页，稳定 ID 消歧，旧 cursor 不授予管理权，无邮箱 / 隐藏总数 | PostgreSQL + HTTP + Web |
| V03 | A→B→A 后提交旧页面，或仅一方版本过期 | 首次新操作冲突；旧成功 ID 仅返回历史事实，不重新交接 | PostgreSQL + Web |
| C01 | A 同时交给 B / C | 最多一个新写入成功；另一个重读后拒绝；没有零 owner 或半完成 | PostgreSQL 屏障竞争 |
| C02 | 两个 owner 同时交给同一 B / 不同 B、C | 同目标最多一个成功；不同目标可各成功，其他 owner 不被降级 | PostgreSQL 屏障竞争 |
| C03 | 同 ID 同请求并发、同 ID 换目标 / 版本 | 一个事实、一条 Audit / receipt；精确重复历史成功，变更请求冲突 | PostgreSQL + HTTP |
| C04 | 候选在授权锁前后被禁用 / suspended；角色由另一交接更改 | 锁前失效则拒绝；锁后外部状态变更等待，不声称替未来禁用命令保护最后 owner | PostgreSQL 异常夹具 |
| C05 | 与 Team / Project / Component / Environment / Repository 创建、授权 / 关联并发 | 旧 owner 已持锁允许先完成；交接先提交后拒绝新 owner 专属写入；无稳定锁环 | PostgreSQL |
| C06 | 与密码恢复、外部解绑、logout 并发 | 恢复 / 解绑 / logout 先提交则旧 Session 不能交接；交接先获有效锁可完成，后来会话撤销仍生效 | PostgreSQL + HTTP |
| C07 | 锁超时、真实 deadlock victim、context 取消及 revision 溢出 | 无自动隐藏重试、半提交或伪成功，保留可诊断的受控错误 | PostgreSQL 故障注入 |
| I01 | A 旧邀请、B 新邀请、其他 owner / Workspace 邀请 | A 旧任期拒绝，B 新邀请可用，其他作用域保持原行为；已加入者不移除 | PostgreSQL + HTTP |
| I02 | A 交接后再次取得 owner | 旧未过期邀请仍无效，新的正代次邀请成功 | PostgreSQL |
| I03 | 邀请创建 / 兑换分别先于和晚于交接 | 严格按锁顺序准入或拒绝，无锁反转；成功先兑换只产生一次成员与审计 | PostgreSQL 屏障竞争 |
| I04 | 已登录 B 接受 A 邀请与 A→B 交接交错 | 验证既有接收账户锁顺序，无账户 / membership 锁环，无角色覆盖 | PostgreSQL |
| I05 | OIDC start 后交接、callback 准入；已绑定身份正常登录 | 准入 callback 复核代次并拒绝旧邀请；已有身份登录保持，不宣称 provider 实测 | 身份 Store 合同 |
| R01 | 原 A 已 member，响应丢失后原请求重试 / GET 查询 | 当前有效成员可读自己历史成功；无新角色变化、审计或 receipt | PostgreSQL + HTTP + 浏览器 |
| R02 | 历史 B 再交接、禁用或改变身份状态 | A 仍合法时只返回原最小历史事实，不查询或泄漏 B 当前状态 | PostgreSQL + HTTP |
| R03 | A 已失效 / suspended、其他 actor、其他 Workspace、无 receipt 的 member | 不凭 receipt 越权；其他作用域 `not_observed`，无记录的 member 不能写入 | PostgreSQL + HTTP |
| R04 | 实际 COMMIT 后丢确认 / HTTP 响应、在途未提交时查询 | 未确认不报确定回滚；先 `not_observed` 后 `applied`，原 ID 重试不重复执行 | PostgreSQL 连接故障 + HTTP |
| R05 | 第一次升权后、降权后、Audit 后、receipt 后失败与延迟提交约束拒绝 | 全事务回滚到原快照；没有新成功证据，之后同 ID 可按原前置条件重新尝试 | PostgreSQL 故障注入 |
| A01 | 新审计完整形状及非法 actor / subject / revision / 授权混用 | 合法一条，非法形状被 DB 拒绝；其他 command 审计全部兼容 | migration + PostgreSQL |
| A02 | Audit / receipt 修改删除、同 ID 换目标、角色变化不增代次 | 不可变与部分唯一约束、role_revision trigger 生效；旧 receipt 不被改写 | PostgreSQL |
| A03 | API、日志、普通 Activity / SSE | 不泄漏邮箱 / Cookie / CSRF / token / 原请求；权限交接不广播私密管理事实 | HTTP + Compose 日志 |
| B01 | 017 → 新 migration，活跃及失效旧邀请、多 owner 与异常基线 | 合法角色 / 旧审计保留；有效邀请绑定 1，失效邀请 0；不隐式修复零 owner | 真实升级数据库 |
| B02 | 新旧工件 / schema mismatch 与同版本备份恢复 | readiness 拒绝不匹配；角色、revision、历史 receipt 保留，临时身份数据不恢复，setup 关闭 | PostgreSQL + 双实例恢复 |
| B03 | 017 旧备份空目标恢复再升级 | 明确可能恢复旧 owner；本地重新核验，不自动重放原实例交接 | 隔离旧工件恢复链 |
| W01 | 近期认证提示、选人确认、成功后能力刷新、接任者操作 | 无手工 SQL 改角色，双方使用正式入口完成交接和新的 owner 配置 | HTTPS 桌面 / 手机浏览器 |
| W02 | 双标签冲突、后退 / pageshow、失权刷新、网络中断、换账户 | 清理失权数据；保留原 ID 可查，换账户不继承操作；无自动重试新写入 | Web + HTTPS 浏览器 |
| W03 | 键盘 / 焦点、复制 ID、重登后粘贴查询、同名成员 | 显式确认可操作，错误可恢复；浏览器持久存储无秘密或原请求 | Web + 浏览器 |

实现验收至少运行 Go 单元 / race / vet、真实 PostgreSQL 全相关回归、升级与备份恢复、Web 检查、同源 HTTPS 浏览器和 `./scripts/check-repo.sh`；具体通过项与未覆盖项另写实施记录，不能把本矩阵当作结果。真实 OIDC / Jenkins、原生中文 IME、生产容量及外部团队持续使用继续独立验收。

## 确认与实施

2026-10-18 所有者已确认上述“单次原子交接、近期认证、双角色代次、邀请任期绑定、本人历史读取例外”的推荐范围，并授权实现与本轮隔离验收及清理。实现及隔离验收完成后，所有者追加授权一致性核对并本地提交，仍不 push；结果单独记录。

已同步领域模型的 Workspace 角色与邀请说明、架构中的配置事务 / 锁顺序、server / Web / deploy 操作说明、备份与 readiness 合同及当前状态；历史 ADR 通过本 ADR 的补充关系衔接，不重写已验收事实。本轮按所有者要求本地提交，不 push。
