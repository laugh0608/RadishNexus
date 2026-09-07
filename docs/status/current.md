# RadishNexus 当前状态

状态日期：2026-09-05

## 当前阶段

M0.5 Golden Path / M1 Web 平台基础纵向原型。正式 Go、PostgreSQL 和 React Web 已建立若干真实业务切片，尚未完成可由普通成员独立操作、持续使用的完整 Golden Path。

当前重点是把已有权限、事务、来源和恢复能力接成用户可感知的上下文闭环。长期范围与阶段退出条件见[路线图](../roadmap.md)，产品验收见 [Golden Path](../golden-path.md)。本页只维护当前判断和顺序，不重复完整 ADR 与历史测试流水。

## 完成线与成熟度

下表依据仓库实现及已有验收记录区分四类证据；公共入口或浏览器演示通过不等于真实团队日常使用通过。

| 能力 | 内部契约与实现 | 公共入口与交互 | 普通用户独立操作 / 持续使用 |
| --- | --- | --- | --- |
| 本地身份与 Session | bootstrap、密码 verifier、Session、CSRF、当前 membership | 同源 HTTPS login / session / logout、Workspace 选择 | 邀请、账号恢复、成员与角色管理入口尚缺；未完成团队使用验收 |
| Message → Thread → Decision → Ticket | migration 006 / 007、权限、来源、原子事件与幂等 receipt | canonical 页面和短请求；contributor / decider 浏览器验收已有记录 | 仍依赖已知 ID；反向发现、自动 Timeline 和基础执行管理未闭环 |
| 单 Channel Message 实时 | 单进程 SSE、当前权限、有界回放、撤权与关闭 | canonical Channel 已接入 ready → history → 增量 | 有技术验收；没有目标团队规模与持续使用的容量证据 |
| CI Run / staging Deployment | verified Jenkins delivery、终态 CI Run、显式环境授权记录 Deployment | Deployment 只读入口；CI Run 仍有内部 query 与静态代表页 | Jenkins 来源验证入口、正式 CI Run 页面和 Deployment 写入口尚缺 |
| EntityLink / Activity | 带来源关系、权限过滤 query、版本化全量重建 | Nexus View 可读 Current / 出向 Relations / 已投影 Timeline | 普通写入后的自动投影、反向关系读取尚缺 |
| 自部署与恢复 | 显式 migration、PostgreSQL 17 同 major 空目标恢复 | 固定工件 Compose 开发拓扑与 HTTPS 演练已有记录 | schema readiness、升级失败恢复、运维与生产容量尚未完成 |
| Document | 编辑器阶段 A 已选 Tiptap / ProseMirror 作为后续结构化编辑候选 | 仅隔离实验，无正式 Document 页面 | 最小业务合同、revision、正式存储与保存流程尚缺 |

## 已确认缺口

以下是 2026-09-05 对代码和验证路径的审阅结论，并非本次已经修复；具体证据与后续工程建议见[审阅记录](reviews/2026-09-05-project-review.md)。

1. **Activity 自动更新**：正式读取使用 `activity_items`，目前只有显式全量重建路径；测试预先调用重建，不能证明正常命令之后 Timeline 自动出现。
2. **反向关系发现**：当前查询只按 EntityLink 起点读取。Ticket 可追溯 Decision、Decision 可追溯 Thread，但原 Thread 尚不能通过该查询发现后续 Decision，Decision 也不能发现生成的 Ticket。
3. **业务就绪检查**：`/health/ready` 只检查数据库连通性，未检查当前二进制与 migration history 的兼容性。
4. **使用入口**：首页仍要求已知稳定 ID；对象发现、最小成员管理、Document 和真实外部交付链尚未齐备。
5. **证据边界**：已有浏览器与数据库验收证明局部技术行为；没有完整 Golden Path 或真实团队持续使用的完成记录。

## 近期执行顺序

以下顺序替代原“明日事项（2026-09-04）”。每个切片先核对既有合同和失败边界；本次文档调整不新增数据模型、权限、公共 DTO、依赖或远程操作授权。

| 顺序 | 下一切片 | 交付与退出判据 |
| --- | --- | --- |
| 1 | Activity 自动更新与双向关系 | 正式写入口创建 / 接受 Decision、创建 Ticket 后，刷新可看到对应 Timeline，并能从来源发现结果、从结果追溯来源；不在验收中手工重建。反向查询复用同一权威关系和当前权限，不复制镜像事实。实现前决定投影时效、事务 / worker 方案和方向 DTO |
| 2 | schema readiness 与最小使用入口 | 缺失、漂移或不兼容 migration 时不报告业务 ready，检查本身不执行迁移；成员能通过受控入口进入可访问 Project / Channel 和基础对象，完成必要成员配置，无需已知 ID 或直接改库。账号与列表协议独立审查 |
| 3 | 最小 Markdown Document | 先审查并冻结最小字段、revision、权限、EntityLink、事件、渲染安全、备份和导出边界，再完成读取、显式保存、冲突与恢复切片；不引入 CRDT |
| 4 | 真实 Jenkins 与 staging 记录链 | 来源验证、幂等和失败隔离连接到已有 CI Run service，用户可读取真实构建并显式记录外部已完成的 staging Deployment；贯通 Ticket / Component / Repository 与交付关系，不依赖预置交付事实 |
| 5 | 小团队场景试用 | 满足评估授权与必要运维条件后，以真实需求连续使用并记录追溯、重复录入和人工干预；按 Golden Path 验收决定下一批功能，不把一次演示当作持续使用完成 |

免费书面授权与评估说明应在邀请外部团队前准备，不能等到完整聊天或 CRDT 完成；版本化结构化导出与全新实例导入仍是 M1 的独立退出条件。账号恢复、安全审计、数据生命周期、数据库最小权限与升级演练按[路线图](../roadmap.md)的试用和生产边界推进，不因本表只列五项而取消。

## Document 预研边界

本轮选择 M0.5 / M1 不引入 CRDT，先完成服务端权威 Markdown、显式保存和 revision 冲突控制。ADR-0021 已保留该接受路径；现按此收束提议，Document 业务合同尚未冻结，ADR 继续保持“提议”。

阶段 A 的 corpus、浏览器和 macOS 中文 IME 证据保留；Tiptap / ProseMirror 只是后续结构化编辑候选，未进入正式 Web 依赖。Yjs 阶段 B 延后，不阻塞最小 Document。再次启动时须说明它要解决的具体设计问题、结束条件、投入上限和依赖授权；旧预检结果不能代替届时的版本与供应链复核。详见 [ADR-0021](../adr/0021-document-editor-and-collaboration-foundation.md)。

## 停止线

- 不把内部 service、静态 fixture、手工投影重建或一次浏览器验收描述成完整产品闭环。
- 引用、反向关系、旧 membership、客户端角色和订阅状态都不授予权限；新入口继续复用既有当前权限与不可发现性。
- SSE replay 不作权威存储，写 command 继续使用短请求；不引入隐藏 polling fallback、多副本 fan-out、独立消息中间件或新的实时 transport。
- 近期不横向补齐完整聊天 UI、附件、表情、复杂搜索、未读与通知；对象发现只服务最小场景。Document 合同冻结前不接入正式存储、依赖或实时协同。
- 不启动 Flutter、微服务拆分、插件市场、通用多语言 SDK、完整软件目录或完整离线文档。
- CI Run 成功不触发 Deployment；本阶段只记录外部已完成 staging 终态，不执行 production、审批或回滚，不自动确认 AI 草案。
- 文档顺序调整不授权安装依赖、运行长期服务、改系统配置、提交、push、PR、发布、部署或发送外部消息；具体授权仍按根协作约定。

## 开放问题

- Activity 更新时效、可靠消费与重建并发策略；反向关系的方向展示、分页和公共合同；
- 首版对象发现与成员管理范围，账号恢复与管理员应急入口；
- Document 最小合同、Markdown 子集 / sanitizer、revision 冲突及恢复 / 导出；
- Jenkins 来源验证、Secret 使用、失败审计、Repository 映射与交付关系；
- PostgreSQL 支持矩阵、schema 兼容窗口、forward repair、数据库权限拆分和容量基线；
- Decision 拒绝、替代、复核与 Ticket 基础执行状态的交互；
- 安全 Audit、消息 / 事件 / receipt 的保留和受控脱敏边界；
- `.nexus` 版本化格式、脱敏与导入映射；免费评估路径及授权签发 / 撤销规则；
- 后续插件运行方式、SDK / 插件许可证、OIDC 关联及搜索边界。

## 证据与历史

- [2026-09-05 项目审阅](reviews/2026-09-05-project-review.md)：源码缺口、工程建议、本轮已执行与未执行验证。
- [2026-09-03 状态快照](history/2026-09-03-status.md)：完整保留此前阶段事实及 9 月 2～3 日浏览器、PostgreSQL、Compose、编辑器验收；其中旧推进顺序已失效。
- [编辑器实验结果](../../experiments/document-editor/RESULTS.md)、[正式服务说明](../../server/README.md)、[部署操作说明](../../deploy/README.md)承载对应证据和运行方式。
- 分支基线仍按[仓库治理](../governance/repository-governance.md)执行。此前晋级与远端质量门结果按历史记录理解；2026-09-05 仅检查本地分支与工作区，未重新核验远端设置或晋级状态。
