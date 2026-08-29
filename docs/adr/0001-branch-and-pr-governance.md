# ADR-0001：分支与 PR 治理

状态：已替代

日期：2026-08-27

替代者：[ADR-0008：`dev` 优先的单维护者开发拓扑](0008-dev-first-development-governance.md)

## 背景

RadishNexus 正从产品和架构文档阶段进入正式仓库建设。仓库需要同时满足三个目标：稳定分支有可验证门禁、单维护者阶段不会被强制审批锁死、未来多人协作时能渐进收紧。

Radish 家族项目已经形成 `master` 稳定分支、`dev` 集成分支、聚合质量门和稳定分支回流的共同模式。本项目采用这些通用不变量，但不复制任何兄弟项目的业务 CI、发布环境或目录所有权。

## 决定

### 分支模型

- `master` 是稳定分支，只通过 PR 进入。
- `dev` 是日常集成分支，普通主题 PR 默认合入 `dev`。
- 主题分支通常从 `dev` 创建。
- 稳定候选通过 `dev -> master` PR 发布。
- `master` 的任何变化都必须回流 `dev`。

```text
topic -> dev -> master -> dev
```

紧急修复可以从 `master` 分出并直接向 `master` 提 PR，但合并后必须建立 `master -> dev` 回流 PR。回流完成以 Git 祖先关系为准，不以“代码看起来一样”为准。

### 合并方式

- 仓库允许 merge commit 和 rebase merge，禁用 squash merge。
- `dev -> master` 默认使用 merge commit，保留候选批次和显式回流点。
- 小型主题 PR 可以使用 rebase merge，以维持易读线性历史。
- 使用 rebase merge 时必须接受提交 SHA 被重写，并以合入后的 SHA 做后续审计。

禁用 squash 不是否定小提交，而是希望评审后的合理提交边界能够保留。质量差的临时提交应在发起 PR 前由作者自行整理。

### 保护和质量门

`master` Ruleset 要求：

- 禁止删除和非快进更新；
- 必须通过 PR；
- 所有审阅对话已解决；
- 严格通过聚合状态 `Candidate Quality`；
- 允许 merge 和 rebase；
- 管理员仅可通过 PR 绕过；
- 当前最少批准数为 0，不要求 CODEOWNERS。

单维护者阶段把批准数设为 1 会形成无法自助解除的门禁，因此当前用 PR 说明、自动检查和对话解决代替形式审批。出现稳定第二维护者及真实目录所有权后，另立 ADR 评估批准数和 CODEOWNERS。

Ruleset 不使用提交消息正则。Conventional Commits 由仓库检查器检查普通提交，合并提交单独豁免，避免平台生成的标题与分支保护冲突。

### CI 契约

Ruleset 只绑定稳定上下文 `Candidate Quality`。工作流内部可以随着代码出现逐步增加 Go、Web、Rust、Flutter、插件、安全和许可证检查，而不频繁修改保护规则。

当前只运行已经真实存在的仓库卫生、检查器、M0 实验和正式 Go 服务检查。不存在的构建不设为必需状态。

实施记录：2026-08-28，M0 Go + PostgreSQL 核心契约实验进入仓库后，其单元测试、`go vet` 和真实 PostgreSQL 集成测试已加入聚合门；`Candidate Quality` 名称与 Ruleset 绑定保持不变。这是本 ADR 预留的内部检查演进，不改变分支或保护策略。

实施记录：2026-08-28，正式 `server/` module 进入仓库后，`Go Server` 单元测试、`go vet`、module 校验和真实 PostgreSQL migration / 权限 / 事务测试加入聚合门；稳定聚合名称和远端 Ruleset 要求不变。

## 未采用的方案

### 只有 `master`

阶段简单，但会把日常集成和稳定候选混在一起，无法表达 RadishNexus 后续跨模块纵向闭环的稳定门槛。

### Git Flow 的多层长期分支

当前没有并行维护多个版本的需求，`release/*`、`hotfix/*` 等长期流程会增加操作和回流成本。需要多版本维护时再单独决策。

### 所有 PR 强制 squash

历史紧凑，但会丢失经过评审的提交边界，并弱化 `dev -> master` 的候选批次表达。

### 立即要求批准和 CODEOWNERS

目前没有真实的第二审批人和稳定所有权团队，配置只会制造虚假安全感或合并死锁。

## 后果

正面影响：

- 稳定历史、集成历史和主题工作职责清晰；
- Ruleset 只依赖稳定聚合门，后续扩展 CI 不需频繁改保护规则；
- 当前单人可工作，未来多人可渐进收紧；
- 紧急修复和稳定发布都有明确回流路径。

成本和风险：

- 维护者必须主动完成 `master -> dev` 回流；
- merge 与 rebase 并存要求 PR 作者理解 SHA 和祖先关系；
- 批准数为 0 时，质量更多依赖检查、清晰 PR 和维护者纪律；
- 本地 JSON 模板不会自动启用远端保护，首次配置需人工核验。

## 远端启用顺序

1. 先合入仓库基线并确认 `Candidate Quality` 至少成功运行一次；
2. 创建并推送 `dev`，确认普通 PR 目标；
3. 在仓库设置中启用 merge commit 和 rebase merge，禁用 squash merge；
4. 导入或通过 API 创建 `master` Ruleset；
5. 用测试 PR 验证必需状态、对话解决、管理员绕过和回流；
6. 读取远端配置并与版本化模板比对。

这些动作会改变 GitHub 远端状态，不能由本地文档或 CI 自动执行，必须获得仓库管理员明确授权。

## 变更要求

修改本决策时，必须同步仓库治理、贡献指南、PR 模板、Ruleset JSON、Workflow 和仓库检查器。分支模型、必需检查名或合并方式发生实质变化时，应使用新 ADR 替代本记录。
