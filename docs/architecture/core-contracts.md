# 核心实体、授权与事件契约

状态：M0 契约基线，已由 ADR-0002 接受

日期：2026-08-28

## 目的

本文件定义 Golden Path 开始实现前必须共享的最小技术契约：稳定实体引用如何表示和解析，跨对象关系如何复用原对象权限，领域事件如何进入 Transactional Outbox，以及 Activity 如何从权威事实投影并在读取时过滤。

业务字段与状态含义以[领域模型](../domain-model.md)为准；总体模块和部署方向以[总体架构](overview.md)为准。本文件不选择 Go Web 框架、数据库访问库、HTTP 路由、ID 生成库或前端状态管理方案。

## 适用范围

初始 M0 冻结 Project、Initiative、Component、Decision、Environment 和 EntityLink 的引用能力，并用 Thread、Ticket、CI Run 和 Deployment 验证接口是否足以承载 [Golden Path](../golden-path.md)。ADR-0004 随首个正式纵向切片继续冻结了 Thread 与 Ticket 的类型前缀、最小字段和授权上下文；ADR-0006 冻结 CI Run 的类型前缀、最小来源字段和完成事实；ADR-0009 冻结显式 staging Deployment、环境级授权和原子关系边界。

M0 不支持：

- 跨 Workspace 引用；
- 插件自定义核心实体类型；
- 通过一个通用 Entity API 修改任意模块对象；
- 把 EntityLink、Activity 或事件 payload 当成目标对象副本；
- 跨实例在线解析 `entity://` URI。

## 稳定身份与引用

### 基本类型

```text
EntityType   由平台注册的、带版本治理的类型名
EntityID     当前实例内、类型内唯一、不透明、不可复用的稳定 ID
EntityRef    (type, id)
ActorRef     (kind, id?)，kind 为 user / system / plugin / import
```

M0 首批冻结以下类型名与 ID 前缀：

| EntityType | ID 前缀 | 说明 |
| --- | --- | --- |
| `project` | `prj_` | 协作与默认权限边界 |
| `initiative` | `ini_` | 有结束条件的阶段目标 |
| `component` | `cmp_` | 长期软件资产 |
| `decision` | `dec_` | 可确认、拒绝和替代的决策 |
| `environment` | `env_` | 稳定部署目标 |
| `entity-link` | `lnk_` | 带来源的跨对象关系 |
| `thread` | `thr_` | 作为讨论证据的 Thread |
| `ticket` | `tkt_` | 可执行工作对象 |
| `ci-run` | `cir_` | 一次构建或流水线运行 |
| `deployment` | `dpl_` | 一次显式记录的部署终态事实 |

Document 和 Repository 等其余 Golden Path 类型进入同一注册表时，其 ID 前缀随各自字段契约一起冻结。前缀用于校验和诊断，不携带权限、Workspace、创建时间或存储位置。Thread 与 Ticket 的首批字段和权限上下文由 [ADR-0004](../adr/0004-project-scoped-collaboration-permissions.md) 冻结；CI Run 的来源和幂等边界由 [ADR-0006](../adr/0006-verified-jenkins-delivery-and-ci-run.md) 冻结；Deployment 由 [ADR-0009](../adr/0009-explicit-staging-deployment.md) 冻结。

### 结构化表示

内部接口、事件和 HTTP 契约优先使用结构化引用：

```json
{
  "type": "decision",
  "id": "dec_01K4EXAMPLE"
}
```

文本、导出和调试场景可以使用等价 URI：

```text
entity://decision/dec_01K4EXAMPLE
```

规范形式满足以下条件：

- `type` 使用注册表中的小写 ASCII 名称；多个单词使用连字符；
- `id` 是不透明 ASCII 字符串，不允许 `/`、查询参数或片段；
- 输入不是规范形式时直接拒绝，不做可能改变身份的大小写或 Unicode 归一化；
- `workspace_id` 不编码进 EntityRef，而是作为每次解析的独立可信上下文；
- EntityRef 只在当前实例的数据边界内可解析，不是公共互联网 URI。

### 生命周期

- 对象创建后 `id` 不变且不复用；名称、key、slug、URL 和外部系统映射变化不影响身份。
- 同一实例的备份恢复保留 Workspace ID、EntityID、EntityLink 和事件 ID。
- 合并导入不得假定两个实例的同值 ID 表示同一对象；冲突必须产生显式映射和导入证据。
- 归档对象仍可解析，并按当前权限显示其历史状态；已删除或不可访问对象不能通过引用泄漏旧标题或摘要。
- 数据库主键可以与 EntityID 相同或分离，但数据库表名、分区和行号不得进入公共引用。

### 类型注册与解析

每个 EntityType 必须注册：

- 类型名和允许的 ID 前缀；
- 所属领域模块；
- 读取授权所需的最小属性加载器；
- 安全展示信息加载器；
- 可作为 EntityLink 起点或终点的关系规则；
- 进入 Nexus View 和 Activity 的投影规则。

注册表只统一解析，不提供通用写接口。创建、修改和状态转换仍由原领域模块验证业务不变量。未注册类型、前缀不匹配、Workspace 不匹配和不允许的关系方向必须失败，不能降级成普通 URL 或无类型 metadata。

## 授权解析

### 统一入口

服务端所有引用消费者都调用同一授权决策入口，至少传入：

```text
principal
workspace_id
entity_ref
capability
request_context
```

`capability` 是对象模块声明的具体能力，例如 `discover`、`read`、`link-from`、`link-to` 或 `decision.accept`。UI 是否展示按钮不参与授权结论。

授权解析可以在受信服务端读取做决策所需的最小权限属性，但在 `read` 通过前不能加载或返回目标标题、摘要、参与者、正文、外部 URL 等展示内容。

### 投影结果

关系、Nexus View 和 Activity 使用三种结果：

| 结果 | 使用条件 | 可返回内容 |
| --- | --- | --- |
| `visible` | 当前主体可以读取关系和目标 | 经过目标模块安全展示器生成的字段 |
| `restricted` | 当前对象允许显示存在一条不可展开的受限关系 | 仅固定文案和 `restricted: true` |
| `hidden` | 关系本身不可发现，或上下文不允许占位 | 完全省略 |

默认受限占位符不得包含目标 EntityRef、类型、标题、摘要、参与者、时间、来源系统或外部 URL，也不能区分“无权访问”“已删除”和“暂不可用”。关系类型只有在原对象模块明确证明不会泄漏敏感语义时才可以显示；默认不显示。

搜索、通知、全局 Activity、Attention Item、导出和 AI 上下文没有一个用户正在读取的安全来源对象，因此对不可读目标使用 `hidden`，不能用受限占位符扩大可发现范围。

### Component 与 CI Run 的 M0 读取

- Component 当前没有 restricted 可见性或对象成员字段；同一 Workspace 的活跃成员可以读取，非成员、暂停成员和跨 Workspace 主体不可发现。
- CI Run 的读取能力来自其稳定 `component_id` 归属；只有当前主体能读取 Component 时才可以读取 CI Run。owner Team 只表达责任，EntityLink、Project 和 Jenkins source 都不授予该读取能力。
- CI Run Current 只投影 status、开始/完成/记录/更新时间和经当前权限解析的 Component；不返回 source ID、external run key、delivery receipt、digest、Secret、原始 payload 或未经治理的外部 URL。
- `ci-run.recorded` Timeline 可以显示通用 `plugin` actor kind，但在来源展示协议冻结前隐藏 plugin/source ID。精确边界见 [ADR-0007](../adr/0007-component-scoped-ci-run-read.md)。

### Environment 与 staging Deployment 的 M0 写入

- Environment 是 Workspace 级稳定部署目标；classification 创建后不可修改。owner Team 表示责任，不授予部署能力。
- staging Deployment 只由明确用户的独立 command 记录。调用者必须是 active Workspace 成员，并持有目标 Environment 的 active 显式授权；Project 角色、Component、EntityLink、CI source 或 plugin actor 均不授予该能力。
- 目标必须是 active staging Environment，来源必须是 succeeded CI Run。CI Run 写入路径不调用 Deployment，production 也不能通过改名或普通参数进入该 command。
- Deployment 是不可变终态事实，同一 Environment 与 CI Run 组合唯一；它保留 authorization、actor、source 和时间，但不冒充外部执行日志或通用 Audit。
- 成功命令原子写入 Deployment、asserted user `deploys` 关系、`deployment.recorded` 和 Outbox。事件只保留 status、Environment 与 CI Run 引用。精确边界见 [ADR-0009](../adr/0009-explicit-staging-deployment.md)。

### EntityLink 写入

创建 EntityLink 必须同时满足：

1. 调用主体可以读取起点和终点，或拥有对应模块显式授予的受控引用能力；
2. 分别通过 `link-from` 和 `link-to`；
3. 关系注册表允许该类型方向和 `relation_type`；
4. 插件 scope、导入权限或系统规则允许声明对应 `origin` 与 `assertion`；
5. 操作写入审计，并与 EntityLink 及领域事件保持事务一致。

能够读取起点不自动获得终点的 `read` 或 `link-to`。插件也不能以 `derived` 为名绕过两端权限、Workspace 或关系类型校验。

### 不存在与无权限

直接解析未知或不可发现 EntityRef 时，外部 API 默认返回不可区分的“未找到”结果。只有用户已获准读取来源对象，并且来源模块明确允许显示受限关系时，才返回上述通用占位符。

权限缓存必须至少按 Workspace、主体和能力隔离。成员、角色、对象可见性、关系任一端权限或插件 scope 变化后，相关缓存、搜索、通知和 Activity 投影必须失效或在读取时重新判定。

## 领域事件 envelope

### 规范结构

```json
{
  "event_id": "evt_01K4EXAMPLE",
  "event_type": "decision.accepted",
  "schema_version": 1,
  "workspace_id": "wrk_01K4EXAMPLE",
  "actor": {
    "kind": "user",
    "id": "usr_01K4EXAMPLE"
  },
  "source": {
    "kind": "web",
    "id": null
  },
  "primary_entity": {
    "type": "decision",
    "id": "dec_01K4EXAMPLE"
  },
  "project_ref": {
    "type": "project",
    "id": "prj_01K4EXAMPLE"
  },
  "correlation_id": "cor_01K4EXAMPLE",
  "causation_id": "evt_01K4PREVIOUS",
  "occurred_at": "2026-08-28T08:00:00.000Z",
  "payload": {}
}
```

字段规则：

- `event_id` 全实例唯一、不可变，是消费者幂等键；
- `event_type` 使用 `<domain>.<past-tense-fact>`，版本只放在 `schema_version`；
- `workspace_id` 必填，事件中的所有 EntityRef 必须属于该 Workspace；
- `actor` 表示授权并导致变化的主体，`source` 表示 `web / api / plugin / system / import` 等入口，两者不能互相替代；
- `primary_entity` 是变化的主要业务对象，不表示事件只能投影到一个对象；
- `project_ref` 只在存在真实 Project 上下文时填写，不作为权限继承依据；
- 根操作创建新的 `correlation_id` 且不填写 `causation_id`；后续事件沿用 correlation 并指向直接原因事件；
- `occurred_at` 使用 UTC 和固定精度，表示权威业务变化发生时间；
- `payload` 由 `event_type + schema_version` 定义，只包含重建投影所需的最小事实。

通用 envelope 和 payload 都禁止写入 Secret、Token、完整消息正文、完整文档、未脱敏外部响应或仅为 UI 方便而复制的标题快照。消费者需要展示内容时，通过 EntityRef 和当前主体权限读取原对象。

### 事务与 Outbox

一次成功业务变化必须在同一 PostgreSQL 事务内写入：

1. 权威业务状态；
2. 不可变领域事件事实；
3. 需要异步消费时的 Outbox 投递意图。

领域事件事实与投递状态在逻辑上分离：事件内容不可修改，投递尝试、租约、下次重试时间和死信状态可以变化。物理实现可以是一张表或多张表，但不能因清理已投递 Outbox 而失去 Activity 重建和备份恢复所需的事件事实。

外部网络调用不得发生在业务数据库事务中。消费者按 `event_id` 幂等处理；重复、乱序和进程崩溃后的再次投递都属于正常输入。M0 仍以业务表保存当前状态，不采用 Event Sourcing。

外部 Webhook 的 delivery ID 是来源作用域内接收命令的幂等身份；只有首次有效处理才创建 CI Run 和领域事件。它不能替代平台 `event_id`，也不能直接成为 EntityID。同一 Workspace、source 与 delivery ID 的 payload digest 必须一致；不一致时 fail closed，不能覆盖 receipt 或把冲突伪装成成功重复。已验证 Jenkins delivery 的精确事务与停止线见 ADR-0006。

## Activity 投影

### 职责

Activity 解释“发生了什么”，不接受普通业务写入，也不反向驱动 Decision、Ticket、CI Run 或 Deployment 状态。投影器只消费显式允许的领域事件类型，并为相关实体生成时间线项。

最小投影字段：

```text
workspace_id
target_ref
event_id
activity_type
actor_ref
occurred_at
subject_refs
projection_version
safe_facts
```

`safe_facts` 只能保存事件中允许长期保留的非敏感事实，例如状态从 `proposed` 变为 `accepted`；不得保存目标标题、消息正文、Secret、插件原始响应或预先展开的权限敏感摘要。

### 可重建与幂等

- 投影唯一键至少包含 `projection_version + event_id + target_ref`；重复消费不得生成重复时间线项。
- 重建读取保留的领域事件事实和必要的权威历史记录，不读取旧 Activity 作为来源。
- 投影版本变化时写入新版本或原子替换完整目标范围，不能向用户暴露半新半旧时间线。
- 删除 Activity 缓存不影响业务事实、审计或 Outbox 投递状态。
- 备份必须覆盖业务状态、稳定 ID、EntityLink、重建所需事件事实和审计；恢复后重新投影应得到等价关系与时间线。

### 读取过滤

查询时间线时先确认 `target_ref` 可读，再对每个 `subject_ref` 使用当前权限重新判定。权限已撤销时，旧投影中的引用不能继续暴露缓存标题或摘要。对象页可以按授权解析规则显示通用受限占位符；全局 Activity、通知、搜索和导出则隐藏不可读项或敏感关联。

## Outbox、Activity 与 Audit 分工

| 记录 | 主要用途 | 是否权威业务状态 | 是否可重建 | 主要读取者 |
| --- | --- | --- | --- | --- |
| 业务记录 | 保存对象当前状态与领域历史 | 是 | 否 | 原领域模块 |
| 领域事件事实 | 描述已发生的业务变化 | 对变化事实权威 | 否；由业务事务原子产生 | 投影器和受控消费者 |
| Outbox 投递状态 | 可靠异步交付和失败恢复 | 否 | 可从待投递事件重新建立 | 后台任务与运维 |
| Activity | 用户可见、权限过滤的时间线 | 否 | 是 | 产品 UI 和 API |
| Audit | 安全操作、授权判断和高风险证据 | 对审计证据权威 | 否；按审计策略保留 | 管理员和安全流程 |

一项操作可以同时产生领域事件、Activity 和 Audit，但它们的 payload、保留周期和读取权限分别定义，不能通过复制同一 JSON 假装职责相同。

## Golden Path 契约走查

1. 用户从私密 Thread 创建 Proposed Decision。Decision 与 `derived-from` EntityLink 在同一事务写入；该关系是 `asserted + user`，因为 `derived-from` 是业务语义，不代表自动推导。
2. `decision.proposed` 和 `entity-link.created` 共享 correlation，分别投影到 Decision 和 Thread；Decision 草案必须保留 evidence 引用。
3. 有确认权限且能读取全部 evidence 的人接受 Decision，产生 `decision.accepted`。Project 管理角色不自动穿透 restricted Thread；系统生成内容只能保留为草案，不能作为 actor 完成接受。
4. 从 Decision 创建 Ticket，Ticket 与 `implements` 关系保留来源，不复制 Thread 正文。读取 Ticket 但不能读取 Thread 的用户只在对象页看到不可识别目标的通用受限占位。
5. Jenkins 重复发送同一 delivery 时只产生一个 CI Run；相同幂等键的 digest 变化直接冲突。正式核心只接收已经完成来源验证与字段映射的 delivery，并把 receipt、CI Run、`ci-run.recorded` 和 Outbox 原子提交；外部失败重试和安全审计由后续 adapter 定义，不阻塞聊天和 Decision 写入。
6. 构建成功只更新 CI Run。只有 active 用户持有目标 Environment 的显式授权后，才能通过独立 command 原子记录 staging Deployment、`deploys` 关系、`deployment.recorded` 和 Outbox；该 command 不执行外部部署。
7. 备份恢复保留所有稳定 ID、关系来源、事件 correlation 和审计；Activity 可以重新投影且不产生重复项。

## M0 验证清单

M0 实验与正式纵向切片累计必须证明：

- 重命名 Project 或 Component 后旧 EntityRef 仍可解析；
- 不同 Workspace 的两端不能创建 EntityLink；
- 公开 Ticket 关联私密 Thread 时不泄漏 Thread 的类型、ID、标题、摘要和参与者；
- 权限撤销后 Nexus View、Activity、搜索和通知不继续返回缓存内容；
- 重复事件和重复 Jenkins delivery 不产生重复 EntityLink、CI Run 或 Activity；
- Activity 清空并重建后，顺序、来源和目标关系与重建前等价；
- 清理已完成的投递状态不会破坏事件事实、备份或 Activity 重建；
- CI Run 成功不会自动产生 Deployment。
- 无授权用户、非 staging Environment 和非成功 CI Run 不能产生 Deployment；事件或关系失败时整单回滚。

## 后续仍需决定

- EntityID 的具体生成算法，以及 Document、Repository 等尚未进入正式纵向切片的类型前缀；
- 后续对象的 PostgreSQL 表、约束、索引，以及事件事实和投递状态的保留与演进策略；
- 关系类型注册表的完整方向、基数和 metadata schema；
- Team 角色继承、对象分享、跨 Project 转换和管理员 break-glass 策略；
- 领域事件保留、压缩和 projection version 迁移策略；
- HTTP/OpenAPI 的错误对象、游标和并发控制字段。

这些事项必须通过后续纵向切片验证，不能由 Web 框架或 ORM 默认行为替项目作出决定。
