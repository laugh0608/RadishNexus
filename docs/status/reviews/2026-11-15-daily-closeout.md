# 2026-11-15 提交回顾与文档收尾

日期按 Asia/Shanghai 记录。审阅范围为当天本地 `dev` 从 `b8878b1` 至 `5ae1bf8` 的八笔提交。下表引用本日各批次的代码、合同与已有技术证据。

| 提交 | 交付及证据 |
| --- | --- |
| `b8878b1` | 登记 `1036b21` 的有限 Web 技术验收与 T01～T08 准入缺项。独立观察与 R08 待安排，试用关闭。 [候选实测](2026-11-10-web-candidate-1036b21.md#2026-11-15-隔离实例实测) · [准备检查](2026-11-15-trial-observation-readiness-1036b21.md#trial-admission-checks) |
| `fd00091` | 确定开发阶段优先交付 Web 功能，将真人独立观察与实测排至项目开发完成后。记录合成账号预演及 T04 技术证据复核，书面授权专业复核挂起。 [合成预演与 T04](2026-11-10-web-candidate-1036b21.md#2026-11-15-合成账号预演) · [路线图](../../roadmap.md#阶段与交付方式) |
| `be1670c` | 交付可空日历截止日期及版本化修改，详情、Project 列表、看板与「我的工单」接入。已有 025→026、Activity 14、v7 双轮取回、隔离恢复和回执核对证据。 [ADR-0053](../../adr/0053-ticket-due-date.md) · [PostgreSQL 验证](../../../server/internal/goldenpath/postgres/ticket_due_date_integration_test.go) |
| `85b0e52` | 交付消息建单、原消息定位、Thread 回复和执行讨论双向引用。已有回环 HTTPS、v1～v7 兼容、v8 双轮取回、匹配 026 工件恢复后升级至 027，以及恢复后精确回执验证。 [ADR-0054](../../adr/0054-message-to-ticket-and-execution-discussion.md) · [功能验证](../../../server/internal/goldenpath/postgres/ticket_execution_integration_test.go) |
| `ad50ff1` | 交付固定工单类型、Project 标签及组合查找。已有 Chromium、v9 双轮取回、027→028 及 028 空目标恢复证据。 [ADR-0055](../../adr/0055-ticket-classification-and-project-search.md) · [技术验收](2026-11-15-ticket-classification.md) |
| `7b6b206` | 交付 Ticket 与 Jenkins 终态 CI Run 的人工关联、解除及历史，CI Run 可反查工单和 staging 记录。已有回环 HTTPS、Chromium、028→029 和整库恢复证据。 [ADR-0056](../../adr/0056-ticket-ci-run-traceability.md) · [技术验收](2026-11-15-ticket-ci-run-traceability.md) |
| `93c7095` | 从原 Thread 经三类来源追溯工单，再逐跳查阅构建及已有部署。已有逐级权限、分页、失效路径清理、Chromium、v9 双轮取回与整库恢复证据。 [ADR-0057](../../adr/0057-thread-delivery-trace.md) · [技术验收](2026-11-15-thread-delivery-trace.md) |
| `5ae1bf8` | 交付 Thread、Decision 和 Ticket 的关系分页、精简详情和兼容入口，安装 schema 030 入向索引。已有权限、浏览器、v9、029 备份恢复后显式升级、故障回滚与重试及 030 恢复证据。 [ADR-0058](../../adr/0058-nexus-view-relation-pagination.md) · [技术验收](2026-11-15-nexus-view-relation-pagination.md) |

## 文档核对

- 对照截止日期、分类、消息建单、构建关联与关系分页的 Go / Web 入口，修正根 README 对工单与交付能力的描述，更新领域模型中的工单类型、实际关系及可见性说明。
- 对照 `ActivityProjectionVersion`、HTTP 白名单和现有 Timeline 读取，统一服务端与核心契约的 Activity 17 基线，注明复核和构建关联历史各自使用专用入口。Golden Path 的演示步骤按对象的 Activity 合同表述。
- 对照 026～030 迁移、v9 scope 和恢复脚本，明确当前配套为 schema 030、Activity 17 与 v9。旧 schema 的工件、历史 profile 和验证结果仍按当时的 ADR 与证据查阅。
- 试用和阶段结论继续绑定各自证据。`1036b21` 是固定的隔离候选，后续分支代码的验收按各批次记录。真人观察、真实 Jenkins 联调、实际宿主复验和完整 Golden Path 沿现有条件安排。

本次文档收尾运行仓库检查和文本差异检查。功能与恢复结果引用上表各批次的实测记录。下一步建议维护在当前状态。 [接下来的事项](../current.md#接下来的事项)
