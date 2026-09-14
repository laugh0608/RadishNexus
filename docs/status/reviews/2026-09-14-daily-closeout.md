# 2026-09-14 提交回顾与文档收尾

审阅日期：2026-09-14。当日实现与合同基线：`d462bef..793ab73`。项目所有者确认最小 Document 合同及其中的固定版本依赖范围，要求今晚提交工作区、按代码核对文档并记录明天事项，最后单独提交文档后结束；本轮未启动 Document 实现。

## 当日提交

| 提交 | 修改与用户结果 | 证据与边界 |
| --- | --- | --- |
| `f9086f3` `feat(workspace): 完成基础对象与首批成员配置` | owner 创建 Team / Project 并明确初始 admin；Project admin 创建 Channel、配置普通角色与受限成员；撤权清理从属授权，migration 009 保留不可变配置 Audit / receipt，创建事件与 Activity 同事务 | [基础配置记录](2026-09-14-foundation-configuration.md)。已从空业务工作区经正式 Web 创建、邀请新账户、授权、发消息及撤权；管理员交接未开放 |
| `f838d51` `feat(auth): 支持首次访问初始化管理员` | 部署者初始化码保护 `GET/POST /api/v1/setup`；首访创建 Workspace owner，Web / CLI 共享 bootstrap 事务锁，已有或恢复账户不会重开；前端清理敏感输入并回到正式登录 | [首访记录](2026-09-14-first-visit-setup.md)。无新 migration / 依赖 / 全局超级权限；完整 Compose 首访及 Linux Secret 实际权限尚未验收 |
| `793ab73` `docs(document): 接受最小 Markdown 文档合同` | 接受 ADR-0021 非 CRDT 路径与 ADR-0028，冻结文档身份、Project 权限、版本 / 冲突 / 恢复、Ticket 来源关系、安全展示及备份 / 导出边界 | 合同已确认，正式 Document 代码、migration、API 和 goldmark 依赖均未接入；实施移至下一工作日 |
| 本记录所在提交 `docs: 完成当日提交回顾与明天事项` | 按当天代码修正入口、架构、初始化操作说明，并同步已确认 Document 真相源及接续清单 | 仅文档收尾；未执行新业务开发或远程操作 |

## 代码与文档核对

本轮以源码和提交差异核对配置领域校验、PostgreSQL 权限与撤权事务、migration 009、Audit / receipt、Activity projector、备份分类、HTTP 装配与共用 Session 校验，以及首次初始化 service、Secret 配置读取、公共 handler、Compose overlay 和 Web SetupGate。对照当日测试与浏览器记录，未将旧记录的阶段限制改写成新批次事实。

确认并修正的文档漂移：

- 根 README 仍把基础对象创建列为未完成；更新为首访初始化、基础对象与首批成员配置已接通，保留管理员交接、完整成员治理、Document 和真实外部交付缺口。
- 总体架构仍称基础配置“继续独立设计”，且未描述网页与 CLI 共用一次性 bootstrap；补齐 ADR-0026 / ADR-0027，明确 owner 不自动拥有私密对象权限。配置 Audit / receipt 已在权威备份分类中，成员明细不进入普通 Activity。
- 服务端公共认证说明把 CLI bootstrap 写成启动前提；改为先 migration，然后选择 CLI 或网页初始化。同步首页实际已提供的初始化与配置能力。
- 部署操作说明修正网页入口的上下文跳转与失败定位，避免把 CLI 成功输出作为唯一初始化判据。网页必须先 migration；初始化成功后正式登录，已有账户或恢复后不得再 bootstrap。
- 当前 Go EntityRef / SQL 注册表尚无 Document；在核心合同与总体架构明确区分“ADR-0028 已冻结 document / doc_”和“运行时代码尚未接入”。领域模型补齐已确认的 Project 读取、不可变快照与恢复追加语义；决策基线同步首访、基础配置和非 CRDT 文档合同，不把未来 API 描述为可调用。
- 当前状态更新为合同已接受、明天实施，并保留 parser 精确包许可证与安全检查待执行。修正 9 月 10 日收尾记录指向已移除明日标题的链接，保留该记录的原始批次事实。

Web README 已描述配置入口、SetupGate 和敏感输入清理；当日 ADR、server / deploy 的详细协议与实现相符，无需重复扩写。Golden Path 与路线图继续要求正式入口、真实外部交付、恢复和团队使用证据，不因本日切片完成降低阶段门槛。

## 验证与结束边界

- 当日实现已有 Go race / vet、真实 PostgreSQL 并发 / 事务测试与备份恢复证据；最终 Web 门禁为 16 个测试文件、102 项测试及格式、Lint、类型、构建和 152 个锁定包检查通过，详见对应批次记录。本轮文档收尾没有重复执行这些业务门禁。
- 合同准备时已运行既有编辑器实验 `npm run check`：28 项测试、构建及 115 包依赖基线检查通过；原有实验 bundle 大小提示保留。这不证明 goldmark 或正式 Document 已通过安全测试。
- 本轮文档收尾运行 `git diff --check` 与 `./scripts/check-repo.sh`。不重新启动数据库、浏览器或 Compose；没有依赖 / lockfile、业务代码或 schema 改动。
- 当日 HTTPS 浏览器证据覆盖从空业务工作区配置到普通成员发消息，以及空账户实例首访创建 owner 并登录；对应任务环境已按原记录清理。完整 Compose 镜像部署、Linux Secret 挂载、生产运维、管理员交接与完整 Golden Path 仍未完成。
- 所有本日提交位于本地 `dev`，没有 push、PR、发布、真实实例 migration 或部署。次日事项集中维护在[当前状态](../current.md#明天事项2026-09-15)；本记录不维护第二套执行计划。
