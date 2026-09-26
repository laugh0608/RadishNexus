# ADR-0031：Session 作用域下的 staging Deployment 显式记录

状态：已接受（项目所有者已确认按此范围实施）

日期：2026-09-26

Supersedes（部分）：[ADR-0009](0009-explicit-staging-deployment.md) 的不开放 HTTP route 与重复命令一律 conflict；保留独立环境授权、不可变终态和同一 Environment / CI Run 最多一条 Deployment。沿用 [ADR-0014](0014-session-scoped-deployment-nexus-view-transport.md) 的正式读取。

## 背景

真实 Jenkins 三态采集与隔离交付已经验证。现有 `RecordStagingDeployment` 可原子记录 Deployment、来源关系、事件、Outbox 和 Activity，但尚无正式写入口、环境选择与请求重试 receipt。直接开放现有命令会让数据库提交后响应丢失只能得到 conflict，用户无法判断自己的记录是否成功。

本切片让已持有环境授权的成员从成功 CI Run 页面选择 staging 目标，显式记录外部已经完成的部署结果。它不执行部署。Environment、Component 及环境授权仍由既有受控配置提供，本次不声称用户可以从空 Workspace 独立建立整条交付链。

## 决定

### 入口与最小交互

新增两个同源 Session 端点，Workspace 和来源 CI Run 由路径确定：

| 方法与路径 | 用途 |
| --- | --- |
| `GET /api/v1/workspaces/{workspace_id}/ci-runs/{ci_run_id}/staging-targets` | 返回当前用户有显式记录授权的 active staging Environment |
| `POST /api/v1/workspaces/{workspace_id}/ci-runs/{ci_run_id}/staging-deployments` | 显式记录该成功构建对应的外部部署终态 |

目标查询先复核 active Workspace membership 与 CI Run / Component 当前读取权限；来源必须 succeeded。仅列出同 Workspace、可读、active、classification 为 staging 且用户持有 active 环境授权的目标；不按 owner Team 或 Project 角色补全授权。结果为空表示当前无可选目标，不提供自助授权捷径。

目标查询按 Environment ID 升序分页，沿用既有发现入口的 `limit` / `after` 参数与 `next_cursor` 响应约定（默认 25，最大 50）。`after` 为 canonical base64url JSON，包含版本、Workspace、CI Run、查询 kind 与最后一个 Environment ID，拒绝跨作用域复用；游标不是授权。响应 `data.items` 只含 `ref`、`name`、`key` 和固定 `classification: staging`，`data.next_cursor` 为下一页游标或 null。未知 query、重复参数、非法游标与范围均拒绝。每页重新复核当前权限，不承诺跨页快照；列表不传授权 ID、授权人或角色。已有 Deployment 的目标仍可出现，写入保留唯一约束，不通过列表选择绕过冲突。

Web 在正式成功 CI Run 页面提供“记录 staging 部署结果”，打开后加载目标。表单要求选择环境、明确选择 succeeded / failed / canceled、填写外部完成时间和可选开始时间；不预选成功、不将构建时间当作部署时间。提交前展示来源构建、目标环境、结果和时间，要求勾选“我确认这是外部已结束的部署结果”后点击“确认记录”。更改事实字段取消勾选。成功后跳转既有 Deployment 页面，读取权威事实与 Timeline。

该表单遵循现有 Radish 家族工作台样式、标签、键盘焦点与手机布局；不增加独立设计系统或页面依赖。失败 / 取消 CI Run 不显示记录动作；客户端隐藏不是授权检查。

### 写入合同与身份

POST body 上限 8 KiB，严格 JSON 对象，字段为：

- `client_operation_id`：沿用现有 1–128 字节 ASCII 可见非空字符规则；
- `environment_id`：规范 `env_` 稳定 ID；来源 CI Run 不允许在 body 覆盖；
- `status`：`succeeded / failed / canceled`；
- `started_at`：必需键，可为 null；
- `completed_at`：必填 UTC RFC3339 时间；
- `confirmed`：必须显式为 true。

时间最多毫秒精度，开始不能晚于完成，完成不能晚于服务端当前时刻加 300 秒；允许历史事实补录。拒绝未知字段、重复键、额外 JSON 值、非 UTF-8 与错误类型。客户端时间输入转换为明确 UTC 后发送，确认页展示用户本地时间与时区。

身份复用 opaque Session、当前 Workspace membership 和存储的 CSRF 摘要检查。POST 要求精确同源 Origin、HTTPS / trusted proxy 和 CSRF；拒绝客户端身份、角色、authorization ID、source 等自报字段。调用 provenance 由 transport 固定为 web，request ID 作为受控 correlation，不能让 body 设置。成功与错误使用既有 envelope、服务端 request ID、`Cache-Control: private, no-store` 与 `Vary: Cookie`。

首次返回 `201`，精确重试返回 `200`；响应只含 `data: {deployment: {type: deployment, id: ...}, duplicate: boolean}`。不存在、跨 Workspace 或当前不可读的来源 / 目标统一 `404 not_found`；可读 staging 目标缺少独立授权为 `403 forbidden`；可读但归档 / 分类不符、来源非成功、重复目标或 receipt 内容冲突为 `409 conflict`。无效输入 / 未确认返回既有 `400 invalid`，Session 失效为 `401 unauthenticated`，CSRF 失败使用既有错误。错误不得包含授权 provenance、SQL 或外部来源字段。

### 权限与事务重试

继续复用唯一 `RecordStagingDeployment` service / store，内部输入增加操作 ID，输出区分新建 / 精确重复，不建立第二个 Deployment 写实现。权限校验位于应用与事务层；目标列表结果不充当 capability。

首次写入与精确重试都先复核并锁定当前 active membership、来源 CI Run 的读取资格、目标读取资格及 active 环境授权；来源必须 succeeded，目标必须 active staging。撤权、成员暂停或目标归档后，即使持有旧操作 ID 也不能通过写入口取得旧结果；合法历史读取仍遵循原只读权限。

复用 `collaboration_command_receipts` 与现有 claim helper，新增命令类型 `deployment.record`、target 类型 `ci-run`、result 类型 `deployment`，result revision 为 null。幂等范围沿用 Workspace / actor / command / target / client operation；摘要覆盖规范化后的 Environment、CI Run、终态、开始 / 完成 UTC 时间和 confirmed，不包含 server ID、请求时间或授权 ID。

同一范围和摘要的顺序 / 并发重试返回同一 Deployment，不新增关系、事件、Outbox 或 Activity；同一操作 ID 修改任何业务事实返回 conflict。不同操作 ID 或不同用户重复同一 Environment / CI Run 仍冲突，不把已有事实认作该用户新命令成功。既有无 receipt 历史记录保留且不回填操作身份。

receipt 与 Deployment、关系、事件、Outbox 和正常 Activity 在同一事务提交；任何失败整单回滚。数据库原有唯一性与授权触发器不放宽。环境授权、归档和 membership 并发变更与命令按锁顺序串行化；验证提交前后两种合法顺序，不在 transport 预查后无锁写入。

Web 在一次确认时生成操作 ID，发送期间冻结本次 payload；网络错误或未知提交结果保留同一 ID / payload，只允许显式重试。不得自动换 ID、修改事实后直接重发或乐观展示成功。当前页面可取消并放弃本地草稿，但须提示未知结果可能已写入；刷新会丢失内存重试材料，不承诺跨刷新离线恢复，也不将敏感会话材料存入浏览器持久存储。

## 迁移、影响与回退

新增 forward-only migration `011_staging_deployment_receipts.sql`，只扩展既有 receipt 的 kind / target / result CHECK，保留 Document revision 的约束和所有既有命令；不新增业务表、不改写 Deployment 历史或放宽 Environment / CI Run 唯一约束。同步 schema readiness、迁移验收和备份恢复合同；receipt 按既有类别保留，恢复不引入 Secrets。

公共协议新增一个有界目标查询和一个 Session 写入口。权限继续使用现有环境显式授权，并在写路径明确复用来源 / 目标读取检查；不增加授权管理 API、默认授权、数据库依赖或 Web 依赖。没有外部部署调用、真实环境配置或远程写操作。

回退应用时必须匹配 schema readiness：不通过删除 migration history 或旧二进制忽略 migration 011 来回退。必要时部署保留新 schema 合同且移除新路由 / UI 的修复工件，保留已提交事实和 receipt。

## 替代方案

- 直接开放旧 command，仅用 Environment / CI Run 唯一性：只能防重复写，无法区分精确重试和另一用户 / 另一结果的冲突。
- 使用 Jenkins delivery receipt：混淆来源机器身份与用户确认，无法正确表达环境授权撤销。
- 同时实现环境创建、授权管理、自动部署及交付反向关系：扩大权限、数据和外部执行边界，超出本切片；随后分别确认。

## 验证与退出条件

1. Service / HTTP：字段、确认、时间、canonical ID、严格 JSON、体积、Session / CSRF / Origin、跨 Workspace、方法、无敏感字段与无自动 Deployment。
2. 真实 PostgreSQL：三个 Deployment 终态、独立环境授权、production 拒绝、非成功 CI Run 拒绝、顺序 / 并发精确重试、改事实冲突、不同操作 / 用户重复冲突、响应丢失恢复、撤权与归档竞争、原子失败回滚和唯一 Activity。
3. 目标分页：当前授权过滤、稳定游标、空状态、翻页撤权及失败 / 不可读来源；不把列表中出现等同于最终写授权。
4. Web：确认前不可提交、事实更改重置确认、重复点击保护、未知结果保留原请求重试、错误清晰呈现、成功跳转与刷新读取、桌面 / 手机布局及键盘交互。
5. migration、schema readiness 与备份恢复验证，Go / Web 定向门禁及仓库检查；只记录实际执行证据。隔离浏览器服务如需启动，先说明目标、影响与清理范围，不把本 ADR 确认视为启动任意长期服务的授权。

Environment / Component 正式配置、环境授权管理、持久 Jenkins 转发、production、审批、执行器、回滚与通用安全 Audit 不在本次范围。
