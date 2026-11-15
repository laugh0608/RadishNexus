# 工单与构建的可追溯关联技术验收

日期：2026-11-15

范围为 [ADR-0056](../../adr/0056-ticket-ci-run-traceability.md)、schema 029 和 Activity 17。运行使用本机隔离 PostgreSQL、合成身份和数据，以及回环 HTTPS。

| 验证 | 实际结果 |
| --- | --- |
| 写入与权限 | 回环 HTTPS 覆盖成功、失败和取消的运行、精确解除、解除后重新关联、原请求重试、归档后只读、并发关联、投影故障回滚和撤权后重试。反向列表混排可读与受限 Project，按当前权限过滤后分页。关联保留 Ticket 原更新时间。 |
| 浏览器 | 本机 Chromium 在 430 像素宽度下通过正常登录、Decision 与 Ticket 创建、键盘确认关联、CI Run 反查、查看已有 staging 记录、返回工单、解除和查看历史。 |
| 兼容与取回 | 保留原 Ticket 和 CI Run Nexus View 形状。v9 双轮导入和导出保留工单协作事实，研发对象、构建关联与对应历史保持在现有排除范围内。 |
| 升级与整库恢复 | 本地 `ad50ff1` 的 schema 028 工件在独立空库恢复后迁移至 029。029 整库恢复核对业务数据及 Activity 重建，核验三个原回执并读取关联代次及已有 staging 记录。 |

Web 的 294 项测试通过，包含未知结果时复用原操作和切换工单后丢弃迟到响应。全量隔离 PostgreSQL、CLI 上下文包及仓库检查通过。浏览器及本轮临时资源已清理。

真人观察、真人实测与真实外部交付联调按[当前执行顺序](../current.md#近期执行顺序)安排。

验证入口见 [回环 HTTPS、并发与 v9 取回](../../../server/internal/goldenpath/postgres/ticket_ci_run_integration_test.go)、[整库备份恢复](../../../server/internal/backuprestore/context_integration_test.go)和 [Web 重试与迟到响应](../../../web/src/collaboration/TicketCIRuns.test.tsx)。
