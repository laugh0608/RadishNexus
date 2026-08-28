# ADR-0004：Project 作用域下的协作对象与权限

状态：已接受

日期：2026-08-28

## 背景

Golden Path 的首段要求从 Project 内的 Thread 创建 Decision，再从 Accepted Decision 创建 Ticket。Project 已定义为协作导航与默认权限边界，私密 Thread 又必须保持更窄的对象权限；如果三类对象没有明确的 Project 授权上下文，服务端只能依赖 EntityLink 反推权限，既容易形成循环，也无法在创建关系前可靠授权。

Project 不能因此退化成所有领域对象的数据库父节点。Initiative、Component、Repository、Environment 等对象仍保持独立领域语义，并通过 EntityLink 与协作对象关联。

## 决定

### Governing Project

- Thread、Decision 和 Ticket 各自记录创建后不可变的 `governing_project_id`，并与对象 `workspace_id` 属于同一 Workspace。
- `governing_project_id` 只提供协作导航、授权和审计上下文，不表达业务上的组成、实现、证据或所有权关系。
- Thread → Decision 和 Decision → Ticket 的来源与实现语义继续分别使用 `derived-from` 和 `implements` EntityLink；不能用相同 `project_id` 代替关系事实。
- M0 的两个转换动作只允许在同一 governing Project 内进行。未来跨 Project 转换必须另行定义权限、展示和审计语义。

### 首批对象契约

- Thread 使用 `thread` 类型与 `thr_` ID 前缀；最小字段为 `title`、`visibility`、`governing_project_id`。`visibility` 为 `project / restricted`。
- Ticket 使用 `ticket` 类型与 `tkt_` ID 前缀；最小字段为 `title`、`status`、`governing_project_id`。`status` 为 `open / in-progress / done / canceled`。
- Decision 保持 ADR-0002 已接受的 `decision` 类型与 `dec_` 前缀，并增加 `governing_project_id` 授权上下文；其业务状态和已冻结字段不变。
- ID 的具体生成算法仍不属于公共合同；前缀只用于类型校验和诊断，不携带权限。

### 最小角色与能力

Project 首批角色为：

| 角色 | 读取 Project 对象 | 创建 Decision / Ticket | Accepted Decision |
| --- | --- | --- | --- |
| `viewer` | 是 | 否 | 否 |
| `contributor` | 是 | 是 | 否 |
| `decider` | 是 | 是 | 是 |
| `admin` | 是 | 是 | 是 |

- `workspace` 可见的 Project 对活跃 Workspace 成员提供只读基线；写入仍需要 Project 角色。
- `restricted` Project 只允许显式 Project 成员读取。
- `restricted` Thread 在 Project 读取权限之上还要求显式 Thread 成员；Project `admin` 不在 M0 自动绕过该对象边界。
- application service 接收由未来认证 adapter 构造的显式 Principal；本 ADR 不选择本地账号、OIDC、Session 或 Token 协议。

### 关系投影

- 创建 Decision 前必须能读取 evidence Thread；确认 Decision 前必须能读取其全部 evidence；创建 Ticket 前必须能读取并验证 Accepted Decision。
- 创建 EntityLink 不授予任何一端权限，也不扩大 governing Project 权限。
- 已获准读取来源对象但无权读取目标对象时，关系投影只返回通用 `restricted` 占位；不返回目标 EntityRef、类型、标题、摘要或时间。
- 直接读取未知或不可发现对象仍返回不可区分的 not-found 结果。

## 未采用的方案

### 从 EntityLink 反推授权 Project

对象在第一条关系建立前没有可判定作用域，多条关系还可能形成冲突。EntityLink 是业务关系事实，不应兼任隐式授权继承图。

### Project 成员自动读取所有私密 Thread

实现简单，但会违反“Project 可见性不能放宽内部私密对象”的既有红线。

### 本轮冻结完整 RBAC

Golden Path 只证明四种 Project 角色与一个 Thread 对象边界。现在引入自定义角色、Team 继承、条件策略或通用策略语言会扩大实现面，且没有真实用例验证。

## 后果

正面影响：

- 权限查询有稳定、可索引且不依赖关系遍历的上下文；
- Thread 的窄权限不会因转换为 Decision 或 Ticket 而泄漏；
- EntityLink 保持业务关系和来源证据职责，不成为授权继承机制；
- 未来认证 adapter 可以替换，而不改变 application service 的主体输入。

成本与风险：

- 协作对象移动到另一 Project 不能直接修改 `governing_project_id`，需要未来显式迁移动作；
- `admin` 是否应通过 break-glass 读取私密 Thread 仍需安全与审计设计；
- Team 角色继承、对象分享和跨 Project 转换仍是后续权限扩展点。

## 迁移与验证

当前没有已部署的生产实例或存量数据库，因此不存在存量迁移。首个正式 schema 已直接建立约束，并验证：

1. Thread、Decision、Ticket 与 governing Project 必须处于同一 Workspace；
2. contributor 可以创建 Decision 和 Ticket，但不能 Accepted Decision；
3. decider 只有在能读取全部 evidence 时才可以人工确认满足字段不变量的 Decision；
4. 关系、领域事件和 Outbox 与业务状态在同一事务提交；
5. 无权读取 restricted Thread 的用户只能得到不含目标标识和展示字段的占位。

已实际执行 `./scripts/check-server.sh` 与 `./scripts/check-server-postgres.sh`；单元测试、真实 PostgreSQL migration、角色拒绝、管理员不穿透、事务回滚和关系投影均通过。

若后续需要可变 Project 归属、权限继承图、跨 Project 转换或管理员越权读取，应以新 ADR 替代对应边界。
