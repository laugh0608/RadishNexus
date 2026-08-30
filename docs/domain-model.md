# RadishNexus 领域模型

状态：M0 领域基线，首批核心字段已冻结

日期：2026-08-30

## 目标

本文件定义 RadishNexus 首期最重要的业务概念及其边界。目标不是建立一个抽象程度很高的“万能实体系统”，而是避免 `Project`、`Ticket` 和插件在实现过程中逐渐承担彼此冲突的职责。

领域模型优先服务于一个产品承诺：让团队能够从讨论追溯到决策、执行、代码、构建、部署和复盘，并明确知道每段上下文来自哪里、当前由谁负责、对谁可见。

## 顶层关系

```text
Workspace
├── Team
├── Project                 协作和权限边界
├── Initiative              有目标和结束条件的阶段性工作
└── Component               长期存在的软件资产
    ├── Repository          外部代码库映射
    ├── CI Run              一次构建或流水线运行
    ├── Environment         dev / staging / production
    └── Deployment          某版本进入某环境的事实

Channel / Conversation / Message
Ticket / Document / Decision
        │
        └── EntityLink ── 连接上述任意对象
```

这些关系不是严格的数据库父子树。一个 Initiative 可以涉及多个 Component，一个 Channel 也可以同时服务于 Initiative 和 Component；跨域关联统一使用 `EntityLink` 表达。

## M0 核心字段约定

本节冻结业务字段的含义，不冻结数据库列类型、索引、HTTP 表示或 ID 生成算法。具体稳定引用、授权和事件协议见已由 ADR-0002 接受的[核心实体、授权与事件契约](architecture/core-contracts.md)。

Project、Initiative、Component、Decision、Environment 和 EntityLink 共享以下字段：

| 字段 | 含义 |
| --- | --- |
| `id` | 对象类型内唯一、创建后不可变且不复用的稳定 ID |
| `workspace_id` | 对象所属 Workspace，创建后不可变 |
| `created_by` | 创建该对象的用户、系统、插件或导入主体 |
| `created_at` | 权威记录中的创建时间 |
| `updated_at` | 最近一次业务变化时间，不因读取或投影重建而变化 |

共同不变量：

- 名称、slug、外部 URL 和数据库行号都不是对象身份；重命名不能改变 `id`。
- 普通备份恢复必须保留 `id`；合并导入遇到冲突时必须显式映射，不能静默复用或改写关系。
- M0 不支持跨 Workspace 的 EntityLink。所有关系、事件和投影都必须能验证同一 `workspace_id`。
- `created_by` 表达来源主体，不等于该主体永久拥有读取或修改权限。
- 删除、归档和移除是不同语义。归档不删除历史，EntityLink 的移除也不删除其来源证据。

## 组织和工作边界

### Workspace

一个独立组织的最高数据、成员、配置和授权边界。任何实体、事件、搜索索引和插件数据都必须能够明确归属到一个 Workspace。

首期 Workspace membership 独立保存 `status: active / suspended` 与 `role: owner / member`。`owner` 表示 Workspace 级管理责任，不自动授予 restricted Project、私密协作对象或 Environment Deployment 权限；这些能力仍由对应对象的显式授权决定。用户 Session 不固定 Workspace，业务请求选择 Workspace 后必须以当前 active membership 重新解析权限。

M1 本地身份基线以不可变小写 ASCII `login_name` 关联 `users`，密码只保存 Argon2id verifier；服务端 Session 只保存 opaque token 与 CSRF token 的 digest。`local_accounts` 纳入受控 PostgreSQL 运维备份，`user_sessions` 只保留 schema，恢复后旧登录态全部失效。精确边界见 [ADR-0012](adr/0012-local-identity-and-session-foundation.md)。

### Team

稳定的人员责任边界，用于成员管理、默认权限和软件资产所有权。首期可以只实现简单团队和成员关系，不建设复杂组织架构。

### Project

用于协作导航和权限管理的工作空间。Project 不是代码仓库、软件服务或一次性计划的同义词。

Project 可以承载频道、工单和文档，也可以关联 Initiative 与 Component。它存在的主要理由是为小团队提供一个容易理解的协作入口，而不是成为所有业务数据的万能父对象。

M0 最小字段：

| 字段 | 约束 |
| --- | --- |
| `key` | Workspace 内唯一的短标识，可修改，不能代替稳定 ID |
| `name` | 用户可见名称 |
| `summary` | 可选的用途说明 |
| `owner_team_id` | 对 Project 负责的 Team |
| `visibility` | `workspace / restricted` |
| `status` | `active / archived` |

`workspace` 表示 Workspace 成员默认可发现，`restricted` 表示必须通过显式成员或角色授权。Project 的可见性只提供默认边界，不能自动放宽其中私密 Channel、Conversation、Document 或其它对象的权限。归档 Project 不级联删除其内容或关系。

### Initiative

具有明确目标、负责人、健康状态和结束条件的阶段性工作，例如“完成私有化部署首版”或“迁移支付链路”。

M0 最小字段：

| 字段 | 约束 |
| --- | --- |
| `title` | 用户可见标题 |
| `summary` | 可选背景摘要 |
| `desired_outcome` | 计划进入 `active` 前必填的完成判断 |
| `owner_user_id` | 计划进入 `active` 前必填的个人负责人 |
| `owner_team_id` | 计划进入 `active` 前必填的责任 Team |
| `status` | `proposed / planned / active / completed / canceled` |
| `health` | `on-track / at-risk / off-track / unknown` |
| `start_at` | 可选开始时间 |
| `target_at` | 可选目标完成时间 |
| `completed_at` | 进入 `completed` 时必填 |

Initiative 与 Project、Component、Ticket、Document 和 Decision 的关联由 EntityLink 表达，不把这些对象 ID 复制成数组字段。`active` 必须有 owner 和 desired outcome；`completed` 必须有 completed time；`canceled` 与 `completed` 是不同终态。健康状态只描述目标风险，不能代替执行状态。

### Component

由团队长期维护的软件资产，例如服务、网站、客户端、库、数据管道或基础设施组件。

M0 最小字段：

| 字段 | 约束 |
| --- | --- |
| `key` | Workspace 内唯一、可修改的短标识 |
| `name` | 用户可见名称 |
| `summary` | 可选资产说明 |
| `type` | `service / web / client / library / data-pipeline / infrastructure / other` |
| `owner_team_id` | 进入 `active` 前必填的责任 Team |
| `lifecycle` | `planned / active / deprecated / retired` |

主要 Repository、CI pipeline、Environment、Runbook、文档和依赖都通过有类型的 EntityLink 表达。`retired` 不删除历史 CI Run、Deployment 或 Decision；`other` 只用于无法归入现有类型的真实资产，不能作为绕过建模的默认值。

首期只需维护最小元数据和引用，不建设完整内部开发者门户或复杂依赖拓扑。

### Repository

外部 Git 代码库的受控映射。RadishNexus 不托管 Git，只保存 provider、稳定外部 ID、URL、默认分支和关联 Component。Repository 凭据属于插件或集成 Secrets，不属于普通业务字段。

### CI Run

一次构建或流水线运行的独立事实。M0 正式切片冻结 `ci-run` 类型与 `cir_` ID 前缀，并先验证 Jenkins 完成事实；CI Run 归属一个 Component，但不等于 Repository、commit 或 Deployment。

首批最小字段：

| 字段 | 约束 |
| --- | --- |
| `component_id` | 同一 Workspace 内稳定且创建后不可修改的 Component |
| `source_kind` | 当前只允许 `jenkins` |
| `source_id` | 受控来源 adapter 的不透明稳定标识 |
| `external_run_key` | source 作用域内稳定的外部运行标识，与 source 组合后唯一 |
| `status` | `queued / running / succeeded / failed / canceled` |
| `started_at` | 可选的来源开始时间 |
| `completed_at` | `succeeded / failed / canceled` 时必填 |

当前 application service 只记录完成终态，不定义 `queued / running` 的乱序更新、回退或重开语义。Jenkins delivery ID 是 receipt 的幂等身份，不是 CI Run ID；完全重复只返回既有 CI Run，相同 delivery 的 payload digest 改变则失败。receipt、CI Run、领域事件和 Outbox 原子提交，Secret 与原始 webhook body 不进入这些记录。精确边界见 [ADR-0006](adr/0006-verified-jenkins-delivery-and-ci-run.md)。

CI Run 成功只表达构建事实，不能自动创建 Deployment。Repository、commit、Ticket 和其它上下文等待相应对象字段与关系方向冻结后再通过 EntityLink 表达。

M0 读取以 Component 为授权边界：同一 Workspace 的活跃成员可以读取 Component 及其 CI Run；非成员、暂停成员和跨 Workspace 主体不可发现。`owner_team_id` 只表示责任，不形成私密访问组；Jenkins source 和 plugin actor 也不授予用户权限。CI Run 的 Nexus View 只投影受控状态、时间与当前 Component，不暴露来源标识和 receipt。详见 [ADR-0007](adr/0007-component-scoped-ci-run-read.md)。

### Environment

一个稳定部署目标，例如 `development`、`staging` 或 `production`。Environment 承载部署保护策略、审批要求和 Secrets 引用，但不保存 Secrets 明文。

M0 最小字段：

| 字段 | 约束 |
| --- | --- |
| `key` | Workspace 内唯一、可修改的短标识 |
| `name` | 用户可见名称 |
| `classification` | `development / staging / production / other` |
| `owner_team_id` | 对该部署目标负责的 Team |
| `status` | `active / archived` |

Environment 独立存在并通过 EntityLink 关联 Component，不能假设一个 Environment 只属于一个 Component。保护策略、审批规则和 Secret binding 是独立受控记录；Environment 只引用这些记录，不保存凭据。`production` 分类不得因改名或 UI 简化而失去独立权限和明确确认要求。

### Deployment

某个版本、制品或提交进入某个 Environment 的事实记录。Deployment 与 CI Run 分开：构建成功不等于已经部署；已经发生的来源、操作者、审批和回滚关系必须保留，M0 尚无审批或回滚时不能伪造这些事实。

M0 正式切片先冻结 `deployment` 类型与 `dpl_` ID 前缀，并只记录显式、终态的 staging 事实：

| 字段 | 约束 |
| --- | --- |
| `environment_id` | 同一 Workspace 内 active 且 classification 为 `staging` 的 Environment |
| `ci_run_id` | 同一 Workspace 内已经 `succeeded` 的 CI Run |
| `authorization_id` | 操作者针对目标 Environment 的 active 显式授权 |
| `status` | `succeeded / failed / canceled` |
| `started_at` | 可选外部执行开始时间 |
| `completed_at` | 必填，且不能早于开始时间 |
| `recorded_by` | 明确记录该事实的用户 |
| `source_kind / source_id` | 受控 `web / api` 调用来源 |
| `recorded_at` | RadishNexus 原子记录时间 |

同一 CI Run 在同一 Environment 最多形成一条 Deployment。CI Run 成功不会调用 Deployment 写入；只有 active Workspace 用户持有目标 Environment 的显式授权后，才能通过独立命令记录。Project 角色、owner Team、EntityLink 和 CI source 都不隐式授予部署能力。

M0 command 只记录调用方已经确认的外部终态，不执行部署、不读取 Secret，也不建立 production、审批、回滚或运行中状态。Deployment、`deploys` CI Run 关系、`deployment.recorded` 事件和 Outbox 原子提交；权威行保留授权、操作者和来源，但不替代未来通用 Audit 与外部执行日志。精确边界见 [ADR-0009](adr/0009-explicit-staging-deployment.md)。

## 协作对象

### Channel、Conversation 与 Message

- Channel 是公开或私密的持续协作空间；
- Conversation 表示私聊和群聊；
- Message 与 Thread 保存原始讨论；
- 讨论可以产生 Ticket、Document 或 Decision，但转换后仍保留双向引用。

### M0 协作作用域

M0 纵向切片继续冻结 `thread` / `thr_` 与 `ticket` / `tkt_` 的稳定引用。Thread、Decision 和 Ticket 各自保存不可变的 `governing_project_id` 作为协作与授权上下文；来源、实现和其它业务关系仍由 EntityLink 表达。Thread 首批冻结 `title` 与 `project / restricted` 可见性；Ticket 首批冻结 `title` 与 `open / in-progress / done / canceled` 状态。完整聊天和工单工作流字段等待 Golden Path 交互验证。精确权限边界见 [ADR-0004](adr/0004-project-scoped-collaboration-permissions.md)。

### Ticket

可执行工作的统一对象。需求、任务、缺陷、事故和改进项通过 type、workflow 和字段配置表达，不在首期拆成彼此无关的模型。

### Document

承载设计、说明、Runbook 和复盘等长内容。在线协作、版本和离线同步属于 Document 自身能力；Document 不能代替结构化 Decision、Ticket 或 Deployment。

### Decision

Decision 是一等对象，不只是文档中的一个标题或被置顶的消息。

M0 最小字段：

| 字段 | 约束 |
| --- | --- |
| `question` | 需要回答的问题 |
| `outcome` | 最终结论；`accepted` 时必填 |
| `status` | `proposed / accepted / rejected / superseded` |
| `proposer_id` | 提议人 |
| `decider_ids` | 实际作出接受或拒绝结论的用户集合 |
| `decided_at` | `accepted / rejected` 时必填 |
| `rationale` | 采用结论的理由；`accepted` 时必填 |
| `alternatives` | 被评估的主要方案及取舍 |
| `consequences` | 已知影响和后续动作 |
| `rejection_reason` | `rejected` 时必填 |
| `review_at` | 可选复核时间 |

原始 Thread、Document、实验、Ticket 或 CI Run 作为 evidence EntityLink；Decision 的替代关系也由 EntityLink 表达，不复制为对象 ID 数组。

字段不变量：

- `proposed` 可以暂缺 outcome，但必须有 question、proposer 和至少一个 evidence 引用；
- `accepted` 必须有 `outcome`、`rationale`、`decider_ids` 和 `decided_at`，并由具备确认权限的人执行状态变化；
- `rejected` 必须有 `rejection_reason`、`decider_ids` 和 `decided_at`，不能通过删除草案伪造从未讨论；
- `superseded` 必须成为一条有效 `supersedes` EntityLink 的终点，该关系的起点是替代它的新 Decision；
- evidence、关联 Ticket 和 Component 统一通过 EntityLink 表达，不复制原始 Thread 或 Document 全文；
- 自动提炼只能写入草案字段，不能成为确认主体或直接产生 `accepted`。

首期产品动作：

- 从 Message 或 Thread 创建 Decision 草案；
- 在原讨论中显示 Decision 卡片和当前状态；
- 将 Accepted Decision 关联到 Ticket、Document 和 Component；
- 提供“待我决策”和“已被替代”视图；
- 所有自动提炼内容必须由人确认后才能成为 Accepted。

## 关系与时间线

### EntityLink

跨对象关系必须是一等记录，而不是散落在各模块中的临时外键或正文 URL。

M0 最小字段：

```text
id
workspace_id
from_ref
relation_type
to_ref
assertion           asserted / derived
origin              user / system / plugin / import
origin_ref
created_by
created_at
source_event_id
metadata
state               active / removed
removed_by
removed_at
removal_reason
```

`assertion` 表达关系是否经过有权主体明确确认，`origin` 表达关系从哪类入口产生；两者不能合并成一个字段。`origin_ref` 指向具体插件、导入批次或系统规则，用户直接创建时可以为空。`source_event_id` 在关系由既有事件触发时必填，用户与关系创建事件同事务的直接操作可以为空。

典型关系包括：

- `derived-from`：Decision 来源于 Thread；
- `implements`：Ticket 实现某 Decision；
- `documents`：Document 解释某 Component 或 Initiative；
- `built-from`：CI Run 来源于 Repository commit；
- `deploys`：Deployment 发布某构建或制品；
- `affects`：Incident 或 Ticket 影响某 Component；
- `supersedes`：新 Decision 替代旧 Decision。

关系必须保留来源和建立时间。插件推导的关系与用户明确建立的关系要能区分，避免自动化结果被误认为人工确认事实。

附加不变量：

- 两端必须是同一 Workspace 中已注册且允许该关系方向的实体类型；
- 创建关系必须同时通过关系类型规则和两端对象权限，EntityLink 不是万能写接口；
- `removed` 是不可逆的历史状态；重新建立关系创建新记录，不能覆盖旧来源和时间；
- 插件、导入器和重试任务必须使用稳定交付身份实现幂等，同一交付不能产生重复 EntityLink；
- 不同来源对同一语义关系的记录可以并存，展示层可以合并显示，但不能丢失各自来源；
- `metadata` 只允许关系类型已声明的非敏感字段，不能存放目标标题快照、Secret 或用来绕过目标模块不变量的数据。

### Activity

每个重要实体都提供统一活动时间线。Activity 是领域事件的权限过滤投影，用于解释“发生了什么”，而不是另一套可随意写入的业务数据库。

时间线至少能展示：

- 状态和负责人变化；
- 新建或解除的关系；
- Decision 状态变化；
- CI Run 和 Deployment；
- 高风险外部操作及审批；
- 插件失败、重试和人工接管。

### 权限规则

- 引用不会授予目标对象权限；
- 无权访问目标时，只显示不泄漏标题和摘要的受限占位符；
- 搜索、通知、时间线和 AI 插件使用同一权限判断；
- 关系两端权限变化后，缓存和派生视图必须及时失效；
- 删除、撤销访问和导出时必须处理关系残留。

## 个人行动投影

### Attention Item

“未读”与“需要我行动”是两种不同状态。Attention Item 是面向用户的行动投影，可以由以下事件产生：

- 被要求作出 Decision；
- 收到审批请求；
- 被分配 Ticket；
- CI Run 失败且用户是责任人；
- Initiative 状态更新过期；
- Playbook Run 中的检查项到期。

每个 Attention Item 必须包含产生原因、目标对象、负责人、截止时间和明确完成条件。阅读消息不能自动完成行动项。

## Nexus View

主要对象页面采用一致的信息结构，而不是只提供一组模块间跳转链接：

1. Current：当前状态、负责人、健康度和关键结论；
2. Relations：相关 Decision、Ticket、Document、Component、CI Run 和 Deployment；
3. Timeline：经过权限过滤的完整活动时间线；
4. Actions：用户当前有权执行的下一步。

首期不要求所有对象共享完全相同的 UI，但这四类信息应成为产品设计和 API 的共同约束。

## 后续对象

### Playbook 与 Run

Playbook 是可重复流程模板；Run 是一次不可被后续模板修改追溯改变的执行实例。适合发布、故障、迁移、值班交接和入职等场景。

Run 可以创建或关联频道、检查表、Ticket、负责人、CI Run、Deployment、状态更新和复盘 Document。该能力在 Golden Path 稳定后引入，不进入首期通用插件运行时。

## 实现约束

- 首期使用 PostgreSQL 普通表、索引和递归查询即可，不引入图数据库；
- 稳定实体引用与数据库主键表示可以分离；
- EntityLink 不能成为绕过各模块不变量的万能写接口；
- Initiative、Component、Decision 和 Environment 先冻结语义，再决定完整 UI；
- 派生的 Attention Item 和 Activity 必须能够从权威业务记录重新构建。
