# RadishNexus 文档

本目录保存 RadishNexus 的产品、架构、授权、治理和阶段真相。当前文档用于冻结已经确认的方向，避免后续讨论把“已决定事项”“当前建议”和“未来可能性”混为一谈。

## 推荐阅读顺序

1. [产品定义](product-definition.md)：说明项目服务谁、解决什么问题，以及明确不做什么。
2. [领域模型](domain-model.md)：定义 Project、Initiative、Component、Decision、Environment、EntityLink 等核心概念。
3. [Golden Path](golden-path.md)：定义最薄纵向原型、演示故事和验收标准。
4. [决策基线](decision-baseline.md)：记录已确认且不得静默漂移的决策。
5. [总体架构](architecture/overview.md)：说明技术栈、模块边界和部署形态。
6. [核心实体、授权与事件契约](architecture/core-contracts.md)：说明稳定引用、权限解析、Outbox 和 Activity 的 M0 契约基线。
7. [插件系统](architecture/plugin-system.md)：说明为什么做插件、哪些能力适合插件，以及停止线。
8. [许可与分发策略](licensing-strategy.md)：说明核心、SDK、插件和免费授权的边界。
9. [产品路线图](roadmap.md)：说明从 Web-first MVP 到 Flutter 客户端的推进顺序。
10. [当前状态](status/current.md)：唯一的当前阶段、下一步和开放问题入口。
11. [仓库治理](governance/README.md)：说明分支、PR、Ruleset、Agent 和文档协作规则。
12. [架构决策记录](adr/README.md)：解释长期工程与治理取舍及其后果。
13. [开发指南](development/README.md)：说明跨语言工程、测试、安全和兼容性标准。
14. [视觉方向与页面参考](design/visual-direction.md)：Radish 家族视觉来源、AFFiNE / Mattermost / AppFlowy 参考分工及待评审的页面建议。
15. [工作台与 Document 代表页设计](design/workbench-v1.md)：Pen 原生设计源、桌面 / 手机预览、状态映射及静态检查边界。

客户端连接方向见[官方客户端与实例连接规划](architecture/client-server-connection-plan.md)及 [ADR-0036](adr/0036-official-client-instance-connection.md)：同一官方安装包运行时支持官方端点或用户自部署后端，无需重新编译，Web 保持各实例同源交付。

试用准备见[受控试用支持说明](trial-support.md)、[评估授权待决事项](evaluation-authorization-decisions.md)及 [ADR-0043：单机停机升级与失败恢复](adr/0043-single-host-upgrade-failure-recovery.md)。支持与授权仍为待确认设计；ADR-0043 已接受，执行见[运维手册](../deploy/upgrade-recovery.md)，实际结果与缺口见[2026-10-24 验收记录](status/reviews/2026-10-24-upgrade-failure-recovery.md)。技术实施不代表试用已开放或已授予运行权。

## 文档职责

- 稳定产品定位只写在 `product-definition.md`。
- 稳定业务概念、关系语义和对象职责只写在 `domain-model.md`。
- Golden Path 的演示故事与验收标准只写在 `golden-path.md`。
- 已确认决策只写在 `decision-baseline.md`，修改时必须写明原因和日期。
- 架构原则写在 `architecture/`，实现细节应在真正开发后通过 ADR 补充。
- 稳定引用、授权解析、事件和投影的跨模块技术契约写在 `architecture/core-contracts.md`，其接受状态由对应 ADR 管理。
- 长期工程和治理取舍写在 `adr/`；已接受结论用新 ADR 替代，不静默重写历史。
- 分支、PR、Ruleset、Agent 和文档维护规则写在 `governance/`。
- 跨语言工程基线写在 `development/`；模块实现说明应随未来代码就近维护。
- 当前推进焦点只写在 `status/current.md`，其他文档不重复维护易过期进度。
- 已结束批次与状态快照归入 `status/history/`，结构性审阅证据归入 `status/reviews/`；标明日期、代码基线、验证范围和历史性质，不作为当前推进或授权依据。
- 根 `LICENSE` 是当前核心内容许可条款的优先真相源；许可策略文档不能替代正式许可证或单独书面授权。
- 根 [AGENTS.md](../AGENTS.md) 与 [CLAUDE.md](../CLAUDE.md) 是逐字一致的执行入口；详细理由仍以治理专题和 ADR 为准。

结构化迁移首片见 [ADR-0044](adr/0044-portable-project-context-package.md) 与[上下文迁移手册](../deploy/context-package.md)，其单 Project 范围不等于整个 Workspace 或研发上下文迁移。

成员生命周期、Project admin 与私密对象管理责任见 [ADR-0045](adr/0045-membership-lifecycle-and-management-handoff.md)；会话准入、再加入和内容隔离由同一合同约束。

## 状态与验证入口

- [当前状态](status/current.md)：能力成熟度、已确认缺口、近期顺序和停止线。
- [2026-10-24 提交回顾与文档收尾](status/reviews/2026-10-24-daily-closeout.md)：升级恢复、上下文迁移、成员生命周期三笔实现的源码核对及接续建议。
- [2026-10-24 成员生命周期与管理交接](status/reviews/2026-10-24-membership-lifecycle.md)：会话代次、私密管理、并发故障、恢复和浏览器证据。
- [2026-10-24 升级失败恢复](status/reviews/2026-10-24-upgrade-failure-recovery.md)：历史工件停写、失败诊断、空目标恢复及独立操作者缺口。
- [2026-10-24 Project 上下文迁移](status/reviews/2026-10-24-portable-context-package.md)：版本化包、历史来源、目标权限、独立往返与部分覆盖边界。
- [2026-10-18 提交回顾与文档收尾](status/reviews/2026-10-18-daily-closeout.md)：决策发现、账户恢复、owner 交接和试用准备设计的提交映射、源码文档核对与下一切片。
- [2026-10-18 Workspace owner 主动交接](status/reviews/2026-10-18-workspace-owner-handoff.md)：角色代次、邀请任期、历史查询、并发与旧工件恢复。
- [2026-10-18 本地账户受控恢复](status/reviews/2026-10-18-controlled-account-recovery.md)：独立 CLI、全账户 Session 撤销、审计与隔离恢复。
- [2026-10-18 Project 决策只读发现](status/reviews/2026-10-18-project-decision-discovery.md)：当前权限、稳定分页、来源隔离与浏览器恢复。
- [2026-10-13 提交回顾与文档收尾](status/reviews/2026-10-13-daily-closeout.md)：checkout 来源、工单发现与状态流转的提交映射、文档核对和接续建议。
- [2026-10-13 Ticket 最小状态流转](status/reviews/2026-10-13-ticket-status-transitions.md)：四状态、精确重试、升级恢复与权限验收。
- [2026-10-13 Project 工单只读发现](status/reviews/2026-10-13-project-ticket-discovery.md)：正式入口、分页、普通成员发现与撤权恢复。
- [2026-10-13 checkout 来源 A](status/reviews/2026-10-13-checkout-provenance.md)：来源接收、不可变关系与双向发现；真实采集 B 暂缓。
- [2026-10-09 提交回顾与文档收尾](status/reviews/2026-10-09-daily-closeout.md)：持久发送、可信采集与双边清理的提交映射、文档核对和次日接续建议。
- [2026-10-09 双边显式清理](status/reviews/2026-10-09-jenkins-cleanup.md)：双方确认、7 天保留、紧凑索引、进程中断与恢复；真实联调暂缓。
- [2026-10-09 可信 collector 代码与离线验证](status/reviews/2026-10-09-jenkins-collector.md)：周期补采、历史缺口、故障与交接契约；真实联调暂缓。
- [2026-10-09 浏览器补验与持久终态快照发送](status/reviews/2026-10-09-delivery-continuation.md)：Ticket / Component 撤权补验、ADR-0035 A 实现与恢复证据；B 后续见采集记录。
- [2026-10-06 提交回顾与文档收尾](status/reviews/2026-10-06-daily-closeout.md)：Repository / Component 与 Ticket / Component 两段人工关联、文档核对及次日接续建议。
- [2026-10-01 提交回顾与文档收尾](status/reviews/2026-10-01-daily-closeout.md)：依赖修复、阶段晋级、组件环境配置与授权管理，以及次日接续建议。
- [2026-09-26 提交回顾与文档收尾](status/reviews/2026-09-26-daily-closeout.md)：Document、工作台视觉、CI Run、Jenkins 与 staging 记录的提交映射和文档核对。
- [2026-09-10 提交回顾与文档收尾](status/reviews/2026-09-10-daily-closeout.md)：Activity、身份、readiness 与发现入口的提交映射和文档审阅结果。
- [2026-09-05 项目审阅](status/reviews/2026-09-05-project-review.md)：源码发现、建议与实际验证范围。
- [2026-09-03 状态快照](status/history/2026-09-03-status.md)：此前阶段事实及浏览器、数据库、部署和编辑器验收流水；旧计划不再生效。

正式产品完成线只看当前状态与对应验收证据；专题中的目标模型、候选技术与历史测试通过不自动表示公共入口、团队使用或生产能力已经完成。
