# 2026-10-28 上下文迁移有界补验

归档说明（2026-10-28 收尾）：本批已提交为 `211f050`。下文保留验收当时的基线、未提交状态及接续判断，不作为当前推进顺序；后续提交映射见[当日收尾](2026-10-28-daily-closeout.md)，当前安排见[当前状态](../current.md#接下来的事项)。

日期：2026-10-28（Asia/Shanghai）。基线：`5d53350`，schema 020；本记录对应本轮未提交的代码和测试。

## 结论与范围

承接[当前状态](../current.md)与 [ADR-0044 首片记录](2026-10-24-portable-context-package.md)的 P05 / P06 / P10 / P13 / P20，完成一批有界补验，修复三处实际边界问题。没有改变包格式、数据库 schema、权限、依赖或公共 API；没有恢复真实 Jenkins、原生中文 IME 或独立操作者验收，不宣布全部迁移范围、生产容量或 M1 退出。

所有者已在本次任务明确授权：使用已有固定 PostgreSQL 镜像建立专属临时容器、回环 HTTPS 服务和 CLI 子进程，仅对本轮子进程注入 SIGKILL；使用合成数据，结束后清理专属资源。不沿用 10 月 24 日的运行授权。

## 修复与根因

| 问题 | 原因与最终处理 | 回归证据 |
| --- | --- | --- |
| 恰好 64 MiB 的合法包写出后无法再读 | `Unpack` 按编码后的 manifest 计数，`WriteDirectory` 额外加换行，磁盘包多出 1 字节；写出改为同一 canonical manifest 字节 | 语义完整、连续 256 版 Document 的 64 MiB 包通过 Unpack → WriteDirectory → ReadDirectory；超出 1 字节拒绝 |
| 普通文件检查后被换成 FIFO 可能阻塞打开；链接替换需再复核 | Lstat 与 Open 之间不是原子操作；采用 nonblocking open、打开后路径与文件描述符身份复核，继续使用 `os.Root` 约束路径范围 | 确定性调度 Lstat 后的普通文件、符号链接、硬链接、FIFO、目录替换；均拒绝且不等待 FIFO writer；已打开根目录不跟随路径替换 |
| 失去唯一有效 evidence 的 Decision 包可通过 prepare，却在数据库 COMMIT 失败 | 包校验没有复用“Decision 至少保留一条 active evidence”的既有领域不变量 | 在 inspect / prepare 共用的 Validate 阶段提前拒绝；保留原数据库约束，不通过放宽约束接受无来源 Decision |

`readRegularSnapshot` 是文件身份校验的内部边界，测试传入真实 Lstat 快照后实施替换；没有可变全局 hook 或生产绕过开关。文件内容仍经摘要、白名单、结构与历史校验，不把一次身份检查当作来源真实性证明。

## 原矩阵接续

原记录保留 10 月 24 日事实，不把本轮证据改写为当日完成。

| 编号 | 本轮证据 | 收尾判断与保留限制 |
| --- | --- | --- |
| P05 | 五类记录在解析层验证上限减一 / 恰好上限 / 超一条；Validate 对每类超限单独拒绝；manifest、JSONL 单行、Document 单版验证边界；64 MiB 完整包写读和超 1 字节拒绝 | 本轮数值边界补验完成。记录数量用例验证解析层配额，不代表五类业务数据同时满额、所有组合的数据库压力验收 |
| P06 | 确定性文件替换与已打开根目录替换测试，原有穿越、链接、特殊文件和未列文件测试全部回归 | 本轮指定竞态补验完成。未穷举恶意文件系统的全部调度、原地写入和存储故障；不扩展支持平台或声称目录不可变 |
| P10 | 正式源创建链导出后，构造明确的历史移除声明，经独立数据库 import → export → 再次 import；Ticket→Decision 与 Ticket→Document 的移除主体、时间、原因、创建来源和 lineage 保留；Nexus 当前关系不复活，历史不产生 Outbox | 合法移除关系的独立数据库往返补齐。当前没有这两类关系的原生移除命令，本测试不声称存在该产品入口；移除唯一 Decision evidence 与 Thread source 仍拒绝 |
| P13 | 真实 CLI 与承载正式 Session / HTTPS adapter / ContextService 的独立服务子进程，在 Activity INSERT gate 分别 SIGKILL；等待事务结束，验证业务 / 历史 / 映射 / 身份无残留及 `not_observed`；原计划 CLI 重试和结果查询仅生成一份 graph | 本轮提交前 OS 进程中断补验完成，并追加 race 复验。服务子进程为集成测试宿主，不等于正式 Compose / 生产进程部署演练；提交后确认丢失继续由原 P14 的真实 COMMIT 测试覆盖 |
| P20 | 完整 64 MiB 包解析 / 落盘 / 读回测量与 Go heap 计数；正式业务创建的 1,000 条新增消息完成导出、prepare、apply、重试、投影重建及再导出 | 代表性容量证据补齐；64 MiB 只测文件 / 解析链，未执行满额数据库 / HTTPS 迁移，仍不承诺生产并发容量、独立操作者持续使用或目标实例性能 |

## 实测规模与耗时

运行环境：本机 macOS arm64、Go 1.26.3，仓库已有 PostgreSQL 固定镜像 `postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193`；使用专属临时数据库，单次运行数据，不作为基准性能承诺。

### 正式业务创建的代表性数据库往返

`TestContextRepresentativeCapacity` 通过 bootstrap / 邀请 / 配置及 application service 创建基础协作链，再通过正式 CreateMessage 写入 1,000 条合成消息；没有 SQL 预置最终业务图。

| 指标 | 实测 |
| --- | --- |
| 包字节 | 1,885,276 |
| 业务对象 / 历史作者 | 1,009 / 2 |
| 历史 / 关系 / 文档版本 | 1,015 / 5 / 3 |
| 新增消息正式创建 | 1.776 s |
| export | 626.277 ms |
| prepare | 52.394 ms |
| apply（含事务内 Activity） | 834.825 ms |
| 独立 Activity 全量重建 | 5.289 ms |
| 验证（含精确重试、重建比较和再导出） | 830.796 ms |

重建前后 Activity 完整快照相等；目标 1,001 条消息、单个 import batch，历史 Outbox 为零。这里的重建用于证明可恢复性，不用于修复正常导入后缺失的投影。

### 64 MiB 文件与解析链

`TestPackageExactTotalByteLimit` 使用语义完整的包：Document 连续 256 版，大部分正文为单版上限，剩余字节用合法 JSON 空白补齐，使 manifest 加文件内容恰好为 67,108,864 字节。该样本用于包边界，不冒充正式数据库创建的满额使用场景。

- Unpack / WriteDirectory / ReadDirectory 合计约 3.138 s；包含构造的测试耗时约 3.990 s。
- 上述三个操作累计 Go allocation 约 1,200,465,448 字节；操作后 HeapInuse 样本 221,855,744 字节。累计分配不是同时占用，也不是进程峰值 RSS；序列化 / 校验重复分配的成本仍应纳入未来容量评估。
- `/usr/bin/time -l` 的 OS 资源采样因本机 `sysctl kern.clockrate` 权限限制失败；测试本身通过，未取得可信进程峰值 RSS。记录 Go runtime 计数，不将其替代为系统内存上限结论。

## 实际验证

- `GOPROXY=off GOTOOLCHAIN=local GOCACHE=/tmp/radishnexus-server-go-cache go test -race ./internal/contextpackage ./cmd/nexus-context -count=1`：通过，含三个缺陷的最终回归。
- `./scripts/check-server-context-package.sh`：通过，原有 `TestContext*` 与新增关系、容量、进程故障测试；使用固定已有镜像，不下载依赖。该脚本的名称筛选不会运行包解析的所有单元用例，上条单独覆盖。
- 独立构建 CLI 后，通过既有 `run-postgres-go-integration.sh` 设置 `GOFLAGS='-race -run=^TestContextProcessDeathAndRetry$ -count=1 -v'`：通过，验证最终进程清理实现与两种 SIGKILL 路径。
- 定向 `go vet ./internal/contextpackage ./cmd/nexus-context` 与 `go vet -tags=integration ./internal/goldenpath/postgres`：通过。
- 64 MiB 单项非 race 测量：通过，具体边界与数据见上节。
- `./scripts/check-repo.sh`：通过（593 个文件）；`git diff --check`：通过。

本轮没有变更 Web，没有重跑浏览器、完整 Web 检查、完整备份恢复矩阵或目标环境部署。历史浏览器 / 备份证据仍引用原记录，不重新标记为本轮通过。SIGKILL 不会执行 CLI 的 deferred logout：数据事务恢复与 Session 撤销是不同事项，本轮只使用可销毁身份 / 数据库；实际中断后仍需按操作手册复核并撤销遗留 Session。

## 资源与下一步

测试容器按脚本 EXIT 清理，子进程按测试 cleanup 停止并 Wait，子进程私有配置、CA、包与计划均位于父测试临时目录内；SIGKILL 后也由父测试回收。本轮结束核对没有本轮测试容器残留。不修改系统信任、真实实例或远程状态，未提交 / push。

下一产品切片的[负责人提案 ADR-0046](../../adr/0046-ticket-assignee.md)已形成，D01～D07 待确认；尤其须确认成员退出后的责任记录与新 profile 的历史负责人语义。它仍是设计，不修改现有领域模型、schema、API 或包版本。当前接续顺位只维护在[当前状态](../current.md#接下来的事项)。
