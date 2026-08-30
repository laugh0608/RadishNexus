# ADR-0010：可验证 PostgreSQL 备份与全新实例恢复

状态：已接受

日期：2026-08-30

## 背景

Golden Path 已经能够从 restricted Thread 形成 Decision 和 Ticket，原子记录 Jenkins CI Run，并由持有 Environment 显式授权的用户记录 staging Deployment。M0.5 的最后一个关键问题是：实例发生迁移或损坏后，这些稳定身份、关系来源、授权 provenance、delivery receipt 和不可变事件能否在全新 PostgreSQL 实例上恢复，而不是只证明 `pg_dump` 成功退出。

正式 migration runner 只向前推进，并通过连续编号、artifact SHA-256、session advisory lock 和单 migration 事务拒绝历史漂移。备份恢复必须复用这条边界，不能通过覆盖数据库、忽略 checksum 或在服务启动时隐式执行 migration 制造成功。

Activity 是从领域事件生成的可重建投影，不是权威业务事实。另一方面，Environment deployment authorization、EntityLink 来源、Jenkins inbound delivery receipt、领域事件和必要 Outbox 状态都是恢复后解释业务链路所需的权威记录。未来还会出现 Secret 和插件配置，因此备份命令不能采用“新增表自动进入备份数据”的开放边界。

本切片验证整库 PostgreSQL 运维备份，不定义跨产品版本的 `.nexus` 可移植上下文包，也不承诺跨 PostgreSQL 大版本恢复。

## 决定

### 备份工件与版本

- format version 1 是一个新建目录，固定包含 `manifest.json` 和 PostgreSQL custom-format `database.dump`；目录先在同级临时路径生成，完成校验后原子改名，既有输出目录不会被覆盖。
- manifest 记录格式版本、UTC 创建时间、PostgreSQL 与 `pg_dump` major、完整 migration artifact identity、纳入和排除的数据表清单，以及 dump 文件名、格式、大小和 SHA-256。
- 当前只接受固定 PostgreSQL 17 server、`pg_dump` 17 和 `pg_restore` 17。正式支持矩阵、跨大版本恢复和 forward repair 必须由后续证据单独冻结。
- 这是数据库备份，不是开放导出格式；内部表、PostgreSQL DDL 和 custom archive 都可以随受控 migration 演进。

### 数据范围与 Secret 停止线

- 备份前，源数据库的 migration history 必须与当前二进制内嵌 migrations 完全一致；少 migration、更多 migration、名称变化或 checksum 漂移均拒绝。
- 当前非系统 schema 必须恰好为 `public` 与 `radishnexus`，extension 必须只有默认 `plpgsql`，数据库不得含 large object；所有 table、partitioned table、view、materialized view、sequence 和 foreign table 都必须由当前二进制完整分类。未知 relation 直接拒绝，不能因全库 dump 而静默纳入。
- 权威数据包含稳定身份、Workspace 和成员关系、Team / Project 权限上下文、Thread / Decision / Ticket、Component / CI Run、Environment / Deployment、环境授权 provenance、EntityLink、领域事件、Jenkins inbound delivery receipt、必要 Outbox 状态和 `radishnexus_schema_migrations`。
- `activity_items` 只保留 schema，不备份数据。恢复后从不可变领域事件重建 projection version 1，并验证重建结果。
- 当前 schema 不存在 Secret 表。未来任何新增 relation 必须先明确归类；Secret、Token、Jenkins 凭据、原始 webhook payload、本机授权材料和只对原实例有效的密钥默认不得进入纳入清单或 manifest。
- dump 排除 large object、publication、subscription、security label、comment 和 tablespace，并使用 `--no-owner` 与 `--no-privileges`；源实例连接信息、role ownership 或 ACL 不会被冒充成可移植授权契约，数据库内业务授权记录仍作为权威数据保存。

### 恢复前提与顺序

- `nexus-restore` 只接受 manifest、大小和 SHA-256 全部匹配的固定工件；manifest 未知字段、migration 漂移、表分类变化和 PostgreSQL major 不匹配均在目标写入前失败。
- 目标必须是全新空数据库：除空 `public` schema 外不得已有自定义 schema 或 user relation。命令不提供 `--clean`、`--create`、自动 DROP 或覆盖模式。
- `radishnexus.valid_entity_id` 在恢复表数据时依赖 `entity_types` 注册表。`pg_dump` 无法从函数查询推断这条数据依赖，因此恢复命令检查 archive TOC，并把 `entity_types` 的 TABLE DATA 显式排在其它 TABLE DATA 前；没有该条目、重复条目或出现 `activity_items` TABLE DATA 均拒绝。
- `pg_restore` 使用 custom archive、显式 TOC、`--single-transaction`、`--exit-on-error`、`--no-owner` 和 `--no-privileges`。恢复过程不并行，不禁用 trigger，也不绕过 check / foreign key 约束。
- archive 恢复后，命令显式调用正式 forward-only migration runner；当前同版本恢复应是幂等校验，未来只有另行支持的旧备份才能继续向前 migration。
- CLI 在权威数据恢复与 migration 成功后原子重建 Activity。Activity 重建失败时命令返回失败，不把目标报告为可用；M0 操作方应丢弃该失败目标并重新创建全新数据库，而不是在原地自动清理。

### 命令与凭据

- `nexus-backup --output <new-directory>` 和 `nexus-restore --input <backup-directory>` 是显式运维命令，不随主服务启动执行。
- 连接由 `DATABASE_URL` 提供。调用 PostgreSQL 官方工具时，连接字段通过子进程环境传递，密码不进入命令参数或成功输出。
- 当前 M0 工具桥接只接受显式 `sslmode=disable` 的本地或受控私有传输；`pgx` TLS config 不能被近似映射为较弱的 libpq 校验模式，任何 TLS 配置都会 fail closed。正式远程 TLS 备份必须先建立不降低 `verify-ca / verify-full`、证书与 key file 语义的独立连接契约。
- 默认从 `PATH` 查找 `pg_dump` / `pg_restore`；受控部署可以分别通过 `RADISHNEXUS_PG_DUMP` 与 `RADISHNEXUS_PG_RESTORE` 指定固定工具路径。
- 命令输出只报告工件位置、格式版本、PostgreSQL major 和 Activity 重建数量，不输出 DSN、密码、payload 或业务正文。

## 未采用的方案

### 只保存 `pg_dump` 文件

没有 manifest、migration identity、数据范围和恢复往返证据时，dump 成功不等于能够恢复，也无法判断文件损坏、工具版本或未来 Secret 表是否进入工件。

### 在现有目标上使用 `--clean`

这会让路径或连接配置错误直接删除用户数据，并掩盖目标环境是否真正全新。M0 恢复只允许显式创建的空目标。

### 先 migration 空目标，再导入 data-only dump

旧备份数据结构未必与当前 schema 相同，也无法自然保留备份时的 DDL 和 migration history。当前选择先恢复完整同版本 archive，再由正式 runner 校验并向前推进。

### 备份 Activity 数据并把它作为恢复依据

Activity 是权限过滤投影。把它当作权威备份会形成第二套事实来源，并掩盖领域事件已经损坏或 projector 无法重建的问题。

### 立即定义 `.nexus` 导出或跨 PostgreSQL 大版本支持

`.nexus` 需要开放对象格式、脱敏、权限裁剪、附件和导入报告；跨大版本支持需要独立兼容矩阵。这些都不是证明当前 Golden Path 整库可恢复所必需的最小范围。

## 后果

正面影响：

- Golden Path 的稳定 ID、权限来源、EntityLink、receipt、领域事件与 Outbox 可以在第二个真实 PostgreSQL 实例上往返验证；
- 未知表和 relation 默认 fail closed，未来引入 Secret 时必须显式审查备份边界；
- Activity 丢弃后仍能重建，继续保持领域事件为权威事实；
- 非空目标、损坏 dump 和 migration 漂移不会触发覆盖或产生伪成功报告；
- 备份恢复与 forward-only migration 形成同一条显式运维路径。

成本与风险：

- format version 1 只支持 PostgreSQL 17 同 major，不是完整生产支持矩阵；
- custom archive 仍会执行源数据库中的 DDL，只能恢复来自受信 RadishNexus 实例且 checksum 完整的工件；
- 单事务恢复在大数据库上可能占用较多锁与时间；超出 M0 数据规模后需要基于真实容量评估 transaction size 和恢复窗口；
- 失败的 Activity 重建不会自动删除已经恢复的权威数据，操作方必须丢弃失败目标并重新开始；
- 当前没有加密、对象存储上传、保留策略、定时任务或图形化管理入口。
- 当前 CLI 尚不支持 TLS 数据库连接，只适用于本地或受控私有网络中的 PostgreSQL 17；不能为了接入远程实例关闭原有 TLS 要求。

## 迁移与验证

本切片不新增数据库 migration。`db.CurrentMigrationHistory` 只暴露内嵌 migration 的 sequence、name 与 checksum，供 manifest 和恢复前后校验使用，不暴露 SQL body。

双实例验证使用相同固定 digest 的两个独立 PostgreSQL 17 容器：

1. 在源实例显式执行 migrations，并通过 application service 建立 Thread → Decision → Ticket、Jenkins CI Run 和显式 staging Deployment fixture；
2. 重建源 Activity，生成不含 Activity 数据的备份；
3. 在第二个全新实例恢复 custom archive，显式校验 migration，并重建 Activity；
4. 对每个纳入表比较完整 JSONB 行快照，覆盖稳定 ID、授权 provenance、EntityLink 来源、delivery digest、event correlation / actor 和 Outbox 状态；
5. 比较恢复前后 Activity 全量快照；
6. 修改 manifest migration checksum、损坏 dump 和对非空目标重复恢复均必须失败，且目标在前两种 preflight 失败后保持空，重复恢复不改变既有数据。

进入跨版本升级、自动调度、远程对象存储、加密备份、Secret backup、增量备份、point-in-time recovery 或 `.nexus` 导出前，必须分别补齐新的数据边界和恢复证据。

## 后续演进

[ADR-0012](0012-local-identity-and-session-foundation.md) 随 migration 005 引入本地账号与服务端 Session 后，显式把 `local_accounts` 归为运维恢复所需的权威数据，把 `user_sessions` 归为只恢复 schema、不恢复数据的实例登录态。密码 verifier 因此随受控 PostgreSQL 备份保留，opaque Session 与 CSRF token digest 不进入数据工件；该变化不授权未来 `.nexus` 可移植导出携带 verifier、Token 或其它 credential。
