# ADR-0032：Component、Environment 最小配置与环境授权管理

状态：已接受（2026-10-01，项目所有者已批准实施）

日期：2026-10-01

Supersedes（部分）：[ADR-0009](0009-explicit-staging-deployment.md) 中环境授权仅由种子数据或未来入口管理的限制，以及 [ADR-0031](0031-session-scoped-staging-deployment-recording.md) 中 Component / Environment 与授权管理不在交付范围的限制。保留显式授权、staging 记录、不可变 Deployment 与当前权限重查；不改变 [ADR-0007](0007-component-scoped-ci-run-read.md)、[ADR-0011](0011-workspace-scoped-deployment-read.md) 的读取语义。

## 背景与源码基线

基线为 `efa002b`。正式 Component、Environment 和授权表已经存在，成功 CI Run 的环境选择与显式记录也已接通；管理员仍不能通过正式 Web 建立这些配置。当前目标是移除这部分预置数据依赖，不宣称本切片完成 Jenkins 持续采集、交付双向关系或完整 Golden Path。

本轮核对发现：

- [migration 003](../../server/db/migrations/003_jenkins_ci_run_core.sql) 已定义 Component 的类型、生命周期、责任 Team 和 Workspace 内唯一 key；Component 不从属于 Project。
- [migration 004](../../server/db/migrations/004_staging_deployment_core.sql) 已固定 Environment classification，Deployment 引用当时的授权 ID。授权目前对 `(workspace_id, environment_id, user_id)` 全量唯一，且 revoked 行不可再修改：这使同一用户撤权后无法重新授权。
- [配置事务](../../server/internal/goldenpath/postgres/configuration.go) 与 [migration 009](../../server/db/migrations/009_workspace_configuration.sql) 已提供成功 Audit、receipt、当前角色复核和原子写入模式，可以扩展白名单，不需要第二套权限框架或配置表。
- [Deployment 写入](../../server/internal/goldenpath/postgres/deployment.go) 按当前 active 授权取 ID，并在处理 receipt 前重新授权；[关系读取](../../server/internal/goldenpath/postgres/relations.go) 对 Component / Environment 使用 active Workspace membership。新增管理能力不得改变这两项边界。

## 已确认的决定与影响

| 决定 | 推荐范围 | 主要影响 |
| --- | --- | --- |
| 配置操作者 | active 账户且为当前 Workspace active owner，才可创建 Component / staging Environment、管理环境授权 | 新增明确的 Workspace 配置能力；Project admin、责任 Team 和插件身份不继承这项能力 |
| 管理与记录分离 | 创建对象不附带授权；owner 如需记录部署，也必须通过单独确认的授权命令授予本人 | 单维护者可以独立配置；本阶段不引入双人审批，也不声称实现职责分离 |
| 对象最小范围 | Component 创建为 active，Environment 创建为 active staging；提供现有对象发现和配置读取 | 不新增 Project 外键、默认关系、自动 CI source 或生命周期编辑入口 |
| 撤权与重新授予 | revoked 授权永久保留；重新授予产生新的授权 ID 和递增代次，同一环境 / 用户至多一条 active 授权 | 必须修改原唯一约束，保留历史 Deployment 外键和旧授予者 / 时间 |
| 旧页面与精确重试 | 新命令比较明确的授权 ID / 状态；旧 receipt 只证明原请求已处理，不重放权限变更 | 避免旧页面撤销新授权或旧授予请求在撤权后复权 |
| 实施成本 | 新增 forward-only migration 012、有限 Session API、现有工作台内的配置交互 | 无新依赖、无外部部署调用；须同步 readiness、恢复、投影和 Go / Web 合同 |

项目所有者已确认下列合同；实现与验证按当前状态单独记录，接受本 ADR 不等于实现或验收完成。

## 对象与权限合同

### Component 与 Environment

- 创建者须在事务内具有 active 账户及 active Workspace owner membership。创建不赋予任何新 Project / Channel 角色，不扩大 restricted 对象访问。
- Component 创建字段为 `key`、`name`、`type`、`owner_team_id`；type 必须显式选择既有七种类型之一，不默认 `other`。服务端固定 `lifecycle=active`，责任 Team 必须存在于同 Workspace；summary 本批不编辑，新对象保存 null。
- Environment 创建字段为 `key`、`name`、`owner_team_id`、`classification`；classification 必须显式为 `staging`，服务端固定 `status=active`。创建后仍禁止修改 classification。
- 名称与 key 沿用 [ADR-0026](0026-foundation-configuration-and-membership.md) 的创建校验：名称 trim 后 1..120 Unicode 字符、最多 480 bytes，拒绝控制字符；key 为 `^[a-z][a-z0-9-]{0,31}$`。两种对象各自在 Workspace 内 key 唯一，不回填或重写旧对象。
- 列表与单对象读取向同 Workspace 的当前 active 成员开放，并通过正式 Session 检查账户状态。Component 包含既有 planned / deprecated / retired 对象，Environment 包含既有各分类及 archived 对象；状态只影响能力，不隐藏历史。
- 普通读取仅包含安全元数据及当前调用者能力；责任 Team ID 只表示归属，不扩展 owner 专用 `/teams` 目录的访问权。
- 不开放改名、key / 类型 / 归属 / 生命周期修改、归档、删除或批量导入。不同对象保持独立：不把 Environment 嵌套为 Component 子对象，不自动生成 EntityLink，不建立 Repository 或 Jenkins source。

### 授予、撤销与重新授予

- 管理者必须是当前 active owner；仅可向 active staging Environment 授予记录权。目标必须是同 Workspace 的 active 成员且账户 active，可以是操作者本人。授权影响明确显示为“记录该 staging 环境的外部部署终态”，不描述为执行部署或 production 权限。
- 创建 Component / Environment 后默认没有授权。Project admin、Component / Environment owner Team、CI source、插件和构建成功都不构成授权；没有授权的 Workspace owner 也不能记录 Deployment。
- 撤销允许清理既有 staging 授权，即使目标账户已禁用、membership 已暂停或 Environment 已归档。对 archived staging 只开放撤销，不开放授予。非 staging 环境本批不接受授权写命令，包括撤销；production 治理另行冻结。
- 重新授予不修改 revoked 行，也不删除旧行；新建新的 `dpa_` ID。每个环境 / 用户的第一代为 1，后续在同一环境写锁下取最大代次加 1；不使用墙钟时间或随机 ID 推断最新授权。
- active 授权可单向变为 revoked，授予来源与代次不可修改，revoked 行不可再变。历史 Deployment 始终指向当时使用的授权，不重新绑定到新授权。
- 授权记录的 active 不等于目标当前可操作；执行命令仍同时要求账户、membership、来源可读性及目标状态。目录以 `eligible` 表示是否可授予，不泄漏账户失效原因。暂停与恢复不自动撤销或重建授权；本批没有新增这类账户治理入口，不声称解决完整成员生命周期。
- 撤销记录权不撤销 Workspace 共享读取权，已有 Deployment 仍按原规则读取。界面必须说明该差异。

### 防止旧状态覆盖

每次授予 / 撤销必填 `expected_authorization`，值为 null 或严格对象 `{id, status}`，status 为 `active / revoked`。null 表示该环境 / 用户从未有授权；有历史时必须携带最新一代的 ID 与状态。

首次处理命令先比较当前最新记录，不匹配则 `409 conflict`，提示刷新后重新确认。ID 与状态都要比较：同一 ID 从 active 变成 revoked 不能被旧页面忽略；撤销再授予后的新 ID 也不能被旧撤销命令误伤。

| 动作与当前状态（expected 已匹配） | 结果 |
| --- | --- |
| 授予，未曾授权或最新为 revoked | 验证目标资格，追加新 active 授权 |
| 授予，最新为 active | 验证目标资格，记录 `changed=false`，不换 ID、不改 provenance |
| 撤销，最新为 active | 将该条记录置 revoked，记录当前操作者与时间 |
| 撤销，最新为 revoked | 记录 `changed=false`，不改写原撤销者 / 时间 |
| 撤销，从未授权 | 仅对同 Workspace 的有效候选成员记无变化；否则统一 not-found |

receipt 命中时先检查当前操作者与环境管理边界，再比较 digest；精确匹配返回原处理结果，不重新比较 expected、不重新修改权限，也不返回用户当前私有状态。撤权后的旧授予 receipt、重新授予后的旧撤销 receipt 都不能更改当前授权。操作者失去 owner 或 active 身份后不能借旧 receipt 查询成功结果；归档环境拒绝授予命令及其重试，仍允许撤销及撤销重试。

## 公共 API 与 Web

路径统一以 `/api/v1/workspaces/{workspace_id}` 为前缀。复用现有 Session、可信 Host / proxy、错误 envelope、`Cache-Control: private, no-store` 和 `Vary: Cookie`；写入继续校验 HTTPS、精确 Origin 与现有 CSRF，不接受客户端角色或 provenance。

| 方法与后缀 | 用途 / 请求字段 | 权限 |
| --- | --- | --- |
| `GET /components` | 分页发现 Component | active Workspace 成员 |
| `POST /components` | `{client_operation_id, key, name, type, owner_team_id}` | active owner |
| `GET /components/{component_id}/configuration` | 元数据与能力 | active Workspace 成员 |
| `GET /environments` | 分页发现 Environment | active Workspace 成员 |
| `POST /environments` | `{client_operation_id, key, name, classification, owner_team_id}` | active owner；仅 staging |
| `GET /environments/{environment_id}/configuration` | 元数据与能力 | active Workspace 成员 |
| `GET /environments/{environment_id}/deployment-authorizations` | 每位曾获授权用户的最新一代记录，按用户分页 | active owner |
| `GET /environments/{environment_id}/deployment-authorizations/{user_id}` | 选择候选人后读取 expected 状态，已有失效用户也可供清理 | active owner |
| `PUT /environments/{environment_id}/deployment-authorizations/{user_id}` | `{client_operation_id, expected_authorization, confirmed:true}`，授予 | active owner；active staging |
| `DELETE /environments/{environment_id}/deployment-authorizations/{user_id}` | 同上，撤销 | active owner；staging，可 archived |

### DTO、分页和错误

- 列表返回 `data:{items,next_cursor}`，按对象 ID 升序；授权列表每个用户只出现一次，按 user ID 升序。默认 25、最大 50，复用 `limit / after` 和 canonical base64url 版本化游标；游标绑定 Workspace、列表 kind 和可选 Environment，不接受跨作用域使用、未知或重复 query。每页重查权限，不承诺跨页快照，不返回总数。
- Component DTO 为 `ref,key,name,type,lifecycle,owner_team_id`；Environment 为 `ref,key,name,classification,status,owner_team_id`。旧 planned Component 的 owner_team_id 可为 null。单对象配置读取额外返回 `capabilities`：Component 本批无可修改动作；Environment 返回当前调用者的 `can_manage_authorizations / can_grant / can_revoke`。这些值仅供展示，不能充当写入凭证。
- 授权管理 DTO 为 `{user:{id,display_name}, eligible, authorization:{id,status}}`；单用户从未授权时 authorization 为 null，但仅对当前有效的同 Workspace 候选人返回。已有授权者可在失效后继续列出以便清理。非 staging 环境允许 owner 只读检查既有授权，配置能力全部为 false；不扩展为历史 Audit 浏览。
- 授权 ID 只在 owner 管理接口中用于并发前置条件。普通对象列表、staging-targets、Deployment Nexus View 均不新增授权 ID、授予者、代次、receipt 或 Audit 字段；不返回邮箱、Secret、Jenkins source 或账户禁用原因。
- 创建首次成功 `201`，精确重试 `200`，`data` 返回安全对象 DTO；授权写入 `200`，仅返回 `{user_id,applied:true}`，表示原请求已处理，不能解释为当前仍获授权。客户端随后刷新权威状态。
- 新写接口沿用配置请求上限 32 KiB、operation ID 校验及严格 JSON：拒绝未知 / 重复字段、缺失 expected 或 confirmed、尾随 JSON、错误类型和非法 UTF-8；禁止写 query。`dpa_` 期望 ID 使用有界稳定 ID 校验，不能通过 body 指定生成 ID、授权者、时间或代次。
- 未登录 `401`；不可读 Workspace / 对象、跨 Workspace 对象或无效候选 `404`；对象可读但缺少 owner 配置权 `403`；key 冲突、expected 变化、幂等冲突、归档授予或非 staging 写入 `409`；非法字段 / 未确认 `400`。先做当前权限检查再返回管理状态冲突，错误不带 SQL 或内部授权明细。

### 最小交互

1. 复用工作台当前 Workspace，提供 Component / Environment 列表和配置详情；owner 可从既有 Team 选择器创建，普通成员可发现可读对象。新对象创建成功后刷新列表，不附带任何授权。
2. owner 进入 staging 环境的授权管理；候选人复用既有 `/members` 目录，展示名重复时用稳定 ID 消歧，不要求人工输入 ID。选择候选人后读取其最新授权状态。
3. 授予与撤销都展示 Workspace、环境、目标成员和影响，显式确认后提交。给本人授权采用同一流程，不自动勾选。已归档环境只显示撤销能力。
4. 成员从已有成功 CI Run 页使用原 staging-targets 和记录表单；列表项不替代提交时授权。撤销后刷新目标列表并验证旧页面提交被拒绝，历史 Deployment 仍可读。
5. 复用既有样式和响应式布局，覆盖加载、空态、错误、分页、焦点与键盘。Workspace / 对象切换或失权清空相关目录和表单，丢弃迟到响应。网络结果不明时保留同一 operation ID / payload 供显式重试；不能自动新建请求或乐观显示授权成功。

本切片不增加 CI Run 列表或持久 Jenkins source 配置；成功构建的获取仍沿用已有入口与受控接入。验收须标明这部分前提，不宣称普通成员已经从空 Workspace 独立建立整条交付链。

## 事务、Audit 与 Activity

### 复用边界与并发

- 扩展既有 configuration service / store 的封闭命令白名单，新增 `component.create`、`environment.create`、`environment.authorization.grant`、`environment.authorization.revoke`，使用专用路由，不暴露通用 command 执行 API。
- 创建命令沿用 Workspace 锁串行化 key 与 receipt 处理；授予 / 撤销使用同 Environment 的排他锁，覆盖尚不存在授权行的竞争。记录 Deployment 继续持有 Environment 与 active 授权的共享锁，保留原数据库触发器。
- 账户先于 membership 加锁；涉及操作者与目标的事务按稳定 user ID 顺序获取所需账户锁，再获取 membership 锁，然后 Environment，最后授权记录。不得先锁 Environment 再等待目标 membership。Deployment 写入需要同步在事务内锁定并检查操作者 active 账户，再按既有 membership → CI Run / Environment → 授权顺序检查；不只依赖 HTTP Session 的事务外检查。
- 新配置命令不锁 CI Run，不引入从 Environment 反向等待 CI Run 的锁依赖。实施时核对现有身份与配置路径，针对 owner 自授权、双 owner 互授、并发首次授权、撤权 / 记录竞争验证无死锁。锁序修正限于受本切片影响的路径。
- 撤销与记录按锁获取顺序串行化：先取得有效授权锁的记录可以先完成；撤销提交后的新请求和重试不得继续使用已撤销授权。重新授予后的新请求仍按当前新授权判断；旧 Deployment receipt 不重写原授权 ID。

### 幂等与成功 Audit

- 继续使用 `workspace_configuration_receipts` 与 `workspace_configuration_audit`，不把权限命令塞入 collaboration receipt。唯一范围保持 Workspace / actor / command / scope / subject / operation；创建 scope 为 Workspace、subject 为空，授权 scope 为 Environment、subject 为目标 user ID。
- 新命令 digest 包含全部规范化业务字段，包括 expected 与 confirmed；扩展输入类型时必须保留旧七类配置命令的 digest 编码，否则历史 receipt 的合法重试会冲突。不能因新增 Go struct 字段而静默改变旧摘要。
- Audit 创建命令的 result_id 为 Component / Environment ID，before / after 为空，changed=true；授权命令 result_id 仍为目标 user ID，before / after 为 `'' / active / revoked`，另增加可空 `authorization_id` 精确关联本次处理的授权记录。从未授权的无变化撤销允许 null，其余授权操作必须关联具体记录。
- Audit 的授权关联同时校验 Workspace、Environment 和目标用户，不能只校验全局 ID；旧命令的该字段为 null。新命令的成员 / 清理数组必须为空，不复用数组保存授权历史。类型、scope、subject、result 和状态组合均扩展数据库白名单，保留旧命令约束，不放开成任意 JSON。
- 新授权、撤销状态、Audit、receipt 处于同一事务；无变化新命令写一条 changed=false Audit，精确重试不追加 Audit。失败整体回滚，不写成功记录。没有通用失败审计、Audit UI 或保留策略的新承诺。

### 创建事件与投影

- Component 创建同事务写 `component.created`，schema_version=1，payload 仅 `{lifecycle:"active"}`；Environment 写 `environment.created`，payload 仅 `{status:"active",classification:"staging"}`。复用 user actor、web source 和服务器 request correlation。
- 对象、成功 Audit、receipt、领域事件、Outbox 与正常 Activity 同时提交；创建事件只投影到主要对象，safe facts 为上述安全状态字段。授权授予 / 撤销只进入窄 Audit，不向普通 Activity 暴露成员、授权 ID 或管理明细。
- 正常投影与全量重建使用相同白名单，Activity projection version 从 2 升至 3，同步恢复和重建测试；旧事件仍可重建。不补造旧 Component / Environment 的创建事件，不新增其完整 Nexus View 页面，也不把投影存在描述成 UI Timeline 已验收。

## 迁移、兼容与回退

建议新增 `012_component_environment_configuration.sql`，实施时核对编号未被其他提交占用；不修改 001～011。

1. 授权表增加不可变正整数 `generation`，既有行回填 1；删除三列全量唯一约束，改为 `(workspace_id,environment_id,user_id) WHERE status='active'` 的部分唯一索引；增加 `(workspace_id,environment_id,user_id,generation)` 唯一约束，支持取最新一代。保留 `(workspace_id,id,environment_id,user_id)` 唯一键及 Deployment 外键。
2. 扩展现有 mutation trigger，禁止修改 generation；继续禁止删除、改授予 provenance、复活或改写 revoked 行。序号分配在受控事务的 Environment 锁内完成，数据库负责正值、代次唯一和至多一条 active 的最终防线。
3. 扩展配置 receipt / Audit 的命令与状态组合 CHECK，给 Audit 增加授权关联列及上述复合外键与条件约束。旧 Audit / receipt 保持内容与可重试性，不回填虚构的授权管理历史。
4. 同步 migration registry、精确 schema readiness、迁移测试、备份快照与恢复一致性。既有表已属于权威备份范围，无需新增表或 Secret 分类；新列、全部授权代次和历史 Deployment 引用必须往返保留，恢复仍排除 Session 等实例授权材料。

新 Go / Web / schema 按匹配版本交付，旧二进制遇到 migration 012 应拒绝 readiness。多代数据存在后不能直接恢复旧全量唯一约束，也不能删历史授权来兼容旧代码。优先采用保留新 schema 的 forward repair 工件；确需回退整个版本时，用升级前备份恢复到全新目标并配套旧工件，明确备份后写入的损失与单独授权。

本提案不授权在业务实例应用迁移、安装依赖、启动长期服务、持久 Jenkins、修改远程状态、提交或 push。实现验收需要隔离数据库 / HTTPS 浏览器时，先说明实例、端口、数据目录与清理范围，再按本次任务授权执行。

## 验收与实施顺序

| 层级 | 必须证明 |
| --- | --- |
| Service / HTTP | 严格输入、旧 digest 兼容、显式确认、Session / Origin / CSRF、方法与游标、普通 DTO 无授权明细；owner / 普通成员 / Project admin / plugin / 跨 Workspace / 暂停与禁用矩阵 |
| 真实 PostgreSQL | 对象唯一 key、同 Workspace Team、无自动授权；并发首次授予仅一条 active；撤销再授权保留两代及原 provenance；expected ID / 状态冲突；receipt 精确重试与未知结果恢复 |
| 撤权与并发 | 旧授予不复权、旧撤销不伤新授权、双 owner 并发、本人授权、管理者失权重试、失效成员清理、归档只撤销；撤权与 Deployment 写入两种合法串行顺序及事务内账户禁用检查 |
| 原子性与恢复 | 授权、Audit、receipt、创建事件 / Outbox / Activity 故障整体回滚；历史 Deployment 指向原授权；多代授权备份恢复、代次继续递增、旧 schema 拒绝；正常投影与重建一致 |
| Web | 正式配置入口、Team / 成员选择、当前能力、显式自授权、无权限空态、分页、旧页面冲突、双击和模糊失败重试、作用域切换、桌面 / 手机和键盘 |
| staging 接续 | 使用正式创建的 Component / Environment，经既有受控真实来源接入取得成功 CI Run，授予成员后从正式表单记录；撤权阻止后续记录但历史可读；production 与失败构建继续拒绝 |

验收须从正式 bootstrap / 配置命令建立新对象与授权，不用 SQL 代替被验收的配置动作。故障注入和暂停 / 归档竞争可使用隔离数据库测试，并标明它们不是新增产品入口。Jenkins 部分沿用已有受控来源配置；若本轮仅重放合法测试 delivery，应单列自动化证据，不能写成重跑真实 Jenkins 构建或持续采集。

推荐实施顺序：

1. 按所有者已确认的权限、重新授权、迁移与公共 API 范围，同步领域模型、决策基线和核心契约。
2. 完成 migration、配置事务 / receipt / Audit、授权代次与 Deployment 账户锁检查；先通过关键数据库竞争和恢复验证。
3. 接通 Session 发现与管理 API、React 配置入口及 staging 接续，保持真实失败状态可见；运行 Go / Web 定向门禁与 `./scripts/check-repo.sh`。
4. 更新当前状态与独立实施记录，区分自动化、真实数据库、浏览器和团队使用证据。原生 IME 人工验收仍暂缓；不把本切片完成写为 M0.5 / M1 退出。

## 替代方案与取舍

- 继续由种子数据管理：实现成本低，但普通管理者无法完成配置，无法推进独立操作验收。
- 将 owner / Project admin / 责任 Team 自动视为部署授权：省去确认动作，却改变明确环境授权的核心语义，不采用。
- 将 revoked 行改回 active：实现简单，但会改写历史授权来源，使旧 Deployment 与新授予混淆，不采用。
- 只允许一次授权，永久禁止重新授予：保持现有唯一约束，却无法覆盖常见的撤权后重新协作，不作为正式管理入口合同。
- 一次加入环境管理员角色、双人审批、production、执行器和完整软件目录：需要更多权限与外部执行合同，超出当前 staging 纵向切片。

采用方案增加有限的管理接口与迁移复杂度，换取可独立配置、可撤销、可重新授予且历史来源不变的闭环。owner 可以显式给本人授权是本阶段的明确权力边界；生产职责分离与 Workspace 管理权交接继续独立设计。
