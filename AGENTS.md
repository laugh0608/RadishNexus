# RadishNexus 协作约定

本文件为人工协作者与 AI 协作者提供启动级长期约束。它只约束 RadishNexus，不复制 Radish 家族其他项目的实现细节，也不承载当前阶段、临时门禁、历史记录或专题实现细节。

## 定位与优先级

- `docs/` 是正式项目文档源；当前阶段、近期顺位、停止线和“当前不做”以 `docs/status/current.md` 为准，产品、领域、架构、治理和许可边界以对应专题或 ADR 为准。
- 项目所有者在当前任务中的明确要求优先于默认协作流程；若要求会改变数据模型、权限、公共协议、许可证、依赖或远程状态，仍须先说明影响并确认范围。
- 只读取当前任务需要的最小文档链；需要追溯事实时再进入 ADR、验证记录或历史材料，不默认展开全部文档或兄弟项目。
- 文档、实现和自动化不一致时，先判断哪一方过期，再在任务范围内统一修正；不通过放宽检查或增加 fallback 掩盖漂移。

## 称呼与语言

- 对话开始或结束总结时称呼项目所有者为 `萝卜SAMA`。
- 默认使用中文讨论、说明和编写项目文档。
- 代码、命令、路径、配置键、类型名、协议字段和外部项目名保留原文。

## 任务启动与协作原则

1. 开始任务先检查 `git status`；实际分支、ahead / behind、暂存区或工作树与描述不一致时先报告，不用 reset、checkout 或清理命令制造预期状态。
2. 区分本次改动、用户已有改动和并行改动；不覆盖、不回滚、不顺手提交任务范围外内容。
3. 明确请求属于回答、诊断、设计、实现、治理、验证还是发布；一种授权不自动扩大为另一种操作。
4. 先查找现有文档、实现、测试、脚本和命名惯例；没有充分理由时不另造平行入口、重复 helper 或第二套配置口径。
5. 需求明确且风险可控时直接完成；不同解释会实质改变用户行为、数据、兼容性、授权或外部影响时，先说明关键判断并请求选择。
6. 优先解决根因和边界问题，交付小而完整、可复验的纵向切片；不以占位、演示路径、默认成功或隐藏错误冒充完成。
7. 控制修改范围，不混入无关重构、格式化、依赖升级或目录搬迁。

详细工作区、实施、验证和交接规则见 [Agent 协作与执行规则](docs/governance/agent-collaboration.md)。

## 执行与授权边界

可在任务范围内直接执行：

- 读取、搜索和编辑仓库文件；
- 文本卫生、格式化、静态分析、构建和测试；
- `git status`、`git diff`、`git log` 等只读 Git 操作；
- 用户明确要求后的精确路径暂存和提交。

以下操作必须获得当前任务的明确授权，并在执行前说明目标、主要副作用和清理或回滚方式：

- 安装、升级或移除依赖，以及会改变 lockfile 的命令；
- 启动长期运行的服务、桌面应用或需要人工交互的进程；
- 写入 GitHub Rulesets、仓库设置、Secrets、环境或其他远程状态；
- push、创建 PR、创建 tag / Release、发布、部署或发送外部消息；
- 修改系统配置、权限、证书、密钥链或全局工具；
- 破坏性 Git 操作、共享分支历史重写或删除数据。

授权只覆盖已经说明的动作、目标和影响；历史会话授权不得沿用。凭据、Token、私钥、恢复码和真实敏感输入不得进入命令参数、日志、截图、fixture、提交或对话摘录。

## 任务文档路由

| 任务 | 优先读取 |
| --- | --- |
| 当前阶段、下一步、临时门禁 | [当前状态](docs/status/current.md) |
| 产品定位、用户价值和长期边界 | [产品定义](docs/product-definition.md) |
| 核心对象、关系和权限语义 | [领域模型](docs/domain-model.md) |
| Golden Path 与纵向原型 | [Golden Path](docs/golden-path.md) |
| 架构、数据、事件和部署 | [总体架构](docs/architecture/overview.md) |
| 插件、Host API 和隔离 | [插件系统](docs/architecture/plugin-system.md) |
| 路线图和阶段门槛 | [产品路线图](docs/roadmap.md) |
| 分支、PR、CI 和 Ruleset | [仓库治理](docs/governance/repository-governance.md)、[ADR 0001](docs/adr/0001-branch-and-pr-governance.md) |
| Agent 协作、工作区和交接 | [Agent 协作与执行规则](docs/governance/agent-collaboration.md) |
| 文档职责、ADR 和记录归位 | [文档治理](docs/governance/documentation-governance.md) |
| 工程、代码、测试和依赖 | [工程规范](docs/development/engineering-standards.md) |
| 许可证、第三方材料和分发 | [许可与分发策略](docs/licensing-strategy.md) |

完整索引见 [docs/README.md](docs/README.md)。询问“下一步做什么”时先读当前状态；不足以判断长期边界时再沿索引进入专题。

## 项目长期边界

- 第一正式产品形态是 React + TypeScript Web App；Web 闭环稳定前不启动 Flutter 客户端。
- 后续 Flutter 统一覆盖移动端和 PC；不采用 Tauri。
- 服务端以 Go 为主要业务语言；Rust 只进入有明确隔离、性能、跨端复用或安全收益的边界。
- 初期采用模块化单体和 PostgreSQL Outbox，不以微服务数量衡量架构成熟度。
- Decision、EntityLink、Activity、权限和审计是产品差异化基础；不能退化为正文链接和无来源摘要。
- Project、Initiative、Component、Repository、Environment、CI Run 和 Deployment 保持领域语义分离。
- Chat、Decision、Ticket、Document 和研发上下文是内建能力；Jenkins 等外部系统通过受控插件或连接器扩展。
- 自部署核心路径不依赖官方云，不按成员数量建立产品或授权限制。

## 产品、安全与数据红线

- 引用不会授予目标对象权限；搜索、通知、Activity、Attention Item、导出和 AI 插件必须复用同一权限语义。
- 自动摘要只能形成草案；不得自动确认 Decision、审批、Deployment 或其它高风险事实。
- 插件默认无权限，不直接访问核心业务表；Secrets 不通过普通 API 返回明文。
- Webhook 和外部写操作必须考虑来源校验、幂等、重放、超时、失败隔离、权限和审计。
- 构建成功不等于已经部署；生产环境操作需要独立权限、明确确认和可追溯记录。
- 备份、恢复和 `.nexus` 导出默认不包含 Secrets、Token 或仅对原实例有效的授权材料。
- 不提交真实凭据、个人数据、私密工作区内容、未脱敏日志或无权提供的第三方代码与资产。

## 实现、验证与文档

- 抽象必须减少真实重复、隔离稳定边界、降低复杂度或表达领域概念；不为“未来可能需要”建立空泛 manager、helper、adapter 或多层框架。
- 错误必须保留上下文和真实原因；不吞异常、不伪造成功、不用未声明 fallback 掩盖契约问题。
- 新增依赖前优先使用标准库和现有能力，并说明用途、版本、维护、许可证、供应链和替代方案。
- 按风险执行定向测试、构建、类型检查、Lint、静态分析和 `./scripts/check-repo.sh`；不要求每次改动都跑尚未相关的全量门禁。
- PR 和交付只记录实际执行过的验证，明确未执行、受阻和需要人工复核的内容。
- 改变产品、领域、架构、权限、公共协议、流程、许可或仓库规则时，同步更新对应真相源和自动化合同。
- 历史流水、批次证据和临时门禁不堆入根入口或长期专题。

## Git 约束

- `master` 是受保护稳定主线，`dev` 是常态集成分支；普通主题分支默认向 `dev` 发 PR。
- `master` 只通过 PR 接收阶段性 `dev` 晋级或明确 hotfix；禁止直接 push、删除和 force push。
- 默认分支允许 merge commit 与 rebase merge，禁用 squash merge；阶段 PR 优先 merge commit。
- 任何进入 `master` 的变更合并后，必须先把最新 `master` 回流到 `dev`，再开始下一轮开发。
- 共享 `dev` 不通过 rebase、reset 或 force push 伪造同步。
- 提交遵循 Conventional Commits，使用真实贡献者 Git 身份，不添加 AI 协作者署名。
- 远程 Ruleset、仓库 Merge options 和 Secrets 的写入必须单独授权；仓库中的 JSON 模板不代表远程已经生效。

## 根入口维护

一条规则只有同时满足以下条件才进入 `AGENTS.md` / `CLAUDE.md`：

1. 跨任务成立；
2. 跨开发阶段成立；
3. 必须在读取专题前立即生效，否则容易造成高风险或大范围错误；
4. 无法只通过任务路由和对应专题可靠承载。

阶段状态、临时门禁、易过期命令、批次事实和专题细节更新其正式真相源，不复制回根入口。两份入口必须逐字一致；修改任一文件时同步另一份并运行仓库检查。
