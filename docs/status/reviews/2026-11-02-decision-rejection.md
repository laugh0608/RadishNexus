# Decision 最小拒绝实施记录

提交归档说明：本批已在本地 `dev` 提交为 `6f9e8b6`。下文“未提交 / 工作区”保留原验收时点，不代表当前 Git 状态；后续替代提案与源码判断见[设计记录](2026-11-02-decision-supersession-design.md)。

日期：2026-11-02

依据：[ADR-0048](../../adr/0048-decision-lifecycle-and-rejection.md)。所有者确认 D01～D07 与隔离验收范围后实施 A；设计当时的源码判断保留在[设计记录](2026-11-02-decision-lifecycle-design.md)。基线 `08b3359`，在 `dev` 工作区实施，尚未暂存、提交或推送；没有修改真实实例、安装依赖或改变 lockfile。

## 交付范围

- 有当前决策权限且可读全部来源的成员，可明确填写理由并将 proposed 拒绝为 rejected。接受与拒绝共享授权、锁、receipt 和事务边界，两个终态均不能互转、编辑或重开。
- 新增严格的 rejection 子资源；理由仅保存在 Decision，事件仅包含 rejected 状态。详情、Project 决策列表、Nexus View 能力、Activity、导出与导入都支持三个正式状态；引用仍不授予来源读取权。
- Web 展示当前能力、拒绝理由、决定人和时间；改变理由或动作使旧确认失效。网络结果未知时冻结原 operation / payload，409 重新读取，失去来源权限清空决定草稿。accepted 的创建 Ticket 能力仍按 Project 贡献权限独立判断。
- migration 023、Activity 11、默认 `project-collaboration-v4`，严格转换旧 v1 / v2 / v3 Decision 为 `rejection_reason:null`。旧 acceptance 请求、digest、receipt 与已有 Ticket / Document 来源语义保持兼容；Go / Web / CLI 与 schema 需配套升级。

替代与复核只保留设计边界，未建立新写入、自动确认、调度或通知。本轮不宣布完整 Golden Path、真实团队持续使用或 M1 退出。

## 实际验证

| 层次 | 已执行结果与范围 |
| --- | --- |
| Go 工程 | `GOPROXY=off GOTOOLCHAIN=local ./scripts/check-server.sh` 通过全量 race、vet、tidy diff 和模块校验。协议复查后再次通过；理由复用既有严格 Unicode 解码，不把未配对 JSON surrogate 有损替换后入库 |
| PostgreSQL 全量 | `GOFLAGS='-race -count=1 -timeout=10m' ./scripts/check-server-postgres.sh` 通过所有包。新增拒绝专项再分别验证五点回滚、COMMIT 确认丢失、当前准入 / 角色、确定性竞争与来源撤权顺序 |
| Web 工程 | `npm run check` 通过 42 个文件、273 个测试、格式、Lint、TypeScript / 生产构建及 152 个现有锁定 package 校验。新用例覆盖未知结果原请求重试、页面恢复 / 迟到响应、撤权清空、三状态 / 能力组合和 Unicode 边界 |
| 备份恢复 | `RADISHNEXUS_DECISION_LEGACY_REF=08b3359 ./scripts/check-server-backup-restore.sh` 通过。当前整库备份保留原生拒绝 receipt、导入拒绝历史及恢复后原请求重试；匹配 schema 022 工件完成空目标恢复，再显式升级 023；错配 readiness / restore 拒绝，旧源不改 |
| CLI | `./scripts/check-server-context-package.sh` 通过实际 `nexus-context` 二进制、loopback HTTPS 与显式测试 CA 的 export / inspect / prepare / apply / result。新增拒绝提案经 CLI 迁移后，从正式 HTTP 读取理由和导入 Activity，非成员读取为 404 |

备份脚本未提供 schema 017 / 019 / 020 / 021 的独立 legacy ref，相应旧专项明确 skip；没有当作本轮证据。schema 022 匹配工件恢复专项验证身份 / schema 基线，旧 accepted 行与 receipt 的兼容由独立迁移测试覆盖，不能合称旧工件已重跑完整业务链。`TestContextImportServerProcess` 的独立进程故障专项因未提供其专用运行参数而 skip；本轮导入事务故障与两轮往返由 PostgreSQL 专项覆盖。

最后补验曾有一次并行启动的 PostgreSQL 未在既定 30 秒内就绪，以及 Web 全量中的既有 Ticket 404 用例超时。保持原门禁条件，数据库独立重跑通过；Ticket / adapter 定向复测与随后 Web 全量均通过。新增来源撤权测试最初只观察 membership 行等待，遗漏删除触发的父 Thread 代次更新锁；按真实锁路径修正观察后，两个固定顺序均通过。未改超时阈值、跳过失败断言或添加成功 fallback。

### ADR 验收矩阵对应

| 编号 | 实际证据 |
| --- | --- |
| R01 | 正式 service 创建 Message / Thread / proposed，HTTP 与浏览器读取拒绝结果；原 accepted → Ticket → Document 回归通过，浏览器另从 accepted 创建 Ticket |
| R02 | contributor / viewer / 单独 owner 身份、decider / admin、私密来源、无 Project 成员、workspace 可见与 restricted、跨 Workspace、归档、暂停 / 移除及 disabled 账户；每次重试重新授权 |
| R03 | 来源撤权后 Decision 仍可读而接受 / 拒绝能力为 false，写入 / 重试拒绝；HTTP / adapter 的 restricted 投影保持不透明。浏览器撤权后无隐藏 Thread ID，accepted 的 Ticket 贡献能力仍独立可用 |
| R04 | Decision 行锁屏障下双接受、双拒绝、接受 / 拒绝两种先后，以及接受 / 拒绝各自同 operation 并发；只保留一个结果事件，精确重复返回原结果。来源撤权先持锁与写入先持锁分别观察真实数据库等待后释放 |
| R05 | 接受既有重试回归；拒绝同 payload 重试、变化理由 / 新 operation 冲突、旧接受冲突、提案再次请求返回已拒绝对象；COMMIT 丢确认后原重试不重复事件；receipt / 原生事件来源约束不接受伪造成功 |
| R06 | service / transport / adapter 联合覆盖严格字段、理由 UTF-8 / surrogate / NUL / 空白 / 字节限制、confirmed / query / 方法及公共三状态 / 能力组合。沿用公共 Session / CSRF / Origin / Host 与错误缓存头合同和回归 |
| R07 | decisions 更新、receipt、事件、Outbox、Activity 五处注入故障，全部回滚后同 operation 可重试；数据库确认丢失后精确重试；rejected 行不可变，原生与导入历史不互相伪造 |
| R08 | 022→023 保留旧 accepted 行、事件与 receipt 快照，包括旧 decided_at 等于 created_at 的合法记录；旧 reserved rejected 基线拒绝迁移且迁移历史不变。readiness、Activity 重建与整库恢复通过 |
| R09 | 严格旧 profile 字段 / 历史白名单与 manifest digest、合法转义状态兼容、v4 拒绝缺理由 / 缺历史 / 主体错配 / 非 accepted 执行来源；两轮独立 PostgreSQL 导入再导出保留拒绝和 accepted 执行链，导入后创建新提案并原生拒绝；实际 CLI 拒绝历史迁移通过 |
| R10 | 普通邀请成员、Project 发现、明确拒绝、修改确认失效、未知网络原请求重试、双标签冲突、来源撤权、桌面 / 390px 手机及 radio 键盘动作；自动化恢复 / 迟到响应分支与真实浏览器导航分别记录如下 |

## 正式 HTTPS / Chrome

使用 `TestDecisionRejectionHTTPSBrowser`、冻结 Web build 和独立 Chrome profile，仅连接本任务 loopback HTTPS 与临时 PostgreSQL。对象通过正式 service 创建，成员 / decider 配置通过正式管理 preview / apply 完成；SQL 仅用于有标记的权限故障注入和结果核对。

- 普通邀请成员登录后，从 Project 决策列表进入待确认提案；填写可公开的拒绝理由，勾选明确确认，修改理由后确认清空，重新确认再提交。详情显示拒绝理由、实际决定人 / 时间、来源与拒绝 Activity，列表显示已拒绝且无创建 Ticket 动作。
- 一次准确拦截 rejection 请求并在发送前中断，页面显示未知结果、冻结原理由和动作；原操作重试成功，数据库仅有一个拒绝事件。这是浏览器发送失败证据；真实 COMMIT 确认丢失另由 PostgreSQL 故障注入证明。
- 两个标签页对同一提案分别准备拒绝 / 接受，另一页先接受后，旧拒绝实际收到 409，清空旧确认并读取 accepted。没有把匹配他人终态推断为本次成功。
- 手机视口 390×844，拒绝控件和长理由无横向溢出；radio 方向键选择及明确确认可操作。保存桌面拒绝结果、手机提案与撤权、手机列表截图并人工检查布局。
- 撤回该成员私密 Thread membership 后，以合成 focus 事件触发当前权限重读：提案仍可读，但决定草稿与写控件清空，页面无隐藏 Thread ID。随后浏览器从 accepted Decision 创建 Ticket，证明该贡献能力没有被来源读取权误绑。
- 真实后退导航重新读取 accepted，未保留旧确认；没有记录实际 BFCache 命中。focus 为合成事件，不作为操作系统原生焦点、中文 IME 或独立人工操作证据。
- fixture 结束时，三条浏览器提案分别形成两条 rejected 与一条 accepted；原 accepted 保留，另外一条原 proposed 未改。最终状态与 accepted / rejected 事件数一致。

早期探索 fixture 在样式修正前主动以失败标记结束，未计作通过；最终冻结样式的独立 fixture 通过。之后的 Unicode 理由校验与代理项边界修正经过 Go / Web 定向和工程全量验证，未改变布局；不将旧截图描述为这些非法输入的浏览器实测。

截图保留在忽略目录 `output/playwright/decision-2026-11-02/`，只含合成数据。浏览器使用任务 profile 和测试证书 SPKI 限定，不修改系统证书信任；未绕过个人浏览器证书警告或操作既有实例。

## 收尾与接续

本轮运行资源按记录的任务 ID 精确停止和清理；已核对没有本轮 PostgreSQL 容器残留，任务 Chrome profile、冻结构建副本、fixture 状态与中间浏览器记录已清理，保留四张合成截图与检查摘要。真实实例升级仍需另行授权。

`./scripts/check-repo.sh` 通过（647 个文件），本轮文档的 40 个本地标题锚点全部解析成功，`git diff --check` 通过。未改依赖、lockfile 或根协作规则，工作区仍待所有者审阅与另行提交。

下一产品事项是独立选择替代或复核的最小切片，冻结其理由、版本、历史和 Ticket / Document 影响后再确认实施。外部试用支持 / 授权、U27、真实 Jenkins B 和原生中文 IME 继续遵循[当前状态](../current.md)的停止线。
