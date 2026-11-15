# 核心 Nexus View 关系分页技术验收

日期：2026-11-15

本轮验证 [ADR-0058](../../adr/0058-nexus-view-relation-pagination.md) 的首批 Thread、Decision 和 Ticket。测试使用 schema 029、Activity 17、上下文包 v9、合成账号、隔离 PostgreSQL 和回环 HTTPS。

| 检查 | 实际结果 |
| --- | --- |
| 兼容与排序 | 三类对象的逐页条目与旧详情中的相应关系逐项一致，保留旧详情形状、顺序与 Ticket 的 include 协商。精简详情保留原始来源、受限 evidence 和 Decision 替代信息。同时间下按链接 ID 顺序接续。 |
| 权限 | 合成 Session 覆盖 400、401、404、409、跨操作者及跨对象游标、归档 Project 和来源权限撤除。合成历史数据混排可读和不可读关系。失去游标边界的 Project 读权，以及解除或重连边界关联，均返回同一 409。 |
| Web | 本机 Chromium 在 430 像素宽度下通过正式登录，使用键盘在 25 条与 2 条之间翻页，保留原 Message，跳转到原 Decision。翻页独立于详情重读。Web 回归覆盖 409 返回第一页，单独重试关系读取时继续使用详情，以及切换 Workspace 时取消旧页。 |
| 取回与整库恢复 | v9 在独立目标取回后可从三类对象读取恢复的协作关系。整库恢复核对三个对象的精简详情、关系页、顺序、边界和整库持有的组件关联。 |

Web 的 299 项测试、类型检查、Lint、构建和锁定依赖检查通过。既有工单状态重试用例有一次间歇失败，定向复跑与后续全量复验均通过。Go 单元与 race 测试、静态检查、定向隔离 PostgreSQL、上下文包 CLI 及备份恢复脚本通过。已清理本轮浏览器、容器、卷和临时材料。

## schema 029 索引对照

Thread 样本有 120 条匹配关系。追加同一 Workspace 内其他 Thread 的 1,200 条关系，再用 `EXPLAIN (ANALYZE, BUFFERS)` 对照同一首页。页宽为 1 或 50 时，准入、查询及事务控制共 16 次 SQL 调用。

| 数据与索引 | 页宽 | 查询执行时间 | 共享块命中 / 读入 | EntityLink 节点 |
| --- | --- | --- | --- | --- |
| schema 029，无新增无关关系 | 1 | 0.237 ms | 13 / 0 | 顺序扫描，过滤 7 行 |
| schema 029，追加 1,200 条 | 1 | 0.299 ms | 113 / 0 | 顺序扫描，过滤 1,207 行 |
| schema 029，追加 1,200 条 | 50 | 0.313 ms | 113 / 0 | 顺序扫描，过滤 1,207 行 |
| 一次性数据库中的候选索引，追加 1,200 条 | 1 | 0.045 ms | 12 / 0 | 索引扫描，取 2 行，过滤 0 行 |

## schema 030 升级与恢复

所有者确认后，030 以事务内 migration 安装该索引。先用本地 `93c7095` 构建匹配的 029 工具，以合成协作图执行真实备份和 CLI 恢复。030 工件拒绝直接恢复 029 备份，恢复后的 029 数据库在显式迁移前也未通过 030 就绪检查。显式迁移完成后重新登录，Thread 的派生 Decision 与 Decision 的实施 Ticket 各跨两页，精简详情、游标、顺序、EntityLink、事件、业务回执、Outbox 和重建后的 Activity 与原库一致。同版本 030 整库恢复也通过。

在另一个隔离库中，触发器在索引 DDL 执行后、030 history 写入时注入失败。观察连接核对索引和 030 history 均已回滚，029 的 history 完整保留。移除故障后重试成功，重复迁移保留原索引 OID 和 history。

使用 migration 030 创建的索引复测两个方向。每个查询源有 120 条匹配关系，另在同一 Workspace 加入 2,400 条无关关系。下表的续页从页宽 50 的游标接续。已观察到页宽 50 使用索引读取 120 条匹配关系，查询返回的探测行仍为 51 条。

| 查询 | 页宽 | SQL 调用次数 | 执行时间 | 共享块命中 / 读入 | EntityLink 索引扫描行数 |
| --- | --- | --- | --- | --- | --- |
| Thread 首页 | 1 | 16 | 0.096 ms | 13 / 0 | 2 |
| Thread 首页 | 50 | 16 | 0.336 ms | 158 / 0 | 120 |
| Thread 续页 | 1 | 17 | 0.246 ms | 13 / 0 | 2 |
| Decision 首页 | 1 | 12 | 0.045 ms | 13 / 0 | 2 |
| Decision 首页 | 50 | 12 | 0.222 ms | 65 / 0 | 120 |
| Decision 续页 | 1 | 13 | 0.067 ms | 12 / 0 | 2 |

以上为本机隔离样本。部署耗时和容量仍按目标数据规模与宿主验证。030 的回环 HTTPS、旧接口、源权限和 v9 取回复验均通过。更新仍固定校验 029 的三处测试断言后，Go 检查、迁移和就绪回归通过。

从仓库根复验旧工件备份恢复与升级。

```bash
RADISHNEXUS_NEXUS_RELATIONS_LEGACY_REF=93c7095 ./scripts/check-server-backup-restore.sh
```

补充验证见[029 工件恢复后升级](../../../server/internal/backuprestore/nexus_relation_legacy_integration_test.go)和[索引失败回滚与重试](../../../server/db/upgrade_failure_integration_test.go)。

真人观察和实测按[当前执行顺序](../current.md#近期执行顺序)安排。

验证入口见[回环 HTTPS、取回及查询计划](../../../server/internal/goldenpath/postgres/nexus_relations_integration_test.go)、[整库恢复](../../../server/internal/backuprestore/context_integration_test.go)与 [Web 回归](../../../web/src/collaboration/CollaborationPage.test.tsx)。
