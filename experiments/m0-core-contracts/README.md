# M0 核心契约实验

状态：可丢弃技术实验，不是生产服务

日期：2026-08-28

## 目的

本实验验证 [ADR-0002](../../docs/adr/0002-stable-entity-reference-and-event-projection.md) 能否落到真实 PostgreSQL 事务和最小 Go 边界，而不是提前建立完整服务。

验证范围：

- 六类 M0 对象的稳定 ID、Workspace 和首批字段约束；
- Decision 与 evidence EntityLink 在同一事务提交；
- 跨 Workspace EntityLink 在数据库边界失败；
- 业务状态、不可变领域事件和 Outbox 投递意图原子写入；
- Activity 按 projection version 幂等清空和重建；
- 已投递 Outbox 状态清理后，事件事实和 Activity 重建不受影响；
- 重复 Jenkins delivery 不产生第二个 CI Run 或领域事件；
- canonical `entity://<type>/<id>` parser 不做隐式归一化。
- Go 标准库 `net/http.ServeMux` 可以表达首期所需的方法和路径参数路由。

本目录中的 Thread 和 CI Run 表只提供实验所需的骨架；后续 ADR-0004 已在正式服务中冻结 Thread 的首批字段和 `thr_` 前缀，但这不把实验 schema 追认成生产设计。实验也不包含正式认证、RBAC、HTTP API、迁移工具、备份工具或生产部署。

## 技术选择

- Go 最低版本：1.25；
- PostgreSQL：17.10；
- PostgreSQL 驱动：`github.com/jackc/pgx/v5 v5.10.0`；
- schema 和投影使用手写、版本化 SQL；
- 不引入 ORM、Web 框架、消息中间件或测试 mock 数据库。

`pgx/v5` 是 MIT 许可的纯 Go PostgreSQL 驱动。版本固定在 `go.mod`，校验和记录在 `go.sum`。实验使用其原生事务和连接池 API，因为当前需要验证 PostgreSQL 约束、JSONB、deferred constraint trigger 和 Transactional Outbox，不需要 ORM 提供第二套对象语义。

## 验证

只运行不需要数据库的 Go 测试：

```bash
./scripts/check-m0-core-contracts.sh
```

使用任务专属临时 PostgreSQL 容器运行真实边界测试：

```bash
./scripts/check-m0-core-contracts-postgres.sh
```

数据库脚本固定使用 `postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193`（PostgreSQL 17.10 Alpine），默认禁止隐式拉取镜像；镜像不存在时会停止并要求显式准备。容器绑定随机本机端口，测试结束后通过 `docker stop` 删除，不保留数据卷。

也可以连接人工准备的空测试数据库：

```bash
cd experiments/m0-core-contracts
DATABASE_URL='postgres://...' go test -tags=integration ./...
```

测试会删除并重建数据库中的 `m0_core` schema，只能对专用测试数据库运行。

## 结果用途

实验结果已经用于：

- 接受 ADR-0003 的 Go 标准库 HTTP 与原生 `pgx/v5` 服务基线；
- 在正式 schema 中分离不可变事件事实与可变 Outbox 投递状态；
- 迁移 canonical EntityRef、Decision evidence、跨 Workspace 拒绝和事务失败用例；
- 建立单一正式 `server/` module，而不把实验升级为第二套服务。

实验暂时保留，用于回归正式服务尚未覆盖的 Activity 重建、Outbox 清理和重复 Jenkins delivery。正式切片覆盖这些失败场景后，再删除或归档本目录；不再向实验追加新的产品能力。

实验目录不能成为绕过 ADR、迁移和模块边界的第二套生产入口。
