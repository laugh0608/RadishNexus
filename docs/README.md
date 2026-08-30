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
- 根 `LICENSE` 是当前核心内容许可条款的优先真相源；许可策略文档不能替代正式许可证或单独书面授权。
- 根 [AGENTS.md](../AGENTS.md) 与 [CLAUDE.md](../CLAUDE.md) 是逐字一致的执行入口；详细理由仍以治理专题和 ADR 为准。

## 当前边界

当前已经建立仓库治理、正式 Go 服务基础、Thread → Decision → Ticket application service、已验证 Jenkins delivery → CI Run、显式 staging Deployment、可重建 Activity、Decision / Ticket / CI Run / Deployment Nexus View application query、本地账号与公共 Session transport、首个 Deployment Nexus View 业务读取端点，以及可运行的 React Web 代表原型和 canonical Deployment 页面，但不代表以下产品能力已经实现：

- 可供真实团队日常使用的完整 Web App、Web Shell 或导航；
- 完整业务 HTTP API、authenticated Web Shell、OIDC / MFA 等完整身份能力或浏览器到 PostgreSQL 的全链路产品联调；
- 可安装插件或 Plugin SDK；
- Flutter 移动端或 PC 客户端；
- Jenkins Webhook 接入、签名验证、Secret 管理、插件 runtime，或 GitLab 等其他外部系统集成；
- 部署执行引擎、production Deployment、跨版本升级、生产级备份策略和高可用能力；
- 已完成专业法律审查的许可证文本。
