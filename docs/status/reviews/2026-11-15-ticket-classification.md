# 工单分类与项目内查找技术验收

日期：2026-11-15

范围为 [ADR-0055](../../adr/0055-ticket-classification-and-project-search.md)、schema 028、Activity 16 和上下文包 v9。运行使用本机隔离 PostgreSQL、合成身份与数据，Web 使用临时 Caddy 回环 HTTPS。

| 验证 | 实际结果 |
| --- | --- |
| 分类写入与权限 | 回环 HTTPS 覆盖消息与 Decision 初始分类及原回执、旧建单空分类兼容、无效标签导致原事务回滚、终态重分类、viewer 只读、归档 Project、跨 Project 标签、并发归档和改名。标签历史与当前值分别按操作时和当前名称读取。 |
| 项目内查找 | Type、标题、标签与既有条件在数据库分页前组合；标题大小写折叠与字面 `%`、`_`，归档标签、全匹配和 v2/v3 游标通过定向测试。 |
| 查询计划 | 1,200 张合成工单分属两个 Project，目标 Project 有 200 张。标题、类型与两个标签共同命中 43 张，按稳定 ID 分成 25／18 两页，每页 12 条 SQL，包含准入、作用域校验和事务。实际查询的 `EXPLAIN ANALYZE` 使用 Project 作用域索引，分别为 0.787／0.379 ms，根节点 shared hit blocks 为 167／118。只用于该数据分布的技术验证。 |
| 上下文取回 | 旧 v1～v8 的严格读取与原 digest 校验通过。v8 保留消息来源 schema 2。v9 双轮导入覆盖两类 Ticket 来源和归档标签，第一轮后原生改名和重分类，第二轮恢复其结果及历史。 |
| 升级与备份 | `85b0e52` 的 schema 027 工件在独立空目标恢复，再迁移到 028。028 整库备份恢复后对比业务表和目录审计，重建 Activity 并核验分类及目录的原回执，归档标签仍可筛选。 |
| 浏览器 | 使用本地 Chromium 和独立 HTTPS fixture，正常登录后在 UI 创建标签、从消息建单、编辑分类，归档后在列表与看板按标题、类型和两个标签查找，并从已接受 Decision 创建带类型的工单。浏览器进程、回环入口和数据库按测试流程关闭。 |

开发完成后按当前计划安排真人观察与实测。 [当前执行顺序](../current.md#近期执行顺序)

验证入口见 [回环 HTTPS 与双轮导入](../../../server/internal/goldenpath/postgres/ticket_classification_integration_test.go)、[代表性查询](../../../server/internal/goldenpath/postgres/project_tickets_integration_test.go)、[备份恢复](../../../server/internal/backuprestore/context_integration_test.go)。
