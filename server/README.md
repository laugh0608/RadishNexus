# RadishNexus Go Server

这是 RadishNexus 唯一的正式 Go 服务 module。当前纵向切片包含：

- 标准库 HTTP 存活与就绪检查；
- 基于原生 `pgx/v5`、连续编号与 SHA-256 漂移检测的显式 PostgreSQL migration；
- Thread → Decision → Ticket 的 application service；
- Project 角色、restricted Thread 和关系投影权限；
- 与业务状态同事务写入的不可变领域事件与 Outbox 投递状态；
- 从领域事件原子、幂等重建的 Activity projection version 1；
- 为 Decision 和 Ticket 返回 Current、Relations 和 Timeline 的权限过滤 Nexus View query。

业务写入尚未暴露为 HTTP API。认证 adapter、公共错误对象和 API 版本策略冻结前，调用方应直接通过 application service 测试领域与权限边界。

当前 Activity 白名单只包含 `decision.proposed`、`decision.accepted` 和 `ticket.created`。重建通过 `postgres.Store.RebuildActivityProjection` 显式触发，不依赖 Outbox 投递状态，也尚未建立常驻 projector worker。Activity 只保存引用和状态等最小安全事实；Nexus View 在读取时按当前权限重新解析 subject，不能读取的目标只形成通用 restricted 占位。

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

runner 使用 session advisory lock 防止并发执行，每个 migration 单独事务提交，并拒绝已应用文件发生漂移。它只支持向前迁移；生产升级仍需在正式部署方案中补齐备份、失败中断、forward repair 和恢复限制。
