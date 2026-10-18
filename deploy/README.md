# Docker Compose 自部署开发拓扑

此目录实现 [ADR-0016](../docs/adr/0016-minimal-docker-compose-self-hosting.md) 的最小受控开发实例。它验证固定容器工件、显式 migration / bootstrap、唯一 HTTPS origin、持久化 PostgreSQL 和正式 Web build 的装配，不承诺公网生产证书、高可用或跨 PostgreSQL major 升级。

拟议的试用支持范围见[受控试用支持说明](../docs/trial-support.md)；单机停机升级、失败分类与空目标恢复设计见 [ADR-0043](../docs/adr/0043-single-host-upgrade-failure-recovery.md)。该提案尚未实施或执行验收矩阵，不能把下述分项命令视为完整升级失败恢复手册，也不自动获得操作实例的授权。

## 边界

- 只有 Caddy 发布宿主 HTTPS 端口；Go server 和 PostgreSQL 没有宿主端口。
- Caddy 使用 internal CA 并覆盖传入的转发 Header；Go server 只信任 Caddy 固定地址的 `/32`。
- PostgreSQL 数据保存在命名 volume；本地备份工件只写入忽略提交的 `deploy/local-data/backups/`。
- 数据库密码通过 Compose Secret 文件按 service 挂载，不进入 Compose environment、命令参数或镜像层。
- migration、bootstrap、identity-migrate、backup 和 restore 都是显式一次性 operation；`docker compose up` 不会自动执行它们。

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

## 网页首次初始化准备

[ADR-0027](../docs/adr/0027-first-visit-administrator-setup.md) 提供部署者凭一次性初始化码创建首位 Workspace owner 的入口。仅对没有任何账户的新实例开放；已有账户的升级或备份恢复不会重新开放。初始化不产生默认 Team / Project / Channel，也不自动建立登录 Session。

在仓库根准备独立随机码文件（不输出码，不复用数据库或用户密码）：

```text
python3 -c 'import os,secrets; fd=os.open("deploy/secrets/setup_code",os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600); os.write(fd,(secrets.token_urlsafe(32)+"\n").encode()); os.close(fd)'
```

将此文件通过受控权限提供给容器内 app 的 UID / GID `10001:10001` 只读访问，并限制宿主访问者。Linux bind-backed Secret 可能保留源文件权限，不能只依赖 Compose 的 `mode` 字段；根据宿主所有权 / ACL 配置并验证 app 可读取，不要为省事向所有用户开放。`RADISHNEXUS_SETUP_CODE_SOURCE_FILE` 可指定其他宿主文件；应用内部只使用绝对路径 `RADISHNEXUS_SETUP_CODE_FILE=/run/secrets/setup_code`。初始化码只挂到 app，不挂到 PostgreSQL、Caddy 或运维备份。

完成下节 migration 后，用可选 overlay 启动已构建的应用：

```text
docker compose -f deploy/compose.yaml -f deploy/compose.setup.yaml config --quiet
docker compose -f deploy/compose.yaml -f deploy/compose.setup.yaml up -d --wait app caddy
```

按下节方法信任本实例 Caddy CA 后访问 HTTPS 首页。部署者通过受控本地方式读取码文件，在表单填写初始化码、管理员邮箱 / 称呼 / 密码和工作区名称。成功后使用新账户正式登录，继续创建团队与项目。不要把码放进 URL、聊天记录、截图或日志。

完成后用基础 Compose 配置重新创建 app，移除初始化 Secret 挂载，再按部署者的数据处理方式销毁码文件。即使未移除文件，任何账户已存在时数据库仍拒绝再次初始化。并发创建仅一个成功；结果不明确时在页面重新检查状态，不重复覆盖账户。没有配置码的新实例会提示部署者处理；不会开放无验证注册。

以下 CLI 路径继续有效，与网页入口共享同一事务锁；两者选择其一，不需要先运行 CLI 再使用网页。

## 全新实例初始化

先只启动 PostgreSQL，并等待官方 healthcheck 成功：

```text
docker compose -f deploy/compose.yaml up -d --wait postgres
docker compose -f deploy/compose.yaml run --rm migrate
```

migration 成功后，可按上文[网页首次初始化准备](#网页首次初始化准备)启动 overlay 并创建管理员；也可选择以下 CLI 路径：从标准输入建立唯一一次本地管理员与 Workspace owner。管理员密码与数据库密码必须不同；密码不会保存为 Compose Secret：

```text
python3 -c 'import getpass,json; print(json.dumps({"email":getpass.getpass("Email: "),"password":getpass.getpass("Password: ")}))' | \
  docker compose -f deploy/compose.yaml run --rm -T bootstrap \
    --display-name "First Admin" \
    --workspace-name "First Workspace" \
    --credentials-stdin
```

采用 CLI 时，migration 和 bootstrap 成功后启动公共入口：

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

## 身份模型升级

从 migration 007 或更早版本升级前，使用旧版本运维工具备份，并准备私有的旧用户 ID 到邮箱 JSON 映射。安排维护窗口，停止公共入口和旧应用写入，再同步更新 Go / Web / operation 工件。使用匹配的新版本工具显式执行 migration 后，旧 Session 全部失效；已保留的密码 verifier 在完成邮箱映射后恢复登录：

```text
docker compose -f deploy/compose.yaml run --rm migrate
docker compose -f deploy/compose.yaml run --rm -T identity-migrate \
  --mapping-stdin < /path/to/private-identity-email-mapping.json
```

格式与失败边界见[服务端账户升级](../server/README.md#账户升级与成员准入)。完成后再启动匹配的新应用与公共入口，并分别验证 `/health/ready` 和邮箱登录；就绪探针只验证 migration history，不验证邮箱是否已映射。禁止猜测邮箱、清库重建或用旧二进制写新 schema。回退需要旧版本工件及升级前备份，恢复至空目标；本次代码更新不代表真实实例已经迁移，也不构成当前版本 Compose 升级演练通过的证据。

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

恢复工件保留本地账号 verifier，以及外部身份映射，但不恢复 Session、未兑换邀请或 OIDC 授权事务。恢复成功后不要再次 bootstrap；直接启动 `app` 与 `caddy` 并重新登录。跨 major、非空目标、损坏工件或 migration 漂移仍会失败，精确边界见 [ADR-0010](../docs/adr/0010-verified-postgresql-backup-and-restore.md)。

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
| 尚未初始化 | 网页读取 `/api/v1/setup`：`required` 可提交初始化码，`unavailable` 需部署者配置码或选择 CLI；`complete` 表示已有账户。CLI 与网页均只允许首次创建成功 |
| Web build 缺失 | application image build 或 Go server 启动失败，不退回 fixture |
| origin / Host / proxy 配置错误 | app 启动错误、认证 transport 的稳定安全错误或 Caddy health 失败 |
| 浏览器不信任证书 | 导出并信任当前 `caddy_data` 中的公开 root CA，不关闭 HTTPS 校验 |

`/health/ready` 现在检查数据库 migration history 与当前应用工件的序号、名称和 checksum 完全匹配。匹配返回 `204`；缺失、未迁移完成、漂移、数据库更新于二进制、连接或查询失败返回 `503`，预算为 2 秒，响应不缓存。应用不自动迁移或修复；先检查显式 `migrate` operation 的结果及工件版本，不以修改历史表绕过失败。`/health/live` 仍只检查进程存活。

就绪成功不代表 bootstrap 或旧账户邮箱映射已完成，也不验证 migration 之外的手工 DDL；初始化与升级操作仍分别确认结果。当前不支持跨 schema 版本滚动兼容，探针只是即时判断，不代替升级窗口协调。详见[服务端健康检查](../server/README.md#存活与业务就绪)。

## 当前 schema 与演练证据

当前源码构建的工件包含 migration 018 和 Activity v7。checkout binding / built-from、Ticket 状态版本与精确 receipt，以及既有对象和关系均纳入权威备份；恢复后显式重建 Activity。014→015 的混合来源恢复与 015→016 的状态升级证据分别见[checkout 来源记录](../docs/status/reviews/2026-10-13-checkout-provenance.md)和[Ticket 状态记录](../docs/status/reviews/2026-10-13-ticket-status-transitions.md)。

Go、Web 与 schema 必须配套更新，不支持跨 schema 版本滚动兼容。升级前停写并保留备份；016 只允许旧 Ticket 全为 open 的合法基线，异常状态须先调查，不手改状态绕过保护。升级后旧二进制不能直接回退，需要恢复升级前备份到全新目标或独立验证 forward repair，并明确备份点后的数据取舍。应用缓存启动时的静态入口，须发布完整匹配的 Web build 并重启应用，不能在运行中替换资源后继续沿用旧入口；浏览器须重新加载。

这些隔离数据库与浏览器结果不代表业务实例已迁移，也不代替完整 Compose 镜像升级演练。2026-10-18 已验收 migration 017 工件的全新 Compose、文件 Secret、就绪探针与账号恢复；旧 016 工件空目标恢复后显式升级已有独立证据。后续业务全链、生产带流量升级和通用升级中断恢复仍须独立验收，见[本轮记录](../docs/status/reviews/2026-10-18-controlled-account-recovery.md)。

## Workspace owner 主动交接与恢复

当前 migration 018 为 Workspace membership 增加角色代次，为邀请绑定创建者任期，并扩展不可变配置审计 / receipt。与匹配的 Go / Web 一起升级；停写、使用旧工件备份、显式 migration、部署新工件后检查 readiness。既有 membership 从代次 1 开始，有效旧邀请绑定该代次，失效邀请不会恢复；迁移不修复零 owner 实例。交接使用 Web Session 权限，不提供运维强制 owner 接管入口，见 [ADR-0042](../docs/adr/0042-workspace-owner-handoff.md)。

同版本备份保留角色、代次与交接历史；恢复不带回 Session、邀请或 OIDC 事务。恢复 017 旧备份会带回备份时的 owner，并可能缺少后来成功的交接 receipt；必须用匹配旧工件恢复到全新隔离目标，再显式升级、重新登录并核验实际管理责任，不从原实例操作 ID 自动重放交接，也不重新 bootstrap。

本轮已用本地 `333c4ce` 旧工件验证该恢复链，见[实施记录](../docs/status/reviews/2026-10-18-workspace-owner-handoff.md)。复验命令从仓库根运行，需要另行授权隔离 Docker 资源：

```bash
RADISHNEXUS_OWNER_TRANSFER_LEGACY_REF=333c4ce ./scripts/check-server-backup-restore.sh
```

脚本只读取本地 Git 对象并在临时目录构建旧工具，不 fetch 或切换工作区；默认未指定旧 ref 时只验证同版本恢复，旧工件测试明确跳过。此证据不等同生产带流量升级或通用升级中断恢复。

## 本地账户受控恢复

使用 [ADR-0041](../docs/adr/0041-controlled-local-account-recovery.md) 的独立 `account-recover` operations service。该 service 默认不运行，二进制不进入常驻 server 镜像。部署者须先独立核实恢复请求和账户归属，核对部署目录、Compose project、数据库 Secret 与目标实例；备份副本可能拥有相同 user ID，inspect 不构成实例身份认证。此入口能接管目标全局账户，需要宿主 / 数据库运维授权；Workspace owner 身份不能替代运维授权。

升级窗口先停写，使用匹配的旧工件留存受控备份，再显式运行当前 migration 018（含 017 恢复审计），并部署匹配的新 app / operations。不得与恢复命令并行迁移；readiness 会拒绝旧、漂移或较新的 schema，但不是升级锁。若要求没有任何已认证的在途操作，先摘流并等待 app 停止；恢复命令本身不停止服务。已有运行授权之外的生产升级、备份位置与保留策略须独立确定。

下列命令从仓库根执行，使用前面配置的 Compose 环境和文件 Secret。`getpass` 从终端无回显读取；不要用真实值替换成命令参数或环境变量，也不要保存密码 JSON。先 inspect：

```text
python3 -c 'import getpass,json; print(json.dumps({"email":getpass.getpass("Verified email: ")}))' | \
  docker compose -f deploy/compose.yaml run --rm --no-deps -T account-recover inspect --request-stdin
```

核对返回的 `user_id`、两个 status 和 `eligible`，保留原样的六位微秒 UTC `password_changed_at`。仅已有 active、邮箱已映射的本地凭证可恢复；临时登录锁可随成功操作清除，禁用账户 / 凭证、OIDC-only、未映射 legacy 不会被激活或补建。

执行 apply 前生成并保存 operation_id，并在受控运维记录中关联人工核验、操作者与目标。下面辅助程序向 stderr 显示安全的 operation_id，邮箱和最终密码不回显；用户本人应在受控终端输入最终密码。没有临时密码自动过期或强制首次改密功能，运维者代设 / 转交密码的责任不能省略。

```text
python3 -c '
import getpass,json,sys,uuid
operation_id = str(uuid.uuid4())
print("Record operation_id before continuing: " + operation_id, file=sys.stderr)
user_id = getpass.getpass("Verified user_id: ")
version = getpass.getpass("Exact inspected password_changed_at: ")
operator_ref = getpass.getpass("Controlled operator_ref (ASCII token): ")
email = getpass.getpass("Verified email: ")
password = getpass.getpass("Final new password: ")
if getpass.getpass("Type APPLY to revoke all old Sessions for this account: ") != "APPLY":
    sys.exit(1)
print(json.dumps({"user_id":user_id,"expected_email":email,"expected_password_changed_at":version,"operation_id":operation_id,"operator_ref":operator_ref,"reason_code":"lost_local_password","new_password":password,"confirmed":True}))
' | docker compose -f deploy/compose.yaml run --rm --no-deps -T account-recover apply --request-stdin
```

发生 `result_unconfirmed`、stdout 丢失或无法判断提交时，保留原 ID；先查询 status：

```text
python3 -c 'import getpass,json; print(json.dumps({"user_id":getpass.getpass("Original user_id: "),"operation_id":getpass.getpass("Original operation_id: ")}))' | \
  docker compose -f deploy/compose.yaml run --rm --no-deps -T account-recover status --request-stdin
```

`applied` 是该操作的历史成功证据，不证明密码仍未被后续操作更新；`not_observed` 只是当前快照未见记录，不能排除在途提交。待原进程结束并诊断连接后，用原 operation_id / 原版本处理重试；已记录操作会拒绝再次 apply，不自动生成新 ID、不验证本次输入密码。通过正式 HTTPS 登录确认最终密码。

密码、旧 Session 撤销和 operator 审计原子提交；稳定 user、各工作区权限、业务引用、外部绑定和未兑换邀请不变。旧 Session / CSRF 的下一次解析拒绝，SSE 在发送 / heartbeat 复核关闭。此流程不等于外部 IdP 登出、邀请撤销、管理员交接或完整失陷处置。

恢复后的备份保留新密码与审计，Session / 邀请 / OIDC 事务数据不恢复；旧备份可能恢复旧 verifier 和旧审计状态。旧工件只能恢复匹配旧备份到隔离空目标，再显式升级、重新 inspect 和执行新的恢复，登录验证后才能开放。当前 Compose 仍共用数据库凭据，不代表数据库最小权限分工已完成。应用与代理不记录密码 / Cookie / CSRF；自定义数据库 statement / 参数 / Detail 日志和外部运维采集必须单独审查，禁止启用会记录凭据的调试采集。

## 仓库演练

准备好配置中记录的五个固定基础镜像后，从仓库根运行：

```text
./scripts/check-self-hosted-compose.sh
```

脚本只使用本次生成的随机 Compose project、临时 Secret、随机 HTTPS 端口和全新命名 volumes。它验证静态配置、镜像构建、显式 migration、一次 bootstrap、重复 bootstrap 拒绝、Caddy CA、伪造转发 Header 清洗、login / Session / logout、正式 Message SSE 在连接保持打开时及时 flush `ready` / `message.created`，独立账户恢复 CLI、旧 Cookie / CSRF 拒绝、SSE 在恢复后通过 heartbeat / 发送复核关闭、代理敏感 Header 过滤、正式邀请 / 独立登录后的 owner 交接、旧邀请失效、本人历史精确重试与接任者配置，以及应用和数据库无宿主端口；退出时删除本次 project、临时网络、volumes 和 Secret，不删除用户实例或备份。SSE 仍由唯一 Caddy origin 转发，Caddy 2.11.4 对 `text/event-stream` 的流式识别已由该门禁固定验证；更换代理或版本必须重跑。

需要配合真实浏览器人工复核时，可显式设置 `RADISHNEXUS_COMPOSE_BROWSER_REVIEW=true`。测试在恢复前后打印私有临时目录中的 `browser-*.json` 标记路径，完成该阶段浏览器操作后创建同名 `.done` 文件继续；每阶段最多等待十分钟，超时失败并按原范围清理。默认自动验收不暂停，不开启生产服务。

Workspace owner 交接的浏览器复核可单独设置 `RADISHNEXUS_OWNER_TRANSFER_BROWSER_REVIEW=true`；脚本在正式创建同名候选后输出私有临时 JSON 与 `.done` 路径，最多等待 20 分钟。此阶段可验证交接、响应丢失后的历史查询及手机布局；继续前将初始管理员恢复为 owner，以便后续自动链使用预期的正式角色。全部角色变更均通过交接 API，不用 SQL 改角色；默认不暂停。该标记沿用同一次性 Compose project 和清理范围，不授权其他实例操作。
