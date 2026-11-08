# ADR-0051：单宿主 PostgreSQL 身份拆分

状态：已接受并实施；隔离验证见[验收记录](../status/reviews/2026-11-08-single-host-database-roles.md)

日期：2026-11-08

## 背景

原 Compose 用初始化 PostgreSQL 的 `radishnexus` 超级用户运行应用和所有运维命令。应用容器持有该凭据时，也能执行 DDL、修改迁移历史和禁用审计保护。本轮覆盖 [ADR-0016](0016-minimal-docker-compose-self-hosting.md) 的单实例专用 PostgreSQL 17 容器，以及 [ADR-0043](0043-single-host-upgrade-failure-recovery.md) 的停写、失败诊断与恢复流程。

## 决定

| 数据库身份 | 正式用途 | 权限与 Secret 边界 |
| --- | --- | --- |
| `radishnexus` | 初始化 PostgreSQL，显式运行 `database-roles`，本机故障诊断 | 集群管理员。管理员 Secret 仅提供给 PostgreSQL 和一次性角色准备容器 |
| `radishnexus_owner` | `migrate`、`restore` | 拥有应用 schema 和 migration history，可在目标数据库创建 schema 与所需对象。两条路径需要相同的所有权。Secret 仅提供给显式维护命令及角色准备容器 |
| `radishnexus_app` | 常驻应用、`bootstrap`、`identity-migrate`、`account-recover` | 读取 history、执行业务 SQL、追加审计，不能修改已有审计、写 history、改 schema、授予角色或禁用触发器。常驻 app 只挂载自己的 Secret |
| `radishnexus_backup` | `backup` | 可读应用表和 history，无项目表写入权、对象所有权和 DDL 权限。为 `pg_dump` 锁定表定义而持有被排除数据表的读取权，导出仍排除 Session、邀请、OIDC 事务和 Activity 数据 |

运行时的 `UPDATE` 权限包含现有行锁与 Project 上下文导入的表锁所需权限。导入需要锁定 `context_export_audit`，为该表单独授予 PostgreSQL 17 的 `MAINTAIN`，保留对历史行的更新、删除和截断限制。上下文目标指纹只额外授予 `pg_control_system()` 的执行权，避免扩大到通用监控角色。应用 SQL 身份仍能读写授权判断所依赖的数据，产品权限与不可发现性继续由原 HTTP 和服务端合同实施。[PostgreSQL 17 表锁权限](https://www.postgresql.org/docs/17/sql-lock.html)

## 安装、切换与恢复

`database-roles` 从独立文件读取三种新凭据，使用 `psql \password` 发送 SCRAM verifier。准备事务检查其他数据库客户端、角色属性和既有对象所有者，只交接应用 schema、表、函数和 migration history。部署者从匹配旧工件做停写备份并在独立旧目标验证恢复后，才对原库切换。旧 app 曾持有管理员 Secret，切换成功后须重新生成并轮换。现场步骤和失败处理见[数据库权限手册](../../deploy/database-roles.md)。[PostgreSQL 17 psql 参考](https://www.postgresql.org/docs/17/app-psql.html)

新工件仍逐文件校验并应用 migration。Compose 的迁移和恢复命令成功后以 schema 所有者单独事务应用 ACL。归档不携带角色、凭据或 ACL，独立空目标先准备自己的角色，按匹配版本恢复、重建 Activity，再配置访问权限。ACL 失败视为整个运维命令失败，已提交的 migration 或 restore 仍依原合同诊断。当前业务 schema、历史 checksum、备份格式和公共 API 不变。

## 取舍

角色和授权随 Compose 运维工件管理，使原 migration runner 仍能用于本地测试与其他显式部署。为 migration 与 restore 分别建立可继承 owner 的登录身份不会收窄目标数据库能力，本轮采用一个仅维护时使用的 owner，配合恢复的独立空目标约束。集群管理员、宿主机权限和归档保管属于部署责任。共享数据库集群与生产支持仍需单独验证。
