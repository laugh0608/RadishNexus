# RadishNexus

> 面向研发团队的、自部署优先的沟通、协作与交付枢纽。

RadishNexus 是 Radish 家族中的团队协作项目。它把私聊、团队频道、决策、工单、协作文档和 CI/CD 上下文放进同一个工作空间，让一次讨论能够持续关联到结论、任务、软件组件、构建、发布和复盘，而不是把多个独立产品简单拼接在一起。

项目当前处于 M0 核心契约实验阶段，尚未进入正式产品实现。稳定引用、事件 Outbox 和 Activity 重建已经有可丢弃的 Go + PostgreSQL 技术实验；第一产品形态仍是 React + TypeScript Web App，Flutter 客户端在 Web 产品达到阶段门槛后再启动，并统一覆盖移动端与 PC 端。

## 已确认基线

- 自部署版不按成员、频道、项目或插件数量设置席位限制。
- 核心代码采用自有、源代码可见的授权模式；获得项目所有者书面授权后可以免费使用。
- Plugin SDK、公共协议和插件采用开放源码许可证，具体组件以各自目录中的许可证为准。
- 插件化服务于明确收益，不为了形式上的“万物皆插件”增加开发负担。
- Jenkins 等外部系统集成优先作为插件；聊天、工单、文档等主产品能力优先作为内建模块。
- Decision 是一等业务对象，必须保留问题、结论、理由、证据和替代关系。
- Project 只表示协作和权限边界；Initiative、Component、Repository 和 Environment 分别表达目标、软件资产、代码来源和部署目标。
- 正式横向补全聊天、工单和文档前，先用 Golden Path 原型验证“讨论 → 决策 → 执行 → 构建 → 部署”的纵向闭环。
- 后端以 Go 为主要业务实现语言，Rust 用于确有复用、隔离、性能或跨端价值的组件。
- 前端采用 React + TypeScript；前期只建设 Web App，不同时维护客户端。
- 后续客户端采用 Flutter，统一覆盖移动端和 PC 端；不采用 Tauri。

以上决策的精确边界以[决策基线](docs/decision-baseline.md)为准。

## 文档入口

- [文档索引](docs/README.md)
- [产品定义](docs/product-definition.md)
- [领域模型](docs/domain-model.md)
- [Golden Path](docs/golden-path.md)
- [决策基线](docs/decision-baseline.md)
- [总体架构](docs/architecture/overview.md)
- [插件系统](docs/architecture/plugin-system.md)
- [许可与分发策略](docs/licensing-strategy.md)
- [产品路线图](docs/roadmap.md)
- [当前状态](docs/status/current.md)
- [仓库治理](docs/governance/README.md)
- [架构决策记录](docs/adr/README.md)
- [开发指南](docs/development/README.md)

## 协作与仓库治理

- `master` 是稳定分支，`dev` 是日常集成分支；普通变更默认通过主题分支向 `dev` 提交 PR。
- `master` 只接受 PR，允许 merge commit 与 rebase merge，禁用 squash merge；合入后必须回流 `dev`。
- `Candidate Quality` 是稳定聚合质量门，当前验证仓库卫生、检查器以及 M0 Go + PostgreSQL 核心契约实验，后续随真实应用接入各技术栈检查。
- 仓库中的 Ruleset JSON 是版本化模板，不表示 GitHub 远端设置已经生效。

开始贡献前请阅读 [CONTRIBUTING.md](CONTRIBUTING.md) 和 [协作约定](AGENTS.md)。安全问题按 [SECURITY.md](SECURITY.md) 私下报告。当前本地基线可运行：

```bash
./scripts/check-repo.sh
```

## 名称

`Nexus` 表示连接点和枢纽。该名称强调 RadishNexus 的核心不是并列提供聊天、工单、文档和 CI/CD 页面，而是让这些对象共享权限、事件、引用和完整上下文。

RadishLink 已用于离线自组网通信项目，因此本项目不再使用 `Link`、`Connect` 等容易混淆的名称。规划子域名可使用 `nexus.radishx.com`，但域名和公开发布安排尚未冻结。

## 许可

仓库根内容当前采用 [RadishNexus Source-Available License](LICENSE)。该许可证不是开放源码许可证；默认只允许在授权平台查看和学习，实际部署、复制、修改、再分发及商业使用需要项目所有者另行书面授权。

未来的 SDK、协议和插件应在各自目录放置独立的开放源码许可证，不能仅依赖根许可证说明。
