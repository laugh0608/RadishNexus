# 2026-10-28 Ticket 负责人有界补验

日期：2026-10-28（Asia/Shanghai）。基线：`c8c5658`，schema 021 / Activity 9。本批承接[首片记录](2026-10-28-ticket-assignee.md)，补齐有价值的并发先后顺序、双标签与页面恢复检查，并修复同名成员确认歧义。此前未覆盖项不回写为此前已通过。

## 修复

同名候选可以位于不同分页。此前下拉选项只在已加载成员重名时附加 ID，二次确认与未知结果待重试文案则只保存姓名，无法从确认内容区分两个同名身份。

本批在选择后显示已选成员标识，确认与待重试状态保存目标姓名和稳定 ID，确认同时显示当前负责人 ID。加载下一页不会改变已选 ID。只使用现有、已授权响应中的 ID，不增加人员资料、目录查询、权限或 API 字段；不修改 schema、导出 profile 或依赖。

## 数据库确定性顺序

`TestTicketAssignmentMutationOrdering` 从正式业务创建链得到 Ticket，重新打开后获取合法确认材料。仅在专属测试数据库安装写入 barrier，利用已有 `contextGate` / `waitProjectionLock`，实际观察第二事务等待 PostgreSQL 锁再释放第一事务，不以 sleep 猜测先后。

| 竞争动作 | 竞争动作先发生 | 分配先发生 |
| --- | --- | --- |
| 状态变为 done | 状态版本变化，旧分配冲突；未分配 / 1，无分配事实残留 | 分配与随后完成均成功，结项责任保留；负责人版本与状态版本独立 |
| 正式 suspend | Management 事务先持有 Workspace gate，旧分配在等待后拒绝 | 分配先提交，随后暂停成功，责任保留且当前不可执行 |
| 正式 Project role 降为 viewer | 管理代次变化，旧分配拒绝 | 分配先提交，随后降权成功，责任保留且当前不可执行 |
| 全局账户 disabled | 合成账户 UPDATE 先持行锁，分配等待后拒绝 | 分配持目标账户共享锁，停用等待分配提交后生效，责任保留且当前不可执行 |

8 个子用例均验证负责人、分配 / 状态版本，以及 receipt、event、Outbox、Activity 数量；成功操作在随后状态或目标资格变化后可精确重试，且最终历史仍可正式导出。全局 disabled 使用 SQL 夹具，没有新增或冒充全局停用产品入口。没有修改生产锁顺序或增加生产测试开关。

候选测试补充当前 active writer 必在完整候选集合中，以及越过最后 ID 后返回非 null 的空数组 / 空 cursor。依据当前合同，仍有分配权的普通操作者自身就是候选，不能把“第一屏没有其他成员”误作无候选场景；丧失写权时 UI 应进入只读。Web 对空响应的防御展示已有自动化证据，本批不伪造一个正常写者完全没有候选的产品状态。

## Web 与真实浏览器

本批使用本聊天已授权范围内的固定已有 PostgreSQL 镜像、合成身份、回环 HTTPS、独立 Chrome；基础链由正式服务创建，负责人事实全部由正式 Web 动作写入。浏览器夹具在首尾分页安排两个同名身份；为验证切换，新增第二个空合成 Workspace 及已有用户的 membership。第二 Workspace 在本次运行中通过相同 SQL 夹具准备，后续复验已归入 fixture 源码。

| 检查 | 实测结果 |
| --- | --- |
| 跨页同名选择 | 先选 `usr_000_duplicate`，加载第二页后仍选同一 ID；改派到 `usr_zzz_duplicate` 时确认明确列出不同的前后 ID |
| 双标签同时确认 | 两个页面从同一分配版本提交不同目标，真实 POST 返回 `[200,409]`；冲突页清理确认并重读，SQL 验证版本 4、仅 3 条分配事件，没有覆盖成功分配 |
| 普通文档后退 | 完整导航到账户页后返回，重新读取当前对象；浏览器报告 navigation type `back_forward`、`pageshow.persisted=false` |
| persisted 恢复处理 | 在正式浏览器合成 `PageTransitionEvent('pageshow', {persisted:true})`，观察真实 GET 200 和旧确认消失；这验证应用恢复分支，不等于真实 BFCache 命中 |
| Workspace 切换 | 准备未提交清空动作，返回首页切换至第二合成 Workspace；旧工单 / 候选不出现在该 Workspace。后退回工单重新读取，旧确认没有恢复 |
| 手机确认 | 390×844 视口，成员 ID 在确认文案内正常换行，document scrollWidth = innerWidth = 390；实际查看本地截图 `output/playwright/assignee-supplement/confirmation-mobile.png` |
| 最终一致性 | 再清空后版本 5、恰好 4 条分配事件；浏览器宿主最终检查通过 |

Web 新增 3 个自动化用例：跨页同名选择与确认绑定、Workspace 切换中旧写入迟到不会刷新新页面或恢复旧结果、版本不变的重新授权也清理未提交确认。原未知结果精确重试、409、失权、历史身份、迟到候选页和只读测试继续通过。

## 实际验证

- `./scripts/check-web.sh`：通过，40 个测试文件 / 255 个测试、格式、Lint、TypeScript / production build 与依赖基线。
- `GOFLAGS='-race -run=^TestTicketAssignmentMutationOrdering$ -count=1 -v'` 配合既有 `run-postgres-go-integration.sh`：通过，8 个顺序子用例。其它 package 的 `no tests to run` 不计为全量集成验收。
- `GOFLAGS='-race -run=^TestTicketAssignmentHistoryEligibilityAndRetry$ -count=1'` 配合相同 runner：通过，含候选自包含与空尾页补充。
- `go vet -tags=integration ./internal/goldenpath/postgres`：通过。
- `TestTicketAssignmentHTTPSBrowser` 在冻结最终 Web build 与 `-timeout=45m` 下：通过；真实双标签、同名候选和切换证据见上表。
- `./scripts/check-repo.sh`、`git diff --check`：收尾通过。

本批 Go 改动只涉及测试，未重跑全量服务端 race、备份恢复或 schema 升级；这些首片证据仍见原记录，不重复标为本批通过。没有新增依赖、锁文件或真实实例变更。

## 收尾与后续

本次明确的有界补验已收尾。真实 BFCache 命中和浏览器 / 操作系统的更多恢复组合仍需目标环境观察；本次没有修改 no-store、安全策略或浏览器默认行为来制造命中，不宣称穷举所有事务交错或兼容组合。原生 IME、真实 Jenkins、独立操作者及真实团队持续使用继续保持各自停止线。

后续可进入优先级 / 筛选的独立设计确认；不把兼容观察扩展为无限验收循环，也不从本批测试推出新数据模型、权限或外部操作授权。当前顺位见[当前状态](../current.md)。

独立 Chrome 已关闭；浏览器 HTTPS 与专属数据库随宿主退出清理，runner 清理自己的容器。本批改动留在工作区，未提交、未 push。
