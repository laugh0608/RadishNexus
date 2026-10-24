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

需要人工复核 production Web build、正式 Session transport 与真实 PostgreSQL 的完整 HTTPS 浏览器链路时运行：

```bash
./scripts/run-authenticated-web-browser-fixture.sh
```

该入口先执行 Web production build，再启动任务专属 PostgreSQL、fixture upstream 与固定 Caddy HTTPS reverse proxy，输出 origin、本次 Caddy CA、contributor / decider 两个测试账号、canonical Deployment / Channel / Thread path、数据库容器标识与 stop 文件，并等待人工浏览器复核。它不会隐式拉取缺失镜像，测试账号不属于产品默认 credential；浏览器必须在连接前核对并临时信任本次 CA，不应绕过证书告警。创建输出的 stop 文件后，脚本会退出并清理容器、volume 与临时状态；操作者仍须按完整指纹删除导入登录钥匙串的 CA。fixture 可复核正式 Channel SSE 增量 / 重连 / 撤权、Message / Thread 写入以及 Thread → Decision → Ticket 的分权协作链，但不会自动替代交互式浏览器检查。精确安全边界见 [Web App](../web/README.md)、[ADR-0015](../docs/adr/0015-same-origin-authenticated-web-shell.md)、[ADR-0018](../docs/adr/0018-session-scoped-channel-message-transport.md)、[ADR-0019](../docs/adr/0019-session-scoped-thread-decision-ticket-transport.md) 与 [ADR-0020](../docs/adr/0020-session-scoped-single-process-message-realtime.md)。

验证空业务工作区的基础配置时，使用 `RADISHNEXUS_BROWSER_FOUNDATION=1 ./scripts/run-authenticated-web-browser-fixture.sh`。该模式通过正式 bootstrap 只创建虚构 owner 与 Workspace，不预置 Team / Project / Channel；后续对象、邀请、成员与消息均从 Web 创建。state 的 `foundation_login` / `foundation_workspace` 标明测试身份与作用域。停止与清理方式相同；这不代表首次访问初始化页面已完成。

首次访问初始化验收可使用 `RADISHNEXUS_BROWSER_SETUP=1`，并指定受保护测试文件的 `RADISHNEXUS_SETUP_CODE_FILE`。该模式只执行 migration，不运行 bootstrap 或业务 seed；它优先于 foundation 模式，使用正式 setup handler。测试码不进入 state 或脚本输出，外部测试文件由操作者在停止后清理。已有 foundation / 默认 fixture 同样装配 setup 路由，并因已有账户返回 complete。

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

不需要数据库的全量竞态测试、`go vet`、`go mod tidy -diff` 与 module checksum 验证：

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

## Docker Compose 自部署开发拓扑

在五个 `tag@sha256` 官方基础镜像已经显式准备、本机允许 Docker build 读取现有 Go / npm 锁定依赖后，运行：

```bash
./scripts/check-self-hosted-compose.sh
```

该入口不会拉取缺失镜像。它创建任务专属 Compose project、临时数据库 Secret、PostgreSQL volume、Caddy CA 和随机本机 HTTPS 端口，验证 Compose/Caddy 配置、固定 image build、PostgreSQL readiness、显式 migration、唯一一次 bootstrap、HTTPS login / Session / logout、Secure Cookie、Caddy 及时 flush 正式 Message SSE 的 `ready` / `message.created`，以及 Go server / PostgreSQL 无宿主端口。退出时只删除自己的临时 project、volume、network 与临时目录，保留本地构建出的 application / operation image cache。运行边界和人工入口见 [部署说明](../deploy/README.md)、[ADR-0016](../docs/adr/0016-minimal-docker-compose-self-hosting.md)与 [ADR-0020](../docs/adr/0020-session-scoped-single-process-message-realtime.md)。

## Project 上下文包往返

`./scripts/check-server-context-package.sh` 用现有 Go 缓存构建独立 CLI，在专属 PostgreSQL 容器运行 `TestContext*`，包含真实 HTTPS 登录、导出 / 计划 / 导入 / 结果查询以及数据库并发和故障测试。格式 / 文件解析全量单元测试仍由 `check-server.sh` 执行。

增加 `--browser-review` 会构建正式 Web，保留合成源 / 目标 HTTPS fixture 等待浏览器复验者提交检查结果；必须实际完成页面流程才能写完成标记。CA 仅供该隔离浏览器 / CLI 使用，不加入系统信任。启动资源、故障注入和清理须获得当前任务授权；脚本不拉取镜像或下载依赖，退出清理自有容器和目录。步骤与限制见[迁移手册](../deploy/context-package.md)，实际结果见[记录](../docs/status/reviews/2026-10-24-portable-context-package.md)。`check-server-backup-restore.sh` 同时验证导入来源与历史身份随备份恢复，恢复后显式重建 Activity。

## 单宿主升级失败与恢复演练

[ADR-0043](../docs/adr/0043-single-host-upgrade-failure-recovery.md) 的操作步骤见[运维手册](../deploy/upgrade-recovery.md)。使用本机 Python 3.12+、Docker / Compose、Git 和已存在的固定基础镜像；仅操作新建的任务专属 project、合成账户与独立恢复副本：

```bash
python3 scripts/check-upgrade-recovery.py --old-ref 333c4ce --new-ref 171527f \
  --report /tmp/radishnexus-upgrade-result.json --browser-review
```

默认无网络构建。获准下载现有锁定构建依赖时才增加 `--allow-locked-downloads`，不升级版本、修改 lockfile 或拉取缺失基础镜像。脚本从本地 Git archive 构建旧/新 app、operations 与 Web，记录真实镜像身份；只读核对实例身份，明确停写，经停写后备份、匹配旧工具空目标恢复、显式升级和唯一入口切换完成验证。它不是业务实例自动升级器，不接受任意 DSN 或已有 volume。

脚本验证错误目标、损坏/缺失备份、临时容器磁盘满、额外 writer、真实 migrate CLI 锁等待预算、旧 app / 新 schema 错配、缺失 Web build、备份后数据取舍、旧 Session 拒绝、恢复后单独升级及权限/来源读取。`--browser-review` 的四个阶段必须由复验者完成并写明确检查结果；没有该参数时浏览器证据标记未运行。退出仅清理任务资源和专属镜像标签，保留构建缓存与指定脱敏报告；清理失败明确报告，不自动清理真实实例。

`check-server-postgres.sh` 中的 `TestUpgradeFailure*` 验证真实历史 migration 的原子回滚、合法前缀、续迁、并发、取消、连接终止、COMMIT 确认丢失及 history 漂移；readiness 不覆盖手工 DDL 的限制也显式测试。COMMIT 故障夹具位于 `server/internal/testsupport/pgfault`，仅 integration 构建使用。

`check-server-backup-restore.sh` 还会编译正式 restore CLI，并运行 `TestBackupRestoreUpgradeFailures`。它在真实 pg_restore 的事务阶段注入 SQL 错误/断连，在权威恢复后注入 Activity 重建错误，证明失败不能报告完成、不能覆盖非空目标；重新恢复使用独立空目标。旧版本证据仍需显式指定 `RADISHNEXUS_OWNER_TRANSFER_LEGACY_REF=333c4ce`，不能以当前版本往返代替。
