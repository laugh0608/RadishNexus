# Activity 正常更新与首批双向关系验证

日期：2026-09-10

基线：`dev` / `1b53cf0`；启动时工作树干净，与本地记录的 `origin/dev` 一致，未 fetch。本文记录该基线之上的本轮未提交改动，不表示已部署或已经晋级。

## 范围与结果

按已确认的 [ADR-0022](../../adr/0022-transactional-activity-and-incoming-relations.md) 接通：

- 五类既有 Activity 白名单事件在原业务事务中投影；提交后重新读取立即可见，失败整单回滚，对应 Outbox delivery 同事务标记完成。
- 全量重建先锁定投影表，再取源事件快照；与正常投影共用逻辑，不读取旧 Activity 或 Outbox 作为事实源。
- Thread 从 `derived-from` 的另一端发现 Decision，Decision 从 `implements` 的另一端发现 Ticket；复用同一 EntityLink 与当前权限，不复制镜像事实。
- 协作 readable relation 明确 direction，restricted evidence 仍保持不透明，不可读 incoming 完全隐藏；Go 与 Web 同步校验并提供双向 canonical 跳转。

不新增 migration、依赖、后台 worker 或实时通道。Timeline 仍属于事件主要对象，不自动向 Thread 分发后续对象事件；真实 Jenkins、Document、对象列表和 schema readiness 不在本轮实现范围。

## 已执行验证

| 验证 | 结果 |
| --- | --- |
| 定向 Go service / PostgreSQL mapping / HTTP transport 测试 | 通过；首次因沙箱禁止回环监听失败，获准在沙箱外重跑通过 |
| `server/` 下 `go test -race ./...` | 通过；不含 integration build tag |
| `server/` 下 `go vet ./...` | 通过 |
| `./scripts/check-server-postgres.sh` | 通过，使用固定 PostgreSQL 17 独立临时容器；正常 HTTP 命令链不调用重建 |
| 追加规模测试后 `GOFLAGS=-v ./scripts/check-server-postgres.sh` | 通过；100 个后续 Decision 均经正式 service 创建，完整读取与稳定排序通过 |
| HTTPS production Web + Chrome 原生交互 | 通过本轮提案、接受、Ticket、刷新双向跳转与撤权清空；证书提示由项目所有者处理 |
| `./scripts/check-server-backup-restore.sh` | 通过：两个固定 PostgreSQL 17 临时实例，Golden Path 备份恢复与重建等价性 |
| `./scripts/check-repo.sh` | 通过，检查 227 个文件 |
| `./scripts/check-web.sh` | 通过：格式、Lint、72 项测试、TypeScript、production build、152 个锁定 package 的依赖基线 |

Web JavaScript 构建为 275.94 kB / 80.63 kB gzip。新测试曾因未安装的 jest-dom matcher 及辅助函数返回类型不正确失败，已改用现有断言与准确类型，并完整重跑 Web 检查通过；没有为测试安装依赖。

数据库验收分别证明：

1. 正式 HTTP 提案、接受和 Ticket 创建后立即 GET，Decision Timeline 为两项、Ticket 为一项；精确重试不增加活动，原 Thread / Decision 能发现后续结果。
2. 原 evidence 权限保持不变；撤销 Thread 读取或反向目标 Project 权限后，重新读取不保留身份、数量或时间线索。跨 Project 负面读取使用独立安全 fixture，不把它当作已开放跨 Project 创建能力。
3. 对 Activity 插入注入失败时，提案、接受、Ticket 创建都失败；业务表、关系、事件、Outbox、receipt 和 Activity 数量不变，原 Current / Timeline 保持一致。
4. 重建插入失败后，原投影保持完整；重建等待并发业务写入时，释放写事务后可读到其活动，未被旧快照覆盖。
5. 清理 Outbox 与 Activity 后显式重建仍得到等价 Nexus View，证明投递状态不是恢复源。

## 规模证据与边界

本机固定 PostgreSQL 17、单请求、100 条可读 incoming Decision 的一次完整 Thread Nexus View 读取为 **509 次 SQL 调用、约 56.34 ms**。该数据包含当前逐项权限和标题读取，不代表 100 个用户或并发容量，也不是性能 SLA。

查询仍随关系数线性增长，当前完整返回，不隐藏截断、不返回未授权总数。后续分页、结果上限与反向索引需要独立审查，不能将本轮小样本当作生产容量证明。

## 浏览器与运维证据

现有 HTTPS browser fixture 已构建并启动；移除了 fixture 中的 Activity 手工重建。固定 Caddy / PostgreSQL 镜像复用本机缓存，无系统证书安装。

内置浏览器因临时 CA 返回 `ERR_CERT_AUTHORITY_INVALID`，没有继续入口；外部浏览器 extension 控制连接曾超时。随后使用原生 Chrome UI，由项目所有者手动处理标准证书提示，进入正式 HTTPS 页面；Agent 未代点安全提示或关闭校验。

Chrome 中已完成本轮真实交互：

1. Contributor 登录并从既有测试 Thread 正式提出新的 Decision；刷新 Thread 可见 incoming Decision，并沿“打开后续对象”进入。
2. Decision 无需重建即可显示 `decision.proposed`；切换 Decider，填写结论与理由并显式确认，刷新后显示 proposal / acceptance 两条 Activity。
3. 从 Accepted Decision 正式创建 Ticket；刷新 Decision 可见 incoming Ticket，沿链接进入后显示 `ticket.created` 和来源 Decision。
4. 从 Ticket 返回 Decision，再返回原 Thread，方向与来源仍正确；检查 Ticket 实际布局，标题、关系、时间线和链接可见。
5. 在该独立临时数据库撤销 Decider 的 Thread membership 后刷新，页面只显示不可区分的不可读错误，旧 Thread 标题和后续关系被清除。
6. 退出测试 Session，关闭本次新建的 Chrome 标签，再通过 fixture stop 标记停止环境。

测试 Decision 和 Ticket 均经正式浏览器写入口生成；没有 SQL 预置最终业务结果、手工 Activity 重建或客户端 fixture fallback。本轮只验证桌面 Chrome，未重跑移动浏览器、浏览器网络模糊失败或多浏览器并发；重试和投影故障由本轮自动化分别覆盖。

双实例备份恢复已通过，测试脚本完成临时容器与网络清理。浏览器 fixture 收到 stop 后正常结束（退出码 0），脚本完成临时容器、卷和文件清理；测试 Session 已退出。真实团队持续使用、在线漏洞审计、远端配置、提交、push、PR 和部署未执行。
