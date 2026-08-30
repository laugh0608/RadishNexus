# ADR-0009：显式 staging Deployment 与环境级授权

状态：已接受

日期：2026-08-29

## 背景

Golden Path 已经能够从已验证 Jenkins delivery 原子记录完成态 CI Run，并通过 Component 作用域安全读取。下一段需要表达一次成功构建如何在受控操作后形成 staging Deployment，同时继续证明“构建成功不等于已经部署”。

Environment 此前只有冻结的领域字段和 `environment` / `env_` 引用，没有正式 PostgreSQL 表。Deployment 的类型前缀、最小字段、授权和事件也尚未冻结。现有 Project 角色不能直接复用：CI Run 归属 Component，Environment 是独立部署目标，二者都没有 governing Project；`owner_team_id` 只表达责任，也不能在没有 Team 成员与部署角色契约时自动授予高风险操作。

本切片还不能假装拥有部署执行引擎。application service 能够安全证明的是：一个明确用户在已有外部执行结果后，以独立授权记录一次终态 staging Deployment。网络调用、Secret、审批、运行中状态和 production 操作仍属于后续边界。

## 决定

### Environment 与 Deployment 身份

- 正式 schema 建立 `environments`，落实已冻结的 `environment` / `env_`、Workspace 内唯一 key、名称、classification、责任 Team 和状态字段。
- Environment classification 为 `development / staging / production / other`，创建后不可修改；名称、key 和责任归属变化不能把一个既有 staging 目标静默改成 production，反之亦然。
- 冻结 `deployment` EntityType 与 `dpl_` ID 前缀。Deployment 是 Workspace 内稳定、不可复用的终态事实，创建后不允许更新或删除。
- M0 Deployment 字段为 `environment_id`、`ci_run_id`、`authorization_id`、`status`、`started_at`、`completed_at`、`recorded_by`、`source_kind`、`source_id` 和 `recorded_at`。
- 状态集合暂时只包含 `succeeded / failed / canceled`；`completed_at` 必填，开始时间不能晚于完成时间。本切片不定义 queued、running、重试、回滚或状态修正。
- 同一 CI Run 在同一 Environment 最多形成一个 Deployment。重复记录返回 conflict，不用第二条事件掩盖重复操作。

### 独立环境授权

- `environment_deployment_authorizations` 为具体用户和具体 Environment 建立显式授权，保留授权 ID、授予者、时间和 active / revoked 状态；授权来源和已撤销事实不可删除或重写。
- 记录 Deployment 时，调用者必须是同一 Workspace 的 active 用户，并持有目标 Environment 的 active 显式授权。
- Project viewer / contributor / decider / admin、Environment owner Team、Component owner Team、EntityLink、CI source 和 plugin actor 都不会自动授予部署能力。
- 当前没有授权管理 API；种子数据或未来受控管理入口负责创建与撤销授权。本 ADR 不建立通用 RBAC framework、Team 私密继承或授权模板。

### staging 边界与 application command

- `RecordStagingDeployment` 只接受 user principal 的 `web / api` invocation、Environment、CI Run、终态结果和受控时间。
- 目标必须是 active `staging` Environment；`development / production / other` 均拒绝。production 操作必须通过独立 ADR、权限、确认和审计边界进入。
- 来源 CI Run 必须已经是 `succeeded`。该前提只说明存在可部署构建，不触发命令，也不证明外部部署成功。
- command 只记录调用方已经确认的终态事实，不发起网络调用、不读取 Secret、不执行脚本，也不接收原始 Jenkins payload。
- CI Run application service 不调用 Deployment service；真实 PostgreSQL 用例在显式命令前断言 Deployment 和 `deployment.*` 事件数量为零。

### 原子事实、关系与可审计证据

- 一次成功命令在同一 PostgreSQL 事务内写入不可变 Deployment、`deployment -> deploys -> ci-run` 的 asserted user EntityLink、schema version 1 的 `deployment.recorded` 领域事件和 Activity projector Outbox。
- `deployment.recorded` actor 是实际用户，source 沿用受控 `web / api` invocation；payload 只包含 status、Environment EntityRef 和 CI Run EntityRef，不复制名称、外部 run key、Secret 或执行日志。
- Activity projection version 1 显式接受该事件，只投影 status、Environment 和 CI Run 引用。它不能反向创建或修改 Deployment。
- Deployment 行保留实际用户、调用来源、记录时间和所使用的授权 ID；授权记录保留授予者与时间。它们与不可变领域事件共同形成 M0 可审计证据，但不冒充未来面向安全运营的通用 Audit API、外部执行日志或审批记录。
- 任一 Deployment、事件、关系或 Outbox 写入失败时全部回滚，不留下半记录事实。

### 明确停止线

- 不实现 deployment executor、Jenkins deploy job、HTTP route、公共响应 schema、Secret binding 或外部网络调用；
- 不实现 production Deployment、审批框架、回滚、运行中状态、重试编排或多 provider 抽象；
- 不让 Project 角色、owner Team、CI source 或成功构建隐式获得部署权限；
- 不暴露 source ID、external run key、receipt、digest、Secret、原始 payload 或未经治理的外部 URL；
- 不为本切片建立 Deployment Nexus View、Web 页面、通知、搜索或 Attention Item。

## 未采用的方案

### CI Run succeeded 时自动创建 Deployment

这会把两个不同生命周期压成一个事实，无法区分“构建完成”“部署被授权”“外部执行完成”和“结果被记录”，也会让重放 webhook 产生高风险副作用。

### 复用 Project admin 或 owner Team

CI Run 与 Environment 没有 governing Project，owner Team 也尚无部署角色语义。复用这些字段会建立隐式权限继承，并让组织关系变化意外改变部署能力。

### 首轮直接实现 production 和审批

production 需要独立确认、审批、Secret、失败接管、回滚与更强审计。把这些行为塞进 staging 事实记录会扩大可信边界，并使当前测试无法证明实际执行语义。

### 只写 Deployment 行，不写事件和关系

这会让 Golden Path 无法追溯构建来源，Activity 和备份恢复也缺少不可变事实。三者必须与业务行原子提交。

## 后果

正面影响：

- CI Run 与 Deployment 的边界由 application service 和数据库约束共同证明；
- staging 操作拥有独立、最小且可撤销的环境级授权，不依赖尚未冻结的继承规则；
- Deployment 保留 CI Run、Environment、操作者、授权和来源，可进入后续备份恢复验证；
- 失败事务、重复目标和越权操作不会产生孤立关系、事件或 Outbox。

成本与风险：

- 当前只记录外部已经完成的终态，不证明 RadishNexus 执行了部署；
- 授权创建与撤销尚无产品入口，需要后续管理契约；
- 同一 CI Run 不能在同一 Environment 记录多次尝试，未来若需要重部署必须引入稳定 operation identity，而不是放宽唯一约束；
- Deployment 尚未提供读取投影或 UI，Activity 事实需要后续读取切片才能展示。

## 迁移与验证

forward-only migration `004_staging_deployment_core.sql` 增加正式 Environment、环境级部署授权、Deployment、`deploys` 关系、不可变约束、Workspace 解析与索引。当前没有生产实例或存量数据，不需要回填。

验证覆盖：

1. CI Run 成功后、显式命令前不存在 Deployment 或 `deployment.*` 事件；
2. 无环境授权用户、非 staging Environment、非成功 CI Run 和重复 Environment / CI Run 组合均失败；
3. 显式授权用户原子写入 Deployment、关系、事件和 Outbox；
4. 事件冲突时整单回滚，Deployment 与关系均不存在；
5. 数据库直接写入也不能绕过 staging、CI Run 成功、active membership 和 active authorization；
6. Deployment、Environment classification 和授权 provenance 不能被静默重写；
7. Activity 重建得到唯一 `deployment.recorded`，只保留 status、Environment 与 CI Run 引用。

引入 Deployment 读取、授权管理入口、执行引擎、production、审批、回滚、公共 transport 或外部动作审计前，必须以独立纵向切片继续冻结边界。

实施记录：2026-08-30，ADR-0011 已以独立纵向切片冻结 Deployment 的 Workspace 作用域安全读取与 Web 代表交互；授权管理、执行引擎、production、审批、回滚和公共 transport 仍未进入本 ADR。
