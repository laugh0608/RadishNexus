# ADR-0005：Forward-only PostgreSQL migration runner

状态：已接受

日期：2026-08-28

## 背景

ADR-0003 要求手写版本化 SQL 和正式 migration runner，但没有冻结执行工具。首个正式 `server/` module 需要可靠处理连续版本、并发部署、失败中断和已应用文件漂移，同时保持原生 `pgx/v5` 数据访问边界。

当前只有一个 PostgreSQL schema、一个服务进程和一条初始 migration；没有模板变量、Go function migration、多数据库、在线 DDL 或自动降级需求。为尚未出现的能力引入完整 CLI 会扩大依赖与供应链面。

## 决定

- migration 文件嵌入 `server/db/migrations/`，使用连续的 `NNN_name.sql` 命名；缺号、重号、非法文件名和空 forward SQL 都必须失败。
- runner 对完整 migration artifact 计算 SHA-256，并在 `public.radishnexus_schema_migrations` 记录 sequence、name、checksum 和 applied time；已应用内容、名称或顺序发生漂移时拒绝继续。
- 同一数据库通过固定 session advisory lock 串行执行 migration；锁覆盖历史核对和全部待执行版本。
- 每个 migration 在独立 PostgreSQL 事务中执行，schema 变化和历史记录原子提交。失败版本不写入历史，后续版本不会继续。
- 服务启动不隐式迁移。部署或开发者通过独立 `cmd/nexus-migrate` 显式执行，避免多个服务副本启动时隐藏升级副作用。
- runner 只支持 forward migration，不提供自动生产 downgrade。失败升级优先回滚未提交事务；已提交版本通过备份恢复或经过验证的 forward repair 处理。
- migration 可以保留分隔符后的开发参考 down SQL，但正式 runner 不读取或执行它，不能把该段当成生产回滚承诺。

## 未采用的方案

### `github.com/jackc/tern/v2`

`tern` 与原生 `pgx` 兼容，也提供事务、锁、版本管理和更完整的 CLI。当前正式 runner 不需要它的模板、SQL 拆分、SSH、Go migration 或 downgrade 能力；只为基础子包引入整套传递依赖不符合首个纵向切片的最小依赖原则。出现在线 DDL、复杂分支合并或独立运维 CLI 需求时可以重新评估。

### `goose`、`golang-migrate` 或 `database/sql` 工具链

这些工具成熟，但会引入另一套驱动或抽象口径，且当前需求没有证明其额外能力能抵消依赖与集成成本。

### 服务启动时自动迁移

操作简单，但会把持久化变更变成副本启动的隐藏副作用，并让备份、维护窗口和失败恢复难以编排。

### 自动执行 down migration

数据库降级经常与数据转换、旧二进制兼容和不可逆操作冲突。一个 SQL down 段不足以证明生产可恢复性，自动入口容易给出错误安全感。

## 后果

正面影响：

- 正式服务除 `pgx/v5` 外没有新的直接运行依赖；
- 迁移历史可验证，已应用 SQL 不能被静默重写；
- 并发部署和单版本失败具有明确行为；
- schema 变更与服务启动分离，便于自部署编排和审计。

成本与风险：

- 项目维护一段窄 runner 代码及其测试；
- 所有 SQL 必须能在事务中执行，`CREATE INDEX CONCURRENTLY` 等在线 DDL 需要新的阶段方案；
- 生产备份、恢复、forward repair 和版本兼容窗口仍需部署切片验证；
- SHA-256 只能发现 artifact 漂移，不能替代 schema 语义 review 或恢复演练。

## 迁移与验证

当前没有生产数据库，初始 migration 从空 PostgreSQL 建立正式 schema。已验证：

```text
./scripts/check-server.sh              PASS
./scripts/check-server-postgres.sh     PASS
```

真实 PostgreSQL 测试会连续调用 runner 两次，确认第二次无副作用，并执行 Thread → Decision → Ticket 权限与事务用例。引入非事务 DDL、分阶段兼容迁移或独立 migration 工具前，应以新 ADR 替代本记录的相应边界。
