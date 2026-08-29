# RadishNexus 决策基线

状态：已确认的初始产品决策

日期：2026-08-29

本文件用于防止后续讨论静默改变当前方向。修改“已确认”事项时，必须同时记录修改日期、原因、影响和迁移方式。

## 已确认

### D-001 项目名称

- 项目名称为 `RadishNexus`。
- `Nexus` 表达消息、工单、文档和交付对象的连接枢纽。
- 不使用 `RadishLink`，该名称已属于离线自组网通信项目。

### D-002 产品形态

- 第一产品形态是 Web App。
- 前期不开发移动端和 PC 安装客户端。
- Web 产品基本完善并满足独立阶段门槛后，再评估客户端实施。

### D-003 客户端技术

- 后续移动端和 PC 端统一采用 Flutter。
- 不采用 Tauri。
- 客户端应尽量复用 API、协议和设计系统，不要求与 React Web 共享页面实现。

### D-004 插件化原则

- 必须设计插件系统，但不为了“插件化”而把每项功能开发成插件。
- 外部系统集成、可选行业能力和独立演进功能优先评估插件。
- 高频核心链路、强事务能力和深度依赖平台数据的能力优先内建。
- 插件基础设施按真实插件需求逐步生长，第一个验证对象是 Jenkins 集成。

### D-005 代码与授权

- 核心代码归项目所有者所有，采用 source-available 模式，不作为开放源码发布。
- 用户获得单独书面授权后可以免费部署和使用。
- 免费授权不按成员、席位、频道、项目或插件数量计费和限额。
- Plugin SDK、公共协议和插件开放源码；每个开放组件必须携带独立许可证。
- 根许可证以 Radish 家族既有的 source-available 边界为参考，并增加免费书面授权和独立开放组件边界；正式权利义务只以本仓库许可证和单独书面授权为准。

### D-006 技术栈

- 服务端以 Go 为主要业务语言。
- Rust 只在离线同步、协同内核、WASM、索引、加密、跨端复用或明确性能收益场景中采用。
- Web 前端采用 React + TypeScript。
- 后续客户端采用 Flutter。
- 初期采用模块化单体，不以微服务数量作为架构成熟度指标。

### D-007 无席位限制

- 产品授权和功能开关不得以用户数量为基础。
- 系统管理员可以配置存储、附件、保留期、并发和 API 速率等运维配额。
- 运维配额用于保护部署资源，不能伪装成商业席位限制。

### D-008 初始功能主线

- 私聊和频道；
- Decision 与待决策事项；
- 工单；
- 在线协作文档、历史和后续离线能力；
- Component、Repository、Environment、Jenkins 与 CI/CD 支持；
- 自部署、升级、备份和恢复；
- 插件 SDK 和受控扩展能力。

### D-009 Decision 是一等对象

- Decision 不是置顶消息、文档标题或 Ticket 字段的别名。
- Decision 必须保存问题、状态、结论、理由、备选方案、证据、决策人和决定时间。
- Message 或 Thread 可以生成 Decision 草案，但只有有权限的人能够确认 Accepted。
- Decision 必须支持 Rejected、Superseded 和替代链路，避免历史结论被静默覆盖。

### D-010 区分协作、目标和软件资产

- Project 只承担协作导航和权限边界，不作为代码仓库、软件服务或阶段性目标的同义词。
- Initiative 表达具有目标、负责人、健康状态和结束条件的阶段性工作。
- Component 表达长期维护的服务、网站、客户端、库或其他软件资产。
- Repository 表达外部代码库映射，RadishNexus 不自建 Git 托管。
- Environment 表达稳定部署目标；CI Run 与 Deployment 是不同事实。
- 首期只建设完成 Golden Path 所需的最小字段和关系，不建设完整内部开发者门户。

### D-011 先验证 Golden Path

- 在横向补全聊天、工单、文档和插件生态前，先完成最薄纵向原型。
- Golden Path 必须覆盖“讨论 → Decision → Ticket → CI Run → Deployment → 时间线”。
- 原型可以使用极简聊天、工单和 Markdown 文档，不以单点模块完备度作为成功标准。
- Golden Path 的目标是验证产品差异化和领域边界，不提前承诺其原型代码直接进入生产。

### D-012 关系与时间线是一等基础能力

- 跨模块关联使用带来源、创建者和时间的 EntityLink，不只保存正文 URL 或临时外键。
- 用户建立、系统推导、插件写入和导入恢复的关系必须可区分。
- Activity 是领域事件的权限过滤投影，可以重建，不作为另一套权威业务数据。
- 引用、反向链接、时间线、通知、搜索和 AI 扩展不得绕过目标对象权限。
- 首期使用 PostgreSQL 实现关系和查询，不引入图数据库。

### D-013 首批 M0 核心字段边界

- Project、Initiative、Component、Decision、Environment 和 EntityLink 已冻结首批最小业务字段与状态不变量，精确含义以[领域模型](domain-model.md)为准。
- 六类对象都显式归属 Workspace，并使用创建后不可变、不复用的稳定 ID；名称、key、URL 和数据库行号不构成身份。
- Project 的默认可见性不能放宽其内部私密对象；Environment 独立于 Component，production 分类不能因 UI 简化而失去保护语义。
- Decision 的 Accepted、Rejected 和 Superseded 状态都必须保留相应主体、理由、时间与证据，自动提炼不能直接确认 Decision。
- EntityLink 分开记录 asserted / derived 事实强度与 user / system / plugin / import 来源，移除关系不能删除旧来源证据。
- 首批 ID 前缀、引用序列化、授权结果和事件持久化边界已由 ADR-0002 接受；其余类型前缀和物理 schema 仍需原型验证。

### D-014 Go 服务端基础栈

- 首期正式服务使用单一 `server/` Go module，并保持模块化单体。
- HTTP server、路由和测试使用 Go 标准库；当前不引入 Web 框架。
- PostgreSQL 使用原生 `pgx/v5` 与 `pgxpool`，SQL 手写并版本化；当前不引入 ORM 或 query builder。
- 事务由 application service 显式控制，领域层不暴露驱动类型或 SQL 错误字符串。
- SQL 代码生成和完整服务目录层级仍需在真实纵向切片中收窄决定。

### D-015 首个协作权限切片

- Thread、Decision 和 Ticket 各自保存不可变的 governing Project 授权上下文；业务关系仍由 EntityLink 表达。
- Thread 与 Ticket 的首批类型前缀分别为 `thr_` 和 `tkt_`，具体 ID 生成算法仍未冻结。
- Project 首批角色为 `viewer / contributor / decider / admin`；只有 `decider` 和 `admin` 可以 Accepted Decision。
- restricted Thread 需要显式 Thread 成员权限，Project 角色不会自动穿透；无权限关系目标只显示不含标识和展示字段的通用占位。
- 认证协议仍未冻结，application service 只接收认证 adapter 提供的显式 Principal。

### D-016 PostgreSQL migration 基线

- migration 使用连续编号、SHA-256 artifact 校验、session advisory lock 和单 migration 事务。
- schema 变更通过独立命令显式执行，不成为服务副本启动的隐藏副作用。
- 正式 runner 只向前推进；自动 down SQL 不作为生产回滚承诺，已提交升级依赖备份恢复或经过验证的 forward repair。
- 当前不为未出现的模板、在线 DDL 或多数据库需求引入额外 migration 依赖。

### D-017 CI Run 与 staging Deployment 边界

- `ci-run` / `cir_` 与 `deployment` / `dpl_` 是不同的稳定业务身份；构建成功不自动创建 Deployment，也不证明外部部署已经发生。
- Jenkins 核心只接收已完成来源认证、重放校验和字段映射的 verified delivery，并原子记录完成态 CI Run、幂等 receipt、领域事件和 Outbox；不保存 Secret 或原始 webhook body。
- CI Run 的 M0 用户读取沿 active Workspace 成员 → Component → CI Run 解析；owner Team、Project、EntityLink 和 Jenkins source 都不授予读取权，不可读对象返回 not-found。
- M0 staging Deployment 只由明确用户通过受控 `web / api` 调用记录；调用者必须是 active Workspace 成员，并持有目标 active staging Environment 的 active 显式授权。
- Project 角色、owner Team、CI source 和成功构建都不隐式授予部署能力。记录必须保留实际操作者、所用授权、来源 CI Run 与 Environment，并与 `deploys` 关系、领域事件和 Outbox 原子提交。
- 当前 Deployment command 只记录调用方已经确认的外部终态事实，不执行部署、不读取 Secret，也不支持 production、审批、回滚或运行中状态。精确技术契约以 [ADR-0006](adr/0006-verified-jenkins-delivery-and-ci-run.md)、[ADR-0007](adr/0007-component-scoped-ci-run-read.md) 与 [ADR-0009](adr/0009-explicit-staging-deployment.md) 为准。

## 尚未冻结

以下事项仍需在实现前通过原型或 ADR 决定：

- SQL 代码生成；
- 文档编辑器与 CRDT 具体技术；
- 插件后端首版采用 WASM、独立进程还是只提供声明式自动化；
- SDK 和官方插件统一采用 Apache-2.0、MIT 或按组件选择；
- 搜索从 PostgreSQL 迁移到独立搜索服务的阈值；
- Flutter 客户端具体启动门槛；
- 免费书面授权的申请、签发、期限和撤销模板；
- 免费评估或社区授权如何做到低摩擦、可离线验证且不依赖遥测或官方在线服务；
- 产品域名、Logo 和中文品牌名；
- 是否以及何时引入 AI 能力。

## 变更记录

- 2026-08-27：建立初始决策基线。
- 2026-08-27：确认 Decision、研发资产分层、Golden Path 以及 EntityLink/Activity 基线。
- 2026-08-28：冻结首批 M0 核心对象字段与不变量，并接受 ADR-0002 的稳定引用、授权与事件投影边界。
- 2026-08-28：接受 ADR-0003，冻结标准库 HTTP、原生 `pgx/v5` 与手写版本化 SQL 的首期服务端基线。
- 2026-08-28：接受 ADR-0004，冻结 Thread → Decision → Ticket 的 Project 作用域、最小角色和私密关系投影边界。
- 2026-08-28：接受 ADR-0005，冻结显式、可校验、事务化的 forward-only PostgreSQL migration 基线。
- 2026-08-29：冻结完成态 CI Run 的 verified delivery、Component 作用域读取，以及显式 staging Deployment 与环境级授权边界。
