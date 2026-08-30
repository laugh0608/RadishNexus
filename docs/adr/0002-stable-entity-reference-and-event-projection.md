# ADR-0002：稳定实体引用与事件投影边界

状态：已接受

日期：2026-08-28

## 背景

RadishNexus 的 Golden Path 要把 Thread、Decision、Ticket、Document、Component、CI Run、Environment 和 Deployment 放进同一条可追溯上下文。若各模块直接保存彼此表主键、URL 或标题快照，重命名、权限撤销、插件导入和备份恢复会产生不一致；若把关系或时间线建设成万能数据层，又会绕过 Decision、Deployment 等原领域模块的不变量。

系统还需要同时满足三个容易混淆的职责：可靠投递业务变化、向用户解释时间线、保留高风险操作证据。直接把 Transactional Outbox、Activity 和 Audit 合并为一张通用事件表，会让投递清理、权限过滤、保留周期和重建边界互相牵制。

因此在选择 Go Web 框架和数据库访问库前，需要先确定跨模块稳定引用、授权解析和事件投影的最小边界。

## 决定

采用[核心实体、授权与事件契约](../architecture/core-contracts.md)作为 M0/M0.5 实现基线，并遵守以下决定。

### 稳定 EntityRef

- 跨模块身份使用平台注册的 `EntityType + EntityID`，文本形式为 `entity://<type>/<id>`。
- EntityID 不透明、不可复用，不编码 Workspace、权限、存储位置或可变名称。
- `workspace_id` 作为解析的独立可信上下文；M0 不允许跨 Workspace EntityLink。
- 类型注册表负责解析、最小授权属性和安全展示，不提供绕过领域模块的通用写接口。
- 普通备份恢复保留 ID；合并导入冲突必须显式映射。

### 关系不授予权限

- 引用消费者统一调用服务端对象授权入口，UI 隐藏不构成权限控制。
- 创建 EntityLink 同时检查两端能力、关系类型、Workspace 和调用来源权限。
- 用户已获准读取来源对象时，可以按来源模块规则看到不可识别目标的通用受限占位；搜索、通知、全局时间线、导出和 AI 上下文默认隐藏不可读目标。
- 受限占位不包含目标类型、ID、标题、摘要、参与者、时间或外部来源，也不能区分无权访问、删除和暂不可用。
- 权限和插件 scope 变化必须使缓存及派生读取结果失效或重新判定。

### 领域事件与 Outbox

- 成功业务变化在一个 PostgreSQL 事务中写入权威状态、不可变领域事件事实和必要的 Outbox 投递意图。
- 事件事实与投递状态逻辑分离；投递状态可以重试和清理，事件事实必须满足 Activity 重建与备份恢复要求。
- 事件 envelope 统一携带 event type、schema version、Workspace、actor、source、primary entity、correlation、causation 和 UTC occurred time。
- 消费者按 event ID 幂等；外部 Webhook delivery ID 只负责入口去重，不能替代平台事件或实体身份。
- M0 仍以业务记录保存当前状态，不采用 Event Sourcing，也不引入独立消息中间件。

### Activity 与 Audit

- Activity 由显式允许的领域事件投影，不提供普通业务写接口，也不作为状态转换来源。
- Activity 只保存引用和允许长期保留的最小安全事实；显示时按当前权限重新解析，不能依赖旧标题或摘要快照。
- 投影按 projection version、event ID 和 target ref 幂等，并能从保留的事件事实和权威历史重建。
- Audit 独立保存安全操作、授权判断和高风险证据，不因 Activity 重建或 Outbox 清理而丢失。

## 未采用的方案

### 各模块直接保存对方表主键

数据库外键局部清晰，但会把模块存储结构变成公共协议，并促使每个模块各自实现权限、反向链接和删除处理。真正的模块内强关系仍可使用外键，跨模块公开身份不采用该方案。

### 名称、slug 或 URL 作为稳定引用

实现简单，但重命名、迁移 provider、改变路由或恢复实例都会破坏追溯，且 URL 容易意外携带租户和部署信息。

### 建立一张万能 Entity 表并允许通用写入

可以统一 CRUD，却会削弱 Decision 确认、Environment 保护和 Deployment 审批等领域不变量，并形成所有模块共享的高耦合写入口。

### EntityLink 自动继承或授予目标权限

使用方便，但公开 Ticket 指向私密 Thread 时会穿透权限边界，也无法安全支持搜索、通知、导出和 AI 上下文。

### 直接把 Outbox 当作 Activity

减少一层投影，但投递记录包含重试与运维状态，用户时间线需要权限过滤和产品语义；二者的 payload、读取者和生命周期不同。

### Activity 直接接受业务模块写入

短期省去事件建模，但会形成第二套不可重建的业务事实，难以处理事务一致性、重复投递、权限撤销和恢复。

## 后果

正面影响：

- 数据库、HTTP、WebSocket、插件、备份和 Nexus View 共享一种稳定身份语义；
- 跨对象引用不会因页面可见或插件写入而绕过原模块权限；
- 关系保留确认强度和具体来源，自动推导不会冒充人工事实；
- 投递失败、用户时间线和安全审计可以分别演进；
- Activity 可以重建，重复 Webhook 和事件不会产生重复投影；
- Go 框架和 ORM 选型可以基于真实契约实验，而不是反向塑造领域模型。

成本与风险：

- 需要维护类型、关系和投影注册表；
- 多态 EntityRef 无法只依赖普通数据库外键保证完整性，必须增加应用校验和集成测试；
- 查询 Nexus View 时需要批量授权与安全展示，错误缓存可能造成信息泄漏；
- 保留可重建事件事实增加存储和版本迁移责任；
- 通用占位、隐藏和完整显示三种结果必须在 API 和 UI 中一致实现；
- ID 算法、物理 schema 和授权策略仍不能被实现默认值提前冻结。

## 迁移与验证

当前仓库尚无产品数据库或 API，因此没有数据迁移。按以下顺序进入实现：

1. 为首批类型建立注册表、EntityRef parser 和跨 Workspace 拒绝测试；
2. 用最小 PostgreSQL schema 验证业务状态、事件事实和 Outbox 投递的单事务写入；
3. 实现 Decision 与 EntityLink 的领域不变量和权限矩阵集成测试；
4. 建立可清空、重放和版本化的 Activity 投影；
5. 用私密 Thread、重复 Jenkins delivery、权限撤销和备份恢复验证 Golden Path 失败路径；
6. 原型证明契约可行后，再通过单独 ADR 选择 Go Web 框架和数据库访问库。

本 ADR 已同步进入[ADR 索引](README.md)和[当前状态](../status/current.md)。若原型证明稳定引用、权限占位或事件持久化边界不可行，应在实现扩张前提出替代 ADR，不能静默改写本记录。
