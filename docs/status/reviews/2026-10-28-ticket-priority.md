# Ticket 最小优先级与项目内筛选实施记录

日期：2026-10-28

依据：[ADR-0047](../../adr/0047-ticket-priority-and-filtering.md)；所有者确认 D01～D07 后实施。基线 `ec1caf3`，本轮工作区未提交，不 push、不迁移真实实例。固定镜像的隔离 PostgreSQL、回环 HTTPS 与独立 Chrome 仅使用合成数据；沿用本聊天明确授权的运行范围，无新增依赖。

## 交付范围

- Ticket 固定 `unset / low / normal / high / urgent`，初始 unset / 1。当前 Project 写权者在非终态显式修改，独立优先级版本与状态版本防止旧确认覆盖；负责人变更不制造无关冲突。
- 独立读取 / 写入端点、严格请求和响应、精确 receipt、不可变事件 / Outbox / Activity 原子事务。详情二次确认、冲突重读、未知结果仅原操作重试；列表与 Timeline 展示同一封闭枚举。
- Project 工单列表三组单选 AND 筛选，SQL 先筛选再按稳定 ID 分页。新 Ticket v2 cursor 绑定操作者与条件，旧 Ticket cursor 失效；其它分页合同不变。
- migration 022、Activity 10、默认 v3 导出与严格旧 v1 / v2 读取；连续状态 / 分配 / 优先级历史、二次导入和原生继续修改。Go / Web / CLI / schema 必须配套停写升级。

## 实际验证

| 层次 | 已执行证据 |
| --- | --- |
| Go 工程 | `GOPROXY=off GOTOOLCHAIN=local ./scripts/check-server.sh` 通过：全量 race、vet、tidy diff、模块校验；没有下载或修改依赖 |
| PostgreSQL 回归 | `run-postgres-go-integration.sh` 全量 race / count=1 覆盖所有包；初跑发现旧 015 升级行比较未排除新增列，以及手工 DDL readiness 夹具仍停在 021，两处测试按新合同修正后定向复验通过，其余包全量通过。新增权限测试的不存在身份已改为真实 reader / owner 夹具，定向复验通过 |
| 优先级专项 | 最新 `TestTicketPriority*` 真实 PostgreSQL race / count=1 通过；覆盖默认值、同值冲突、优先级 / 状态 ABA、同版本双写、独立负责人变化、版本上限、终态、归档、私密来源、跨 Workspace、只读成员、失权后 receipt 不恢复写权 |
| 原子与先后顺序 | 事件、receipt、Outbox、Activity 故障均回滚；真实 COMMIT 确认丢失后精确重试。完成与优先级、暂停与优先级分别验证双方先持锁的四个固定顺序，等待真实 PostgreSQL 锁再释放屏障；核对版本与四类写入数量 |
| 筛选 | 30 条正式命令生成的四状态 / 五优先级 / 三责任样本，108 种筛选组合 × limit 1 / 25 / 50，完整遍历无重漏；25 / 26 页大小边界、空尾页、并发加入匹配集合须回首页、归档读取和权限重查。终态 / 只读 me 保留责任语义，历史导入负责人不匹配本人或未分配 |
| HTTP | 优先级 body、GET / POST、Session / CSRF / Origin / Host、错误与 no-store；筛选 query、旧 / 异身份 / 异 Project / 异条件 cursor、响应字段和筛选结果漂移；新增及既有 HTTP 测试通过 |
| 历史与迁移 | 021→022 旧行 unset / 1、不改旧时间与事件；v1 / v2 旧字段和事件白名单、原 manifest digest、v3 损坏历史拒绝；两次独立导入、导入后原生优先级变化、重建前后 Timeline 一致 |
| 备份恢复 | `RADISHNEXUS_PRIORITY_LEGACY_REF=ec1caf3 ./scripts/check-server-backup-restore.sh` 通过，包含当前权威事实 / 优先级历史恢复，以及匹配 021 工件恢复空目标后显式升级 022；既有恢复故障测试通过。未提供其它 legacy ref 的历史版本专项明确 skip，没有当作本轮证据 |
| Web | 全量 269 个测试、TypeScript、Lint、生产构建及现有 152 个锁定 package 校验通过。新增 Timeline 测试补齐必需的 implements 来源夹具后通过；动作未知结果 / 409 / 401 / 404、迟到响应、页面切换、筛选请求代次与恢复均有定向用例 |

### 正式 HTTPS / Chrome

使用 `TestTicketPriorityHTTPSBrowser` 与冻结 Web build，先经正式命令准备 Message → Thread → accepted Decision 和 26 条分页工单，再由浏览器从 Project 决策入口创建新的验收 Ticket。未将 SQL 插入的样本冒充业务创建链。

- 默认未设置，设置高、改为紧急、清回未设置、普通与低；详情、Project 列表与优先级 Activity 一致。
- 三组 AND 筛选，26 条匹配记录分为 25＋1，无重复；切换条件、空态、重置和同页刷新保留条件。整页重载恢复默认全部。
- 双标签先后提交，旧确认实际返回 409 并读取他人的新值。拦截已成功的 POST 响应模拟客户端未知结果，原 operation 和 payload 完全相同地重试，只产生一次事件。
- 完成后优先级保留且只读，重开后可修改。390px 手机无横向溢出；设置和确认按钮可用键盘 Enter 完成。原生 select 的方向键自动化未取得稳定证据，不将其写为已完成的纯键盘全流程。
- 普通邀请成员重新登录、从 Project 筛选进入详情、认领本人、修改优先级，再以 active / me / urgent 找回唯一工单。
- 测试库将该成员降为 viewer 后，me 仍保留责任记录，详情不提供优先级写动作；随后撤回 restricted Project 读取权，刷新及真实前进 / 后退不保留项目名、工单条目或写控件。这两次权限变化是隔离夹具状态注入，未宣称本轮通过管理页面完成撤权操作。
- `pagehide / pageshow(persisted=true)` 分支以合成事件触发；真实导航已验证，但本轮未记录实际 BFCache 命中，不把分支测试等同浏览器缓存命中。
- fixture 最终核对新 Ticket 的优先级版本等于事件数＋1，浏览器测试通过。控件样式收尾后，另起隔离 fixture 用最终构建复验四次优先级修改、桌面 / 手机布局、AND 筛选、25＋1 分页与刷新保留条件；与前述行为验收分别记录。截图在忽略目录 `output/playwright/priority/`，仅含合成数据。

### 查询计划

在独立浏览器测试库的单一事务内追加 10,000 条合成 Ticket，仅用于查询计划，之后完整 ROLLBACK。以当前列表 SQL（包括负责人 join / 资格摘要）做 `EXPLAIN (ANALYZE, BUFFERS)`：无筛选、active / me / urgent、done / unassigned / normal 的 limit 26 查询均使用既有 `tickets_pkey`，单次执行分别约 0.142 / 0.173 / 0.218 ms。未添加组合索引。

这是单 Project、热缓存、固定选择率的窄测量，没有覆盖多租户数据倾斜、prepared generic plan、极低选择率深页或生产并发，也不是容量承诺。后续如有真实慢查询，应依据目标数据和计划增量设计索引。

仓库检查 `./scripts/check-repo.sh`（629 个文件）与 `git diff --check` 通过。浏览器和所有本轮临时 PostgreSQL / HTTPS 资源已关闭，临时状态、构建副本和浏览器配置已清理；截图保留在忽略目录。

## 剩余边界与下一步

本片已完成“明确优先级并在 Project 内找回工单”的实现与上述分层验证，不代表完整看板、可配置 workflow、通知、保存视图、生产容量或整个 M1 退出。长期团队使用、实际 BFCache / 浏览器兼容和原生选择控件纯键盘复核继续独立观察；不据此无限重复已完成的测试。

下一产品事项先设计 Decision 拒绝 / 替代 / 复核的生命周期和来源合同，确认后才实现。外部试用支持与评估授权、ADR-0043 U27、真实 Jenkins B 和中文 IME 仍按当前状态中的停止线处理。
