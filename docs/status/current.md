# RadishNexus 当前状态

状态日期：2026-08-27

## 当前阶段

产品定义、架构基线与仓库治理基线。

当前已经建立本地和 GitHub 远端仓库、`master` / `dev` 分支、协作规则、GitHub 模板、无第三方依赖的仓库检查器与 `Candidate Quality` 质量门；`master` Ruleset 已在远端启用。尚未实现 Web App、服务端、插件或客户端。

## 当前结论

- 项目名确定为 `RadishNexus`。
- 产品定位为自部署优先的研发团队沟通、协作与交付枢纽。
- 自部署使用不按席位计费或限额。
- 核心采用 source-available 和单独书面授权模式；书面授权可以免费。
- SDK、公共协议和插件开放源码，并使用各自独立许可证。
- Web App 是第一产品形态，采用 React + TypeScript。
- 前期不开发移动端和 PC 客户端。
- 后续客户端统一采用 Flutter，不采用 Tauri。
- 服务端以 Go 为主，Rust 只进入有明确收益的边界。
- 初期采用模块化单体。
- 插件系统按真实收益渐进建设，Jenkins 是第一个验证场景。
- 聊天、工单和文档作为内建模块，不为了插件化而插件化。
- Decision 是一等业务对象，保留问题、结论、理由、证据和替代关系。
- Project、Initiative、Component、Repository 和 Environment 已明确分工。
- CI Run 与 Deployment 是不同交付事实。
- EntityLink 和 Activity 是上下文关联与 Nexus View 的基础。
- 在横向补全各模块前，先完成 Golden Path 纵向原型。
- 仓库采用 `master` 稳定分支、`dev` 集成分支和主题分支；普通 PR 默认进入 `dev`。
- `master` 允许 merge commit 和 rebase merge，禁用 squash merge，并要求变化回流 `dev`。
- `Candidate Quality` 作为稳定聚合质量门；当前只包含仓库卫生和检查器测试。
- GitHub 远端默认分支为 `master`，`master` Ruleset 已启用并要求 PR、严格状态检查和已解决对话。
- GitHub Private vulnerability reporting 已启用；未修复漏洞优先通过仓库 Security Advisory 私下报告，入口和备用联系方式以 [SECURITY.md](../../SECURITY.md) 为准。

## 当前文档基线

- [产品定义](../product-definition.md)
- [领域模型](../domain-model.md)
- [Golden Path](../golden-path.md)
- [决策基线](../decision-baseline.md)
- [总体架构](../architecture/overview.md)
- [插件系统](../architecture/plugin-system.md)
- [许可与分发策略](../licensing-strategy.md)
- [产品路线图](../roadmap.md)
- [仓库治理](../governance/README.md)
- [ADR-0001：分支与 PR 治理](../adr/0001-branch-and-pr-governance.md)
- [开发指南](../development/README.md)

## 下一步建议

下一次继续推进时，优先完成 M0 和 M0.5，而不是直接生成完整工程：

1. 根据领域模型冻结 Project、Initiative、Component、Decision、Environment 和 EntityLink 的最小字段；
2. 设计稳定实体引用、权限检查、事件 envelope 和 Activity 投影；
3. 选择 Go Web 基础栈并建立 Golden Path 所需的最小服务；
4. 用 React 原型实现 Nexus View，而不是先铺完整聊天界面；
5. 实现“Thread → Decision → Ticket → Jenkins CI Run → staging Deployment”纵向实验；
6. 验证重复 Webhook、私密引用、外部故障和备份恢复；
7. 对候选文档编辑器和协同方案做独立原型，不让 CRDT 阻塞 Golden Path；
8. 起草低摩擦、可离线验证的免费评估与使用授权模板，并安排法律复核。

## 开放问题

- Go 服务端具体库；
- 文档编辑器和 CRDT；
- 首版插件运行方式；
- Initiative、Component 与 Project 的首版导航表现；
- Decision 的确认权限、复核周期和替代交互；
- `.nexus` 上下文包的开放格式与脱敏规则；
- SDK 和插件的具体开放源码许可证；
- 认证第一阶段只做本地账号，还是同时加入 OIDC；
- 消息和文档搜索边界；
- 免费书面授权的签发与撤销规则；
- 免费评估是否公开授予，或使用离线自助签发；

## 停止线

在对象、权限、事件和插件最小实验完成前：

- 不创建大量微服务；
- 不同时开发 Flutter；
- 不建设插件市场；
- 不承诺完整离线文档；
- 不接入大量 CI/CD 平台；
- 不建设完整软件目录、图数据库或战略项目组合模块；
- 不把 AI 生成内容直接确认为 Decision、状态更新或外部操作；
- 不为了展示功能数量扩张到音视频、代码托管或复杂项目组合管理。
