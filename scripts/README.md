# 仓库脚本

本目录保存开发者与 CI 共用的轻量自动化。脚本应可从仓库根运行、默认无网络依赖，并在失败时给出可操作的文件和原因。

## 仓库检查器

macOS / Linux：

```bash
./scripts/check-repo.sh
```

Windows PowerShell：

```powershell
pwsh ./scripts/check-repo.ps1
```

PR CI 会传入 base SHA，同时检查本 PR 的普通提交是否遵循 Conventional Commits：

```bash
./scripts/check-repo.sh --base-ref <base-sha>
```

检查范围包括：

- 必需治理文件、路径长度和意外大文件；
- UTF-8、LF、末尾换行和行尾空白；
- JSON 与 Markdown 相对链接；
- 本机绝对路径和私有会话链接；
- `AGENTS.md` / `CLAUDE.md` 镜像一致性；
- PR Workflow、模板和 `master` Ruleset 契约；
- Git 空白错误和 PR 新增提交标题。

单元测试：

```bash
python3 -m unittest discover -s scripts/tests -p 'test_*.py'
```

检查器只验证能被仓库静态判定的基线，不声称远端 Ruleset 已启用，也不替代未来 Go、React、Rust、Flutter、数据库、浏览器或部署测试。

## M0 核心契约实验

不需要数据库的 Go 测试与静态检查：

```bash
./scripts/check-m0-core-contracts.sh
```

使用固定 digest 的临时 PostgreSQL 17.10 容器运行真实事务、约束、Outbox 和 Activity 重建测试：

```bash
./scripts/check-m0-core-contracts-postgres.sh
```

后者只操作任务专属容器和其中的 `m0_core` schema，结束后自动删除容器；默认不会隐式拉取缺失镜像。实验范围和手工数据库入口见 [M0 核心契约实验](../experiments/m0-core-contracts/README.md)。
