# Docker Compose 自部署开发拓扑

此目录实现 [ADR-0016](../docs/adr/0016-minimal-docker-compose-self-hosting.md) 的最小受控开发实例。它验证固定容器工件、显式 migration / bootstrap、唯一 HTTPS origin、持久化 PostgreSQL 和正式 Web build 的装配，不承诺公网生产证书、高可用或跨 PostgreSQL major 升级。

## 边界

- 只有 Caddy 发布宿主 HTTPS 端口；Go server 和 PostgreSQL 没有宿主端口。
- Caddy 使用 internal CA 并覆盖传入的转发 Header；Go server 只信任 Caddy 固定地址的 `/32`。
- PostgreSQL 数据保存在命名 volume；本地备份工件只写入忽略提交的 `deploy/local-data/backups/`。
- 数据库密码通过 Compose Secret 文件按 service 挂载，不进入 Compose environment、命令参数或镜像层。
- migration、bootstrap、backup 和 restore 都是显式一次性 operation；`docker compose up` 不会自动执行它们。

该入口使用 [Docker Official Images](https://hub.docker.com/search?image_filter=official)：Caddy `2.11.4-alpine`、PostgreSQL `17.10-alpine`、Go builder `1.26.7-alpine3.23`、Node builder `24.16.0-alpine3.23` 和 Alpine runtime `3.23.5`。实际配置同时固定完整 digest；升级时必须重新核验版本、许可证、漏洞和 multi-platform manifest。

## 首次准备

需要 Docker Engine 与 Docker Compose。命令从仓库根运行。

复制非密配置并检查 public origin、宿主端口和 proxy 网络没有冲突：

```text
cp deploy/.env.example deploy/.env
```

创建只属于本实例的数据库密码文件；不要复用账号密码或把结果打印到终端：

```text
mkdir -p deploy/secrets deploy/local-data/backups
umask 077
openssl rand -base64 32 > deploy/secrets/postgres_password
chmod 0600 deploy/secrets/postgres_password
```

`RADISHNEXUS_PUBLIC_ORIGIN` 的端口必须与 `RADISHNEXUS_HTTPS_PORT` 相同。若必须调整 proxy 网络，`RADISHNEXUS_PROXY_SUBNET` 必须包含 `RADISHNEXUS_PROXY_IPV4`，且 `RADISHNEXUS_TRUSTED_PROXY_CIDR` 必须是同一个 proxy 地址的 `/32`；不要改成 private range 或 `0.0.0.0/0`。

镜像不会使用 `latest`。在受控联网步骤中显式准备 `deploy/compose.yaml` 和 `deploy/Dockerfile` 记录的精确 `tag@sha256` 基础镜像，然后构建本地 application 与 operation 工件：

```text
docker compose -f deploy/compose.yaml config --quiet
docker compose -f deploy/compose.yaml build app migrate
```

build 只运行现有 `go.mod` / `go.sum` 和 `web/package-lock.json` 固定的下载与构建，不应修改任何 lockfile。

## 全新实例初始化

先只启动 PostgreSQL，并等待官方 healthcheck 成功：

```text
docker compose -f deploy/compose.yaml up -d --wait postgres
docker compose -f deploy/compose.yaml run --rm migrate
```

随后从标准输入建立唯一一次本地管理员与 Workspace owner。管理员密码与数据库密码必须不同；密码不会保存为 Compose Secret：

```text
read -r -s bootstrap_password
printf '\n'
printf '%s\n' "$bootstrap_password" | docker compose -f deploy/compose.yaml run --rm -T bootstrap \
  --login admin \
  --display-name "First Admin" \
  --workspace-name "First Workspace" \
  --password-stdin
unset bootstrap_password
```

只有 migration 和 bootstrap 成功后才启动公共入口：

```text
docker compose -f deploy/compose.yaml up -d --wait app caddy
docker compose -f deploy/compose.yaml ps
```

Caddy internal CA 保存在 `caddy_data` volume。导出公开根证书供受控浏览器或 CLI 信任；私钥不会被复制：

```text
docker compose -f deploy/compose.yaml cp \
  caddy:/data/caddy/pki/authorities/local/root.crt \
  deploy/local-data/caddy-root.crt
curl --cacert deploy/local-data/caddy-root.crt https://localhost:8443/health/ready
```

把 URL 换成 `.env` 中的精确 origin。浏览器必须显式信任该 CA 后再登录；不要用关闭证书验证作为日常运行方式。

## 运维命令

查看明确的进程状态和日志：

```text
docker compose -f deploy/compose.yaml ps
docker compose -f deploy/compose.yaml logs postgres app caddy
```

备份目录必须尚不存在：

```text
docker compose -f deploy/compose.yaml run --rm backup \
  --output /backups/backup-YYYYMMDD-HHMMSS
```

restore 只用于全新、尚未 migration 或 bootstrap 的空 PostgreSQL 目标：

```text
docker compose -f deploy/compose.yaml run --rm restore \
  --input /backups/backup-YYYYMMDD-HHMMSS
```

恢复工件保留本地账号 verifier，但不恢复 Session。恢复成功后不要再次 bootstrap；直接启动 `app` 与 `caddy` 并重新登录。跨 major、非空目标、损坏工件或 migration 漂移仍会失败，精确边界见 [ADR-0010](../docs/adr/0010-verified-postgresql-backup-and-restore.md)。

普通停止保留 PostgreSQL、Caddy CA 和备份：

```text
docker compose -f deploy/compose.yaml down
```

不要把 `down --volumes` 当作普通重启；它会删除该 Compose project 的 PostgreSQL 数据和 Caddy CA。备份目录是宿主 bind mount，不会由 `down` 删除。

## 失败定位

| 现象 | 判断入口 |
| --- | --- |
| PostgreSQL 未就绪 | `docker compose ps postgres` 与 `logs postgres`；migration 不应继续 |
| Secret 缺失、空、多行或 URL 歧义 | application / operation 直接失败并指出配置键，不回显密码 |
| migration 尚未完成 | `migrate` operation 未成功；应用不会替它自动修改 schema |
| 尚未 bootstrap | `bootstrap` operation 尚无成功输出；重复执行只允许第一次成功 |
| Web build 缺失 | application image build 或 Go server 启动失败，不退回 fixture |
| origin / Host / proxy 配置错误 | app 启动错误、认证 transport 的稳定安全错误或 Caddy health 失败 |
| 浏览器不信任证书 | 导出并信任当前 `caddy_data` 中的公开 root CA，不关闭 HTTPS 校验 |

`/health/ready` 只证明 Go server 当前能够 ping PostgreSQL，不声称 migration 或 bootstrap 已完成；这两个状态始终以显式 operation 结果为准。

## 仓库演练

准备好配置中记录的五个固定基础镜像后，从仓库根运行：

```text
./scripts/check-self-hosted-compose.sh
```

脚本只使用本次生成的随机 Compose project、临时 Secret、随机 HTTPS 端口和全新命名 volumes。它验证静态配置、镜像构建、显式 migration、一次 bootstrap、重复 bootstrap 拒绝、Caddy CA、伪造转发 Header 清洗、login / Session / logout、正式 Message SSE 在连接保持打开时及时 flush `ready` / `message.created`，以及应用和数据库无宿主端口；退出时删除本次 project、临时网络、volumes 和 Secret，不删除用户实例或备份。SSE 仍由唯一 Caddy origin 转发，Caddy 2.11.4 对 `text/event-stream` 的流式识别已由该门禁固定验证；更换代理或版本必须重跑。
