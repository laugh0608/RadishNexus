# 参与 RadishNexus

感谢你参与 RadishNexus。项目当前处于 M0 正式服务与 Golden Path 纵向切片阶段，贡献的首要目标是验证“讨论、决策、执行、构建和部署上下文不断链”，而不是快速堆叠聊天、工单或插件数量。

## 开始之前

建议按以下顺序阅读：

1. [当前状态](docs/status/current.md)
2. [产品定义](docs/product-definition.md)
3. [领域模型](docs/domain-model.md)
4. [Golden Path](docs/golden-path.md)
5. [仓库治理](docs/governance/repository-governance.md)
6. 与改动直接相关的架构、插件、工程规范或 ADR

安全漏洞不要创建公开 Issue 或 Pull Request，请遵循 [SECURITY.md](SECURITY.md)。参与讨论和审查时同时遵循[社区行为准则](CODE_OF_CONDUCT.md)。

本仓库采用 [RadishNexus Source-Available License](LICENSE)，不是开放源码许可证。查看源码不自动授予部署、运行、复制、修改、再分发、衍生开发或商业使用权；外部提交者不得推定已经取得上述权利。提交贡献即表示接受 `LICENSE` 第 5 节的贡献授权条款。

## 贡献类型

- 缺陷修复：说明最小复现、实际结果、期望结果和影响范围，并优先修正根因。
- 产品或领域提案：先说明用户问题、目标与非目标、对象边界、权限、兼容性和验证计划。
- 功能开发：先确认对应产品、领域或架构专题已经定义边界，再交付小而完整的纵向切片。
- 插件或外部集成：说明 Manifest 权限、Secrets、网络范围、Webhook、失败隔离、幂等和 Host API 影响。
- 文档与 UI：保持产品事实、页面状态和当前实现一致，不用文案或静态原型暗示尚未实现的能力。
- 测试与运维：覆盖正例、负例、边界条件、故障恢复、权限变化和自部署升级影响。

改变长期产品定位、领域语义、权限、公共 API、事件、插件 Host API、数据格式、许可或仓库治理时，应先形成 Issue、设计讨论或 ADR。小型修复和不改变正式边界的文档改进可以直接提交 PR。

## 分支、提交与 Pull Request

- `master` 是受保护稳定主线，`dev` 是日常集成分支。
- 普通贡献从 `feature/*`、`fix/*`、`docs/*`、`proposal/*`、`experiment/*`、`refactor/*`、`test/*` 或 `chore/*` 向 `dev` 发起 PR。
- 只有阶段性 `dev` 晋级或明确的 `hotfix/*` 才向 `master` 发起 PR。
- 不直接 push 到 `master`，不 force push 共享分支。
- `master` 允许 merge commit 与 rebase merge，禁用 squash merge；阶段 PR 优先 merge commit。
- 合并到 `master` 后，下一轮开发前必须把最新 `master` 回流到 `dev`。
- 提交遵循 Conventional Commits，例如 `feat(decision): add superseded state`、`fix(authz): hide private relation metadata`、`docs(governance): define ruleset baseline`。
- 提交使用贡献者自己的 Git 身份，不添加 AI 协作者署名。

完整规则见 [ADR 0001](docs/adr/0001-branch-and-pr-governance.md)。

## 实现与数据要求

- 保持 [领域模型](docs/domain-model.md) 中 Project、Initiative、Component、Decision、Environment、CI Run 和 Deployment 的职责分离。
- EntityLink 必须保留来源和创建时间；引用、搜索、Activity、Attention Item 和通知不能绕过目标对象权限。
- 插件不能直接访问核心业务表；高风险 CI/CD 写操作使用独立权限、确认和审计。
- 错误在正确 owner 内失败关闭，不通过吞异常、伪造成功、默认值或未声明存储 fallback 掩盖问题。
- 不提交真实 API Key、Webhook Secret、访问令牌、个人数据、私密消息、专有输入或未经脱敏的日志。
- 第三方代码、字体、图标、模型、数据和其它资产必须记录来源、版本、许可证和必要归属。
- 修改架构、接口、迁移、配置、权限、安全、用户可见行为或仓库规则时，同步更新相关文档和测试。

更完整的工程规则见 [工程规范](docs/development/engineering-standards.md)，协作者执行边界见 [AGENTS.md](AGENTS.md)。

## 本地验证

当前无第三方依赖的仓库治理入口为：

```bash
./scripts/check-repo.sh
```

Windows PowerShell：

```powershell
pwsh ./scripts/check-repo.ps1
```

仓库检查器单元测试：

```bash
python3 -m unittest discover -s scripts/tests -p "test_*.py"
```

M0 核心契约 Go 检查：

```bash
./scripts/check-m0-core-contracts.sh
```

修改 M0 schema、事务、事件、Outbox 或 Activity 时，还需运行真实 PostgreSQL 边界测试：

```bash
./scripts/check-m0-core-contracts-postgres.sh
```

修改正式 Go 服务时运行：

```bash
./scripts/check-server.sh
```

涉及 migration、权限、业务事务、EntityLink、领域事件或 Outbox 时，还需运行：

```bash
./scripts/check-server-postgres.sh
```

PostgreSQL 脚本使用固定镜像、随机本机端口和任务专属临时容器，结束后自动删除且不保留数据卷；镜像不存在时不会隐式下载。详细边界分别见 [M0 核心契约实验](experiments/m0-core-contracts/README.md)和[正式 Go 服务](server/README.md)。

修改正式 React Web App 时，先在 `web/` 运行 `npm ci`，再从仓库根运行：

```bash
./scripts/check-web.sh
```

该入口覆盖格式、Lint、组件状态测试、严格 TypeScript、production build 与锁依赖基线。依赖漏洞数据需要联网刷新；CI 额外执行 `npm audit --audit-level=high`。

实现代码进入仓库后，还必须执行与改动范围匹配的 Go、React / TypeScript、Rust 或 Flutter 格式化、静态分析、测试和构建。PR 只记录真实执行过的命令，并明确列出未执行、受环境阻塞或需要人工完成的验证。

## Pull Request 说明

请使用仓库 PR 模板，并覆盖所有适用内容：

- 目标、范围、原因和明确非目标；
- 关联 Issue、Decision、ADR 或专题；
- 领域、权限、API、事件、存储、迁移、插件、自部署和许可影响；
- 实际验证、未验证范围和测试环境；
- 已知风险、失败模式和回滚方式；
- 目标为 `master` 时的候选门禁和 `master -> dev` 回流安排。

## 许可证

仓库根内容受 [RadishNexus Source-Available License](LICENSE) 约束。SDK、公共协议和插件只有在各自目录携带明确独立许可证时，才按该独立许可证分发。提交贡献即表示你有权提供相关内容，并按根许可证第 5 节向版权所有者授予贡献许可；这不改变仓库其余内容的授权范围。
