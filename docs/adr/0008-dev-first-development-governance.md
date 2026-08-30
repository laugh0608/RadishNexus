# ADR-0008：`dev` 优先的单维护者开发拓扑

状态：已接受

日期：2026-08-29

Supersedes：[ADR-0001：分支与 PR 治理](0001-branch-and-pr-governance.md)

## 背景

ADR-0001 在仓库初始化时把 `topic -> dev -> master -> dev` 写成固定流向。该拓扑能够为多人贡献提供合并前审查，但 RadishNexus 当前由单维护者串行推进 Golden Path；把每个小型纵向切片都放入临时分支、推送、PR 和 rebase merge，会产生重复分支、额外远端状态和提交 SHA 改写，却没有对应的并行隔离或第二人评审收益。

本次重新对照 Radish、RadishMind、RadishCatalyst、RadishFlow 与 RadishAxiom 的现行治理。五个项目共同保留 `master` / `dev` 分工和稳定主线回流；其中 RadishCatalyst 明确把直接 `dev` 开发作为默认，RadishMind、RadishFlow 与 RadishAxiom 明确允许连续或单人开发直接进入 `dev`，Radish 仍保留更保守的主题分支建议。RadishNexus 选择与当前单维护者串行阶段匹配的 `dev` 优先变体，并保留下列家族共同边界：

- `master` 是受保护稳定主线，只通过 PR 接收阶段晋级或 hotfix；
- `dev` 是日常开发与集成面，稳定主线变化必须在下一轮开发前回流 `dev`；
- `dev` 承担真实日常开发；是否强制主题分支应由维护规模、并行写入和审查需求决定，而不是由工具默认决定；
- 未保护的 `dev` 由直接提交者承担本地验证，完整远端强制门禁统一放在稳定主线 PR；
- 兄弟项目的业务 CI、发布环境、目录所有权和具体 required jobs 不属于可复制的共同规则。

RadishNexus 现有远端状态已经与这组边界兼容：Ruleset 只保护 `master`，`dev` 没有 required checks；`PR Checks` 在目标为 `dev` 或 `master` 的 PR 上运行，普通 `push -> dev` 不触发。无需为本次开发拓扑修正改变远端设置或新增日常 push CI。

## 决定

### 分支职责

- `master` 是稳定候选与正式发布历史，只通过 PR 接收阶段性 `dev` 晋级或明确 hotfix。
- `dev` 是单维护者阶段的默认日常开发与集成分支。串行常规任务直接在 `dev` 设计、实现、验证和提交。
- 主题分支或 worktree 不是普通切片的默认前置条件，只在以下情况使用：
  - 项目所有者明确要求；
  - 外部贡献需要独立审查；
  - 多人或多 Agent 确实会并行写入共享工作区；
  - 高风险实验、兼容性验证或大范围重构需要可丢弃隔离；
  - hotfix 必须直接面向稳定主线。
- Agent 不得仅因工具默认、分支前缀约定或“每个切片一个 PR”的习惯自动创建 `codex/*`、主题分支或 worktree。实际分支与用户要求不一致时先报告，不通过切换或复制制造预期状态。

默认拓扑为：

```text
日常串行开发 -> dev -> master PR -> master -> dev 回流
```

按需隔离拓扑为：

```text
topic/worktree -> dev -> master PR -> master -> dev 回流
```

### `dev` 的开发与检查

- 当前单维护者阶段不保护 `dev`，也不要求普通提交通过 PR 进入 `dev`。
- 普通 `push -> dev` 不自动触发 CI。直接开发者必须在提交或推送前执行与改动风险匹配的格式化、静态分析、测试、构建和仓库检查，并只报告实际结果。
- 目标为 `dev` 的 PR 保留给外部贡献、并行隔离、明确评审或需要远端合并前证据的任务；它会运行现有 `PR Checks`，但 `dev` 不绑定 required context。
- push、创建 PR 和其它远端写入仍须获得当前任务的明确授权；直接在本地 `dev` 开发不扩大远端操作权限。
- 共享 `dev` 禁止使用 rebase、reset、force push 或历史重写伪造同步。发现 ahead、behind、并行提交或冲突时先检查分支图，再选择 fast-forward 或普通 merge。

### 稳定主线与回流

- `master` 继续禁止直接 push、删除和非快进更新，必须通过 PR 严格通过 `Candidate Quality` 并解决全部 review conversation。
- `dev -> master` 阶段 PR 优先使用 merge commit；仓库仍允许 rebase merge，禁用 squash merge。
- 任何进入 `master` 的变化都必须在下一轮 `dev` 开发前回流。可快进时优先 fast-forward；否则使用普通 merge并执行与冲突或文件变化匹配的验证。
- 回流不自动创建 tag、Release、发布包或部署，也不授权其它远端状态修改。

### 重新评估条件

出现以下任一情况时，重新评估 `dev` Ruleset、push CI 或默认主题分支策略：

- 有两名或以上稳定维护者并行提交；
- 持续接受外部代码贡献；
- 多个自动化协作者并行写入共享分支；
- 曾因直接进入 `dev` 绕过本地验证造成构建、权限、数据或治理回归；
- 发布节奏要求每次 `dev` push 都形成可追溯的远端候选证据。

## 替代方案

### 每个切片强制主题分支与 PR

适合多人评审、外部贡献或高并发修改，但在当前单维护者串行阶段只增加分支、远端 PR 和 SHA 重写成本。需要真实隔离时仍可按需使用，不再作为默认仪式。

### 为每次 `dev` push 自动运行完整 CI

能提供更密集的远端证据，但会复制本地开发检查并增加日常 CI 噪声。当前完整门禁在 `dev -> master` PR 统一执行；达到重新评估条件后再引入 push CI。

### 只有 `master`

会混合日常开发与稳定候选，失去阶段晋级和回流边界，继续不采用。

## 后果

正面影响：

- 日常工作与实际单维护者节奏一致，不再为普通纵向切片制造临时分支；
- `master` 保护、`Candidate Quality` 和 `master -> dev` 回流等稳定不变量保持不变；
- 外部贡献、并行任务和高风险实验仍有明确隔离入口；
- 现有 Workflow 与 Ruleset 无需远端迁移。

成本与风险：

- 直接提交者必须承担本地验证责任，不能把“未自动运行 CI”理解为无需检查；
- 单个 `dev` 提交不会自动产生远端质量证据，阶段性证据集中到 `master` PR；
- 若维护者或自动化协作者数量增长，必须及时重新评估共享分支保护。

## 迁移与验证

- 将 `AGENTS.md`、`CLAUDE.md`、`CONTRIBUTING.md`、根 README、仓库治理和当前状态统一到 `dev` 优先口径。
- ADR-0001 标记为已替代，保留其历史背景与原决定，不重写正文。
- 保留当前 `master` Ruleset JSON、PR 模板和 `PR Checks` 触发条件；它们已经支持未保护 `dev`、可选 `dev` PR 和受保护 `master` PR。
- 运行仓库检查器、检查器单元测试、`git diff --check` 与根入口同步检查，确认没有遗留“普通任务必须开主题分支”的可执行口径。
