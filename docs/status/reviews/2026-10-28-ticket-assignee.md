# 2026-10-28 Ticket 最小负责人实施与验收

日期：2026-10-28（Asia/Shanghai）。基线：`211f050`，schema 020。依据已确认的 [ADR-0046](../../adr/0046-ticket-assignee.md) D01～D07，在本地 `dev` 完成负责人首片；本记录随实现一同提交，不改写此前批次的证据。

## 交付范围

- migration 021：可空负责人、独立分配版本、同 Workspace 外键、精确 receipt / 事件约束、分配版本唯一性及导入历史类型；既有 migration 不变。
- 正式服务与 HTTP：读取、稳定 ID 候选分页、设置 / 改派 / 清空。操作者复用当前准入和 Project 写权；目标复用同一资格谓词，绑定成员生命周期与 Project 管理代次。原操作精确重试返回原 applied 事实，不再次执行。
- 事件、Outbox 与 Activity v9 同事务；Project 工单列表与详情使用同一最小身份摘要。退出、暂停或降权不抹除责任记录、不赋予权限。
- Web：选择 / 由我负责 / 清空的二次确认，终态只读、冲突重读、未知结果保留原操作、分页迟到响应丢弃与失权清理。候选页包含本人后才显示“由我负责”，不会绕过候选资格。
- 当前导出使用 `project-collaboration-v2`，保留严格旧 v1 读取并转换为未分配 / 1；计划仍绑定原 manifest 摘要。历史负责人及全部前后身份逐项映射为不可登录历史身份，不按姓名或邮箱绑定目标成员。两个独立目标往返后仍可显式分配目标实例成员。

负责人切片不引入优先级、看板、通知、依赖或新的自动授权。Go / Web / schema 须配套停写升级，不宣称滚动兼容。

## 自动化证据

| 范围 | 本轮实际证据 |
| --- | --- |
| 输入与协议 | Go 领域验证、HTTP 严格必填 / nullable、当前 Session / CSRF、作用域 cursor 与输出校验；Web 严格 DTO、bigint 字符串、未知结果原请求重试、409 重读、历史身份、迟到页与只读状态 |
| 资格与责任 | contributor 可分配且不能读取来源私密 Thread；viewer / 无项目角色 owner 不进入候选；角色降权保留责任但显示不可执行，撤权后旧 receipt 不恢复授权；列表与详情一致 |
| 正式生命周期 | `TestTicketAssignmentFormalMembershipLifecycle` 使用正式 Management Preview / Apply，验证 suspend → resume → remove → 定向 reinvite / AcceptInvitation → 显式重授 Project contributor → viewer → contributor；每步保留负责人 / 分配版本且候选与摘要一致。全局 disabled 使用合成 SQL 夹具验证；没有新增全局停用产品入口 |
| 并发与重试 | A→B→A 旧版本拒绝、成员生命周期与 Project role 代次变化拒绝、两个并发分配仅一笔成功；工单完成后精确重试返回原历史事实；目标暂停后原操作仍可重试 |
| 原子性 | receipt、domain event、Outbox、Activity 故障回滚；真实 COMMIT 确认丢失后原操作重试仅一份事实；投影全量重建与正常结果相同 |
| 包与历史 | 严格 v1 三字段、v2 必填可空字段、原 manifest / plan digest、历史作者闭包、前后责任与版本连续性、最终状态一致；二次跨实例映射、后续原生分配及重建。补充拒绝缺失 nullable 字段；保留旧合法同时间状态历史，拒绝分配事件的歧义顺序 |
| 数据库升级 | 020→021 旧 Ticket 初始 null / 1，旧数据不补造分配事件；既有迁移失败、合法前缀及状态升级测试同步到最新 schema 并复验 |
| 备份恢复 | 当前版完整备份矩阵通过，含有分配历史的导入数据；指定真实本地 `211f050` 工件的 020 备份 → 旧工件空目标恢复 → 当前迁移通过。019 membership / 旧 owner-transfer 工件未指定，本轮这些旧工件专项明确跳过 |

完整 Go integration 首跑发现已有迁移测试把最新版本硬编码为 20，以及旧状态快照包含了新增字段；修正测试的当前版本与原字段快照范围后，数据库 integration 全包复验通过。没有放宽 production readiness 或 migration checksum。

## 正式 HTTPS 浏览器

本轮已获所有者授权，仅使用固定已有 PostgreSQL 镜像、本机回环 HTTPS、隔离 Chrome 和合成身份。基础 Message → Thread → Accepted Decision 由正式 application service 建立；浏览器从 Decision 页面创建新的 Ticket，未用 SQL 预置最终责任事实。25 个额外候选身份 / grant 是测试夹具，不代表批量成员产品入口。

最终 Web 构建的实际检查：

1. 新 Ticket 初始未分配；27 个候选按 25＋2 分页，展示长中文名称，确认后由本人负责，再改派受邀成员。
2. 清空请求由浏览器代理先真实发送并等待服务端完成，再丢弃响应；页面保持“结果尚未确认”。刷新读到未分配仍保留原 operation；Shift+Tab / Enter 重试后 SQL 确认分配版本 4、仅 3 条分配事件。后续重新认领得到版本 5、共 4 条事件。
3. 开始处理 → 确认完成，负责人保留且分配入口只读；重新打开后恢复候选入口。
4. Project 工单列表与详情负责人、状态一致，canonical 地址可返回。
5. 390×844 手机视口无水平溢出，确认 / 重试可用键盘操作；1440×1080 桌面及手机截图实际检查。截图为本地忽略的 `output/playwright/assignee/desktop-current.png` 与 `mobile-current.png`，未作为长期版本化资产提交。
6. 合成夹具将操作者 Project role 降为 viewer，刷新后保留“当前不可执行”的责任摘要且移除候选；再移除读取角色，刷新清空对象和候选，浏览器后退到列表仍显示不可访问。撤权由夹具 SQL 注入，正式成员生命周期另由上表测试验证，不冒充浏览器管理 UI 操作。

浏览器宿主最终测试通过且验证至少 4 条真实分配事件。早先宿主因 Go 默认 10 分钟超时结束，另一次为更换最终 Web 构建主动终止，均不记为通过；最终运行显式使用 `-timeout=45m` 和冻结 build，未在运行中覆盖服务资产。控制台出现故意丢弃响应及撤权后的预期网络错误，未观察到应用脚本异常。

## 实际检查命令

- `./scripts/check-web.sh`：通过，format、Lint、252 个测试、TypeScript / production build、152 个锁定 package 的依赖检查。
- `GOPROXY=off GOTOOLCHAIN=local GOCACHE=/tmp/rn-assignee-go-cache go test -race ./...`（server）：通过。
- 同环境 `go vet ./...` 及 `go vet -tags=integration ./db ./internal/goldenpath/postgres ./internal/backuprestore`：通过。
- 隔离 PostgreSQL 的 `go test -tags=integration ./...`：除上述已修复 db 断言外通过；修复后 `go test -tags=integration ./db -count=1` 通过。
- 隔离 PostgreSQL 的 `go test -race -tags=integration ./internal/goldenpath/postgres -run '^TestTicketAssignment(History|Stale|Portable|Failure|Formal)' -count=1`：通过；覆盖最终包校验与正式生命周期测试。
- 最终包校验 `go test -race ./internal/contextpackage` 与补充的 `go test ./internal/contextpackage -run '^TestAssignment' -count=1`：通过。
- `./scripts/check-server-backup-restore.sh`、`RADISHNEXUS_ASSIGNMENT_LEGACY_REF=211f050 ./scripts/check-server-backup-restore.sh`：通过，跳过项已在上表说明。
- `TestTicketAssignmentHTTPSBrowser`：在授权环境、最终冻结 Web build 与 `-timeout=45m` 下通过；默认无显式浏览器环境变量会跳过。
- `./scripts/check-repo.sh` 与 `git diff --check`：收尾检查通过。

## 剩余限制与接续

本轮不声称穷举状态完成与目标停权的全部事务交错；分配并发与代次保护已有定向测试，进一步锁调度仍可有界补验。浏览器双标签同时分配、全部 Workspace 切换 / bfcache 组合、重复显示名跨候选页及空候选交互尚未逐项实机验收，部分由服务端 / Web 自动化覆盖。正式生命周期测试覆盖指定顺序，不能替代真实组织交接的持续观察。

没有执行目标生产环境迁移、真实 Jenkins、原生中文 IME、独立操作者 U27 或真实团队持续使用；不宣布完整执行管理或 M1 退出。下一步先补齐有价值的负责人验收余项，再独立确认优先级 / 筛选的产品与数据合同，顺位只维护在[当前状态](../current.md)。

本轮关闭独立浏览器，HTTPS fixture 退出并清理专属数据库，停止专属 PostgreSQL 容器；备份脚本自行清理临时容器 / 网络。未修改系统信任、依赖、远程配置或真实实例；本地提交，不 push。
