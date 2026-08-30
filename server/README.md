# RadishNexus Go Server

这是 RadishNexus 唯一的正式 Go 服务 module。当前纵向切片包含：

- 标准库 HTTP 存活与就绪检查；
- 基于原生 `pgx/v5`、连续编号与 SHA-256 漂移检测的显式 PostgreSQL migration；
- Thread → Decision → Ticket 的 application service；
- 已验证 Jenkins delivery → 完成态 CI Run 的 application service；
- 显式授权用户记录终态 staging Deployment 的 application service；
- Project 角色、restricted Thread 和关系投影权限；
- 与业务状态同事务写入的不可变领域事件与 Outbox 投递状态；
- 正式 Component、CI Run 与不可变 inbound delivery receipt schema；
- 正式 Environment、环境级部署授权与不可变 Deployment schema；
- 从领域事件原子、幂等重建的 Activity projection version 1；
- 为 Decision、Ticket、CI Run 和 Deployment 返回 Current、Relations 和 Timeline 的权限过滤 Nexus View query；
- 把已验证用户身份转换为 application `Principal` 的最小认证 adapter；
- 把 application sentinel error 转换为内部 HTTP 状态与安全机器码的显式映射。
- PostgreSQL 17 同 major 的版本化备份、全新空目标恢复、migration 校验与 Activity 重建命令。

业务写入尚未暴露为 HTTP API。认证 adapter、公共错误对象和 API 版本策略冻结前，调用方应直接通过 application service 测试领域与权限边界。Jenkins 核心同样不读取请求或验证签名；只有完成来源认证、重放校验和字段映射的调用方才能构造 `VerifiedJenkinsDelivery`。receipt 只保存规范化 SHA-256 和最终引用，不保存 Secret 或原始 webhook body。

当前 Activity 白名单包含 `decision.proposed`、`decision.accepted`、`ticket.created`、`ci-run.recorded` 和 `deployment.recorded`。重建通过 `postgres.Store.RebuildActivityProjection` 显式触发，不依赖 Outbox 投递状态，也尚未建立常驻 projector worker。Activity 只保存引用和状态等最小安全事实；Nexus View 在读取时按当前权限重新解析 subject，不能读取的目标只形成通用 restricted 占位。

CI Run 的 M0 用户读取由所属 Component 控制：同一 Workspace 的活跃成员可读，非成员、暂停成员和跨 Workspace 主体得到 not-found；owner Team 和 Jenkins source 都不授予读取权。CI Run Current 只返回 status、受控时间与当前 Component，Timeline 隐藏 plugin/source ID，并且不返回 external run key、receipt、digest、Secret、原始 payload 或外部 URL。该 query 仍是内部 application contract，尚未形成 HTTP 或公共响应 schema。

staging Deployment 只记录外部已经完成的终态事实，不执行部署。目标必须是 active staging Environment，来源必须是 succeeded CI Run，调用者必须是 active Workspace 用户并持有该 Environment 的显式授权；Project 角色、owner Team 和 CI source 不隐式授予部署能力。Deployment、`deploys` 关系、`deployment.recorded` 和 Outbox 同事务提交。

Deployment 的 M0 读取与写授权分离：同一 Workspace 的 active 成员只有同时能读取目标 Environment 与来源 CI Run 时才可读取；非成员、暂停成员和跨 Workspace 主体得到 not-found，Environment 归档不隐藏既有历史。Current 只返回终态、受控时间、Environment 与来源 CI Run；Relations 和 Timeline 复用当前权限，不返回 authorization ID、调用 source、Jenkins receipt、digest、Secret、原始 payload 或外部 URL。该 query 仍是内部 application contract；授权管理入口、production、审批、回滚和执行引擎均未建立。

当前认证 adapter 不读取 Header、Cookie、Token 或 OIDC claims，也不负责验证凭据；未来的本地 session 或 OIDC verifier 只有在成功认证后才能向它提供 `VerifiedUser`。HTTP error mapping 不是公共响应 schema，也不写 response body；未来 handler 仍需单独确定 request ID、日志、内容类型和公共错误对象。不可读资源由 application service 返回 `not found`，transport 不把它改写成 `forbidden`。

## 本地检查

从仓库根运行：

```text
./scripts/check-server.sh
./scripts/check-server-postgres.sh
```

## 数据库迁移

迁移不会在服务启动时隐式执行。进入 `server/`、设置 `DATABASE_URL` 后显式运行：

```text
go run ./cmd/nexus-migrate
```

runner 使用 session advisory lock 防止并发执行，每个 migration 单独事务提交，并拒绝已应用文件发生漂移。它只支持向前迁移；当前已建立 PostgreSQL 17 同 major 的最小恢复，跨版本生产升级仍需补齐失败中断、forward repair、兼容窗口和恢复限制。

## 最小备份与恢复

当前备份是 PostgreSQL 17 同 major 的整库运维工件，不是 `.nexus` 开放导出格式。工件目录包含 `manifest.json` 和 custom-format `database.dump`；源库必须完整匹配当前 migrations，所有非系统 relation 必须已经由当前二进制分类。`activity_items` 只保留 schema，不备份投影数据；恢复成功后命令从不可变领域事件重建 Activity。

进入 `server/` 并设置 `DATABASE_URL` 后，备份到一个尚不存在的新目录：

```text
go run ./cmd/nexus-backup --output /path/to/new-backup-directory
```

恢复到一个全新的空 PostgreSQL 17 数据库：

```text
go run ./cmd/nexus-restore --input /path/to/completed-backup-directory
```

两个命令默认从 `PATH` 查找 PostgreSQL 17 的 `pg_dump` 和 `pg_restore`。受控环境可以用 `RADISHNEXUS_PG_DUMP` 与 `RADISHNEXUS_PG_RESTORE` 指定固定绝对路径。连接密码只通过 `DATABASE_URL` 和子进程环境传递；不要把真实 DSN 写入命令参数、日志或仓库文件。

当前 M0 工具桥接只接受显式 `sslmode=disable` 的本地或受控私有连接；TLS 配置会直接失败，因为 `pgx` TLS config 不能被安全近似成 libpq 的 `verify-ca / verify-full` 参数。不要为了使用备份命令而关闭远程数据库原有 TLS 要求；远程 TLS 工具连接需要后续独立冻结。

恢复命令拒绝非空目标，不执行 `--clean`、DROP 或自动覆盖；它以单事务恢复 archive，再显式运行正式 migration 并重建 Activity。manifest checksum、dump SHA-256、PostgreSQL major、migration history、表分类或恢复 TOC 不符合当前合同都会在可能时于目标写入前失败。失败目标不得原地清理后冒充恢复成功，应丢弃并重新创建全新数据库。

双实例往返验证从仓库根运行：

```text
./scripts/check-server-backup-restore.sh
```

该脚本使用两个独立的固定 PostgreSQL 17 容器，验证完整 Golden Path fixture、恢复前后所有纳入表、Activity 重建，以及 manifest 漂移、dump 损坏和非空目标失败路径；不会隐式拉取缺失镜像。
