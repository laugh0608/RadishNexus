# 2026-11-08 提交回顾与文档收尾

日期为 2026-11-08，Asia/Shanghai。审阅本地 `dev` 当日收尾前的 18 笔提交，从基线 `d266dd8` 到看板提交 `d1a8952`。文档收尾另行提交。

## 当日提交

| 提交 | 交付或记录 |
| --- | --- |
| `4f2c12e` | 确认已接受 Decision 的人工复核合同，明确证据、版本、权限和历史。 [ADR-0050](../../adr/0050-decision-manual-review.md) |
| `4f2b208` | 接通人工复核写入、精确重试和按当前证据权限读取历史，交付 migration 025、Activity 13、v6 上下文包与 Web 入口。 [实施验收](2026-11-08-decision-manual-review.md) |
| `380df67` | 整理 T01～T08 准入表，登记首轮本机 Compose 预演。 [初版准入与预演](2026-11-08-m1-controlled-trial-readiness.md) |
| `127ba0c` | 单宿主 Compose 拆分应用、schema owner、只读备份和 PostgreSQL 管理员身份，验证旧库切换、迁移续跑、凭据轮换与空库恢复。 [角色边界](../../adr/0051-single-host-database-roles.md) · [隔离验收](2026-11-08-single-host-database-roles.md) |
| `a90ee9e` | 登记 `127ba0c` 的 app、operations、`nexus-context` CLI、Compose 与 Caddy 配置，整理目标宿主 T03、T06、T07 清单。 [候选登记](2026-11-08-m1-candidate-127ba0c.md#本机工件与生效配置) |
| `91f1cae` | 记录回环 HTTPS、角色权限和完整 migration history 的本机 T03 结果。 [T03 验收](2026-11-08-m1-candidate-127ba0c.md#本机-orbstack-t03-验收) |
| `b430e3a` | 记录本机 T06/T07 的停写、备份、独立空库恢复和已确认写入比对。 [T06/T07 验收](2026-11-08-m1-candidate-127ba0c.md#本机-orbstack-t06-与-t07-验收) |
| `1cbd98c` | 升级恢复脚本增加登记候选模式，核对 Git 快照、镜像与配置身份，在专属实例执行隔离恢复并输出脱敏报告。 [候选验收入口](../../../scripts/README.md#已登记候选的隔离验收) |
| `27aca28` | 记录交接桌面走查和多账号 Chrome 协作、来源及权限结果。 [交接走查](2026-11-08-m1-candidate-127ba0c.md#首批试用交接桌面走查) · [多账号复验](2026-11-08-m1-candidate-127ba0c.md#本机多账号浏览器复验) |
| `52aef0e` | 记录 T04 本机账号恢复、Workspace owner 主动交接、成员暂停与恢复。 [T04 技术复验](2026-11-08-m1-candidate-127ba0c.md#本机-t04-账号恢复owner-交接与成员暂停恢复) |
| `12d1811` | 记录单 Project 数据取回、匹配空实例导入及访问权限。 [T08 隔离往返](2026-11-08-m1-candidate-127ba0c.md#本机-t08-单-project-数据取回) |
| `751d421` | 记录 T05 请求关联、故障定位、同库恢复和三类脱敏报告样例。 [T05 实测](2026-11-08-m1-candidate-127ba0c.md#本机-t05-故障定位与报告) |
| `ee8cceb` | 记录本机临时窗口复验及回环试用实例配置。T07 实测 29.564 秒，已确认写入丢失 0 条。 [临时窗口结果](2026-11-08-m1-candidate-127ba0c.md#本机临时窗口复验结果) |
| `3ae58da` | 记录运行中合成实例的协作、追溯与角色使用过程。 [本机使用记录](2026-11-08-m1-candidate-127ba0c.md#本机试用使用观察) |
| `3b0dc72` | 从本机试用实例导出受限项目，独立接收实例验证讨论、决定、工单、文档版本和访问权限。 [当前实例 T08 演练](../../trial-support.md#当前本机实例-t08-退出演练) |
| `3cda047` | 将 T02 拟议授权边界、T05 支持分流及待填项接入试用交接单，将近期工作转回功能开发。 [T02 与 T05 复核说明](2026-11-08-m1-candidate-127ba0c.md#首批试用-t02-与-t05-复核说明) |
| `7df53d2` | 交付 Workspace「我的工单」，按本人责任和当前 Project 权限过滤后分页。 [ADR-0052](../../adr/0052-workspace-my-tickets.md) · [实施验收](2026-11-08-workspace-my-tickets.md) |
| `d1a8952` | 交付项目工单状态看板，四列独立分页，复用负责人、优先级筛选及既有详情。 [实施验收](2026-11-08-project-ticket-board.md) |

## 代码与文档核对

- 对照 migration 025、Activity projector、复核 HTTP 和 contextpackage 默认 profile，更新根 README、架构、领域模型和 server README。修正 Decision 列表的四状态说明，并区分 v5 替代历史与当前 v6 复核历史。
- 对照 Compose、角色准备 SQL、运维装配脚本及候选验收入口。部署说明补上角色切换和空库恢复的证据，试用支持说明区分初版核对基线、本机候选证据和实际准入。
- 对照 Workspace 分页查询、绑定操作者的游标和 Web 看板。补齐领域模型、能力专题、架构契约和根入口中的个人工单与项目看板说明。看板复用现有 Project 列表接口，各状态请求沿用既有权限规则。

本次收尾使用各实现记录中的 Go、真实 PostgreSQL、Web 与隔离 Chrome 结果。看板批次 Web 检查通过 287 项。候选试用工件仍以 `127ba0c` 的登记为准，工单个人入口和看板各有独立验证记录。

下一步建议维护在「接下来的事项」。本次结束于文档收尾。 [当前执行顺序](../current.md#接下来的事项)
