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

## Web App

先在 `web/` 使用 Node 24 LTS 与 npm 11 安装锁定依赖：

```bash
cd web
npm ci
```

然后从仓库根运行：

```bash
./scripts/check-web.sh
```

该入口默认不访问网络，覆盖 Prettier、Oxlint、Vitest + jsdom、严格 TypeScript、Vite production build，以及 lockfile 来源、integrity、许可证和 lifecycle script 基线。CI 在 `npm ci` 后额外运行 `npm audit --audit-level=high`；本地需要刷新漏洞数据时可在获得网络与依赖操作授权后运行同一命令。

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

## 正式 Go 服务

不需要数据库的单元测试、`go vet`、`go mod tidy -diff` 与 module checksum 验证：

```bash
./scripts/check-server.sh
```

使用同一固定 PostgreSQL digest 的临时容器，验证正式 migration、权限、业务事务、EntityLink 和 Outbox：

```bash
./scripts/check-server-postgres.sh
```

两个 PostgreSQL 入口复用 `run-postgres-go-integration.sh`，只操作各自任务专属容器并在退出时自动清理；默认不会隐式拉取缺失镜像。共享 runner 只有在容器内连续两次真实 `psql SELECT 1` 成功后才通过 readiness，避免 PostgreSQL 初始化重启窗口让宿主端测试遇到瞬时 EOF。

使用两个独立的固定 PostgreSQL 17 容器，验证版本化 backup manifest、custom archive、全新空目标恢复、正式 migration、Activity 重建和关键失败路径：

```bash
./scripts/check-server-backup-restore.sh
```

该入口在源实例通过 application service 生成 Thread → Decision → Ticket → CI Run → staging Deployment fixture，比较恢复前后所有纳入表和 Activity 全量快照，并证明 migration manifest 漂移、dump 损坏和非空目标均 fail closed。脚本只删除自己创建的临时目录、网络和容器，也不会隐式拉取缺失镜像。
