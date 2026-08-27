# GitHub Rulesets

本目录保存可审查、可版本化的 GitHub Ruleset 模板。JSON 文件存在于仓库中不代表远端保护已经启用。

## 当前模板

- [`master-protection.json`](master-protection.json)：只匹配 `refs/heads/master`，禁止删除和非快进更新，要求 PR、已解决对话和严格通过 `Candidate Quality`。

仓库合并策略为允许 merge commit 与 rebase merge、禁用 squash merge。当前为单维护者阶段，最少批准数为 0，不要求 CODEOWNERS；管理员绕过限制为“仍须通过 PR”。完整理由见 [ADR-0001](../../docs/adr/0001-branch-and-pr-governance.md)。

模板显式保留 GitHub 默认启用的 `require_extra_approval_for_unattributed_changes`。该公开预览选项只为未归属到具体人的 Copilot PR 增加一次审批；当前最少批准数为 0，因此不会形成额外门禁。未来提高批准数时必须重新评估这一行为。

## 启用前置条件

1. `master`、`dev` 和默认 PR 目标已按治理文档建立；
2. `.github/workflows/pr-check.yml` 已在远端至少成功产生一次 `Candidate Quality`；
3. 仓库级合并方式与模板一致；
4. 当前操作者具有管理 Ruleset 的权限；
5. 已读取远端现有 Ruleset，确认不会覆盖其他管理员策略；
6. 模板中的 bypass actor 与远端角色语义已经核对。

## 导入与核验

优先使用 GitHub 设置页面的 Rulesets 导入功能并逐项复核。也可以在已获得明确远端写授权后使用 GitHub CLI 调用仓库 Rulesets API；创建和更新是不同操作，更新前必须先读取目标 Ruleset ID。

不要把示例命令放进普通 CI 自动执行，也不要因为本地 JSON 校验通过就跳过测试 PR。启用后至少验证：

- 直接更新和强推 `master` 被拒绝；
- 未通过 `Candidate Quality` 的 PR 无法合并；
- 未解决对话会阻止合并；
- merge/rebase 可用而 squash 不可用；
- `master` 的变化能够通过常规 PR 回流 `dev`。

## 变更同步

Ruleset 变化必须同步：

- [仓库治理基线](../../docs/governance/repository-governance.md)；
- [ADR-0001](../../docs/adr/0001-branch-and-pr-governance.md)，或用新 ADR 替代；
- PR 模板、Workflow 和仓库检查器；
- GitHub 远端实际设置（另行授权后）。

本仓库不会把签名提交、标签保护、合并队列、强制批准或 CODEOWNERS 作为当前默认项；满足真实治理需求后再评估。
