# 2026-11-02 Decision 生命周期与最小拒绝设计

日期：Asia/Shanghai。核对基线 `08b3359`，dev 工作区初始干净；本地跟踪记录与 origin/dev 无 ahead / behind，未 fetch，不代表远端实时状态。

本文保留确认前的设计事实。随后所有者已确认 D01～D07 及隔离验收范围；实际实施、验证和清理见[实施记录](2026-11-02-decision-rejection.md)，下文“待确认 / 未执行”描述设计交接时点。

## 目标与产出

所有者要求按整体规划和当前开发进度推进，已同意先设计 Decision 生命周期、选择首片，再确认数据 / 权限 / 公共协议影响后实施。本轮形成 [ADR-0048](../../adr/0048-decision-lifecycle-and-rejection.md)提案，D01～D07 待确认，R01～R10 为待实施验收合同。

主要归属为沟通与决策，协同身份权限、Ticket / Document 来源、Activity 和上下文迁移。当前状态是唯一执行顺位来源；本记录只保留本轮判断和证据，不创建第二套任务清单。

## 源码核对与取舍

| 核对范围 | 静态发现与设计结论 |
| --- | --- |
| Decision schema | migration 001 预留四状态、理由、复核与替代约束；正式能力不能按预留字段推定。A 复用理由列，计划新 migration，不改旧 migration |
| 接受 / 创建与 receipt | 接受锁住同一 Decision，当前 evidence 和角色重新检查；旧 digest 无版本。A 保持无编辑、无重开、无回转，以锁内 proposed 裁决竞争，保留旧请求与 receipt；不为未来回转预加版本 |
| 授权与锁 | admission / Project / 对象的现有顺序须统一；Decision → Ticket 的共享锁路径纳入同一顺序核对，权限和来源条件不变；新拒绝需同接受权限与明确确认 |
| 详情 / 列表 / 提案重试 | 只支持两状态，Web 对非 proposed 直接显示接受操作结果；新增拒绝须同步三状态、完整 DTO 理由、服务端能力，并保留提案 200 返回同对象当前状态 |
| Activity / 包格式 | 两种 Decision 事件、v3 字段与历史数量均为严格合同；A 需 Activity 11 和 v4，而非跳过 rejected 或把理由塞进旧 profile |
| 执行对象与后续动作 | A 无 accepted → rejected，既有 Ticket / Document 不被改写；替代 / 复核明确理由、主体、来源、历史原则，但写入模型 / 版本 / 协议仍独立冻结 |

这些发现来自源码与正式合同核对，未将其描述为运行重现或新功能验收。

## 影响与确认范围

- 数据：复用现有 rejection_reason，计划 schema 023、receipt / 事件 / 导入历史约束及 Activity 11；不新增依赖或 Decision 版本，旧业务事实不重写。
- 权限：新增拒绝动作，限当前有决策权且可读全部 evidence 的用户；新 capabilities 由服务端计算，Decision、Ticket 和私密来源各自权限保留。
- 公共协议：新增 rejection POST，完整 Decision 增 nullable 理由、三状态和 Nexus View 能力；Project 列表只扩展状态。Go / Web / CLI 配套升级，旧接受 body / digest 保留。
- 可移植历史：默认新导出 v4，严格兼容旧 v1～v3，拒绝理由 / 事件和二次导入来源不丢失；旧服务不支持 v4，无降级导出。
- 操作与恢复：确认设计后才编码；ADR 已列出可同时确认的隔离 PostgreSQL / loopback HTTPS / 独立 Chrome profile、合成故障和精确清理范围，当前未启动。真实实例另行授权；升级失败按匹配旧工件的备份空目标恢复或 forward repair，恢复旧备份涉及的后续写入损失须明确。

以上影响尚未获所有者逐项确认，不把“按计划推进”推定为对新设计细节的接受。

## 验证与交接

本轮只修改 ADR、当前状态和导航，未修改已接受 ADR、稳定领域 / 权限合同、业务源码、数据库 migration 或依赖。

- `./scripts/check-repo.sh` 通过（635 文件，含两份新文档）。
- 六份变更 Markdown 的本地链接 / 锚点另行核对通过：245 个本地链接、36 个锚点引用；两份新文档也纳入文本卫生检查。
- `git diff --check` 通过；差异与状态核对确认仅有本轮六份文档，暂存区为空。
- 兼容性审阅保留旧接受 body / digest，明确旧 accepted 的合法相等时间可升级导出 v4，避免 profile 升级误拒旧事实；迁移 / 原生写入的严格新约束与旧包验证分别说明。

业务、数据库、恢复、CLI 与浏览器验证尚未执行，R01～R10 全部保持待实施。

未暂存、提交、push、创建 PR、启动服务、Docker、浏览器或操作真实实例。原 Jenkins、原生中文 IME 与独立操作者复验继续暂缓，未扩大试用授权或阶段完成结论。
