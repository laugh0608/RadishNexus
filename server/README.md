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
- 为 Decision、Ticket 和 CI Run 返回 Current、Relations 和 Timeline 的权限过滤 Nexus View query；
- 把已验证用户身份转换为 application `Principal` 的最小认证 adapter；
- 把 application sentinel error 转换为内部 HTTP 状态与安全机器码的显式映射。

业务写入尚未暴露为 HTTP API。认证 adapter、公共错误对象和 API 版本策略冻结前，调用方应直接通过 application service 测试领域与权限边界。Jenkins 核心同样不读取请求或验证签名；只有完成来源认证、重放校验和字段映射的调用方才能构造 `VerifiedJenkinsDelivery`。receipt 只保存规范化 SHA-256 和最终引用，不保存 Secret 或原始 webhook body。

当前 Activity 白名单包含 `decision.proposed`、`decision.accepted`、`ticket.created`、`ci-run.recorded` 和 `deployment.recorded`。重建通过 `postgres.Store.RebuildActivityProjection` 显式触发，不依赖 Outbox 投递状态，也尚未建立常驻 projector worker。Activity 只保存引用和状态等最小安全事实；Nexus View 在读取时按当前权限重新解析 subject，不能读取的目标只形成通用 restricted 占位。

CI Run 的 M0 用户读取由所属 Component 控制：同一 Workspace 的活跃成员可读，非成员、暂停成员和跨 Workspace 主体得到 not-found；owner Team 和 Jenkins source 都不授予读取权。CI Run Current 只返回 status、受控时间与当前 Component，Timeline 隐藏 plugin/source ID，并且不返回 external run key、receipt、digest、Secret、原始 payload 或外部 URL。该 query 仍是内部 application contract，尚未形成 HTTP 或公共响应 schema。

staging Deployment 只记录外部已经完成的终态事实，不执行部署。目标必须是 active staging Environment，来源必须是 succeeded CI Run，调用者必须是 active Workspace 用户并持有该 Environment 的显式授权；Project 角色、owner Team 和 CI source 不隐式授予部署能力。Deployment、`deploys` 关系、`deployment.recorded` 和 Outbox 同事务提交。当前尚未建立 Deployment Nexus View、Web 页面、授权管理入口、production、审批、回滚或执行引擎。

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

runner 使用 session advisory lock 防止并发执行，每个 migration 单独事务提交，并拒绝已应用文件发生漂移。它只支持向前迁移；生产升级仍需在正式部署方案中补齐备份、失败中断、forward repair 和恢复限制。
