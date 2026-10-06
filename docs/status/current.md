# RadishNexus 当前状态

状态日期：2026-10-06

## 当前阶段

M0.5 Golden Path / M1 Web 平台基础纵向原型。正式 Go、PostgreSQL 和 React Web 已建立若干真实业务切片，尚未完成可由普通成员独立操作、持续使用的完整 Golden Path。

本地邮箱账户与邀请已按 [ADR-0023](../adr/0023-local-account-and-radish-oidc-login.md) 建立。项目所有者已明确将 Radish OIDC 接入延后，schema readiness 和 Project / Channel 首批发现入口已完成，基础对象与首批成员配置已接通，部署后首次访问初始化也已接通。最小 Markdown Document 已按 ADR-0028 接入正式 Go、PostgreSQL 与 Web，自动化和隔离 HTTPS 浏览器已验证创建、保存、冲突、恢复与撤权；正式页面的 macOS 原生中文 IME 尚未验收，项目所有者现已明确暂缓人工验收，并明确视觉语言主要参考 RadishX 的 Radish 家族规范、页面重点参考 AFFiNE / Mattermost / AppFlowy；已确认使用 pen.dev 推进共享工作台与 Document 代表页，静态稿已按实际界面参考优化至 v1.1，并获所有者认可；共享工作台与 Document 代表页现已落到正式 React，验证范围见[视觉落地记录](reviews/2026-09-26-workbench-ui.md)。长期范围与阶段退出条件见[路线图](../roadmap.md)，产品验收见 [Golden Path](../golden-path.md)。本页只维护当前判断和顺序，不重复完整 ADR 与历史测试流水。

## 完成线与成熟度

下表依据仓库实现及已有验收记录区分四类证据；公共入口或浏览器演示通过不等于真实团队日常使用通过。

| 能力 | 内部契约与实现 | 公共入口与交互 | 普通用户独立操作 / 持续使用 |
| --- | --- | --- | --- |
| 本地身份与 Session | 账户 / 密码拆分、邮箱 bootstrap、旧账户显式映射、Session、CSRF、邀请和外部身份事务 | 同源 HTTPS 邮箱登录、Workspace 选择、账户页和一次性邀请；真实 OIDC provider 尚未装配 | 本地与迁移自动化、邮箱登录与退出浏览器已有证据；新账户邀请兑换浏览器已有证据；Radish 联调、恢复入口、完整成员治理与团队使用仍待验收 |
| Message → Thread → Decision → Ticket | migration 006 / 007、权限、来源、原子事件与幂等 receipt | canonical 页面和短请求；contributor / decider 浏览器验收已有记录 | 自动 Timeline 与首批双向关系已通过浏览器闭环；独立协作对象列表与基础执行管理未闭环 |
| Project / Channel 发现 | 当前权限过滤、归档可读、稳定 ID 分页、当前成员复核 | 首页 Workspace → Project → Channel 导航；ID 工具保留为次级入口 | 服务端、真实数据库、Web 交互与隔离浏览器验收通过，含分页、撤权刷新、Workspace 切换及桌面 / 手机布局；基础配置已有独立切片证据，见下一行 |
| 基础对象与首批成员配置 | migration 009、明确初始 admin、普通角色 / 受限成员、配置 Audit / receipt、撤权清理、备份恢复 | owner 创建 Team / Project，Project admin 创建 Channel 与配置成员 | 从正式 bootstrap 的空业务工作区，经 Web 创建、邀请新账户、双层授权、成员发消息与撤权已验收；首次访问 Web 初始化已有独立证据，管理员交接仍缺 |
| 单 Channel Message 实时 | 单进程 SSE、当前权限、有界回放、撤权与关闭 | canonical Channel 已接入 ready → history → 增量 | 有技术验收；没有目标团队规模与持续使用的容量证据 |
| CI Run / staging Deployment | verified Jenkins delivery、终态 CI Run、显式环境授权记录 Deployment、精确重试 receipt | CI Run / Deployment 正式读取；成功构建可选择已授权 staging 环境并确认记录外部部署结果 | 真实 Jenkins 三态采集、Deployment 写入自动化与隔离浏览器已通过；Component / staging Environment 创建、发现与显式授权管理已接通，真实数据库、恢复及隔离浏览器已有证据；交付关系和持续采集仍缺 |
| Repository 映射与 Component 关联 | migration 013、稳定外部身份、人工来源多对多关系、精确解除 / 重新关联、配置 Audit / receipt、Activity v4 | owner 创建映射与关联，成员分页发现及双向跳转 | 从正式 bootstrap 空业务工作区完成页面创建、关联、解除 / 重连、桌面 / 手机 / 键盘、成员只读及失权清空；数据库升级、并发、回滚与恢复通过；无 provider 连接或 CI 来源推导 |
| Ticket 与 Component 人工关联 | migration 014、人工 `affects`、Project 写权限、精确解除 / 重连、协作 receipt、Activity v5 | Ticket 关联区、组件稳定详情与按权限过滤的反向分页 | Go / PostgreSQL / Web、013 升级与恢复通过；正式页面创建链、关联 / 解除 / 重连、桌面 / 手机与键盘通过；浏览器权限撤回未完成，自动化已覆盖，见[实施记录](reviews/2026-10-06-ticket-component.md) |
| EntityLink / Activity | 带来源关系、权限过滤 query、同事务 Activity 更新与版本化全量重建 | Nexus View 可读 Current / 出向与首批入向 Relations / 正常更新的 Timeline | 正常写入与双向发现已通过真实 PostgreSQL / HTTP 和浏览器；未完成持续使用观察 |
| 自部署与恢复 | 显式 migration、只读 schema readiness、PostgreSQL 17 同 major 空目标恢复 | 健康端点拒绝 migration 缺失 / 漂移 / 版本不匹配；固定工件 Compose 开发拓扑与 HTTPS 演练已有记录 | 新探针已有真实数据库证据；本轮未重跑 Compose，升级失败恢复、运维与生产容量尚未完成 |
| Document | migration 010、不可变版本、精确 receipt、当前 Project 权限、单一受限 Markdown parser、备份恢复 | Ticket 创建、Project 文档列表、阅读 / 编辑 / 预览、显式冲突重新应用、历史恢复 | 真实数据库及隔离 HTTPS 双标签页、恢复、手机布局、撤权已有证据；原生中文 IME、普通成员独立持续使用仍待验收 |

## 已确认缺口

9 月 5 日的原始发现保留在[审阅记录](reviews/2026-09-05-project-review.md)。9 月 10 日已按 [ADR-0022](../adr/0022-transactional-activity-and-incoming-relations.md) 接通正常 Activity 更新及 Thread ← Decision、Decision ← Ticket 的反向发现；正式命令后的 HTTP 读取、撤权、失败回滚和重建并发已通过真实 PostgreSQL 测试，不在正常更新验收中手工重建。本轮 Chrome 浏览器已验证提案、接受、创建 Ticket、刷新后双向跳转与撤权清空，证据见[本轮记录](reviews/2026-09-10-activity-relations.md)。

当前剩余缺口：

1. **升级运维**：只读 schema readiness 已按 [ADR-0024](../adr/0024-read-only-schema-readiness.md) 完成；跨 schema 兼容窗口、升级失败恢复与生产编排仍待建立。
2. **使用入口**：首页已可直接浏览可读 Project / Channel；Team / Project / Channel 创建与首批成员配置已接通；部署后首次访问创建管理员已接通；管理权交接和其他对象列表仍缺。Document 的原生 IME 复核和真实外部交付链仍独立推进。
3. **关系与时间线范围**：核心 Nexus 关系仍全量读取，未建立通用分页；Repository / Component 配置和 Ticket / Component 关联已有专用双向分页和 active 反向索引。Timeline 保留事件主要对象语义，Thread 不展示后续对象 Activity，其余交付反向关系尚未开放。
4. **证据边界**：自动化、真实数据库与本轮浏览器验收证明讨论到执行的局部闭环；Document 的剩余人工复核、真实交付链、完整 Golden Path 和真实团队持续使用仍分别验收。

## 近期执行顺序

每个切片先核对既有合同和失败边界。本地账户调整已完成自动化验证；Radish OIDC 属于未来规划，不阻塞近期顺序，本轮不新增相关依赖。恢复该工作时再审查依赖、client registration、配置与联调范围；后续表项不自动获得依赖或远程操作授权。

| 顺序 | 下一切片 | 交付与退出判据 |
| --- | --- | --- |
| 1 | 交付链剩余关系与持续采集 | 在已完成的 CI Run 读取、Jenkins 隔离三态采集、staging 显式记录、Repository 映射 / Component 关联及 Ticket / Component 人工关联基础上，设计具体交付来源和持续采集；逐段移除对预置配置的依赖 |
| 暂缓 | 最小 Markdown Document 人工验收 | 正式实现和隔离浏览器已验证；待所有者恢复人工验收时补 macOS 原生中文 IME，见[实施记录](reviews/2026-09-26-markdown-document.md)，不阻塞上述切片 |
| 2 | 小团队场景试用 | 满足评估授权与必要运维条件后，以真实需求连续使用并记录追溯、重复录入和人工干预；按 Golden Path 验收决定下一批功能，不把一次演示当作持续使用完成 |

免费书面授权与评估说明应在邀请外部团队前准备，不能等到完整聊天或 CRDT 完成；版本化结构化导出与全新实例导入仍是 M1 的独立退出条件。账号恢复、安全审计、数据生命周期、数据库最小权限与升级演练按[路线图](../roadmap.md)的试用和生产边界推进，不因本表只列近期切片而取消。

### 当前接续：交付来源与持续终态采集

Ticket 与 Component 人工关联已按 ADR-0034 实现。下一段先设计持久终态采集的运行、来源配置、失败恢复和幂等边界，再明确 Ticket 与具体 CI Run / Deployment 的来源证据；同属一个 Component 不能推导已交付事实。真实 Jenkins → 新配置对象 → 浏览器记录 staging 的整链仍待验收。方案确认前不扩展公共协议、业务数据模型或外部运行状态。

### 明天事项：2026-10-07 接续建议

今天到此收尾，以下为下一次人工接续清单，不创建提醒或自动任务：

1. **先补收尾验收**：在最终工件上补 Ticket / Component 浏览器后退、Project 写权 / 读权收回、反向列表与待确认表单清理，补最终布局截图。启动临时服务时重新明确本次资源与清理范围；沿用现有自动化结果，不能把上轮 fixture 超时写成通过。
2. **主线先做设计**：核对 Jenkins finalized 快照、有限重试 sender 与受控 source 配置，提出持久终态采集最小方案，明确持久队列、重启恢复、去重 / 重放、失败可见性、凭据和生命周期。涉及新模型、协议、依赖或外部运行状态时先说明影响并确认范围，再实施。
3. **另列交付来源与整链验收**：明确 Repository / commit / CI Run 的来源证据，再决定 Ticket 如何关联具体交付；不按 Component 共属自动推导。规划“正式新配置对象 → 真实 Jenkins → 浏览器记录 staging → 撤权阻止新记录且历史仍可读”的整链。原生中文 IME 保持暂缓，真实团队试用与生产门槛不变。

今天的提交回顾与文档审阅见[10 月 6 日收尾记录](reviews/2026-10-06-daily-closeout.md)。

### 本轮完成：Ticket 与 Component 人工关联

所有者确认 [ADR-0034](../adr/0034-ticket-component-relations.md) 后，基于 `f878090` 在本地 `dev` 实施。migration 014、Project contributor / decider / admin 写权限、人工 `affects` 关系、精确解除 / 重连、双向权限分页、Activity v5 与组件稳定详情入口已接通。原 Ticket 的 Decision 来源、Current 与内容时间不变。

Go、真实 PostgreSQL、013→014 升级、备份恢复和 Web 全量检查通过；HTTPS 浏览器从正式空业务工作区完成 Message → Thread → Decision → Ticket、Component 创建、关联 / 解除 / 重连、双向跳转、稳定地址重载与桌面 / 手机 / 键盘操作。权限撤回和迟到响应由数据库及 Web 自动化验证；浏览器环境到达时限后清理，未完成浏览器权限撤回与后退专项，不将环境超时记为通过。具体证据见[实施记录](reviews/2026-10-06-ticket-component.md)。

本切片已提交为 `e156a5d`，未 push 或应用业务实例迁移。本轮临时服务与隔离浏览器已清理；不代表完整 Golden Path 或真实团队持续使用完成。

### 本轮完成：Repository 映射与 Component 关联

2026-10-06 已核对本地 `dev`，起始 HEAD 为 `5abc6a9`，工作区干净，相对本地 `origin/dev` 领先一笔联系邮箱更新提交；未 fetch 或核验实时远端。PR #12 的 `master` 晋级已包含在当前历史中，Component / Environment 配置实现仍为 `0f9d5cf`。

所有者已确认 [ADR-0033](../adr/0033-repository-mapping-and-component-relations.md)，现已实现：外部稳定身份、URL 与默认分支边界、Workspace 当前权限、多对多 EntityLink、正式创建 / 双向发现 / 解除、Audit / receipt、migration 013 和 Activity v4。Go、真实 PostgreSQL、012 升级、备份恢复、Web 全量及隔离 HTTPS 浏览器验收通过。未知结果重试不会复活旧关系或误删新关系，成员只读与撤权清空已验证；范围与证据见[实施记录](reviews/2026-10-06-repository-mapping.md)。临时服务已清理；实现已提交为 `f878090`，未 push 或应用业务实例迁移。

Ticket ↔ Component 已按上方 ADR-0034 完成；下一步设计具体交付来源与持久终态采集。保留“新配置对象 → 真实 Jenkins → 浏览器记录 staging → 撤权后阻止新记录 / 历史仍可读”的整链验收缺口。原生中文 IME 人工验收继续暂缓；业务迁移、服务启动、依赖和外部 / 远程操作按当前任务授权执行，本节不创建自动任务。

此前提交回顾与文档审阅见 [10 月 1 日收尾记录](reviews/2026-10-01-daily-closeout.md)。

### 本轮完成：最小配置与环境授权

2026-10-01 从 `efa002b`（PR #12 合并）开始，本地 `dev` / `master` 与远程跟踪引用当时一致，本轮未 fetch 或核验远端设置。所有者要求提交提案并批准实施，提案已提交为 `a206be5`，实现已提交为 `0f9d5cf`。以下 9 月 26 日记录中的“未 push”保留当时含义。

[ADR-0032](../adr/0032-component-environment-configuration-and-authorization.md) 已接受并实现：owner 正式创建 Component / staging Environment，成员发现对象，owner 显式授予、撤销、重新授予记录权。migration 012 保存不可变授权代次和旧 Deployment 来源；配置成功 Audit / receipt、创建事件与 Activity v3 同步落地。创建对象、owner Team 和 Project 角色均不隐式授予部署权。

Go、真实 PostgreSQL、升级、备份恢复和 Web 检查通过；HTTPS 浏览器从正式 bootstrap 空业务工作区验证 Team / Component / Environment 创建、自授权、撤销、重新授予、桌面 / 手机和键盘。正式配置对象接续 CI Run / staging 记录由数据库自动化证明；本轮没有重跑真实 Jenkins，也没有验证新配置对象从 Jenkins 到浏览器记录的整链。具体证据、修复与未覆盖项见[实施记录](reviews/2026-10-01-delivery-configuration.md)。临时验收服务已清理，未应用业务实例 migration、push 或部署。

Repository 映射已在 10 月 6 日按独立确认的 ADR-0033 完成；其余交付关系与持续采集沿近期顺序推进。原生 IME 人工验收继续暂缓，真实团队试用仍受评估授权与运维退出条件约束。

### 本轮完成：CI Run、Jenkins 与 staging 记录

共享工作台改版已提交为 `b19b722`。所有者随后确认 [ADR-0029](../adr/0029-session-scoped-ci-run-nexus-view.md)，现已将 CI Run 内部安全查询接到正式 GET 接口、React 页面、Deployment 来源跳转和首页次级 ID 工具，复用当前 Component 权限与来源脱敏。服务端、真实 PostgreSQL 与 Web 自动化通过；精确范围、浏览器检查及下一段分工见[实施记录](reviews/2026-09-26-jenkins-next-slice.md)。

CI Run 读取切片已提交为 `a394ed6`，未 push。所有者随后确认 [ADR-0030](../adr/0030-authenticated-jenkins-delivery-adapter.md)，受控来源绑定、文件 Secret、HMAC / 重放窗口、终态映射、脱敏运行记录与有限重试发送工具已实现，无数据库迁移或新依赖。Go、隔离 TLS 和真实 PostgreSQL 自动化通过，含提交后响应丢失的幂等恢复；精确证据与边界见[来源接入记录](reviews/2026-09-26-jenkins-adapter.md)。

Jenkins adapter 已提交为 `0001a82`，未 push。所有者确认本机 Docker 隔离联调后，已使用固定 controller / agent 镜像完成成功、失败与取消三类真实构建，由 controller 在 finalized 后生成快照，经现有 sender 与 HTTPS receiver 写入一次性 PostgreSQL；重复发送、Session 读取、唯一 Activity 和不自动生成 Deployment 均通过。运行配置位于所有者指定的独立 Docker 目录，持久化全部使用 Compose 同目录相对 bind mount，不使用 named volume。测试容器和网络已移除，controller 数据与快照保留；精确证据与未覆盖项见[真实 Jenkins 联调记录](reviews/2026-09-26-real-jenkins-lab.md)和[实验操作说明](../../experiments/jenkins-lab/README.md)。这仍是受控联调，不代表普通成员独立配置或持续交付完成。

真实 Jenkins 联调已提交为 `be780b7`，未 push。所有者随后确认 [ADR-0031](../adr/0031-session-scoped-staging-deployment-recording.md)，现已接通成功 CI Run → 已授权 staging 环境 → 显式确认外部部署终态 → Deployment 回读。新增两个 Session 端点与 migration 011，复用唯一 Deployment 事务和既有 receipt，精确重试不重复生成事实。Go、真实 PostgreSQL、备份恢复、Web 和隔离 HTTPS 浏览器验收通过；桌面 / 手机、失败结果与成功构建分离、撤权后禁止记录及历史仍可读已有证据，见[实施记录](reviews/2026-09-26-staging-deployment-recording.md)。本轮临时服务和测试数据已清理，未启动 Jenkins 或部署到业务实例。

staging 记录切片已提交为 `486a330`，未 push。下一步统一按上方近期顺序与当前接续推进；当日提交和文档核对见[收尾记录](reviews/2026-09-26-daily-closeout.md)。

### 本轮完成：最小 Markdown Document

9 月 26 日接通从 Ticket 创建、Project 列表、不可变 revision、当前权限、保存冲突、恢复追加版本与双向关系；正常命令在同一事务生成 receipt、领域事件、Outbox 与 Activity。受限 Markdown 展示不使用浏览器 HTML 注入，原文除换行归一化外不被展示投影覆盖。

Go、真实 PostgreSQL、备份恢复、Web 检查及隔离 HTTPS 浏览器已验证，精确范围见[本轮记录](reviews/2026-09-26-markdown-document.md)。原生 macOS 中文 IME 尚未在正式页面复核，不引用编辑器实验结果代替。临时浏览器、数据库和代理已清理；项目所有者已要求提交本轮工作区改动，未 push 或部署。人工验收按所有者要求暂缓，保留未验收状态，不以暂缓代替通过。[视觉方向与参考分工](../design/visual-direction.md)已记录。项目所有者随后确认使用 pen.dev，并参考 Radish、RadishFlow、RadishMind 的设计工作方式；已使用本机现有 Pen 创建[共享工作台与 Document 首轮设计稿](../design/workbench-v1.md)，首稿提交后，所有者给出初步认可并要求继续优化；现已根据 AFFiNE 实际编辑页、AppFlowy 官方编辑器配图和 Mattermost 官方讨论串截图优化至 v1.1，包含 10 个画板，新增桌面信息展开与手机信息页。所有者认可并提交静态稿后，已授权并完成共享工作台与 Document 的有限 React 视觉落地：家族配色、桌面侧栏、手机导航、集中阅读 / 编辑及按需文档信息，详见[实施与验证记录](reviews/2026-09-26-workbench-ui.md)。本轮浏览器使用虚构 API 数据验证实际 DOM，不替代真实后端与原生 IME 验收。下一业务切片按表中的真实 Jenkins 顺位推进，不扩大为富文本或 CRDT。

### 本轮完成：基础配置与首次访问初始化

9 月 14 日已接受并实施 [ADR-0026](../adr/0026-foundation-configuration-and-membership.md)：owner 显式建立本人初始 Project admin，Project admin 配置其他成员的普通角色，受限 Channel 按明确成员管理。新增配置 Audit / receipt 与 migration 009，撤权清理从属私密授权，精确重试不复权。领域、HTTP、Web、备份分类和正常 Activity 更新已同步。

真实 PostgreSQL、备份恢复、Go 检查和 Web 检查已通过；真实 HTTPS 浏览器已从正式 bootstrap 的空业务工作区创建 Team / Project / Channel，邀请新账户、授予 Project 与 Channel 权限，普通成员进入频道发消息并刷新读取；撤权后发现与旧地址均不可读。精确证据与未覆盖范围见 [9 月 14 日记录](reviews/2026-09-14-foundation-configuration.md)。这不是完整 Golden Path 或真实团队持续使用结论。

项目所有者已确认并实施 [ADR-0027](../adr/0027-first-visit-administrator-setup.md)：部署者配置一次性初始化码，首次访问创建首位 Workspace owner，完成后关闭初始化并进入正式登录。网页与 CLI 复用唯一事务锁，已有或恢复账户不会重开入口；真实数据库并发 / 回滚、恢复和 HTTPS 浏览器已有证据，见[首次初始化记录](reviews/2026-09-14-first-visit-setup.md)。没有新增实例级超级权限、默认业务对象或依赖。最小 Markdown Document 已进入上述验收收尾；管理员交接、恢复和真实 Jenkins 接入继续按独立范围推进。

## Document 实施边界

M0.5 / M1 采用服务端权威 Markdown、显式保存与 revision 冲突控制，不引入 CRDT；已接受合同见 [ADR-0021](../adr/0021-document-editor-and-collaboration-foundation.md) 和 [ADR-0028](../adr/0028-minimal-markdown-document.md)。正式实现使用固定 `goldmark v1.8.6`，精确包 LICENSE、checksum、依赖图与模块漏洞公告已核验；检查方法及局限见[实施记录](reviews/2026-09-26-markdown-document.md)。

读取、预览、历史和恢复复用同一 parser 与当前权限；Project 可见性、来源、不可变版本和恢复 receipt 已纳入真实数据库与备份测试。实现细节与端点见 [server](../../server/README.md#最小-markdown-document) 和 [Web](../../web/README.md#最小-markdown-document) 说明。当前剩余原生 IME 人工复核已按所有者要求暂缓，不能把中文字符串填充与 jsdom 当作输入法组合事件证据。

阶段 A 的 corpus、浏览器和 macOS 中文 IME 证据保留；Tiptap / ProseMirror 只是后续结构化编辑候选，未进入正式 Web 依赖。Yjs 阶段 B 延后，不阻塞最小 Document。再次启动时须说明它要解决的具体设计问题、结束条件、投入上限和依赖授权；旧预检结果不能代替届时的版本与供应链复核。详见 [ADR-0021](../adr/0021-document-editor-and-collaboration-foundation.md)。

## 停止线

- 不把内部 service、静态 fixture、手工投影重建或一次浏览器验收描述成完整产品闭环。
- 引用、反向关系、旧 membership、客户端角色和订阅状态都不授予权限；新入口继续复用既有当前权限与不可发现性。
- SSE replay 不作权威存储，写 command 继续使用短请求；不引入隐藏 polling fallback、多副本 fan-out、独立消息中间件或新的实时 transport。
- 近期不横向补齐完整聊天 UI、附件、表情、复杂搜索、未读与通知；对象发现只服务最小场景。Document 仅按已接受 ADR-0028 实施，正式富文本依赖、CRDT 与实时协同仍未授权。
- 不启动 Flutter、微服务拆分、插件市场、通用多语言 SDK、完整软件目录或完整离线文档。
- CI Run 成功不触发 Deployment；本阶段只记录外部已完成 staging 终态，不执行 production、审批或回滚，不自动确认 AI 草案。
- 文档顺序调整不授权安装依赖、运行长期服务、改系统配置、提交、push、PR、发布、部署或发送外部消息；具体授权仍按根协作约定。

## 开放问题

- 关系分页、反向索引、查询容量与跨对象 Timeline；事务内 Activity 更新、重建并发及首批 direction 合同已由 ADR-0022 冻结；
- 管理员交接、其他协作对象发现、账号恢复与应急入口；基础配置和首访 owner 初始化已由 ADR-0026 / ADR-0027 冻结；
- Document 正式页面原生中文 IME 与持续使用证据；正式富文本与 CRDT、受控脱敏和可移植导出仍未完成；
- Jenkins 持续终态采集与业务实例配置、持久来源管理 / 安全审计、Repository 与 CI Run 的来源关系及其余交付关系；
- PostgreSQL 支持矩阵、schema 兼容窗口、forward repair、数据库权限拆分和容量基线；
- Decision 拒绝、替代、复核与 Ticket 基础执行状态的交互；
- 安全 Audit、消息 / 事件 / receipt 的保留和受控脱敏边界；
- `.nexus` 版本化格式、脱敏与导入映射；免费评估路径及授权签发 / 撤销规则；
- 后续插件运行方式、SDK / 插件许可证及搜索边界；OIDC 关联目标已由 ADR-0023 冻结，真实 provider、协议验签、浏览器与 Radish 联调延至未来独立切片，当前关闭 Radish 登录。

## 证据与历史

- [2026-10-06 提交回顾与文档收尾](reviews/2026-10-06-daily-closeout.md)：两笔实现提交、源码与文档核对及 10 月 7 日接续建议。

- [2026-10-06 Ticket 与 Component 人工关联](reviews/2026-10-06-ticket-component.md)：migration 014、Project 权限、双向分页、关系代次、升级 / 恢复和浏览器证据边界。

- [2026-10-06 Repository 映射与 Component 关联](reviews/2026-10-06-repository-mapping.md)：migration 013、关系历史、当前权限、升级 / 恢复与浏览器证据。

- [2026-10-01 提交回顾与文档收尾](reviews/2026-10-01-daily-closeout.md)：当日四笔提交、源码对应文档修正与 10 月 2 日接续建议。

- [2026-10-01 组件、环境与授权管理](reviews/2026-10-01-delivery-configuration.md)：migration 012、不可变授权代次、正式配置、并发 / 恢复与浏览器证据。

- [2026-09-26 提交回顾与文档收尾](reviews/2026-09-26-daily-closeout.md)：当日八笔实现 / 设计提交、文档漂移修正与次日接续入口。

- [2026-09-26 工作台与 Document 视觉落地](reviews/2026-09-26-workbench-ui.md)：正式 React、响应式、键盘与交互回归及证据边界。

- [2026-09-26 最小 Markdown Document](reviews/2026-09-26-markdown-document.md)：正式切片、安全解析、并发 / 备份、浏览器证据与原生 IME 待复核边界。

- [2026-09-14 提交回顾与文档收尾](reviews/2026-09-14-daily-closeout.md)：当日实现与合同提交、代码 / 文档核对、剩余验收边界和次日接续入口。

- [2026-09-14 首次访问初始化](reviews/2026-09-14-first-visit-setup.md)：一次性初始化码、首位 owner、并发关闭、恢复与浏览器证据。

- [2026-09-14 基础对象与成员配置](reviews/2026-09-14-foundation-configuration.md)：实现、数据库 / Web / 浏览器、恢复验证及首次初始化剩余边界。

- [2026-09-10 提交回顾与文档收尾](reviews/2026-09-10-daily-closeout.md)：当日四笔实现提交、源码与文档核对、验证及剩余边界。
- [2026-09-10 Project / Channel 发现](reviews/2026-09-10-project-channel-discovery.md)：只读列表、分页与权限、首页交互和浏览器验收边界。

- [2026-09-10 schema readiness](reviews/2026-09-10-schema-readiness.md)：只读检查、真实数据库失败矩阵、超时与恢复证据。

- [2026-09-10 账户与联合登录调整](reviews/2026-09-10-identity-alignment.md)：本地实现、旧账户迁移、协议事务证据与 OIDC 延后规划边界。

- [2026-09-10 Activity 与双向关系](reviews/2026-09-10-activity-relations.md)：本轮实现边界、自动化与数据库证据、浏览器验收状态。

- [2026-09-05 项目审阅](reviews/2026-09-05-project-review.md)：源码缺口、工程建议、本轮已执行与未执行验证。
- [2026-09-03 状态快照](history/2026-09-03-status.md)：完整保留此前阶段事实及 9 月 2～3 日浏览器、PostgreSQL、Compose、编辑器验收；其中旧推进顺序已失效。
- [编辑器实验结果](../../experiments/document-editor/RESULTS.md)、[正式服务说明](../../server/README.md)、[部署操作说明](../../deploy/README.md)承载对应证据和运行方式。
- 分支基线仍按[仓库治理](../governance/repository-governance.md)执行。此前晋级与远端质量门结果按历史记录理解；2026-09-05 仅检查本地分支与工作区，未重新核验远端设置或晋级状态。
