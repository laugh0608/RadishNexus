# RadishNexus 领域模型

状态：初始领域基线

日期：2026-08-27

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

## 组织和工作边界

### Workspace

一个独立组织的最高数据、成员、配置和授权边界。任何实体、事件、搜索索引和插件数据都必须能够明确归属到一个 Workspace。

### Team

稳定的人员责任边界，用于成员管理、默认权限和软件资产所有权。首期可以只实现简单团队和成员关系，不建设复杂组织架构。

### Project

用于协作导航和权限管理的工作空间。Project 不是代码仓库、软件服务或一次性计划的同义词。

Project 可以承载频道、工单和文档，也可以关联 Initiative 与 Component。它存在的主要理由是为小团队提供一个容易理解的协作入口，而不是成为所有业务数据的万能父对象。

### Initiative

具有明确目标、负责人、健康状态和结束条件的阶段性工作，例如“完成私有化部署首版”或“迁移支付链路”。

建议字段：

- title、summary 和 desired outcome；
- owner 和参与 Team；
- `proposed / planned / active / completed / canceled`；
- `on-track / at-risk / off-track / unknown`；
- start、target 和 completed time；
- 最新状态更新及其历史；
- 关联 Project、Component、Ticket、Document 和 Decision。

### Component

由团队长期维护的软件资产，例如服务、网站、客户端、库、数据管道或基础设施组件。

建议字段：

- component type；
- owner Team；
- lifecycle；
- 主要 Repository；
- 关联 CI pipeline、Environment、Runbook 和文档；
- 依赖和被依赖关系。

首期只需维护最小元数据和引用，不建设完整内部开发者门户或复杂依赖拓扑。

### Repository

外部 Git 代码库的受控映射。RadishNexus 不托管 Git，只保存 provider、稳定外部 ID、URL、默认分支和关联 Component。Repository 凭据属于插件或集成 Secrets，不属于普通业务字段。

### Environment

一个稳定部署目标，例如 `development`、`staging` 或 `production`。Environment 承载部署保护策略、审批要求和 Secrets 引用，但不保存 Secrets 明文。

### Deployment

某个版本、制品或提交进入某个 Environment 的事实记录。Deployment 与 CI Run 分开：构建成功不等于已经部署，部署完成也必须保留来源、操作者、审批和回滚关系。

## 协作对象

### Channel、Conversation 与 Message

- Channel 是公开或私密的持续协作空间；
- Conversation 表示私聊和群聊；
- Message 与 Thread 保存原始讨论；
- 讨论可以产生 Ticket、Document 或 Decision，但转换后仍保留双向引用。

### Ticket

可执行工作的统一对象。需求、任务、缺陷、事故和改进项通过 type、workflow 和字段配置表达，不在首期拆成彼此无关的模型。

### Document

承载设计、说明、Runbook 和复盘等长内容。在线协作、版本和离线同步属于 Document 自身能力；Document 不能代替结构化 Decision、Ticket 或 Deployment。

### Decision

Decision 是一等对象，不只是文档中的一个标题或被置顶的消息。

最小字段：

- question：需要回答的问题；
- outcome：最终结论；
- status：`proposed / accepted / rejected / superseded`；
- proposer、deciders 和 decided time；
- rationale：采用该结论的理由；
- alternatives：被评估的主要方案；
- consequences：已知影响和后续动作；
- evidence：原始 Thread、Document、实验、Ticket 或 CI Run；
- supersedes / superseded-by；
- review date：需要重新评估时使用。

首期产品动作：

- 从 Message 或 Thread 创建 Decision 草案；
- 在原讨论中显示 Decision 卡片和当前状态；
- 将 Accepted Decision 关联到 Ticket、Document 和 Component；
- 提供“待我决策”和“已被替代”视图；
- 所有自动提炼内容必须由人确认后才能成为 Accepted。

## 关系与时间线

### EntityLink

跨对象关系必须是一等记录，而不是散落在各模块中的临时外键或正文 URL。

建议字段：

```text
id
workspace_id
from_entity
relation_type
to_entity
origin              user / system / plugin / import
created_by
created_at
source_event_id
metadata
state               active / removed
```

典型关系包括：

- `derived-from`：Decision 来源于 Thread；
- `implements`：Ticket 实现某 Decision；
- `documents`：Document 解释某 Component 或 Initiative；
- `built-from`：CI Run 来源于 Repository commit；
- `deploys`：Deployment 发布某构建或制品；
- `affects`：Incident 或 Ticket 影响某 Component；
- `supersedes`：新 Decision 替代旧 Decision。

关系必须保留来源和建立时间。插件推导的关系与用户明确建立的关系要能区分，避免自动化结果被误认为人工确认事实。

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
