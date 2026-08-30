# ADR-0011：Workspace 作用域下的 Deployment 安全读取

状态：已接受

日期：2026-08-30

## 背景

ADR-0009 已冻结显式 staging Deployment 的写入边界：只有 active Workspace 用户持有目标 Environment 的 active 显式授权后，才能记录一次来自 succeeded CI Run 的终态事实。该切片已经原子保留 Deployment、`deploys` EntityLink、`deployment.recorded` 领域事件与 Outbox，但故意没有建立读取投影或 UI。

Golden Path 要求从 Deployment 反向找到构建来源，并在 Nexus View 中看到 Current、Relations 和 Timeline。读取边界不能简单复用高风险写权限：能够记录 Deployment 与能够看到团队共享的 staging 交付事实是不同能力，授权撤销也不能让已有审计历史从原参与者视野中消失。同时，读取不能只检查 Deployment 行本身，否则未来 Environment 或 CI Run 引入更细可见性时会通过关系和 Activity 泄漏目标。

本切片仍没有公共认证与 HTTP 响应契约。为了展示而创建临时 Header、Cookie 或无验证 handler，会把未冻结的 transport 近似成正式安全边界。

## 决定

### 读取授权与不可发现性

- M0 staging Environment 没有 restricted 可见性或对象成员字段；同一 Workspace 的 active 成员可以读取，非成员、暂停成员和跨 Workspace 主体不可发现。
- Deployment 的读取能力由其稳定 `environment_id` 与 `ci_run_id` 共同决定。当前主体只有同时能读取目标 Environment 和来源 CI Run 时，才能读取 Deployment。
- Environment 的 owner Team、Project 角色、EntityLink、CI source 和环境级部署授权本身都不授予 Deployment 读取能力；环境级授权只控制记录命令。
- 直接读取未知或不可读的 Environment、CI Run 或 Deployment 统一返回 not-found，不区分对象不存在、主体不在 Workspace 或依赖对象不可读。
- Environment 归档不会删除或隐藏已有不可变 Deployment 历史。未来引入 restricted Environment、Team 私密资产、production 可见性或对象分享前，必须以新 ADR 扩展读取规则，不能靠查询分支静默改变。

### Deployment Nexus View

Deployment 进入 transport-independent `GetNexusView` application query，并在同一 repeatable-read 事务中加载 Current、Relations 和 Timeline：

- Current 只返回稳定 Deployment 引用、终态 status、started / completed / recorded 时间，以及按当前权限解析的 Environment 和来源 CI Run。
- Deployment 不存在可变 `updated_at`；内部 Current 的更新时间等于不可变 `recorded_at`，不伪造后续状态变化。
- Relations 读取现有 asserted user `deploys` 关系，并再次按来源 CI Run 当前权限投影。
- `deployment.recorded` Timeline 只显示 status、可读的 Environment 与 CI Run，以及实际 user actor；Activity 仍从不可变领域事件重建，不反向修改 Deployment。
- Current、Relations 和 Timeline 不返回 `authorization_id`、调用 source ID、owner Team、external run key、Jenkins source、delivery receipt、digest、Secret、原始 payload、执行日志或未经治理的外部 URL。
- 未来依赖对象变为不可读时，直接 Deployment 读取整体返回 not-found；不能用只保留部分 Current 的方式泄漏其存在。来自其它可读来源对象的关系和 Activity 仍遵循通用 restricted / hidden 规则。

### Web 代表交互

- Web 原型继续只消费权限过滤后的 discriminated union，不接收角色、授权 ID 或原始领域行。
- 默认代表入口展示 succeeded / failed Deployment、Environment、来源 CI Run、`deploys` Relation 和唯一 `deployment.recorded` Timeline。
- failed Deployment 必须明确表达“来源构建成功，但独立部署结果失败”，不能改写或弱化 CI Run 事实。
- 页面继续明确标注静态 fixture；本切片不增加业务 HTTP route、无类型 `fetch`、临时凭据解析或公共响应 schema。

### 明确停止线

- 不实现公共 Deployment API、认证、授权管理入口、搜索、通知、Attention Item 或导出；
- 不实现 deployment executor、外部网络调用、Secret binding、production、审批、回滚、运行中状态或重试；
- 不让读取权限反向授予部署能力，也不让部署授权成为发现历史的必要条件；
- 不从 CI Run 反向制造未冻结的 Deployment 关系或把构建成功表现为已经部署。

## 未采用的方案

### 只有持有 Environment 部署授权的人才能读取

这会混淆执行能力与团队交付可见性，并让授权撤销导致已有事实从历史中消失。当前 staging 事实按 Workspace 共享读取，写入仍保持更强的显式授权。

### 只检查 Deployment 所属 Workspace

当前结果看似等价，但会绕过 Environment 与 CI Run 的原对象权限，并在未来权限收紧时形成泄漏。读取必须从一开始组合依赖对象的当前权限。

### 把授权 provenance 与 Jenkins 来源全部放进 Current

这些字段属于内部权威记录、外部来源边界或未来 Audit 展示，不是普通 Nexus View 的最小安全事实。提前返回会扩大敏感面并把尚未治理的来源协议冻结进 UI。

### 为原型直接增加临时 HTTP handler

当前认证 verifier、session、request ID、公共错误对象和 API 版本均未冻结。临时 handler 不能提供真实端到端安全证据，只会制造需要兼容的第二套口径。

## 后果

正面影响：

- Golden Path 能从 Deployment 看到目标 Environment、来源 CI Run、关系和可重建时间线；
- 读取与高风险写授权保持分离，同时为未来对象级可见性保留组合检查；
- Deployment 失败不会改写来源构建，归档 Environment 也不会抹掉历史；
- Web 可以验证真实展示需求而不提前开放公共 transport。

成本与风险：

- M0 active Workspace 成员可以读取全部 staging Environment 与 Deployment；未来出现私密或 production 环境时必须重新决策；
- 当前只提供正向 `Deployment -> CI Run` 关系，不在 CI Run 页面生成反向 Deployment 列表；
- user actor 仍是内部 application projection，公共 API 如何展示成员身份尚未冻结；
- 静态 Web fixture 不能替代认证、HTTP 和真实数据联调。

## 迁移与验证

本决策不新增数据库迁移；复用 migration 004 的 Environment、Deployment、`deploys` 关系和 Activity 事实。

验证覆盖：

1. 没有部署授权的 active Workspace 成员可以读取完整 Deployment Nexus View；
2. 非成员、暂停成员和跨 Workspace 主体得到 not-found；
3. Environment 归档后仍能读取已有 Deployment 历史；
4. Current 只包含终态、受控时间、Environment 与 CI Run，Relations 和 Timeline 与已有事实一致；
5. 输出不包含授权 ID、Jenkins source、delivery、digest、external run key、Secret 或原始 payload；
6. Web succeeded / failed、loading、error、桌面与窄屏状态通过自动化和真实浏览器复核，失败 Deployment 不冒充 CI Run 失败；
7. 服务端与 Web 定向检查继续通过，且没有为展示新增临时 HTTP route。
