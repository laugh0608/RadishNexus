# ADR-0003：Go 服务端基础栈与数据访问

状态：已接受

日期：2026-08-28

## 背景

ADR-0002 已冻结稳定实体引用、授权结果、领域事件、Outbox 和 Activity 的逻辑边界。下一步需要选择能够承载这些边界的 Go HTTP 与 PostgreSQL 基础能力，但当前没有复杂路由、ORM、代码生成或多数据库兼容需求。

过早引入完整 Web 框架或 ORM 会把传输层和持久化层的便利模型带进领域层；完全依靠标准库数据库接口又不能提供 PostgreSQL 驱动和连接池。选型需要通过真实 Transactional Outbox、deferred constraint trigger、JSONB 和 Activity 重建实验，而不是只比较功能列表。

[M0 核心契约实验](../../experiments/m0-core-contracts/README.md)已经验证：

- Go 标准库 `net/http.ServeMux` 可以表达首期所需的方法匹配和路径参数；
- 原生 `pgx/v5` 可以在同一事务写入 Decision、EntityLink、领域事件和 Outbox；
- PostgreSQL 17.10 可以执行跨 Workspace 拒绝、deferred Decision evidence 约束、重复 delivery 去重和 Activity 重建；
- 清理可变 Outbox 投递状态不会破坏不可变事件事实和 Activity 重建。

## 决定

首期 Go 服务采用以下基线。

### Go 与 HTTP

- `go.mod` 最低版本使用 Go 1.25；开发和 CI 使用 Go 官方仍支持的版本。
- HTTP server、路由和测试以标准库 `net/http`、`http.ServeMux` 和 `httptest` 为基础。
- 路由使用 Go 1.22 起支持的 method pattern 和 path wildcard，例如 `GET /v1/decisions/{id}`。
- 超时、request ID、认证、授权、结构化错误、日志和 tracing 以显式 middleware 组合，不建立通用 framework wrapper。
- 只有标准库无法满足经过测量的路由、middleware 或协议需求时，才重新评估第三方 Web 框架。

Go 官方从 1.22 起在 `ServeMux` 中提供 method 和 wildcard 路由，足以覆盖当前原型边界，详见 [Go 1.22 release notes](https://go.dev/doc/go1.22#enhanced_routing_patterns)。

### PostgreSQL 访问

- 使用 `github.com/jackc/pgx/v5` 原生 API 和 `pgxpool`，不通过 `database/sql` adapter。
- 首个固定版本为 `v5.10.0`，随 `go.sum` 提交；该版本采用 MIT 许可。
- 事务边界由 application service 显式控制，外部网络调用不进入数据库事务。
- SQL 保持手写、版本化并接受真实 PostgreSQL 集成测试；首期不引入 ORM 或 query builder。
- 领域层不暴露 `pgx` 类型、数据库行结构或 SQL error string；repository / transaction adapter 负责转换为带上下文的可判定错误。
- `pgx` 的 pool、tracing、超时和取消使用显式配置，不依赖包级可变单例。

`pgx/v5` 提供原生 PostgreSQL 驱动、连接池和 PostgreSQL 特性访问，版本策略以 v5 为当前稳定主版本，详见 [`pgx/v5` package documentation](https://pkg.go.dev/github.com/jackc/pgx/v5)。许可证见 [`pgx` LICENSE](https://github.com/jackc/pgx/blob/v5.10.0/LICENSE)。

### 暂不冻结

- SQL 代码生成；出现第二批稳定查询和真实重复后，再评估 `sqlc` 等方案；
- EntityID 生成算法；
- HTTP/OpenAPI 错误对象、分页、并发控制和版本发布策略；
- PostgreSQL 正式最低/最高支持版本与升级矩阵；
- 正式 `server/` 内部目录的全部层级。

正式实现使用一个 `server/` Go module，并按领域模块组织；不把 `experiments/m0-core-contracts` 作为第二个生产服务入口。实验中的稳定 parser、事务用例和 SQL 约束可以迁移，实验 runner 与支撑表可以丢弃。

## 未采用的方案

### Gin、Echo、Fiber 或其它完整 Web 框架

这些框架可以快速提供路由和 middleware，但当前标准库已经覆盖必要路由能力。现在引入会增加依赖、抽象和安全升级面，却没有被 Golden Path 证明的收益。

### `database/sql` 加 PostgreSQL driver

接口通用，但项目已经确认 PostgreSQL，不需要伪装多数据库兼容。原生 `pgx` 能更直接地使用连接池、错误类型、JSONB、批量与 PostgreSQL 事务能力。

### ORM

ORM 能减少简单 CRUD，但多态 EntityRef、deferred 领域约束、Outbox、投影重建和权限查询仍需显式 SQL。ORM 的 identity map、自动关系和迁移默认值容易形成第二套领域语义。

### 现在引入 `sqlc`

生成类型安全查询具有潜在价值，但正式模块当前只有首个协作纵向切片，查询边界仍会变化。等第二批领域查询出现真实重复后再评估，避免为尚未稳定的 schema 固化生成合同。

### `psql` 子进程作为 Go 数据访问层

可以避免驱动依赖，但错误、取消、事务、连接复用和参数绑定都不适合作为服务实现。`psql` 仍可用于人工运维和独立 SQL 诊断。

## 后果

正面影响：

- Go 服务初期只有一个直接 PostgreSQL 依赖，没有 Web 框架或 ORM；
- HTTP、领域和数据边界保持可测试且不相互泄漏类型；
- Transactional Outbox 和 Activity 使用真实 PostgreSQL 语义验证；
- 标准库与手写 SQL 降低隐藏控制流，便于审查权限、事务和幂等；
- 后续若引入 framework 或 generator，可以依据已出现的真实重复作出新决策。

成本与风险：

- 团队需要自行维护 middleware、错误映射和 SQL scan；
- 手写 SQL 需要严格集成测试、迁移检查和 review；
- `pgx` 成为供应链与安全升级责任，必须跟踪其稳定版本和安全修复；
- PostgreSQL 特性会提高迁移到其它数据库的成本，但这不是当前产品目标；
- 授权策略和正式服务布局仍需后续窄决策，不能由实验代码默认决定；migration runner 已由 ADR-0005 另行冻结。

## 迁移与验证

当前没有已部署的生产实例或存量数据库，因此不存在数据迁移。实施顺序如下：

1. 建立单一 `server/` Go module 和最小启动命令；
2. 迁移 EntityRef parser、领域不变量测试和必要 SQL，不复制实验 runner；
3. 先实现 Thread → Decision → Ticket 的事务与权限纵向切片；
4. 为 PostgreSQL migration、备份恢复和失败中断建立正式验证入口；
5. 把 Go 单元测试、`go vet` 和真实 PostgreSQL 集成测试持续保留在 `Candidate Quality`；
6. 原型保留到正式切片覆盖同等失败场景后，再决定删除或归档。

当前已实际执行：

```text
./scripts/check-m0-core-contracts.sh             PASS
./scripts/check-m0-core-contracts-postgres.sh    PASS
./scripts/check-server.sh                        PASS
./scripts/check-server-postgres.sh               PASS
go mod verify                                    PASS
```

实施状态：2026-08-30，单一 `server/` module、EntityRef、正式事务与权限切片、Activity 重建、外部 delivery 幂等、PostgreSQL 17 同 major 备份恢复和 `Candidate Quality` 服务检查已经落地。跨版本正式升级、forward repair 与生产恢复窗口仍未完成；M0 实验继续保留，直到 Golden Path 正式服务达到阶段晋级完成线。

本 ADR 已同步工程标准、当前状态和 ADR 索引。若需要改用第三方 Web 框架、ORM 或 `database/sql`，应以新 ADR 替代本记录。
