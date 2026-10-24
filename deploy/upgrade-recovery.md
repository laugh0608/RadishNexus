# 单宿主停机升级与失败恢复手册

状态：ADR-0043 的执行手册；具体已验收环境和缺口见[实施记录](../docs/status/reviews/2026-10-24-upgrade-failure-recovery.md)。本手册不授予实例操作权，也不承诺生产恢复时间。

适用于明确授权的非生产单宿主 Compose、PostgreSQL 17、单 Go app 与同源 HTTPS。迁移仍逐文件向前提交，schema 必须与工件精确匹配；恢复只写独立空目标。跨 major、滚动升级、PITR、自动降级和生产运维另立范围。操作流程依据 [ADR-0043](../docs/adr/0043-single-host-upgrade-failure-recovery.md)，备份格式和命令依据[部署说明](README.md#运维命令)。

## 先准备操作记录

每次维护建立私有记录，填实以下字段；不要直接复用演练中的路径、端口或实例名。

| 记录项 | 必须核对的内容 |
| --- | --- |
| 操作与责任 | 操作编号、部署负责人、停写/开放决定人、执行者、复验者、约定维护窗口 |
| 数据取舍 | 备份点、最后已确认事实、未确认请求、可接受停机和损失；备份后新写入是否允许舍弃 |
| 源身份 | 宿主、Docker context、Compose project、配置绝对路径、PostgreSQL 容器 ID、数据 volume、数据库 system identifier |
| 恢复目标身份 | 独立的 project、容器、数据 volume、system identifier；不得只比较 Workspace / user ID |
| 工件 | 旧/新 commit、app 与 operations 的实际 image ID 或 digest、配套 Web build、Caddy / Compose 配置；不能只记 `:local` |
| 恢复材料 | 匹配旧工具、已验证备份、独立保管的 Secret 和 CA/配置；目录权限、空间与保留责任 |
| 写入来源 | app、已有 SSE / 在途请求、Webhook / worker、定时 CLI、其他操作者及自动重启机制 |
| 结果 | 每阶段时间、实际 history、错误类别、验证结果、开放或保持隔离的决定、清理清单 |

只把脱敏结果写入仓库。完整备份含密码 verifier 和业务授权，不能作为普通诊断附件。不要记录 Secret、Cookie、邀请码、完整 DSN、SQL 参数、原始业务正文或 CA 私钥。

以下 shell 示例假设操作者已填写非敏感变量；配置中密码必须沿既有文件 Secret 方式提供。`upgrade_compose` / `restore_compose` 指向分别固定工件和独立 volume 的已审阅配置。切换旧/新工件时完整替换 app、operations、Web 与配套配置，不在运行容器内覆盖静态文件。

```bash
# 填写精确值，不使用默认 project，也不从当前目录猜目标。
: "${upgrade_project:?source Compose project required}"
: "${upgrade_compose:?absolute source Compose config required}"
: "${restore_project:?independent restore Compose project required}"
: "${restore_compose:?absolute restore Compose config required}"
source_compose=(docker compose --project-name "$upgrade_project" --file "$upgrade_compose")
restore_compose=(docker compose --project-name "$restore_project" --file "$restore_compose")
```

不要通过打印完整 `docker inspect`、Compose 环境或 `env` 收集诊断；下面只输出身份字段。若使用 bind mount，应记录实际 `Source` 而不是空 volume 名。

```bash
docker context show
source_postgres="$("${source_compose[@]}" ps --quiet postgres)"
: "${source_postgres:?source postgres is not running}"
docker inspect --format '{{.Id}} {{index .Config.Labels "com.docker.compose.project"}} {{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Type}} {{.Name}} {{.Source}}{{end}}{{end}}' "$source_postgres"
"${source_compose[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U radishnexus -d radishnexus <<'SQL'
SELECT system_identifier FROM pg_control_system();
SELECT current_database(), current_user, current_setting('server_version');
SELECT sequence, name, checksum, applied_at
FROM public.radishnexus_schema_migrations ORDER BY sequence;
SQL
```

把完整 history 与准确工件内 `server/db/migrations/*.sql` 的连续序号、名称和完整文件 SHA-256 比较。备份 preflight 会再次验证精确 history 及表分类。history 不验证任意手工 DDL；有未知改表、未分类表或材料缺失时停止，不能修改 history 或删除对象使检查通过。

## S0：工件与恢复材料核对

1. 记录现有 app / operations 的实际镜像身份，保留对应源码、构建配置、Web 和旧工具。用审阅后的配置指定不可变 image ID / digest，禁用隐式构建与拉取。目标 image ID 必须是本次选定工件。
2. 核对宿主可用磁盘和数据库量，留出备份、恢复副本及失败目标空间；不要用填满宿主磁盘验证故障。数据库量可用 `SELECT pg_database_size(current_database())` 读取。
3. 确认 Secret 文件可读、CA 与配置材料可用、HTTPS origin 精确、数据库不暴露宿主端口。检查仅输出可用与否，不 `cat` Secret。不得关闭 TLS 校验或扩大可信代理范围。
4. 检查旧镜像、旧 Web 和备份是否匹配。新工具拒绝旧 manifest 是正常保护，不能改 manifest 迁移列表或 checksum。
5. 若备份损坏、路径不可写、空间不足、历史漂移或环境身份不明，停在本阶段；保留源库与有效备份。

## S1：明确停写

先停止公共入口，再停止 app 和写入来源清单中其他任务。现有 Compose 的 `unless-stopped` 不会自行重启已明确停止的容器，但其他操作者、重新执行 `up` 或外部编排仍可能重启，需协调禁止。

```bash
"${source_compose[@]}" stop caddy app
"${source_compose[@]}" ps --status running --quiet app caddy
"${source_compose[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U radishnexus -d radishnexus <<'SQL'
SELECT pid, application_name, backend_type, state, xact_start,
       wait_event_type, wait_event
FROM pg_stat_activity
WHERE datname=current_database() AND pid<>pg_backend_pid()
ORDER BY pid;
SQL
```

`ps` 不得返回 app / caddy；所有额外客户端、在途事务和 writer 必须能解释并停止。检查已有 SSE 已关闭，维护入口不能再发起业务写入。不要输出 `pg_stat_activity.query`，其中可能含敏感参数。若需终止数据库 backend，先核对精确 PID、用途和所属实例，单独记录决定；不能按宽泛条件杀掉全部连接。

readiness / liveness、Caddy 探测间隔、`depends_on` 和客户端断线都不是停写证据。未知结果的业务请求按现有 receipt / 查询接口核实，不自动重试高风险操作。无法证明停写时不得迁移。

## S2：停写后备份并往返验证

源配置仍使用旧 operations。选择尚不存在的备份目录；失败不会覆盖已有成功目录。

```bash
: "${upgrade_backup_path:?new path inside the configured /backups mount required}"
"${source_compose[@]}" run --rm --no-deps -T backup --output "$upgrade_backup_path"
```

成功输出不等于已证明可恢复。校验 `manifest.json`、dump 文件 SHA-256 / 字节数与实际旧工具，再把完整备份复制到独立恢复目标的受控挂载目录；不要把 Secret 塞入备份格式。目标数据库必须刚建立且为空，不先运行 migration / bootstrap。

```bash
"${restore_compose[@]}" up -d --no-build --pull never --wait postgres
# 先按开头身份命令核对 restore project、容器、volume 和 system identifier。
: "${restore_backup_path:?matching backup path in restore mount required}"
"${restore_compose[@]}" run --rm --no-deps -T restore --input "$restore_backup_path"
```

目标必须采用与备份相符的旧 operations。完整 CLI 还会重建 Activity；只有 `restore completed` 且权威快照/关系/权限复验通过才能作为可恢复材料。源库仍保持停写；恢复副本此时也不开放公共入口。失败目标保留隔离，后续重试建立另一个空目标，不清库重跑。

## S3：显式迁移与结果判断

将源配置切到已核对的新 operations，但保持 app / caddy 停止。先记录完整迁移前 history，再显式执行：

```bash
"${source_compose[@]}" run --rm --no-deps -T migrate
```

无论退出成功、超时还是连接中断，都用新的只读连接核对实际 history 和本次 schema / 数据不变量。017→018 至少核对角色未被隐式转移、`role_revision` 初始为 1、有效旧邀请绑定任期、失效邀请不复活、旧 Audit / receipt 保持、不可变约束仍存在。

诊断锁与在途迁移时只读取元数据：

```sql
SELECT a.pid, a.application_name, a.state, a.xact_start,
       a.wait_event_type, a.wait_event,
       l.locktype, l.mode, l.granted, l.classid, l.objid
FROM pg_stat_activity a LEFT JOIN pg_locks l ON l.pid=a.pid
WHERE a.datname=current_database()
  AND a.pid<>pg_backend_pid()
ORDER BY a.pid, l.locktype;
```

| 实际证据 | 下一步 |
| --- | --- |
| runner / 事务仍在执行或等待锁 | 保持隔离，等待或针对已核对目标处理；不要再启动修复者 |
| 018 未提交，完整 017 history 和旧不变量保持 | 查明失败原因；确认无残留 writer 后，用同一目标工件显式续迁 |
| 016→018 中 017 已提交、018 失败 | 保留 017 前缀，仅续迁未提交的 018；不能把整个批次当作已回滚 |
| COMMIT 确认丢失，但 018 history 与关键 DDL / 数据都存在 | 按已提交处理；同工件重复迁移只确认已有事实，不重建记录 |
| 完整 history 与目标匹配，但 app / Web / 配置有问题 | 保持摘流，修复明确的装配问题并重新验证；不得换回旧 app 接新 schema |
| history 缺号、checksum 漂移、部分 DDL 与 history 不一致 | 停止并保留证据，另行审阅修复方案；不手改 history、checksum 或已发布 migration |

CLI 的请求 context 为 2 分钟，不是升级、网络断开、锁清理和退出的统一硬上限。不能按耗时或退出码猜数据库是否提交。重复执行前必须取得状态证据。

## S4：隔离验收

仅在部署负责人批准的受限验收入口启动整套匹配新工件：

```bash
"${source_compose[@]}" up -d --no-build --pull never --wait app caddy
: "${upgrade_origin:?exact HTTPS origin required}"
: "${upgrade_ca:?trusted CA certificate file required}"
curl --fail --silent --show-error --cacert "$upgrade_ca" "$upgrade_origin/health/ready"
```

浏览器完整重新加载，确认使用目标 Web build。普通成员通过正式 UI 登录、从 Project 找到 Ticket / Decision / Document，核对来源和 Timeline、Ticket 状态与 Document 历史，并执行一次协作写入及精确重试；无权成员仍不能发现受限对象。检查桌面及手机视口、SSE 与页面恢复。

所有验收写入记入操作记录：一旦写入发生，回到 S2 备份点会丢弃它们。普通 017→018 升级不会撤销所有 Session；旧备份恢复才要求重新登录，不能混为一谈。

## S5：唯一入口开放

重新开放前由部署负责人确认工件、数据状态、权限、测试结果和数据取舍。原源库与恢复副本有相同业务 ID，必须再次按宿主/project/volume/system identifier 辨认。

只允许一个目标接受用户写入，另一个保持 app / caddy 停止、网络隔离。首批拓扑没有自动流量切换器，不把改 DNS 或探针变绿当作自动完成。重新开放后观察错误、连接及读写，记录决定人与时间。旧工件和备份按约定保留，不提前自动删除。

## 选择恢复旧版本时

1. 对当前源库重新停写，保留失败状态与诊断摘要。列出备份后的业务/验收写入；若原维护授权未覆盖其损失，必须重新确认取舍。
2. 使用匹配旧工具，将验证过的备份恢复到独立空目标。新工具不能直接接旧 manifest，旧 app 也不能直接接新 schema。
3. 核对实体、来源关系、Ticket 状态、Document 版本、Audit / receipt 与重建 Activity。Session、邀请和 OIDC 事务应为空；已有实例 setup 必须仍关闭。
4. 旧密码 verifier、旧 owner 与缺失的后续 receipt 是快照事实。重新核实管理责任；缺失历史不能证明原操作未发生，不自动重放交接、恢复口令或重新赋权。
5. 用旧 app / Web 在隔离入口完成正式用户复验，再切换唯一入口。恢复旧服务本身不附带“再次升级到 018”的决定。
6. 若另行选择恢复后升级，重新按 S0～S5 执行，记录独立决定及新的备份点。

`pg_restore` 事务失败与 Activity 重建失败必须分别诊断。前者需要新连接核对是否回滚；后者可能已经有完整权威数据，但仍不能开放。不要以“表存在”“history 匹配”或“可以登录”替代完整恢复完成。

## 隔离验收入口

仓库提供独立演练脚本，不接受外部 DSN、已有 project 或用户 volume。它只从本地 Git 提取历史工件，建立随机临时 project、文件 Secret、localhost HTTPS 端口、独立数据卷与恢复副本；通过正式 API 建立合成业务链。

```bash
python3 scripts/check-upgrade-recovery.py \
  --old-ref 333c4ce --new-ref 171527f \
  --report /tmp/radishnexus-upgrade-result.json \
  --browser-review
```

默认构建关闭网络，缺少固定基础镜像或构建依赖时失败。只有已获下载现有锁定依赖授权时才增加 `--allow-locked-downloads`；它使用原始 Dockerfile，不修改版本或 lockfile，不拉取缺失基础镜像。旧工件只用于隔离演练，试用候选还须包含后续修复并重新验收。

`--browser-review` 在升级前、升级后、恢复旧服务、恢复后再次升级四个阶段提供私有 marker。复验者通过 UI 操作，完成后向同名 `.done` 文件写入明确结果，例如 `{"passed":true,"checks":["普通成员登录并从 Project 找回工单","手机视口无横向溢出"]}`；未做的检查不能填入。每阶段最多等待 20 分钟，超时失败并清理。未指定该参数时自动化仍可执行，但报告将浏览器逐项标为未运行。

底层故障矩阵由现有定向入口承接：

```bash
GOPROXY=off GOTOOLCHAIN=local ./scripts/check-server-postgres.sh
GOPROXY=off GOTOOLCHAIN=local RADISHNEXUS_OWNER_TRANSFER_LEGACY_REF=333c4ce \
  ./scripts/check-server-backup-restore.sh
```

数据库测试使用真实历史 migration，在隔离数据库中注入提交前 SQL 错误、context 取消、连接终止、真实 COMMIT 确认丢失、锁竞争及部分成功前缀；不修改已发布 migration。恢复测试调用真实 CLI / `pg_restore`，验证事务内错误、Activity 重建失败和错误工具 major；同版本与旧版本恢复证据分别记录。

脚本退出仅清理本轮随机 project 的容器、网络、volume、临时 Secret 和专属镜像标签；保留 Docker 构建缓存与指定位置的脱敏 JSON。清理失败会在报告标记并保留私有临时目录，不报告完整成功。真实运维的原库、成功备份和失败目标不采用这套测试自动清理规则。

## 复验与完成线

逐项记录 ADR-0043 U01～U28 的通过、失败、部分覆盖或未执行，并给出精确工件/环境、注入阶段和证据。不得将旧记录直接改写为本轮通过。分开记录停写、备份、迁移、恢复及验证耗时；Activity 重建若与 CLI 合并计时应明确标注，不编造独立测量。

另一操作者应依据手册走查至少四种状态：017 未提交失败、017 合法前缀、018 已提交但确认丢失、权威恢复成功但 Activity 重建失败。应能明确选择停止、续迁、空目标恢复或等待新授权，无需隐式知识。自动化通过不能代替独立操作者验收，也不代表外部试用、完整 Golden Path 或生产支持已开放。
