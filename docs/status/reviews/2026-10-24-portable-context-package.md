# 2026-10-24 Project 上下文包首片实施与验证

## 结论与范围

[ADR-0044](../../adr/0044-portable-project-context-package.md) D01～D06 已由所有者确认并实现：`project-collaboration-v1` 单 Project 当前可读协作闭包，经受认证 CLI 导出到版本化目录包，再导入同版本新实例的空业务 Workspace。真实数据库、HTTPS CLI、正式 Web 后续协作及备份恢复已验证；这不是完整 Workspace / 研发上下文迁移，也不是全部 M1 退出证明。

本轮从 `ece637b` / schema 018 起步，新增 migration 019 与 Activity projection 8。所有者在验收后已授权将本轮改动纳入本地提交，未 push；未更改依赖或 lockfile，未操作真实业务实例、系统证书信任或远程状态。所有者单独授权的资源限于专属临时 PostgreSQL、回环 HTTPS / Chrome、故障注入及精确清理。ADR-0043 U27 独立人工复验按所有者要求继续暂缓，外部试用与评估授权未开放。

## 已交付

- 严格版本化 manifest / JSONL / Markdown、校验和、字段白名单、闭包 / 历史 / 文档版本校验、资源上限、受控文件读取与不覆盖输出；使用现有 Markdown parser。
- 复用正式 Session、CSRF、当前对象权限与近期认证；源导出为一致快照，并与撤权建立行锁顺序。目标 prepare 只读，apply 重查空业务实例和精确计划，原子写入对象、身份、来源、投影与 receipt。
- 新 ID 和持久映射，disabled 历史账户与 suspended membership；初始 restricted Project 只给负责人 admin，其他成员经正式配置加入。源 ACL、凭据、Session、源 operation / receipt 和 Outbox 均不迁移。
- 导入历史明确记录来源作者 / 时间 / 类型，与真实导入操作者分离；Activity 可全量重建，数据库拒绝导入历史 Outbox。原生协作命令保留约束，导入后可继续发消息、确认 proposed Decision、转换 Ticket 和保存 Document。
- `nexus-context export / inspect / prepare / apply / result`，HTTPS 验证、私有凭据 / 计划文件、拒绝重定向、临时 Session 注销；结果仅返回本人成功摘要，逐项映射留在私有计划与数据库。
- Project / Channel / Thread / Decision / Ticket / Document 来源提示，Ticket / Document 导入历史标识，以及权限失效和页面切换清理。

协议与操作步骤以[迁移手册](../../../deploy/context-package.md)为准；长期身份 / 事实语义见[领域模型](../../domain-model.md)，事件与外部副作用边界见[核心合同](../../architecture/core-contracts.md)。

## 验收矩阵

“通过”仅指本行描述的实际测试；“部分”明确列出未覆盖内容。所有源业务数据使用正式 bootstrap、邀请及 application service 创建；测试 SQL 仅用于隔离故障、权限边界和约束攻击，不用直接灌入最终协作图冒充正式创建。

| 编号 | 结果 | 实际证据与限制 |
| --- | --- | --- |
| P01 | 通过 | 正式 Team / Project / private Channel，Message→Thread→两种 Decision→Ticket→Document；Ticket 四状态与重新打开，文档创建 / 保存 / 恢复三版本 |
| P02 | 通过 | 非 admin 主体导出拒绝，撤销 Project 角色 / 停用账户与导出提交顺序测试；HTTP Session / CSRF / Origin / Host 在应用调用前校验 |
| P03 | 通过 | 私密来源撤权造成必要闭包不完整，整体拒绝且无导出审计；目标 outsider 各类来源查询为 404，正式 Web 不发现私密对象；错误不带隐藏来源 ID |
| P04 | 通过 | 导出审计处真实 SQL gate：并发新消息不进入旧快照；导出先持锁则停用等待，撤权先发生则序列化失败且无包 / 审计 |
| P05 | 部分 | 未知 profile / 版本 / 字段、重复 key / 记录、错误摘要 / 行数、缺失来源 / 作者 / 事件 / 版本、循环因果、文档超限和标题限制均拒绝；未逐项在全部数量 / 总字节上限进行满额压力验证 |
| P06 | 部分 | 路径穿越、绝对路径、文件 / 目录符号链接、根目录链接、硬链接、FIFO、未列文件、截断输入与私有文件权限检查通过；打开前后 inode / 文件类型 / 链接数复核已实现，未强制调度每一种文件替换竞态 |
| P07 | 通过 | bootstrap 后仅身份目标可 prepare；prepare 不写库；目标 identity 不符、prepare 后新增 Team、非空 apply 拒绝，原目标数据保持 |
| P08 | 通过 | 逐项新 ID、关系 / 历史映射、历史账户无凭据和 Session；数据库拒绝启用历史身份；不按邮箱 / 同名自动合并 |
| P09 | 通过 | 新 Project restricted，owner 不自动穿透；outsider 初始不可读，正式配置授予 Project contributor 与 private Channel member 后可继续协作；浏览器撤权后刷新 / 后退不可读 |
| P10 | 部分 | accepted / proposed、Ticket 五段历史与四状态、Document 连续三版与恢复来源、原作者 / 时间往返及 import 标识通过；移除关系字段支持解析 / 保存，但本轮未构造移除关系的独立数据库往返样本 |
| P11 | 通过 | 伪造 import event、改写来源、伪造 Document revision、历史 Outbox 被数据库 CHECK 拒绝；原生 Ticket 无 receipt 事件拒绝；缺号 / 不完整来源在 prepare 前拒绝；后续原生写入通过 |
| P12 | 通过 | 两个并发同 operation apply 收敛到一批；精确重试相同结果；改变包 / 目标 / 计划冲突，同包新 operation 拒绝；结果查询不向其他主体披露 receipt 或对象映射 |
| P13 | 部分 | Activity SQL 故障、context 取消和 PostgreSQL backend 终止均整体回滚，历史身份 / 映射 / 对象 / 投影不残留，result 为 not_observed；未额外对 CLI / server 进程注入 SIGKILL |
| P14 | 通过 | PostgreSQL wire proxy 在真实 COMMIT 后丢弃确认，返回 unconfirmed；独立 result 证实 applied，原操作重试不重放 |
| P15 | 通过 | 导入 Activity 与全量重建快照一致，历史 Outbox 为零且 SQL 插入拒绝；复用源 operation 字符串的新目标命令成功；备份恢复来源表后重建一致 |
| P16 | 通过 | 既有受限 Markdown parser 与导入共同校验；正文 URL / entity URI / 代码块原样保留；浏览器中脚本文本作为代码显示，未执行；导入无抓取 URL 或 provider 调用路径 |
| P17 | 通过 | 同时检查 HTTP envelope 与解码文件，未含合成密码 / 邮箱 / token digest / request / client operation；字段白名单和 API 错误脱敏测试；不保证用户正文没有主动写入的敏感信息 |
| P18 | 通过 | 导入后原生编辑 / 确认再导出，第三独立实例再次导入成功，来源链保留；缺失 / 超深来源拒绝；8 层包可 inspect，prepare 拒绝再添第 9 层 |
| P19 | 通过（Agent 浏览器） | 正式 Web 桌面 / 手机流程及授权 / 撤权见下节；不是独立人工或目标团队持续使用验收 |
| P20 | 部分 | 记录小规模包及耗时，自有容器 / HTTPS / 浏览器退出清理；没有生产容量、64 MiB 满额负载或独立操作者持续使用证据 |

## 正式浏览器复验

使用 `check-server-context-package.sh --browser-review`，正式 Web build 与真实应用 handler、两个独立临时数据库、回环 HTTPS；Chrome 仅信任该合成会话的测试 CA。以下操作实际完成后才写 fixture 完成标记，fixture 另查数据库 Ticket 状态版本至少 6、Document revision 至少 4。

1. 负责人登录，发现 restricted Project 并读取导入来源；Channel、private Thread 和两种 Decision 可按来源跳转。
2. Ticket 展示导入的五段历史和 Decision / Document 关系；将 canceled v5 正式重新打开为 open v6，新事件保持原生语义。
3. Document 读取 1 / 2 / 3 版与恢复来源，代码块中的 `<script>` 保持文字，正式保存 v4；在 390×844 手机布局复核并截图。
4. 负责人继续发消息。outsider 初始登录看不到项目，直接 Decision 地址不可读；经正式成员配置授予 Project contributor 和 private Channel membership 后重新登录，可读历史 / v4 并发送消息。
5. 负责人通过正式配置撤销 Project 角色，私密授权随之清理；outsider 重新登录并刷新 Document、回首页、后退恢复，页面均不可读，无旧文档内容残留。

截图位于忽略提交的 `output/playwright/context-package-20261024/ticket-desktop.png` 与 `document-mobile.png`，仅含合成数据。本轮没有导出浏览器认证状态。早期 fixture 因未装配 setup / Ticket component 生产路由失败，补齐真实 handler 后完成最终复验；没有降低产品授权检查。

## 实际执行的验证

| 入口 | 结果 |
| --- | --- |
| `./scripts/check-server.sh` | Go 格式、全模块测试 / race、vet、module tidy 差异与 module verify 通过，无依赖变动 |
| `./scripts/check-server-postgres.sh` | 全 PostgreSQL 集成与迁移 / 升级回归通过，包含 migration 019 与上下文权限 / 并发故障测试 |
| `./scripts/check-server-context-package.sh` | 专属数据库与真实 CLI HTTPS 往返、提交确认丢失、取消、再导入通过；最后收窄 result 字段后再次定向回归通过 |
| `./scripts/check-server-context-package.sh --browser-review` | 上述正式 Web 流程通过；不是仅检查 CLI 或 API |
| `./scripts/check-server-backup-restore.sh` | Golden Path、导入上下文、owner 交接、账户恢复及升级失败恢复通过；旧 017 工件用例本次未提供工件而 SKIP，不重复宣称其通过 |
| `go test ./internal/contextpackage ./cmd/nexus-context ./internal/platform/httptransport` | 最后新增来源深度 / Document 标题与私有文件 / TLS / 严格 HTTP 用例通过 |
| `./scripts/check-web.sh` | 36 文件 / 233 测试、格式、Lint、类型检查、build 和 152 个锁定 package 合同通过；保留基线 App.tsx 两处 ref cleanup warning |
| `./scripts/check-repo.sh`、`git diff --check` | 通过，仓库检查 564 个文件；没有放宽检查 |

首次导入备份测试误把 Service.Restore 当成同时重建 Activity，因既有合同要求 CLI 显式重建而失败；修正测试为先证明空投影，再调用正式 RebuildActivityProjection，最终全套通过。备份测试入口已显式纳入 ImportedContext，避免用过滤器跳过新增用例。

一次实测数据：包 15,563 bytes、9 个业务对象、15 条历史、3 个 Document revision、2 个历史作者；export 12.44 ms、prepare 13.30 ms、apply（含投影）34.50 ms，建源 / 独立往返 / 再导入与读取校验共约 849.73 ms（不含退出清理）。这是本机小样本单次观测，未单独拆分投影耗时，不是吞吐或延迟承诺。

## 交接与剩余边界

本轮专属 PostgreSQL / HTTPS fixture 和 Chrome 会话已退出，保留忽略提交的合成截图和本机测试日志，不清理其他任务资源。最终代码核对将结果 API 收窄为最小成功摘要，完整映射仍在 prepare 私有计划和数据库来源记录中；导入权限、幂等和正文没有因此改变。

暂不覆盖 Windows CLI、Compose operations 内置 CLI、完整 Workspace / 研发实体 / 附件 profile、已有业务目标合并、历史身份认领、源 ACL 等价迁移和生产规模。P05 / P06 / P10 / P13 / P20 的未覆盖项保留为具体补验工作；首片闭环通过不把这些写成全矩阵通过。下一实施范围由[当前状态](../current.md)维护，不能据此自动启动新切片或外部试用。
