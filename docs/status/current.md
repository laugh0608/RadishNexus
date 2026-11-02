# RadishNexus 当前状态

状态事实日期：2026-11-02；最近功能交付：2026-11-02；规划导航整理：2026-11-01。

整体目标与阶段范围见[路线图](../roadmap.md)，能力归属与任务 / 验收来源见[专题地图](../topics.md)。完整历史流水保留于[原状态快照](history/planning-structure-2026-11-01.md)，不恢复旧运行授权。

## 已确认待实施的客户端方向

2026-10-12 已确认[客户端连接规划](../architecture/client-server-connection-plan.md)与 [ADR-0036](../adr/0036-official-client-instance-connection.md)：同一官方安装包运行时选择官方端点或用户自部署后端，无需重新编译。本轮仅更新文档；官方端点与 Flutter 选择能力尚未交付，当前 Web Golden Path / M1 和已有停止线继续有效。

## 当前阶段

M0.5 Golden Path / M1 Web 平台基础纵向原型。正式 Go、PostgreSQL 和 React Web 已建立若干真实业务切片；完整 Golden Path、普通成员独立操作及真实团队持续使用尚未整体验收。Decision 最小拒绝已按 [ADR-0048](../adr/0048-decision-lifecycle-and-rejection.md)实施并通过定向数据库、迁移 / 备份和正式 Web 验收；替代与复核仍须独立冻结。

## 完成线与成熟度

| 能力主线 | 当前基线与未关闭范围 | 正式来源 |
| --- | --- | --- |
| 身份与管理 | 本地账户、邀请、初始化、恢复、owner / 成员管理交接已有技术证据；真实 OIDC / Radish 联调与团队持续使用仍未验收 | [身份专题](../topics.md#identity) |
| 沟通与决策 | 讨论创建 / 确认 Decision、Project 决策发现已交付；最小拒绝及三状态读取已交付，替代与复核待独立冻结 | [沟通与决策专题](../topics.md#communication-decisions) |
| 工单与上下文 | 工单发现、四状态、负责人、优先级 / 筛选、首批关系和 Activity 已有证据；完整执行管理、通用关系分页与持续使用未闭合 | [工单与上下文专题](../topics.md#tickets-context) |
| 文档协作 | 最小 Markdown 正式保存、冲突、版本 / 恢复及隔离浏览器已有证据；原生中文 IME 暂缓、持续使用未验收 | [文档专题](../topics.md#documents) |
| 交付集成 | 受认证接入、显式 staging 记录、配置、持久发送及 checkout 接收已有相应证据；ADR-0035 / 0037 B 真实采集与整链暂缓 | [交付专题](../topics.md#delivery-integration) |
| 运行与产品交付 | Web、自部署、升级恢复、单 Project 上下文包及补验已有指定环境证据；U27、生产容量、更广迁移、有效试用授权与独立持续使用未闭合 | [运行交付专题](../topics.md#product-delivery) |

内部契约、正式入口 / 浏览器、普通用户独立操作与持续使用分别判断。原逐切片矩阵保留于[历史快照](history/planning-structure-2026-11-01.md#完成线与成熟度)，原验收记录仍限定其版本、环境及范围。

## 已确认缺口

9 月 5 日的原始发现保留在[审阅记录](reviews/2026-09-05-project-review.md)。9 月 10 日已按 [ADR-0022](../adr/0022-transactional-activity-and-incoming-relations.md) 接通正常 Activity 更新及 Thread ← Decision、Decision ← Ticket 的反向发现；正式命令后的 HTTP 读取、撤权、失败回滚和重建并发已通过真实 PostgreSQL 测试，不在正常更新验收中手工重建。本轮 Chrome 浏览器已验证提案、接受、创建 Ticket、刷新后双向跳转与撤权清空，证据见[本轮记录](reviews/2026-09-10-activity-relations.md)。

当前剩余缺口：

1. **升级运维**：只读 schema readiness 已按 [ADR-0024](../adr/0024-read-only-schema-readiness.md) 完成；单机停机升级失败恢复已按 [ADR-0043](../adr/0043-single-host-upgrade-failure-recovery.md) 实施手册与隔离演练，故障与浏览器证据见[记录](reviews/2026-10-24-upgrade-failure-recovery.md)；独立操作者复验已按所有者要求暂缓，跨 schema 兼容窗口与生产编排仍待建立。
2. **使用入口**：首页已可直接浏览可读 Project / Channel；Team / Project / Channel 创建与首批成员配置已接通；部署后首次访问创建管理员已接通；Workspace owner 主动交接已接通，Project admin / 私密对象管理权交接与成员生命周期已接通；其他对象列表仍缺。Document 的原生 IME 复核和真实外部交付链仍独立推进。
3. **关系与时间线范围**：核心 Nexus 关系仍全量读取，未建立通用分页；Repository / Component 配置和 Ticket / Component 关联已有专用双向分页和 active 反向索引。Timeline 保留事件主要对象语义，Thread 不展示后续对象 Activity，其余交付反向关系尚未开放。
4. **证据边界**：自动化、真实数据库与本轮浏览器验收证明讨论到执行的局部闭环；Document 的剩余人工复核、真实交付链、完整 Golden Path 和真实团队持续使用仍分别验收。

## 近期执行顺序

继续按“对象发现 → 基础执行 → 试用准备”的产品方向推进；已完成的发现、身份 / 运维、迁移及工单切片不再作为下一任务。具体接续以下列五项为准，原顺位与完成表保留在[历史快照](history/planning-structure-2026-11-01.md#近期执行顺序)。

### 接下来的事项

1. **Decision 最小拒绝已完成，接续替代 / 复核设计**：[ADR-0048](../adr/0048-decision-lifecycle-and-rejection.md)的 proposed → rejected、正式三状态读取、当前权限、旧确认冲突 / 精确重试、Activity 与上下文 v4 已实施，具体证据和限制见[本轮记录](reviews/2026-11-02-decision-rejection.md)。后续先独立选择并冻结替代或复核的最小切片，明确理由存储、版本、替代链 / 历史与已有 Ticket / Document 的影响，再经所有者确认后实现；不自动改写 accepted 或已建执行对象。
2. **已完成切片转入有界观察**：上下文迁移补验、负责人及其并发 / 同名补验、优先级与项目内筛选均已交付，提交与验证映射见[当日收尾记录](reviews/2026-10-28-daily-closeout.md)。后续遇到实际 BFCache、原生选择控件键盘、团队使用或慢查询问题时定向复验；不重复全量测试冒充持续使用，也不把单 Project 合成查询计划当成生产容量。当前 schema 023 / Activity 11 / v4 包须 Go / Web / CLI 配套升级；真实实例操作仍单独授权。
3. **试用准备与使用观察分别验收**：确认[支持说明](../trial-support.md)的非生产数据范围、目标环境、联系人与退出安排，以及[授权待决事项](../evaluation-authorization-decisions.md) E01～E12；技术准入和有效授权均满足后才开放外部受控试用。届时观察讨论—决策—工单—文档追溯、维护者干预和成员交接，独立团队持续使用与生产容量不由 Agent 浏览器测试替代。
4. **保留阶段范围与停止线**：M1 文件、通知、审计运营、Attention Item、数据库最小权限和生产运维仍有未交付范围；本日实现不宣布整个 M1 或完整 Golden Path 退出。完整聊天、CRDT、Flutter 与通用插件平台仍服从原路线图门槛；更广迁移 profile 另行规划。
5. **继续暂缓**：ADR-0043 独立操作者 U27、真实 Jenkins ADR-0035 / ADR-0037 B 与原生中文 IME 人工验收。恢复时另行明确资源和验收范围；真实 Jenkins 恢复后再接续 Ticket 与具体构建的交付来源和结果追溯。

以上为当前接续事项，不创建自动任务或启动新服务。10 月 13 日的原收尾事实见[当日记录](reviews/2026-10-13-daily-closeout.md)，10 月 9 日的提交与原计划见[历史收尾记录](reviews/2026-10-09-daily-closeout.md)，当前顺位以本节五项为准。

### 交付链保留完成线与恢复顺序

Ticket 与 Component 人工关联已按 ADR-0034 实现，浏览器后退、显式刷新读 / 写权撤回及最终布局已补验。[ADR-0035 A](../adr/0035-durable-jenkins-terminal-delivery.md) 已获确认并实现：单 source / 单 worker 持久发送、不可变输入、重启预算、分类失败、精确恢复、只读状态与确认记录。没有业务 migration、公共 v1 协议或依赖变化；这不代表 controller 持续采集或漏采恢复完成。

以下为交付链恢复时的依赖与未完成项，不改变本页当前执行顺位：

1. **来源接收与发现**：[ADR-0037](../adr/0037-ci-run-repository-checkout-provenance.md)已于 2026-10-13 确认，A 的 sources / payload v2、不可变 binding、built-from、Activity 6、正式来源展示与双向发现已实施；验收证据见[原实施记录](reviews/2026-10-13-checkout-provenance.md)。真实 checkout 采集 B 继续暂缓，Ticket 具体交付关系再独立冻结；不按 Component 共属推导已交付。
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

- 关系分页、反向索引、查询容量与跨对象 Timeline；事务内 Activity 更新、重建并发及首批 direction 合同已由 ADR-0022 冻结；
- 其他协作对象发现与超出 ADR-0041 / 0042 / 0045 的应急入口；Project admin / 私密对象管理交接及成员生命周期已由 ADR-0045 冻结；基础配置和首访 owner 初始化已由 ADR-0026 / ADR-0027 冻结；
- Document 正式页面原生中文 IME 与持续使用证据；正式富文本与 CRDT、受控脱敏和可移植导出仍未完成；
- Jenkins 持续终态采集与业务实例配置、持久来源管理 / 安全审计、真实 checkout 采集与其余具体交付关系；
- PostgreSQL 支持矩阵、schema 兼容窗口、forward repair、数据库权限拆分和容量基线；
- Decision 替代、复核的独立历史、版本与 Ticket 执行状态交互；最小拒绝已按 ADR-0048 实施；
- 安全 Audit、消息 / 事件 / receipt 的保留和受控脱敏边界；
- `.nexus` 版本化格式、脱敏与导入映射；免费评估路径及授权签发 / 撤销规则；
- 后续插件运行方式、SDK / 插件许可证及搜索边界；OIDC 关联目标已由 ADR-0023 冻结，真实 provider、协议验签、浏览器与 Radish 联调延至未来独立切片，当前关闭 Radish 登录。

## 证据与历史

[2026-11-02 Decision 拒绝实施记录](reviews/2026-11-02-decision-rejection.md)保留本轮交付与分层验收；[2026-11-02 Decision 设计记录](reviews/2026-11-02-decision-lifecycle-design.md)保留本轮源码核对、首片取舍与确认边界；[2026-10-28 收尾记录](reviews/2026-10-28-daily-closeout.md)保留最近功能批次的提交与验证映射；[规划整理记录](reviews/2026-11-01-planning-alignment.md)记录导航整理及检查。此前详细流水和完整证据目录见[原状态快照](history/planning-structure-2026-11-01.md#证据与历史)；[2026-09-03 快照](history/2026-09-03-status.md)继续保留原历史边界。

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
