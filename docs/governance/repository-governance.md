# RadishNexus 仓库治理基线

状态：已采用

日期：2026-08-29

## 目标

这套基线让仓库在只有一名维护者时保持低摩擦，同时为多人协作留下可逐步收紧的路径。它只吸收 Radish 兄弟项目中已经证明通用的治理模式，不继承任何项目特有的业务检查、发布流程或技术栈假设。

## 治理资产

| 资产 | 职责 |
| --- | --- |
| `AGENTS.md` / `CLAUDE.md` | 可执行的 Agent 协作规则；两份文件必须逐字节一致 |
| `CONTRIBUTING.md` | 人类贡献者入口、分支和验证说明 |
| `.github/PULL_REQUEST_TEMPLATE.md` | 统一 PR 证据、风险和回流检查 |
| `.github/ISSUE_TEMPLATE/` | 缺陷报告、变更提案和安全问题分流 |
| `.github/workflows/pr-check.yml` | 稳定的聚合质量门 `Candidate Quality` |
| `.github/rulesets/master-protection.json` | `master` 保护策略的版本化模板 |
| `scripts/check-repo.*` | 本地与 CI 共用的仓库静态约束 |
| `docs/adr/` | 对长期工程和治理取舍的不可变记录 |

模板进入仓库不代表 GitHub 远端已经启用对应设置。远端 Ruleset、分支、合并方式和状态检查必须按[启用说明](../../.github/rulesets/README.md)单独核对。

## 分支职责

- `master`：稳定候选与正式发布历史，只接受 PR，不直接提交。
- `dev`：单维护者阶段的默认日常开发与集成分支，串行常规任务直接在这里推进。
- 主题分支或 worktree：只在项目所有者明确要求、外部贡献、并行写入、高风险隔离或 hotfix 等有真实收益时使用；普通主题 PR 以 `dev` 为目标。
- 紧急修复：可以从 `master` 创建，但合入 `master` 后必须立即回流 `dev`。

默认流向为：

```text
日常串行开发 -> dev -> master -> dev
```

需要隔离时可以使用 `topic/worktree -> dev`，但 Agent 不得只因自身默认流程为每个切片自动创建 `codex/*` 或其它临时分支。`master` 不能成为只向前发布、从不回流的孤立历史；每次 `master` 变化后，都应通过明确的祖先关系证明把变化带回 `dev`。

## 提交约束

- 使用 Conventional Commits：`type(scope): summary` 或 `type: summary`。
- 推荐类型：`feat`、`fix`、`docs`、`refactor`、`test`、`chore`、`ci`、`build`、`perf`、`revert`。
- 一次提交只表达一个可解释的意图；生成文件和来源文件尽量同提交更新。
- 提交作者只能是真实参与者。不得添加 AI、Agent 或工具作为共同作者。
- 合并提交不受 Conventional Commits 标题限制；远端 Ruleset 不额外使用提交消息正则，以免与平台生成的合并提交冲突。

## PR 规则

PR 不是普通串行开发进入 `dev` 的强制入口。以下情况以 `dev` 为 PR 目标：

- 外部贡献；
- 多人或多 Agent 并行隔离；
- 高风险实验或大范围重构需要独立审查；
- 项目所有者明确要求远端合并前证据。

只有以下变更以 `master` 为目标：

- 已在 `dev` 验证的稳定候选；
- 明确的紧急修复；
- 仓库初始化时尚不存在 `dev` 的首个治理基线。

每个 PR 至少说明：

1. 目标与明确不在范围内的事项；
2. 受影响的产品、领域、架构、安全、许可或运维边界；
3. 实际执行的验证及其结果；
4. 风险、失败方式和回滚/恢复路径；
5. 是否需要同步文档、ADR、Ruleset 或 `master -> dev` 回流。

涉及 Decision、EntityLink、Activity、权限、私密数据、Webhook、插件能力或 Deployment 事实的变更，必须在 PR 中显式说明语义和越权失败路径，不能只写“已测试”。

## `dev` 日常开发与检查

- 当前单维护者阶段不保护 `dev`，普通提交可以直接进入 `dev`。
- 普通 `push -> dev` 不自动触发 CI；直接开发者必须执行与改动风险匹配的本地验证。
- 目标为 `dev` 的 PR 继续运行 `PR Checks`，为外部贡献、并行任务和明确评审提供反馈，但 `dev` 不绑定 required checks。
- 直接开发不授权远端写入；push、PR 和仓库设置仍须按当前任务单独授权。
- 出现稳定第二维护者、持续外部贡献、并行自动化写入或直接提交导致回归时，重新评估 `dev` Ruleset 和 push CI。

## 合并策略

- 允许 merge commit 和 rebase merge。
- 禁用 squash merge，避免把审阅后的提交结构压成无法区分的单提交。
- `dev -> master` 默认优先 merge commit，让稳定候选的边界和回流点可见。
- 可选的主题 PR 可以 rebase merge；直接进入 `dev` 的提交不为制造 PR 记录而改写 SHA。
- 删除源分支不等于完成回流；以提交祖先关系为准。

选择 rebase merge 时，平台会生成新的提交 SHA。因此后续回流和审计不能只比较原主题分支 SHA，应检查实际合入后的历史。

## `master` Ruleset 基线

| 项目 | 当前策略 | 原因 |
| --- | --- | --- |
| 目标 | 仅 `refs/heads/master` | 保持 `dev` 日常开发与可选 PR 的低摩擦 |
| 删除 / 强推 | 禁止 | 保留稳定历史 |
| 进入方式 | 必须经 PR | 让检查、说明和讨论集中 |
| 状态检查 | 严格要求 `Candidate Quality` | 使用稳定聚合上下文，内部检查可演进 |
| 对话 | 必须全部解决 | 防止带着未处理意见合入 |
| 批准数 | 当前为 0 | 单维护者阶段不制造自我审批死锁 |
| CODEOWNERS | 当前不要求 | 没有真实所有权团队时不建立虚假门禁 |
| 合并方式 | merge / rebase | 与仓库策略一致 |
| 管理员绕过 | 仅经 PR | 紧急情况下仍保留 PR 证据 |

这些数值不是永久承诺。出现稳定的第二维护者和明确的目录所有权后，应在 ADR 中评估把批准数调为 1 并引入 CODEOWNERS；在此之前不提前设置。

## CI 契约

Ruleset 只依赖一个稳定聚合状态 `Candidate Quality`。当前聚合以下真实检查：

- `Repo Hygiene`：运行仓库检查器，验证文本、链接、模板和治理契约；
- `Repository Checker Tests`：验证检查器本身的关键行为；
- `M0 Core Contracts`：运行 Go 单元测试、`go vet` 和真实 PostgreSQL 事务/约束集成测试。
- `Go Server`：运行正式服务单元测试、`go vet`、module 校验和真实 PostgreSQL migration / 权限 / 事务集成测试。
- `Web App`：运行 React / TypeScript 格式、Lint、状态测试、production build、依赖来源与许可证检查。

当前工作流只在目标为 `dev` / `master` 的 PR 和人工 `workflow_dispatch` 上运行；普通 `dev` push 不触发。插件或 SDK 真正进入仓库后，再把对应格式化、单元测试、构建、安全和许可证检查接入工作流，并保持 `Candidate Quality` 名称稳定。不得为尚不存在的目录或工具创建必需状态检查。

## 变更同步矩阵

| 变更 | 必须同步 |
| --- | --- |
| 分支、合并或 PR 策略 | ADR、治理文档、PR 模板、Ruleset、检查器 |
| 必需状态检查名称 | Workflow、Ruleset JSON、Ruleset 说明 |
| Agent 规则 | `AGENTS.md` 与 `CLAUDE.md` 同步修改 |
| 产品或领域语义 | 对应真相源、必要 ADR、测试/契约 |
| 许可边界 | 根许可证、许可策略、贡献说明；必要时法律复核 |
| 新语言或应用 | 工程标准、忽略文件、CI 和本地检查入口 |

## 明确停止线

- 不从兄弟项目复制业务 CI、发布密钥、环境名称或部署脚本。
- 不把尚未存在的测试、构建或扫描任务设置为必需检查。
- 不在没有真实所有权关系时创建 CODEOWNERS。
- 不因模板已经提交就宣称远端 Ruleset 已启用。
- 不通过本地规则文件执行远端变更；远端写操作必须另行获得授权。
- 不在没有可发布产品和恢复演练前创建自动发布流水线。
