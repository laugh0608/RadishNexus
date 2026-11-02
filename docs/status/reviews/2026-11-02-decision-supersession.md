# Decision 最小替代实施记录

提交归档说明：本批已在本地 `dev` 提交为 `d2adfa5`。下文“尚未提交 / 工作区”保留原验收时点；当日提交回顾与文档收尾见[收尾记录](2026-11-02-daily-closeout.md)，不代表原运行授权延续。

日期：2026-11-02，Asia/Shanghai。

依据：[ADR-0049](../../adr/0049-decision-supersession.md)。所有者明确确认 D01～D07 与本任务隔离验收资源后实施；原提案与源码判断见[设计记录](2026-11-02-decision-supersession-design.md)。基线 `29bf809`，在 `dev` 串行实施；本批尚未暂存、提交或推送。原拒绝切片提交为 `6f9e8b6`，替代设计提交为 `29bf809`；本地跟踪记录领先 origin/dev 2 个提交，未 fetch，不代表远端实时状态。

## 交付范围

- 同 Workspace / Project 两条已接受结论，由当前有决策权且可读双方全部 evidence 的成员明确替代；新 → 旧建立不可变 active supersedes。旧结论变 superseded，新结论仍 accepted；单前驱 / 后继、无分叉、无环，不撤销或重开。
- 新增 decision_revision，替代双方各 +1；保留原 question、outcome、rationale、deciders、decided_at 与执行来源。理由只存于 EntityLink 严格 metadata，双方 Decision 均可读才返回，不进入事件正文或 Activity。
- 严格 supersession POST 与六项服务端能力；替代响应只确认原操作事实。原接受 / 提案 / 已建 Ticket 精确重试返回原对象的真实当前事实，新 operation 不能从 superseded 创建 Ticket；已有 Ticket / Document 按自身权限继续维护。
- 单条 decision.superseded 显式投影到双方，正常事务、重建与导入复用分支。migration 024 / Activity 12 / 默认 project-collaboration-v5，严格读取旧 v1～v4，保留源 digest、双对象历史、理由与 Ticket 创建时有效的接受来源。
- 正式 Web 从新结论详情复用 Project 分页选择旧结论、对照来源和双方版本再确认；四状态、双向理由和 Timeline、三类未知原请求重试及撤权清理均已接通。无新依赖、lockfile、根协作规则或真实实例变更。

复核记录、review_at、编辑、自动确认、通知、调度、跨 Project 或任意图不在本片；不宣布完整 Golden Path、M1 退出或真实团队持续使用。

## 实际验证

| 层次 | 已执行结果与范围 |
| --- | --- |
| Go 工程 | `GOPROXY=off GOTOOLCHAIN=local ./scripts/check-server.sh` 通过全量 race、vet、tidy diff 与模块校验；改动 Go 文件已 gofmt |
| PostgreSQL | `GOFLAGS='-race -count=1 -timeout=10m' ./scripts/check-server-postgres.sh` 全量通过。其后新增反向竞争、撤权顺序、独立双方来源 / 跨范围、原提案及变化 payload 重试和旧 proposed 时间补验，替代定向 race 再次通过 |
| Web 工程 | `npm run check` 通过 43 个测试文件、282 个测试、格式、Lint、TypeScript、生产构建和 152 个既有锁定 package 校验。覆盖四状态 / 六能力、微秒时间、严格响应、未知请求、撤权、取消及迟到响应 |
| 备份恢复 | `RADISHNEXUS_SUPERSESSION_LEGACY_REF=29bf809 ./scripts/check-server-backup-restore.sh` 通过。当前备份保留原生 / 导入替代与拒绝历史、原生 receipt，恢复后原请求可核验；匹配 schema 023 工件在空目标恢复，再显式升级至 024 |
| 实际 CLI | `./scripts/check-server-context-package.sh` 通过实际构建的 nexus-context 与 loopback HTTPS / 专用测试 CA 的 export / inspect / prepare / apply / result；迁移替代和拒绝历史后通过正式 HTTP 读取、继续原生替代并重新导出 |
| 正式浏览器 | 冻结最终 Web build，运行 `TestDecisionSupersessionHTTPSBrowser` 与独立 Chrome profile；普通邀请成员通过正式页面操作。fixture 最终状态 / 版本及 6 条替代事件、12 条 Activity、原 Ticket 版本 6、Document 版本 4 的数据库断言通过 |

备份专项未提供 schema 017 / 019 / 020 / 021 / 022 的 legacy ref，其对应旧专项明确 skip，未当成本轮重跑证据。schema 023 匹配工件恢复专项验证身份 / schema 基线；旧三状态 Decision、事件、关系与 receipt 的原样保留由独立 023→024 迁移快照覆盖，不能合称旧工件重跑了完整业务链。CLI 的独立进程故障专项 `TestContextImportServerProcess` 因未提供专用参数而 skip；导入事务故障回归与两轮往返由数据库测试覆盖。

### ADR 验收矩阵对应

| 编号 | 实际证据与边界 |
| --- | --- |
| R01 主链 | 正式 Message / Thread / A / B 接受后替代，C 继续替代 B；正式详情、双向理由、列表与双方 Activity；中间结论最终 superseded / 4 |
| R02 权限 | viewer / contributor / owner 无隐含决策权、decider / admin、归档、暂停 / 移除、disabled；双方来自不同私密 Thread，仅可读任一方来源时写入均拒绝；跨 Project 冲突、真实同库另一 Workspace 端点不可发现。重试重新检查当前权限，来源撤权仍可读 Decision，禁止决定动作 |
| R03 竞争 | 真实 receipt 屏障和 Decision 锁等待固定顺序：两新争一旧、一新争两旧、互相替代分别含反向；链正反顺序、同 operation 并发；Ticket 创建 / 替代与来源撤权 / 替代分别两种先持锁顺序，无重复效果 |
| R04 重试 | 原替代在后来新结论也被替代后返回同一不可变结果；变化理由 / 任一 base revision 冲突；COMMIT 丢确认后原请求只产生一次效果。原提案、接受、Ticket 重试返回同一当前对象；新 Ticket operation 冲突 |
| R05 原子 | 第二个 Decision 更新、link、receipt、event、Outbox 和第二个 Activity 注入故障，状态 / 版本 / 各表全部回滚；删除 / 移除 / 改写边及重开 superseded 拒绝。原生与导入路径受精确来源约束，导入不生成源 receipt 或历史 Outbox |
| R06 Transport | service / HTTP / Web 联合覆盖严格六字段、确认、Unicode / surrogate / NUL / 字节与版本范围、query / 方法 / CSRF / Origin / 缓存头、四状态 / 六能力及不匹配响应；公共 Session / Host / TLS 复用既有回归。1µs 合法差异不被毫秒截断 |
| R07 升级恢复 | 真实 023 三状态快照只增加版本，保留旧 accepted 等时决定及 proposed 较晚更新时间；预留异常边拒绝迁移，readiness 拒绝旧工件 / 新 schema 错配；Activity 重建及当前备份、匹配旧工件空目标恢复通过 |
| R08 可移植历史 | 严格 v1～v5 字段、原 digest、双对象事件 / 版本 / 来源、缺事实 / 分叉 / 环等损坏包拒绝；合法旧时间与 Ticket 创建时语义保留；两轮独立导入再导出、导入后原生继续替代与实际 CLI 通过 |
| R09 Web | Project 入口、25+1 分页、对照 / 改理由清确认、真实双标签 409；提交成功后丢响应的替代 / 接受 / Ticket 原请求重试；来源撤权清理，旧工单 / 文档继续操作；恢复与迟到响应的自动化分支及真实导航分别记录 |
| R10 完成线 | Go / PostgreSQL race、Web 工程、仓库卫生与文档链接检查；桌面 / 390px / 键盘分层验收。合成成员由 Agent 操作，未替代独立用户、团队持续使用、生产容量或真实 BFCache |

## 正式 HTTPS / Chrome

仅使用本任务临时 PostgreSQL、loopback HTTPS、合成账户和数据。Team / Project / Channel / Thread / Decision / Ticket / Document 通过正式 service 创建；成员 Project decider 和 Thread 访问由正式管理 preview / apply 配置。SQL 仅用于标记的来源权限撤销 / 恢复及结果核对，没有手改业务状态冒充命令完成。

- 从首页 Project → 决策列表进入新结论，验证 25+1 分页和选择器分页；proposed / rejected / superseded 保留显示且不可选择。对照双方结论、理由、来源、版本，修改替代理由使旧确认消失；Space / Tab / Enter 完成明确提交。
- B 替代 A 后，旧页保留原接受内容、原 Ticket 和新结论入口；新页显示替代关系。随后 C 替代 B，链中间结论版本为 4，双对象 Activity 解释各自变化。
- 网络拦截先用 route.fetch 取得服务端成功响应，再对页面 abort：替代 200、接受 200、Ticket 创建 201 均已实际提交。分别在重读到新版本或 superseded 后，从原页面重试原请求；替代确认原边、接受显示当前 superseded、Ticket 返回同一原 ID，没有重复创建。
- 双标签冲突将旧页已明确提交的请求暂留在发送前；另一标签正常替代同一旧结论后放行旧请求，实际返回 409，清空旧确认并重新读取。一次断言最初使用错误提示文字而超时，复查实际 409 提示和清理结果通过，未放宽行为要求。
- 撤回合成成员的私密来源 Thread membership 后，以明确合成 focus 事件重读。替代表单、临时理由和隐藏 Thread ID 清空；Decision 本身仍可读。已有原 Ticket 仍可重新打开到状态版本 6，关联 Document 显式保存版本 4，二者未更换来源。
- 390×844 下对照卡片、superseded 详情和四状态列表已截图并查看；对照与详情无横向溢出。真实浏览器后退 / 前进正常读取；CLI 仅切换选中标签没有触发权限重读，因此本轮使用显式合成 focus，不把它写作操作系统原生焦点证据。

浏览器使用专属 profile、临时证书 SPKI 和仅在任务 Node 进程生效的 NODE_EXTRA_CA_CERTS；无系统信任修改。macOS 沙箱阻止初次 Chrome 启动，按本任务已确认的隔离范围通过受审运行权限启动。默认浏览器启动关闭 BFCache，本轮没有真实 BFCache 命中、原生 IME、独立人工或持续使用证据。

截图与检查摘要保留于忽略目录 `output/playwright/supersession-2026-11-02/`，仅含合成数据；它们不是仓库追踪文件。任务 Chrome 已关闭，profile、冻结构建、fixture 状态、中间脚本 / 包与浏览器记录已精确清理；各 PostgreSQL 脚本按任务容器 ID 自动停止并移除。

## 修正与接续

初轮全量回归揭示非 supersedes DELETE trigger 返回 NEW 会抑制删除、导入 Link 仍执行旧空 metadata 合同，均修正根因并重新通过全量。旧 schema 版本期望、profile 与不再允许直接注入的损坏 fixture 同步更新，未禁用新约束。前端最终 Lint 发现 effect 同步清理状态，改为请求条件变化时清理并通过最终工程检查及冻结构建浏览器验收。新增跨来源权限测试首次错误调用只允许首次配置的入口，改用明确标记的权限 fixture；正式业务事实仍由命令创建。

旧 proposed 的 updated_at 可晚于 created_at，迁移、DTO 和 v5 保留这一合法历史；未用更严格的相等条件破坏旧包。Go / Web 定向和迁移补验覆盖该兼容边界。所有失败均在修正后执行相应验证，未增加 fallback、禁用约束或把未运行写成通过。

收尾 `./scripts/check-repo.sh` 通过（666 文件），13 份本轮 Markdown 的 461 个本地链接、40 个标题锚点全部解析通过，改动 Go 文件格式及 `git diff --check` 干净。最终替代定向 PostgreSQL race 与 Go 工程检查通过；任务容器已全部移除，保留五张合成截图和检查摘要。

当前顺位已更新到[状态页](../current.md)：下一产品事项先设计独立复核记录、人工结论和后续动作；review_at 是否进入首片仍须冻结合同。真实实例升级、提交 / push / PR 和发布另按当前任务明确授权处理；外部试用支持 / 授权、U27、真实 Jenkins B 与原生中文 IME 的暂缓保持有效。
