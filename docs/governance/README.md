# 仓库治理

本目录描述 RadishNexus 如何协作、变更和维护仓库。治理文件只约束仓库工作方式，不替代产品、领域、架构或许可真相源。

## 阅读入口

- [仓库治理基线](repository-governance.md)：分支、提交、PR、CI、Ruleset 与维护边界。
- [Agent 协作规范](agent-collaboration.md)：人类与自动化 Agent 共同工作时的授权、验证和交接要求。
- [文档治理](documentation-governance.md)：文档分层、真相源和更新矩阵。
- [ADR-0001：分支与 PR 治理](../adr/0001-branch-and-pr-governance.md)：当前分支模型及其取舍。

仓库根目录的 [AGENTS.md](../../AGENTS.md) 是可直接执行的协作规则，[CONTRIBUTING.md](../../CONTRIBUTING.md) 是贡献者入口。两者应与本目录保持语义一致。

## 规则优先级

发生冲突时，按以下顺序处理：

1. 法律、许可证和安全披露要求；
2. 仓库根目录的 `AGENTS.md`；
3. 已接受的产品决策与 ADR；
4. 本目录中的治理基线；
5. 当前状态、计划和自动化实现。

低层文件不能静默覆盖高层约束。确需改变稳定规则时，应先修改对应真相源，并记录原因、影响和迁移方式。
