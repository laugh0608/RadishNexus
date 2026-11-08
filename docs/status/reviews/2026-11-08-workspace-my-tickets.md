# Workspace「我的工单」实施验收

日期：2026-11-08。基线为 `3cda047`，验收对象为本轮 Go 与 Web 改动。读取范围和接口见 [ADR-0052](../../adr/0052-workspace-my-tickets.md)。

| 层次 | 实际结果 |
| --- | --- |
| Go | `./scripts/check-server.sh` 通过，包括竞态检查、静态检查与 Go 模块校验。正式路由检查覆盖匿名拒绝、允许的方法和响应缓存头。 |
| PostgreSQL 与 HTTP | `./scripts/check-server-postgres.sh` 通过。用正式协作与工单命令建样并经 Session 接口读取，验证跨 Project 分页、受限项目撤权、只读成员、归档、改派和终态。换用其他用户、Workspace 或 Project 列表重放游标均拒绝。 |
| Web | `./scripts/check-web.sh` 通过，285 项测试通过。新增覆盖工作台入口随 Workspace 切换、返回时重读、访问拒绝和旧作用域迟到响应。Vite 仍提示主 bundle 超过 500 kB。 |
| 隔离 Chrome | 通过回环 HTTPS 打开实际构建。合成成员从工作台进入列表，看到所属项目、状态与优先级，打开工单详情和来源区，后退重新读取列表。390 px 视口无页面横向溢出。切到另一 Workspace 后显示空态。 |

Chrome 使用临时测试实例和合成数据。查询规模与生产容量尚未测量。
