# 从原讨论追踪工单、构建与部署技术验收

日期：2026-11-15

本轮验证 [ADR-0057](../../adr/0057-thread-delivery-trace.md) 的三个只读层级。环境为本机 OrbStack、隔离 PostgreSQL、合成账号与数据、回环 HTTPS，沿用 schema 029、Activity 17 和单 Project v9。

| 验证 | 实际结果 |
| --- | --- |
| 来源与去重 | 回环 HTTPS 验证已替代 Decision 仍保留原工单、历史 Thread 可走 Decision 路径、Thread 回复建单、显式执行讨论、合并来源及解除一条来源后保留另一条。同一 Channel 的顶层消息与相邻 Thread 保持各自的来源边界。另一 Project 的可读工单及构建从原对象打开，在当前 Thread 的嵌套路径返回 404。 |
| 权限与分页 | 以 Session 验证 restricted Thread、同 Project 成员读权和归档后读取。三个层级按稳定 ID 翻页，拒绝重放到另一操作者、Thread、Ticket 或 CI Run 的游标。解除后旧嵌套路径返回 404，原对象详情按自身权限读取。核对新响应的字段及原 Thread Nexus View、执行讨论列表的字段边界。 |
| 浏览器 | 430 像素宽度下，合成账号经登录、接受 Decision、创建工单和选择已有构建，回到原 Thread 用键盘逐层展开，打开原 staging 详情。各子页首次展开才发起读取。构建解除后，旧部署路径返回 404，页面重读构建层并清掉旧结果。手动重读恢复到工单首页。 |
| 取回与恢复 | v9 两轮分别导入隔离目标，按映射 ID 重读三条协作来源，原构建和部署仍由整库恢复承载。schema 029 整库备份恢复到独立空目标并重建 Activity 后，逐项比对原 Thread、工单、构建和部署的三级读取结果。 |
| 查询测量 | 代表数据含同一 Thread 的 120 张工单、30 条重复可达路径和相邻 Thread 的 60 张工单。页宽 1、页宽 50 和页宽 50 的续页各触发 16 条 SQL 调用，含数据库准入和事务控制。页宽 50 的续页查询返回最多 51 行用于计算游标。这次 `EXPLAIN (ANALYZE, BUFFERS)` 的查询执行时间为 0.261 ms，命中 93 个共享块，读入 0 个共享块。此测量记录的是本机合成样本。 |

Web 通过 296 项测试、类型检查、Lint、构建和锁定依赖检查。新增回归覆盖独立翻页、父级翻页时取消子请求、忽略迟到结果，以及子级失权后重读父页。Go 的 race 测试、静态检查、依赖校验、上下文包 CLI 和整库备份恢复脚本通过。历史 schema 的恢复升级证据沿 [ADR-0056 技术验收](2026-11-15-ticket-ci-run-traceability.md)。

首轮全量回归由 macOS 的 Go 测试连接 OrbStack PostgreSQL。多个既有 Session 用例返回 `invalid session`。后续时钟抽样未确认原因。将受影响的 `goldenpath/postgres` 套件完整放入一次性 PostgreSQL 容器复跑后通过，最终版 ADR-0057 专项另外在原宿主入口复跑通过。

真人观察、真人实测与真实 Jenkins 联调沿[近期执行顺序](../current.md#近期执行顺序)安排。

验证入口见[回环 HTTPS、v9 与查询计划](../../../server/internal/goldenpath/postgres/thread_delivery_integration_test.go)、[整库备份恢复](../../../server/internal/backuprestore/context_integration_test.go)及 [Web 分页和失权回归](../../../web/src/collaboration/ThreadDelivery.test.tsx)。
