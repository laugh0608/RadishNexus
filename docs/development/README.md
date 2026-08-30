# 开发指南

RadishNexus 当前处于 M0.5 Golden Path 与 M1 Web 平台基础交界的纵向切片阶段。本目录定义跨语言工程约束和已经存在的可信验证入口，不提前承诺尚未选型的编辑器、实时协议、插件 runtime 或未来客户端目录结构。

## 入口

- [工程标准](engineering-standards.md)
- [总体架构](../architecture/overview.md)
- [领域模型](../domain-model.md)
- [Golden Path](../golden-path.md)
- [仓库治理](../governance/repository-governance.md)
- [贡献指南](../../CONTRIBUTING.md)

## 当前最小验证

从仓库根运行：

```bash
./scripts/check-repo.sh
python3 -m unittest discover -s scripts/tests -p 'test_*.py'
```

PowerShell 环境可以运行：

```powershell
pwsh ./scripts/check-repo.ps1
```

当前 M0 核心契约实验已经提供独立的 Go 与 PostgreSQL 定向入口：

```bash
./scripts/check-m0-core-contracts.sh
./scripts/check-m0-core-contracts-postgres.sh
```

该目录是可丢弃技术实验，不是第二个生产服务入口；范围、依赖和清理方式见 [M0 核心契约实验](../../experiments/m0-core-contracts/README.md)。

正式 `server/` module 的单元、静态、module 漂移和真实 PostgreSQL 边界入口为：

```bash
./scripts/check-server.sh
./scripts/check-server-postgres.sh
./scripts/check-server-backup-restore.sh
```

前两个入口覆盖无数据库检查与单实例真实 PostgreSQL 业务边界；备份恢复入口使用两个独立 PostgreSQL 17 容器验证 custom archive、全新空目标恢复、migration 和 Activity 重建。正式服务当前开放健康检查、三个公共认证操作和一个 Deployment Nexus View 业务读取；业务写入仍通过 application service 验证，不为其它页面建立临时业务 API。范围、migration、公共路由和备份恢复入口见[正式 Go 服务](../../server/README.md)。

正式 React Web App 的格式、Lint、组件状态测试、严格 TypeScript、production build 与锁依赖入口为：

```bash
./scripts/check-web.sh
```

首次运行前需要在 `web/` 使用 Node 24 LTS 与 npm 11 执行 `npm ci`。当前 Web 范围和安全边界见 [Web App](../../web/README.md)。后续 Rust、Flutter 或插件真正进入仓库时，再加入对应真实检查，不维护占位命令。

需要人工复核 production Web build → HTTPS → Session → PostgreSQL → canonical Deployment → logout 的完整链路时，显式运行：

```bash
./scripts/run-authenticated-web-browser-fixture.sh
```

该 fixture 使用一次性 PostgreSQL 容器和测试 credential，完成后通过脚本输出的 stop 文件退出并清理；它不是常驻开发服务，也不替代 `check-server.sh`、`check-web.sh` 或后续正式 Docker Compose 新实例演练。
