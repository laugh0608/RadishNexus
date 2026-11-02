# Decision 最小替代设计与拒绝切片提交

日期：2026-11-02，Asia/Shanghai。

归档说明：提案已提交为 `29bf809`；此后所有者明确确认 D01～D07 与隔离验收资源，实施与验证以[后续记录](2026-11-02-decision-supersession.md)为准。下文保留设计当时的源码判断和未确认状态，不再作为当前停止线。

所有者要求“提交工作区更改，继续推进下一步”。已核对并以现有贡献者身份在 `dev` 提交 63 个拒绝切片文件：`6f9e8b6 feat(decision): 实现最小拒绝与可迁移决策历史`。提交前仓库检查（647 文件）及差异卫生通过；提交后工作区干净，本地跟踪记录领先 origin/dev 1 个提交，未 fetch 或 push，不代表远端实时状态。

随后按[当前顺位](../current.md#接下来的事项)继续设计，形成 [ADR-0049](../../adr/0049-decision-supersession.md)提案。推荐先做同 Project 的单链替代，复核记录与到期触发后置；D01～D07 尚未确认，没有修改 schema 023 / Activity 11 / v4 的正式实现。

## 源码核对与关键判断

| 核对范围 | 静态发现与提案处理 |
| --- | --- |
| [基础关系约束](../../../server/db/migrations/001_golden_path_foundation.sql) | 预留 supersedes / superseded，只要求旧结论存在 active 入向边；metadata 与来源已不可变。提案复用理由 metadata，新增同 Project、唯一前后继、防环、原子版本与不可移除约束，无独立替代表 |
| [接受 / 拒绝事务](../../../server/internal/goldenpath/postgres/decision.go) | 原接受重试要求当前 accepted，superseded 会落入损坏 provenance 错误。提案保留旧 body / digest / receipt，通过原接受事件与未改字段证明成功，再返回真实当前 accepted / superseded |
| [Ticket 创建](../../../server/internal/goldenpath/postgres/store.go) | accepted 状态在 receipt 前检查；替代后会阻断已成功但丢响应的原创建重试。提案按当前贡献权先核验原 receipt，仅新请求要求当前 accepted；新创建时间改为锁内产生，避免执行 / 替代先后与调用前时间漂移 |
| [关系读取](../../../server/internal/goldenpath/postgres/relations.go)与 [Timeline](../../../server/internal/goldenpath/postgres/nexus_view.go) | 出向通用读取已有，入向未包括 supersedes；restricted / hidden 有不同不透明语义。提案只扩展明确的双向替代，理由须双方可读，单事件显式投影到双方，不扩散相邻对象 |
| [HTTP 完整 DTO](../../../server/internal/platform/httptransport/collaboration.go)与 [Web adapter](../../../web/src/collaboration/api.ts) | 三状态，accepted / rejected 的 updated_at 等于 decided_at；不能容纳替代后保留接受时间的对象。提案新增 decision_revision，以历史解释第四状态及更新时间，区分原操作事实与当前状态 |
| [上下文校验](../../../server/internal/contextpackage/validate.go) | Decision 历史只按 primary 分组，Ticket 必须引用当前 accepted，supersedes 不在 Link 白名单。提案用 v5 明确双对象事件重放、严格理由与链闭包、Ticket 创建时已确认语义；旧 v1～v4 按原白名单转换，不丢弃关系或伪造历史 |
| [上下文类型](../../../server/internal/contextpackage/types.go)、导出 / 导入与 receipt | v4 无版本 / 替代理由，源 receipt 不进入包。提案将理由保存在 Link 显式字段、版本随 Decision，复用原 actor / time / event / lineage；整库备份保留原生 receipt，导入仍只产生历史来源 |

这些是源码与契约推导，没有把未执行的新状态迁移、并发、浏览器或 CLI 描述为运行发现。原拒绝验证仍限于[实施记录](2026-11-02-decision-rejection.md)所列版本和资源。

## 审查重点与影响

- 新 / 旧结论必须都已接受且分别满足当前 evidence 权限；新接受与明确替代是两个动作，不把 proposed 自动确认。
- 两条 Decision 都增加版本，替代双方各 +1；旧接受主体 / 结论 / 时间和所有已有 Ticket / Document 来源、版本与执行状态不变。
- 不单独建立“替代关系 ID 数组”或新事实表：边承载端点与来源，严格 immutable metadata 承载理由，单领域事件承载双对象版本变化。
- 原接受 / 提案 / Ticket 创建的未知请求与新写操作分开：原成功可以核验，当前 superseded 不授权新增 Ticket；UI 必须保留原请求重试，同时在当前撤权后清空。
- 新状态、版本、六项能力、关系理由与原接受重试响应语义需要 Go / Web / CLI 配套升级；原决定重试资格与首次替代资格分开，避免已提交成功的未知请求因当前动作不再可用而被清空。计划 migration 024 / Activity 12 / v5，只是提案，不把这些版本写成当前已交付。
- 迁移、完整替代链、双对象投影与合法旧 Ticket 的恢复均纳入 R01～R10；不能仅有拒绝 / 接受数据库回归就宣布替代功能完成。

## 本轮验证与授权边界

本轮仅新增提议 ADR / 本记录，更新当前状态、专题与文档 / ADR 导航，并给已提交拒绝记录增加归档说明。稳定领域模型、决策基线、核心契约、已接受 ADR、业务源码、migration、依赖与根协作规则均未改。

本轮验证：

- `./scripts/check-repo.sh` 通过（649 文件）。
- 七份变更 Markdown 的 255 个本地链接、36 个标题锚点均解析通过。
- `git diff --check` 通过，核对仅含本轮七份文档，没有业务、migration、依赖或根规则改动。
- 审阅补明确首次替代资格与原决定重试资格的区别：后来终态 / outgoing 变化不能冒充撤权或使未知原请求消失；当前权限丧失仍须清空对应动作。

不重复已完成的业务全量测试代替新功能验收。本轮未启动服务、数据库、Docker 或浏览器，未推送、创建 PR、操作真实实例或恢复 Jenkins / IME / U27 暂缓项。

按 [AGENTS.md](../../../AGENTS.md) 的“若要求会改变数据模型、权限、公共协议……仍须先说明影响并确认范围”，先完成可审阅的 D01～D07 后再请求实现确认。新隔离 PostgreSQL / loopback HTTPS / 独立 Chrome 的资源、影响与精确清理范围已在 ADR 文末列明，可与实现范围一并确认；旧拒绝切片的运行授权不沿用。
