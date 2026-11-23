# RadishNexus 当前状态

2026-11-23 已接通[从讨论返回原消息](reviews/2026-11-23-thread-source-message-navigation.md)及[频道内的可读讨论列表](reviews/2026-11-23-channel-thread-discovery.md)，完成[从频道原消息找回讨论及从回复返回所属讨论](reviews/2026-11-23-message-started-thread.md)，并完成 [Ticket Activity 历史分页与有限 Web 故事技术复核](reviews/2026-11-23-ticket-activity-pagination.md)。

2026-11-15 完成工单截止日期、消息转工单与执行讨论、工单分类与项目内查找的开发和技术验证。工单与构建的可追溯关联已按 ADR-0056 接入。同日完成固定候选的本机 Web 协作技术验收及试用观察准备检查。规划收口记录日期为 2026-11-10。

整体目标与阶段范围见[路线图](../roadmap.md)，能力归属与任务 / 验收来源见[专题地图](../topics.md)。完整历史流水保留于[原状态快照](history/planning-structure-2026-11-01.md)，不恢复旧运行授权。

2026-11-14 已停用 Pencil。现有工作台稿与 PNG 随评审留档，后续代表设计见[视觉方向](../design/visual-direction.md)。

## 已确认待实施的客户端方向

2026-10-12 已确认[客户端连接规划](../architecture/client-server-connection-plan.md)与 [ADR-0036](../adr/0036-official-client-instance-connection.md)：同一官方安装包运行时选择官方端点或用户自部署后端，无需重新编译。本轮仅更新文档；官方端点与 Flutter 选择能力尚未交付，当前 Web Golden Path / M1 和已有停止线继续有效。

## 当前阶段

M0.5 Golden Path / M1 Web 平台基础纵向原型。正式 Go、PostgreSQL 和 React Web 已建立若干真实业务切片；完整 Golden Path、普通成员独立操作及真实团队持续使用尚未整体验收。Decision 最小拒绝已按 [ADR-0048](../adr/0048-decision-lifecycle-and-rejection.md)实施并通过定向数据库、迁移 / 备份和正式 Web 验收；[ADR-0049 最小替代](../adr/0049-decision-supersession.md)已确认并实施，同 Project 单链、双方版本、原操作重试、双对象 Activity 与 v5 历史已有分层证据。人工复核已接通 Go、PostgreSQL 与 Web，并通过正式 HTTPS Chrome 验收。 [ADR-0050](../adr/0050-decision-manual-review.md)

当前以 **Web 功能开发** 为主。固定候选 `1036b21` 已在本机 OrbStack 合成环境通过有限 Web 协作技术验收，涵盖正式 Chrome 协作、恢复及单 Project 取回。合成账号预演和 T04 技术证据复核作为后续迭代的技术基线。开发批次由 Agent 自行验证。书面授权专业复核挂起，授权未签发，试用保持关闭。完整 Golden Path、M1、M4 和生产准备分别沿原门槛判断。 [候选实测和预演](reviews/2026-11-10-web-candidate-1036b21.md#2026-11-15-合成账号预演) · [路线图](../roadmap.md#推进原则)

## 完成线与成熟度

| 能力主线 | 当前基线与未关闭范围 | 正式来源 |
| --- | --- | --- |
| 身份与管理 | 本地账户、邀请、初始化、恢复、owner / 成员管理交接已有技术证据；真实 OIDC / Radish 联调与团队持续使用仍未验收 | [身份专题](../topics.md#identity) |
| 沟通与决策 | 可从频道列表分页发现可读讨论、在讨论与可读原消息之间往返，或从回复返回所属讨论。讨论创建 / 确认 Decision、Project 决策发现已交付；最小拒绝、单链替代及四状态读取已交付，最小人工复核已通过正式浏览器验收 | [沟通与决策专题](../topics.md#communication-decisions) |
| 工单与上下文 | 已交付工单状态、负责人、优先级、截止日期、消息建单、执行讨论、分类与 Project 标签。列表和状态看板支持组合查找，「我的工单」展示分类。已接通 Ticket 与 CI Run 的人工关联，也可从原 Thread 分层追溯工单、构建和 staging 部署。核心关系与 Ticket Activity 已可分页读取 | [工单与上下文专题](../topics.md#tickets-context) · [我的工单验收](reviews/2026-11-08-workspace-my-tickets.md) · [看板验收](reviews/2026-11-08-project-ticket-board.md) |
| 文档协作 | 最小 Markdown 正式保存、冲突、版本 / 恢复及隔离浏览器已有证据；原生中文 IME 暂缓、持续使用未验收 | [文档专题](../topics.md#documents) |
| 交付集成 | 受认证接入、显式 staging 记录、配置、持久发送及 checkout 接收已有相应证据；ADR-0035 / 0037 B 真实采集与整链暂缓 | [交付专题](../topics.md#delivery-integration) |
| 运行与产品交付 | Web、自部署、升级恢复、单 Project 上下文包及补验已有指定环境证据；U27、生产容量、更广迁移、有效试用授权与独立持续使用未闭合 | [运行交付专题](../topics.md#product-delivery) |

内部契约、正式入口 / 浏览器、普通用户独立操作与持续使用分别判断。原逐切片矩阵保留于[历史快照](history/planning-structure-2026-11-01.md#完成线与成熟度)，原验收记录仍限定其版本、环境及范围。

## 已确认缺口

9 月 5 日的原始发现保留在[审阅记录](reviews/2026-09-05-project-review.md)。9 月 10 日已按 [ADR-0022](../adr/0022-transactional-activity-and-incoming-relations.md) 接通正常 Activity 更新及 Thread ← Decision、Decision ← Ticket 的反向发现；正式命令后的 HTTP 读取、撤权、失败回滚和重建并发已通过真实 PostgreSQL 测试，不在正常更新验收中手工重建。本轮 Chrome 浏览器已验证提案、接受、创建 Ticket、刷新后双向跳转与撤权清空，证据见[本轮记录](reviews/2026-09-10-activity-relations.md)。

当前剩余缺口：

1. **升级运维**：只读 schema readiness 已按 [ADR-0024](../adr/0024-read-only-schema-readiness.md) 完成；单机停机升级失败恢复已按 [ADR-0043](../adr/0043-single-host-upgrade-failure-recovery.md) 实施手册与隔离演练，故障与浏览器证据见[记录](reviews/2026-10-24-upgrade-failure-recovery.md)；独立操作者复验已按所有者要求暂缓，跨 schema 兼容窗口与生产编排仍待建立。
2. **使用入口**：首页已可直接浏览可读 Project / Channel；Team / Project / Channel 创建与首批成员配置已接通；部署后首次访问创建管理员已接通；Workspace owner 主动交接已接通，Project admin / 私密对象管理权交接与成员生命周期已接通；其他对象列表仍缺。Document 的原生 IME 复核和真实外部交付链仍独立推进。
3. **关系与时间线范围**：Thread、Decision 和 Ticket 已接入关系分页与 schema 030 入向索引。Ticket Activity 已按 ADR-0059 分页。其他 Nexus View 的时间线仍沿各自读取合同。Repository / Component 配置、Ticket / Component 和 Ticket / CI Run 已有专用双向分页。Timeline 默认沿事件主要对象投影，替代命令显式投影双方 Decision。Thread 的“执行与交付”支持从三类来源找到工单，再逐级读取关联构建与已有 staging 部署。 [ADR-0057 技术验收](reviews/2026-11-15-thread-delivery-trace.md)
4. **证据边界**：自动化、真实数据库与本轮浏览器验收证明讨论到执行的局部闭环；Document 的剩余人工复核、真实交付链、完整 Golden Path 和真实团队持续使用仍分别验收。

## 近期执行顺序

2026-11-23，所有者确认继续按整体路线图推进 Web 功能。各批次按功能风险使用自动化脚本、合成账号及浏览器操作验证。11 月 10 日的原顺位和证据边界见[收口记录](reviews/2026-11-10-planning-closeout.md)。

### 接下来的事项

1. **下一片建议从 Project 决策页查找待决定的提案**。当前列表可显示各状态，成员要逐页寻找待决定事项。先明确状态筛选、稳定分页和旧游标兼容范围，沿用 Project 读权和既有人工决策权限。[Project 决策发现](../adr/0040-project-decision-discovery.md) · [M1 路线图](../roadmap.md#m1web-平台基础)
2. **频道讨论发现与原消息导航已完成合成账号验证**。频道列表按来源与目标双端读权翻页。详情可定位最初的发言；只撤销来源回复读权时，仍可读的子讨论保留。[列表验证](reviews/2026-11-23-channel-thread-discovery.md) · [来源导航验证](reviews/2026-11-23-thread-source-message-navigation.md)

11 月 15 日交付覆盖工单截止日期、消息建单与执行讨论、分类与查找、构建关联及原讨论追溯，并完成核心协作关系分页和 schema 030 升级恢复验证。各实现及证据沿对应 ADR 查阅。 [2026-11-15 提交回顾](reviews/2026-11-15-daily-closeout.md)

已完成的负责人、优先级及上下文迁移补验沿[原记录](reviews/2026-10-28-daily-closeout.md)转入有界观察；遇到实际 BFCache、原生控件、慢查询或新候选差异时定向复验，不把合成查询计划写成生产容量。单宿主身份拆分沿[ADR-0051](../adr/0051-single-host-database-roles.md)及[隔离证据](reviews/2026-11-08-single-host-database-roles.md)复用，M1 文件、通知、审计运营、Attention Item 与更广生产运维仍按路线图独立推进。

<a id="resume-conditions"></a>

### 暂缓项与恢复条件

真人观察和真人实测暂缓。等项目真正开发完成、产品能力可以对标既有参考项目后，再由所有者决定是否开展。下表维护恢复条件。条件就绪后仍由所有者确认对应执行范围；没有新增证据前，原“暂缓 / 未验收”状态不变。协调角色是待落实的职责，不表示已有具体人员承诺。

| 项目与当前状态 | 恢复条件及协调角色 | 影响与验收来源 |
| --- | --- | --- |
| 独立成员观察、R08：暂缓 | 所有者决定开展后，由所有者落实参与者、任务和观察窗口。维护者完成适用准入准备，成员从正常入口独立操作 | 独立使用和产品价值保持未验证。 [ADR-0050 R08](../adr/0050-decision-manual-review.md#验收矩阵) · [使用观察](../golden-path.md#从演示到持续使用) |
| 书面授权专业复核：挂起 | 所有者确认恢复后落实复核人，并提供实际主体、候选、用途、期限和取回范围 | T02 待确认，书面授权未签发，试用保持关闭。 [评估授权待决矩阵](../evaluation-authorization-decisions.md#待决矩阵) |
| U27 独立运维走查：暂缓 | 所有者决定开展后，由部署负责人落实另一操作者、目标环境、匹配工件、数据取舍及恢复演练范围，再确认执行授权 | 独立运维能力保持未验收。试用准入沿 T06 / T07 与 ADR-0043 判断。 [原矩阵](../adr/0043-single-host-upgrade-failure-recovery.md) |
| 原生中文 IME 真人实测：暂缓 | 所有者决定开展后，由维护者与所有者落实实际输入法、浏览器、操作者和正式 Document 场景，确认人工复核安排 | 所选场景包含中文输入时，须实际验证组合、候选确认、Enter 与保存读回。中文文档体验保持未验收。 [原缺口](reviews/2026-09-26-markdown-document.md) |
| ADR-0035 B / 0037 B 与真实交付整链：暂缓 | 部署负责人落实受支持的原生 Linux 持久目录、真实 Jenkins / SCM、凭据受控提供、重启 / 挂载及双边清理范围，所有者确认联调授权 | 不阻塞有限 Web 协作技术批次，继续阻塞完整交付链结论；按下节依赖恢复。Ticket 与 CI Run 的人工关联已由 ADR-0056 接通，交付验证以实际外部记录为准 |

真实团队持续使用、生产容量、完整聊天、CRDT、Flutter、通用插件平台及更广迁移 profile 保持原阶段门槛。未就绪事项只在本节登记条件，后续复核由实际候选、人员或环境变化触发，不创建自动任务。

### 交付链保留完成线与恢复顺序

Ticket 与 Component 人工关联已按 ADR-0034 实现，浏览器后退、显式刷新读 / 写权撤回及最终布局已补验。[ADR-0035 A](../adr/0035-durable-jenkins-terminal-delivery.md) 已获确认并实现：单 source / 单 worker 持久发送、不可变输入、重启预算、分类失败、精确恢复、只读状态与确认记录。没有业务 migration、公共 v1 协议或依赖变化；这不代表 controller 持续采集或漏采恢复完成。

以下为交付链恢复时的依赖与未完成项，不改变本页当前执行顺位：

1. **来源接收与发现**：[ADR-0037](../adr/0037-ci-run-repository-checkout-provenance.md)已于 2026-10-13 确认，A 的 sources / payload v2、不可变 binding、built-from、Activity 6、正式来源展示与双向发现已实施；验收证据见[原实施记录](reviews/2026-10-13-checkout-provenance.md)。Ticket 已能人工关联具体 CI Run，并读取该构建的既有 staging 记录。真实 checkout 采集 B 继续暂缓。 [ADR-0056](../adr/0056-ticket-ci-run-traceability.md)
2. **ADR-0035 B：真实终态采集验收（暂缓）**：独立 controller 与双边清理已具备离线证据，但本机 Docker bind mount 实测为 FUSE，超出支持范围。所有者已明确暂缓真实联调，原生 Linux 环境下的真实重启 / 挂载验收尚未完成，恢复时另行明确资源与授权，见[采集记录](reviews/2026-10-09-jenkins-collector.md)和[清理记录](reviews/2026-10-09-jenkins-cleanup.md)。ADR-0037 B 是已确认来源方案中的后续切片，不与既有终态采集完成线混为一谈。
3. **真实整链验收**：正式新配置对象 → 真实 Jenkins → 浏览器记录已完成的外部 staging → 撤权阻止新记录且历史按当前权限可读。没有实际外部 staging 行为时只验收到 CI Run，不伪造 Deployment。

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

- 其他对象的关系分页、查询容量与跨对象 Timeline。核心协作对象的入向索引见 ADR-0058，事务内 Activity 更新、重建并发及首批 direction 合同见 ADR-0022；
- 其他协作对象发现与超出 ADR-0041 / 0042 / 0045 的应急入口；Project admin / 私密对象管理交接及成员生命周期已由 ADR-0045 冻结；基础配置和首访 owner 初始化已由 ADR-0026 / ADR-0027 冻结；
- Document 正式页面原生中文 IME 与持续使用证据；正式富文本与 CRDT、受控脱敏和可移植导出仍未完成；
- Jenkins 持续终态采集与业务实例配置、持久来源管理 / 安全审计、真实 checkout 采集与其余具体交付关系；
- PostgreSQL 支持矩阵、schema 兼容窗口、forward repair、其他部署拓扑的数据库权限和容量基线；
- 人工复核的独立使用证据，以及后续 `review_at` 与触发模型；
- 安全 Audit、消息 / 事件 / receipt 的保留和受控脱敏边界；
- `.nexus` 版本化格式、脱敏与导入映射；免费评估路径及授权签发 / 撤销规则；
- 后续插件运行方式、SDK / 插件许可证及搜索边界；OIDC 关联目标已由 ADR-0023 冻结，真实 provider、协议验签、浏览器与 Radish 联调延至未来独立切片，当前关闭 Radish 登录。

## 证据与历史

[有限 Web 候选](reviews/2026-11-10-web-candidate-1036b21.md)记录 `1036b21` 的源码差异、离线检查、工件身份及 11 月 15 日的隔离实测。

[2026-11-10 规划收口](reviews/2026-11-10-planning-closeout.md)记录所有者确认的有限 Web 批次、排期调整与本轮文档检查；不新增运行证据。

[2026-11-08 收尾记录](reviews/2026-11-08-daily-closeout.md)汇总当日提交、实际验证及本次文档核对。

[2026-11-02 收尾记录](reviews/2026-11-02-daily-closeout.md)保留当日三笔设计 / 实现提交、代码与文档核对及接续边界；[2026-11-02 替代实施记录](reviews/2026-11-02-decision-supersession.md)保留四状态、单链、双对象历史、恢复 / 往返与浏览器验收限制；[2026-11-02 替代设计与提交记录](reviews/2026-11-02-decision-supersession-design.md)保留拒绝提交映射、后续提案与源码判断；[2026-11-02 Decision 拒绝实施记录](reviews/2026-11-02-decision-rejection.md)保留本轮交付与分层验收；[2026-11-02 Decision 设计记录](reviews/2026-11-02-decision-lifecycle-design.md)保留本轮源码核对、首片取舍与确认边界；[2026-10-28 收尾记录](reviews/2026-10-28-daily-closeout.md)保留上一功能批次的提交与验证映射；[规划整理记录](reviews/2026-11-01-planning-alignment.md)记录导航整理及检查。此前详细流水和完整证据目录见[原状态快照](history/planning-structure-2026-11-01.md#证据与历史)；[2026-09-03 快照](history/2026-09-03-status.md)继续保留原历史边界。

以下旧标题作为历史跳转保留，不再提供当前排期：

<a id="本轮完成成员生命周期与管理交接"></a>

- [本轮完成：成员生命周期与管理交接](history/planning-structure-2026-11-01.md#本轮完成成员生命周期与管理交接)

<a id="本轮完成首片结构化导出与新实例导入"></a>

- [本轮完成首片：结构化导出与新实例导入](history/planning-structure-2026-11-01.md#本轮完成首片结构化导出与新实例导入)

<a id="本轮实施单机升级失败恢复试用条件仍待确认"></a>

- [本轮实施：单机升级失败恢复；试用条件仍待确认](history/planning-structure-2026-11-01.md#本轮实施单机升级失败恢复试用条件仍待确认)

<a id="本轮完成workspace-owner-主动交接"></a>

- [本轮完成：Workspace owner 主动交接](history/planning-structure-2026-11-01.md#本轮完成workspace-owner-主动交接)

<a id="本轮完成自部署本地账户受控恢复"></a>

- [本轮完成：自部署本地账户受控恢复](history/planning-structure-2026-11-01.md#本轮完成自部署本地账户受控恢复)

<a id="本轮完成project-决策只读发现"></a>

- [本轮完成：Project 决策只读发现](history/planning-structure-2026-11-01.md#本轮完成project-决策只读发现)

<a id="本轮完成project-工单只读发现"></a>

- [本轮完成：Project 工单只读发现](history/planning-structure-2026-11-01.md#本轮完成project-工单只读发现)

<a id="本轮完成ticket-最小状态流转"></a>

- [本轮完成：Ticket 最小状态流转](history/planning-structure-2026-11-01.md#本轮完成ticket-最小状态流转)

<a id="checkout-来源切片-a2026-10-13"></a>

- [Checkout 来源切片 A：2026-10-13](history/planning-structure-2026-11-01.md#checkout-来源切片-a2026-10-13)

<a id="本轮补验与设计2026-10-28"></a>

- [本轮补验与设计：2026-10-28](history/planning-structure-2026-11-01.md#本轮补验与设计2026-10-28)

<a id="本轮完成ticket-最小负责人"></a>

- [本轮完成：Ticket 最小负责人](history/planning-structure-2026-11-01.md#本轮完成ticket-最小负责人)

<a id="本轮完成ticket-最小优先级与筛选"></a>

- [本轮完成：Ticket 最小优先级与筛选](history/planning-structure-2026-11-01.md#本轮完成ticket-最小优先级与筛选)

<a id="本日收尾2026-10-28"></a>

- [本日收尾：2026-10-28](history/planning-structure-2026-11-01.md#本日收尾2026-10-28)

<a id="历史收尾2026-10-24"></a>

- [历史收尾：2026-10-24](history/planning-structure-2026-11-01.md#历史收尾2026-10-24)

<a id="本轮完成ticket-与-component-人工关联"></a>

- [本轮完成：Ticket 与 Component 人工关联](history/planning-structure-2026-11-01.md#本轮完成ticket-与-component-人工关联)

<a id="本轮完成repository-映射与-component-关联"></a>

- [本轮完成：Repository 映射与 Component 关联](history/planning-structure-2026-11-01.md#本轮完成repository-映射与-component-关联)

<a id="本轮完成最小配置与环境授权"></a>

- [本轮完成：最小配置与环境授权](history/planning-structure-2026-11-01.md#本轮完成最小配置与环境授权)

<a id="本轮完成ci-runjenkins-与-staging-记录"></a>

- [本轮完成：CI Run、Jenkins 与 staging 记录](history/planning-structure-2026-11-01.md#本轮完成ci-runjenkins-与-staging-记录)

<a id="本轮完成最小-markdown-document"></a>

- [本轮完成：最小 Markdown Document](history/planning-structure-2026-11-01.md#本轮完成最小-markdown-document)

<a id="本轮完成基础配置与首次访问初始化"></a>

- [本轮完成：基础配置与首次访问初始化](history/planning-structure-2026-11-01.md#本轮完成基础配置与首次访问初始化)
