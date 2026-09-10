# 2026-09-10 提交回顾与文档收尾

审阅日期：2026-09-10。实现基线：`61bbd22`；当日范围为 `1b53cf0..61bbd22` 的四笔本地提交。项目所有者要求先提交工作区，再按代码核对文档、记录明天事项，最后单独提交文档；本记录不授权后续执行或远程操作。

## 当日实现提交

| 提交 | 代码变化与用户结果 | 证据与限制 |
| --- | --- | --- |
| `73cc04b` `feat(context): 接通 Activity 自动更新与双向关系` | 白名单业务事件在同一事务更新 Activity；重建先锁投影表再读取来源；Thread 可发现后续 Decision，Decision 可发现后续 Ticket，反向目标逐项复权 | [Activity 与关系记录](2026-09-10-activity-relations.md)。关系仍全量读取，只有两类入向关系；Timeline 不向所有关联对象传播 |
| `684d37d` `feat(identity): 接通邮箱账户与成员邀请` | migration 008 拆分账户、密码凭证与外部身份；邮箱登录、独立展示名、一次性邀请、显式旧账户映射与恢复排除临时认证状态 | [身份记录](2026-09-10-identity-alignment.md)。真实 Radish provider 未实现和装配，capability 关闭；账户恢复、完整成员治理和邀请全流程浏览器证据仍缺 |
| `d0e8c94` `fix(health): 按 migration 历史判断业务就绪` | `/health/ready` 每次只读核对完整连续 migration 序号、名称和 checksum，失败或超时为通用 `503`，匹配为 `204` | [readiness 记录](2026-09-10-schema-readiness.md)。不证明 bootstrap、邮箱映射、手工 DDL 一致性或并发升级协调；当前版本 Compose 未重跑 |
| `61bbd22` `feat(workspace): 接通项目与频道发现入口` | 当前权限过滤后的 Project / Channel 列表与首页分页导航；收紧欢迎区，适配桌面与手机；保留次级 ID 工具 | [发现入口记录](2026-09-10-project-channel-discovery.md)。读取入口已验收，基础对象创建与成员配置尚未开放 |

## 代码与文档核对

按提交差异核对了 Activity 投影与关系查询、身份 bootstrap / Session / 邀请装配、migration 008 与备份分类、readiness checker、发现列表 SQL / HTTP 和首页消费。对照总体架构、核心契约、领域模型、决策基线、Golden Path、路线图及 server / web / deploy 说明，修正以下文档漂移：

- 根 README 的能力段仍停留在首个 Deployment 入口；改为简短的当前能力与缺口摘要，并将详细成熟度统一指向当前状态。
- Web README 仍声称没有 Channel list API、首页只能输入 ID；改为已开放的 Project / Channel 列表与尚未开放的其他对象列表，并补齐邮箱、账户页和邀请说明。
- `nexus-bootstrap` 实际只输出 user / Workspace ID，且只建立用户、账户、凭证和 owner membership；修正“输出规范化 login”与 bootstrap 创建 Session 的说法。已有任意 `user_accounts` 即拒绝再次 bootstrap；Team / Project / Channel 不由该命令创建。
- 总体架构明确邮箱认证已实现、OIDC 真实 provider 延后；基础 Session 路由与后续账户 / 邀请扩展分开说明。决策基线修正邮箱输入文字错误，并引用 ADR-0025 的发现合同。
- 部署说明补齐身份升级时显式 migration 命令、维护窗口以及 migration history 与邮箱登录的分别复验；总体架构明确旧 Compose 演练没有覆盖 migration 008 与新业务就绪探针。没有把文档中的操作步骤写成已执行结果。
- 9 月 10 日稍早身份记录中的“尚未执行新页面浏览器验收”保留其批次事实；后续[发现入口验收](2026-09-10-project-channel-discovery.md)已覆盖邮箱登录 / 退出和 Workspace 切换。当前状态采用后续证据，仍不宣称邀请全流程或 OIDC 浏览器验收完成。

Activity、首批入向关系、备份分类和精确 schema readiness 的专题说明与本次实现一致。领域模型、Golden Path、路线图仍描述未完成的完整用户路径与阶段门槛，无需因局部切片通过降低退出条件。已接受 ADR 与历史验证流水不重写；本次收尾只修正文档，没有修改业务代码、依赖、schema 或远程设置。

## 验证与结束状态

- 当日实现已按各切片运行 Go race / vet / 模块检查、真实 PostgreSQL 集成测试；身份变更额外完成双实例备份恢复，证据分别保留在上表记录。
- 最终发现入口与布局的 `./scripts/check-web.sh` 通过：91 个测试、格式、Lint、类型检查、production build 和 152 个锁定 package 检查。
- 真实隔离浏览器验证邮箱登录、Project / Channel 分页、Workspace 切换、归档与空态、撤权刷新、频道进入及退出；1440px 桌面和 390px 手机截图已复核，320px / 390px 无横向溢出。临时环境和证书材料已清理，没有修改日常浏览器或系统信任。
- 文档收尾运行 `git diff --check` 与 `./scripts/check-repo.sh`；不为文档变更重复启动数据库、浏览器或 Compose。
- 当日只在 `dev` 本地提交，没有 push、PR、发布、真实实例迁移或 Radish / RadishMind 修改。明天首项工作及范围确认点集中在[当前状态的明天事项](../current.md#明天事项2026-09-11)，不在本记录维护第二套计划。
