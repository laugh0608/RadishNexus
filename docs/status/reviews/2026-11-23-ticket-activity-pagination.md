# Ticket Activity 历史分页技术验收

日期：2026-11-23

基线为 `dev` 上的 `0c7072c` 加本次工作区修改。测试使用隔离 PostgreSQL、schema 030、Activity 17、v9 上下文包和合成账户。[ADR-0059](../../adr/0059-ticket-activity-pagination.md)

## 读取与权限

- 回环 HTTPS 对照 Decision、Message 两种建单来源，遍历无 include 和七种合法 include。页宽为 1 时，所有新页拼接反序后与旧详情中公开的事件逐条相同，包括同时间事件、截止日期、分类和执行讨论。两种关系模式均支持分离历史。数据库查询跟踪确认显式分离时没有读取 Activity 表。
- 私密讨论在授权前被省略，取得权限后可成为页末边界。撤回来源 Channel 权限后，以该讨论续页返回 409，以另一条仍可读事件续页则跳过这条讨论。失去建单 Message 读权仍可看到纯受限来源占位，失去工单 Project 读权返回 404。页间新增优先级事件在刷新首页后可读。
- 已验证跨操作者、工单、include 复用游标及非法参数返回 400，未登录为 401，错误方法为 405。构建关联及解除经专用历史读取，通用时间线省略这些事件。[分页集成测试](../../../server/internal/goldenpath/postgres/ticket_timeline_integration_test.go) · [构建关联对照](../../../server/internal/goldenpath/postgres/ticket_ci_run_integration_test.go)

## 查询测量

以实际写命令累积优先级变更，用 30 次建立及解除关联产生 60 条对测试成员不可见的讨论历史。随后向同一 Workspace 的另一工单写入 800 条优先级变更。各组均执行 `ANALYZE` 并以真实查询参数运行 `EXPLAIN (ANALYZE, BUFFERS)`。表中 SQL 次数包括事务和准入，耗时仅为分页 SQL 的一次本机测量。[查询计划用例](../../../server/internal/goldenpath/postgres/ticket_timeline_integration_test.go)

| 样本 | 页宽 | SQL 次数 | 时间线索引扫描行数 | 查询耗时 |
| --- | ---: | ---: | ---: | ---: |
| 8 次本工单优先级变更 | 1 | 14 | 2 | 0.055 ms |
| 100 次本工单优先级变更 | 1 | 14 | 2 | 0.036 ms |
| 再加入 60 条隐藏讨论事件 | 1 | 14 | 62 | 0.061 ms |
| 再加入 800 条无关事件 | 1 | 14 | 62 | 0.060 ms |
| 同一数据集首页 | 50 | 14 | 111 | 0.068 ms |
| 从第 50 条后续读 | 1 | 15 | 2 | 0.049 ms |

六组均反向扫描已有的 `activity_items_target_timeline`。新增无关工单事件未扩大本次索引扫描范围。同工单连续不可读事件仍需扫描和过滤。本批采用原索引。生产容量与实际数据分布仍待目标环境验证。

## Web 故事、恢复与边界

| 项目 | 实际结果 |
| --- | --- |
| 正式入口 | 本机 Chrome、430 像素宽、回环 HTTPS。从工作区首页登录后选择 Project 和 Channel，发送消息、发起 Thread、提出并接受 Decision、创建 Ticket。用同源命令连续写入 26 次合成优先级变更以形成两页。工单首页 25 条，键盘翻到更早的 2 条并返回最新历史。 |
| 构建与结果 | 从工单选择 Component 与已有 CI Run 并确认关联，沿来源链接返回 Decision 和原 Thread，逐层展开该工单的构建和已有 staging 记录，打开 Deployment 详情。有限故事在 15 分钟窗口内走通，使用自动化合成账号。 |
| 失败处理 | Web 自动化覆盖分页冲突后仅重启一次、临时服务错误、401/403/404 清空、切换工单取消旧请求及丢弃迟到结果。在 Chrome 为一次续页注入 503，点击重试后以相同游标取回原来的 2 条，期间仍可读取工单详情。[Web 回归](../../../web/src/collaboration/TicketStatusActions.test.tsx) |
| 导入、备份与恢复 | v9 导入的映射工单与后续原生写入共同进入备份。在独立空目标恢复后重建 Activity，逐页对照旧详情的逆序结果及恢复前的分页快照，覆盖事件、导入标记、subject 和边界。[备份恢复测试](../../../server/internal/backuprestore/context_integration_test.go) |
| 环境边界 | 本批演示使用隔离环境已有的合成构建和 staging 终态。真实 Jenkins 采集、真实外部部署及真人独立操作仍按原恢复条件安排。有限 Web 故事未发现阻断缺陷。[暂缓项与恢复条件](../current.md#暂缓项与恢复条件) |

从仓库根复验本批自动化：

```bash
./scripts/check-server.sh
GOFLAGS='-run=Test(TicketTimeline|TicketCIRunHTTPS|TicketExecutionHTTP|TicketClassificationHTTPS|NexusRelationPageHTTPS|ThreadDeliveryHTTPS) -count=1 -v' ./scripts/check-server-postgres.sh
./scripts/check-server-backup-restore.sh
./scripts/check-web.sh
./scripts/check-repo.sh
```

Go race 全量首轮有三项现有 Channel SSE 测试遇到本机 TLS 握手超时。原检查器重跑通过，未调整测试超时。Web 类型、Lint、306 项 Vitest、构建和 152 个锁定包的依赖检查通过。Vite 仍提示主 JS chunk 大于 500 kB。浏览器测试服务在一次中途重建后仍指向上一版静态资源，已用匹配的最终工件重新启动服务并完成上述复验。
