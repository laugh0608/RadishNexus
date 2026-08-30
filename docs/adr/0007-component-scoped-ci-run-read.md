# ADR-0007：Component 作用域下的 CI Run 读取

状态：已接受

日期：2026-08-29

## 背景

ADR-0006 已经建立正式 Component、完成态 CI Run、不可变 delivery receipt 和 `ci-run.recorded` Activity，但刻意没有冻结用户读取权限或 Nexus View 形状。下一段 Golden Path 需要让用户查看单一权威 CI Run 及其时间线，同时继续遵守“引用不授予权限”和“source/plugin 身份不等于用户主体”。

Component 当前是 Workspace 级长期软件资产，没有 `visibility`、对象成员或 governing Project 字段。`owner_team_id` 表达责任边界，不是私密访问控制；如果读取实现把 owner Team、Jenkins source 或关联 Project 临时解释为权限来源，会提前建立没有产品契约支撑的继承规则。反过来，直接暴露权威 CI Run 行又会泄漏 source ID、external run key 或未来可能出现的外部 URL。

因此本切片需要冻结一个足够窄、可由当前数据模型证明的读取规则，并明确哪些权威字段不会进入用户投影。

## 决定

### 读取授权

- M0 的 Component 可由同一 Workspace 的活跃成员读取；非成员、`suspended` 成员和不同 Workspace 的主体不可发现。
- CI Run 读取能力来自其所属 Component：只有当前主体能够读取 Component 时才能读取 CI Run。Component 是 CI Run 的稳定模块内归属，不通过 EntityLink 临时反推权限。
- `owner_team_id` 只表达责任 Team，不授予或限制读取；`planned / active / deprecated / retired` lifecycle 不删除或隐藏已有 CI Run 历史。
- Jenkins source、plugin actor、delivery receipt、event actor 和任何外部凭据都不授予用户读取能力，也不能替代 application `Principal`。
- 直接读取未知、不可读、暂停成员可见范围外或跨 Workspace 的 Component / CI Run，统一返回 not-found，不区分不存在和无权访问。
- 本规则是 M0 最小授权。引入 restricted Component、Team 私密资产、对象分享或 Project 继承前必须以新 ADR 扩展，不能靠查询分支静默改变。

### CI Run Nexus View

CI Run 进入现有 transport-independent `GetNexusView` application query，并继续在一个 repeatable-read 事务中加载 Current、Relations 和 Timeline。

Current 只允许返回：

- CI Run EntityRef；
- `status`；
- `started_at`、`completed_at`、`recorded_at` 和当前记录的 `updated_at`；
- 经当前权限解析的 Component EntityRef 与名称。

CI Run Current 不返回 `source_id`、`external_run_key`、delivery ID、payload digest、receipt、Secret、原始 webhook body、未经治理的 Jenkins URL 或 Repository / commit 占位。当前没有稳定用户标题，不能把 external run key 复制进通用 `title`。

### Timeline 与来源脱敏

- `ci-run.recorded` Timeline 继续只使用 Activity version 1 中的 status、occurred time 和 Component subject。
- Component subject 在读取时重新走相同授权解析并加载当前名称，不使用事件中的标题快照。
- 用户投影可以保留通用 actor kind `plugin`，用于说明该事实来自受控自动化；在 source 展示协议冻结前，不返回 plugin/source ID。
- EventID、Activity type、projection version 和安全 status 仍是内部 query 的稳定事实；receipt 与 digest 不进入 Activity。
- 完全重复或并发重复 delivery 只有一个领域事件，因此同一 CI Run 只形成一个 Timeline item；投影重建不改变该结果。

### 明确停止线

- 不新增 HTTP route、公共响应 schema、分页、游标或 Web fixture 接线；
- 不增加 restricted Component、Team 继承、Project 继承或通用 RBAC framework；
- 不暴露 Jenkins source、external URL、external run key、receipt、digest、Secret 或原始 payload；
- 不建立 Repository / commit 关系，不创建 Deployment，也不把 CI 成功解释为部署；
- 不实现运行中 CI 状态更新、失败认证审计或插件 runtime。

## 未采用的方案

### owner Team 自动形成私密读取组

Team ownership 当前只表示资产责任，领域模型没有声明 Component 对非 owner Team 私密。把责任字段兼作访问控制会让组织调整意外改变历史 CI Run 可见性。

### 继承关联 Project 的权限

Component 与 Project 是并列领域对象，当前也没有冻结相应 EntityLink 方向和继承语义。用任意关联 Project 决定读取会在多 Project 关系下产生冲突，并违反“关系不授予权限”。

### Jenkins source 代表用户读取

source 只说明外部事实来自哪个受控 adapter，不是用户身份或授权主体。让 source ID 进入用户授权会绕过 Workspace membership，并混淆插件 scope 与产品读取权限。

### 直接返回完整 CI Run 行

实现简单，但会把尚未治理的外部标识和后续 provider 字段变成用户协议，也扩大 receipt、URL 和来源信息泄漏面。Nexus View 应只投影当前交互需要的安全事实。

## 后果

正面影响：

- CI Run 与 Component 使用一条清晰、可复验且不依赖 source 的授权链；
- 用户可以看到状态、时间和当前 Component 上下文，同时核心 provenance 仍保留在权威记录与事件中；
- 暂停成员、跨 Workspace 请求和未知对象保持不可区分的 not-found；
- 后续 Web 代表交互可以消费真实安全投影，不需要临时 fixture 字段或公共 API。

成本与风险：

- M0 的所有活跃 Workspace 成员都能读取 Component 与 CI Run，尚不能表达私密软件资产；
- source 展示、external run key、外部跳转和 Repository commit 仍需独立协议；
- CurrentProjection 增加 CI Run 专用可选字段，未来形成公共 API 前仍需设计显式 discriminated response；
- Activity 仍由显式重建触发，尚未建立常驻 projector worker。

## 迁移与验证

本 ADR 不改变 PostgreSQL schema；读取继续使用 migration 003 已有的 Component 归属、Workspace 外键和 Activity。实现增加 Component / CI Run 授权解析、CI Run Current 安全投影和 plugin actor ID 脱敏。

验证必须覆盖：

1. 活跃 Workspace 成员读取 CI Run Current、Component 和唯一 Timeline；
2. 非成员、暂停成员与跨 Workspace 主体得到 not-found；
3. Current 和 Timeline 不含 source ID、external run key、receipt、digest、Secret、原始 payload 或 Jenkins URL；
4. Component lifecycle 不删除既有 CI Run 历史，owner Team 不成为隐式读取条件；
5. Activity 重建与重复 delivery 不产生重复 Timeline；
6. Decision / Ticket Nexus View 现有权限和 restricted placeholder 行为不变。

已实际执行：

```text
./scripts/check-server.sh             PASS
./scripts/check-server-postgres.sh    PASS
./scripts/check-m0-core-contracts.sh  PASS
./scripts/check-repo.sh               PASS
git diff --check                      PASS
```

若后续需要私密 Component、Team / Project 继承、外部来源展示或公共 CI Run API，应以新 ADR 和独立纵向切片扩展本记录。
