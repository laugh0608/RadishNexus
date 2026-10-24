# 2026-10-24 单机停机升级与失败恢复

状态：手册、隔离演练入口和真实故障测试已实施；自动化与四阶段浏览器演练通过。U27 独立操作者复验未执行，其他部分覆盖见下表，不能宣称 ADR-0043 全矩阵退出或已开放外部试用。

所有者确认按 ADR-0043 推进本机非生产隔离 Compose、合成数据、故障注入及清理，后续明确允许仅下载现有 lockfile / go.sum 锁定依赖。所有者随后授权将本轮工作区改动本地提交；未授权 push、真实实例、系统证书或外部试用操作。

## 基线与实现

- 起点 `dev` / `master` 为 `67d460d`，工作区干净；未 fetch，本地跟踪引用不能解释为实时远端状态。
- 历史版本对为 `333c4ce51f39382f144d921880311841fa20c97f` / schema 017 → `171527ff67efe7b89d52db97086cf98d317d1e3e` / schema 018，使用各自原始 Dockerfile 与本地固定基础镜像。后续依赖修复已在当前分支，历史工件只用于隔离演练，不作为对外试用候选。
- [运维手册](../../../deploy/upgrade-recovery.md)落实身份核对、停写、备份往返、失败分类、显式续迁/恢复与唯一入口；[演练脚本](../../../scripts/check-upgrade-recovery.py)复用正式 Compose 与 migration / backup / restore。
- [迁移故障测试](../../../server/db/upgrade_failure_integration_test.go)复用真实历史 migration。实际 PostgreSQL COMMIT 确认丢失代理提取为 [integration 专用共享夹具](../../../server/internal/testsupport/pgfault/commit.go)，账户恢复与 owner 交接继续复用，不进入生产构建。
- [恢复故障测试](../../../server/internal/backuprestore/upgrade_failure_integration_test.go)使用真实 restore CLI 和 pg_restore，在事务内 CREATE TABLE 阶段注入 SQL 错误或终止 backend；在权威恢复后注入 Activity 写入失败。不以 mock 返回值代替事务故障。
- 未新增 schema、产品权限、公共协议、依赖或 lockfile；生产 migration 逻辑不变，仅修正文档注释中的串行化表述。

## 环境与证据索引

宿主 Darwin arm64，Docker 29.8.2、Compose 5.5.0；Docker 可用 10 CPU、16,818,991,104 字节内存（最终复跑实测）；PostgreSQL `17.10-alpine` 与 Caddy `2.11.4-alpine` 固定 digest 见脚本。浏览器为独立 headless Chrome；桌面 1440×1000，手机视口 390×844。只发布 localhost Caddy 端口，app / 数据库不发布宿主端口。3 个合成账户、1 个受限 Project、1 个 Channel，单 app、一个测试 SSE 连接；不构成持续使用或容量证据。

浏览器演练报告为本机 `/tmp/radishnexus-upgrade-report-20261024-i.json`，总耗时 519.728 秒（含人工驱动浏览器等待和清理），`status=passed`、`cleanup=passed`。对应 source / restored 为 `radishnexus-upgrade-qof5unmx-source` / `radishnexus-upgrade-qof5unmx-restored`，数据库 system identifier 分别为 `7700108579650183201` / `7700109455580721185`，业务 ID 相同而数据库 / volume 不同。

| 工件 | 实际 image ID |
| --- | --- |
| 017 app / Web | `sha256:d9f66a37d1017ba57b0ce18ad3bb1e241ff4d07a57d40882a9734bd3a3b0e595` |
| 017 operations | `sha256:5e3b9b362a744491efef707bc5c0017fb46282fecd2f380cc748f019ae1ae926` |
| 018 app / Web | `sha256:2fdae7d6f73a5a4e98ca2affa0b56f020ab3f1e773bf7d4d67b417394af1603c` |
| 018 operations | `sha256:6193d35118355f1017faf2103b53cac8c184d74c2b0504c5c05bfe1361514f20` |

Python HTTPS 客户端在每个实例使用其实际 CA 验证证书；浏览器仅该独立临时 context 对本轮精确 localhost origin 忽略自签证书错误，未写系统信任。故浏览器结果证明 UI / Cookie / CSRF 与权限行为，不能充当操作系统证书信任验收；真实目标环境必须按手册配置正常信任，不能沿用这个测试例外。

截图及快照位于本机忽略目录 `output/playwright/upgrade-20261024/`，关键截图已实际打开复核：`before-ticket-desktop.png`、`before-document-mobile.png`、`after-document-history-desktop.png`、`restored-old-document-mobile.png`、`restored-upgraded-write-mobile.png`、`restored-upgraded-ticket-desktop.png`。Document 手机视口测得 `scrollWidth=innerWidth=390`。Ticket 桌面长 ID 与邻接链接较拥挤，未把本轮结果解释为整站视觉验收。

## 实际验证

| 验证 | 本轮结果与证据 |
| --- | --- |
| `GOPROXY=off GOTOOLCHAIN=local ./scripts/check-server.sh` | 通过；Go race、vet、tidy diff 和模块校验；本机日志 `/tmp/radishnexus-upgrade-server.log` |
| `GOFLAGS=-race GOPROXY=off GOTOOLCHAIN=local ./scripts/check-server-postgres.sh` | 通过；含新增故障及已有账户恢复 / owner 交接确认丢失测试；db 13.929 秒、authn/postgres 188.713 秒；日志 `/tmp/radishnexus-upgrade-postgres-final.log` |
| `GOPROXY=off GOTOOLCHAIN=local RADISHNEXUS_OWNER_TRANSFER_LEGACY_REF=333c4ce ./scripts/check-server-backup-restore.sh` | 最终复跑通过，含同版本 / 旧工件恢复与新增四场景；日志 `/tmp/radishnexus-upgrade-backup-final3.log`（新增故障组 1.66 秒） |
| `python3 scripts/check-upgrade-recovery.py --allow-locked-downloads --browser-review --report /tmp/radishnexus-upgrade-report-20261024-i.json` | 通过；四个 browser barrier 都有真实 UI 证据；恢复副本写入不改变失败源快照 |
| 最终脚本复跑 | 通过；`--allow-locked-downloads`、不启用 browser barrier，报告 `/tmp/radishnexus-upgrade-report-20261024-j.json`，292.284 秒，status / cleanup 均 passed；含 Secret / CA 缺失、request ID、verifier 日志扫描、Docker 资源与清理复核。浏览器明确 not_run，UI 证据只引用 i 报告 |
| 仓库检查 | `./scripts/check-repo.sh` 通过（540 文件），`git diff --check` 与 Python 语法检查通过 |

没有修改 Web 源码，未重复全部 Web 单测；历史正式 Dockerfile 的 Web 构建实际通过。初始离线构建因缓存缺失失败，获准后下载既有锁定依赖；没有用替代包或改 lockfile 绕过。演练脚本开发期纠正了 Docker IPAM 空值、正式 API 字段 / key / 幂等返回码和既有认证限流等待，均未放宽产品合同。恢复测试超时保护曾错误覆盖 fixture 清理 context，已改为只约束子进程并重新验证。

## U01～U28 逐项结果

“通过”仅指下述明确环境和范围；“部分覆盖”不等于该编号全部通过。DB 表示当前工作区真实 PostgreSQL integration；Compose 表示上述正式历史工件双实例；UI 表示本轮实际 Chrome 操作。

| 编号 | 结果 | 观察与限制 |
| --- | --- | --- |
| U01 | 通过 | 记录完整 commit / app-Web / operations image ID；新 restore 工具拒绝 017 manifest，旧 app 接 018 时 live 200 而 ready 503；配套组合恢复服务 |
| U02 | 通过 | source / restored 的容器、volume、system identifier 独立记录；用源身份校验目标显式失败，不能仅凭相同业务 ID 放行 |
| U03 | 通过 | DB history 缺号 / checksum 漂移拒绝且不改写；额外手工列不被 readiness 发现，明确该限制；备份现有 integration 拒绝未分类 relation |
| U04 | 通过 | 缺失 / 损坏备份在空库写入前拒绝；只填满 operation 的 16 MiB tmpfs，备份因无空间失败而原有效备份 hash 不变；最终复跑补验 Secret / CA 缺失 |
| U05 | 部分覆盖 | 正式 bootstrap / 邀请 / 配置 / 协作 API 建立链路，普通成员浏览器独立登录与发现；停写前记录快照。未执行真实团队长期持续协作或并发业务请求结果未知演练 |
| U06 | 部分覆盖 | 实际 SSE ready 后保持连接，停止 Caddy / app 后关闭；额外测试数据库客户端使停写检查拒绝。未部署真实 Webhook / worker，未覆盖所有外部自动重启系统 |
| U07 | 通过 | 两 runner 串行无重复 history；短预算测试锁等待取消；正式 migrate CLI 等待约 120.529 秒后报 advisory lock 错误，history 不变，清理锁后可续迁；不解释为全局硬时限 |
| U08 | 通过 | 018 所有 DDL 已执行、history INSERT 前触发真实 SQL 错误；本条 DDL/history 原子回滚，原 017 history 与关键 DDL 保持 |
| U09 | 通过 | 在可观测数据库 barrier 分别 context 取消、终止 migration backend；新连接核对完整前缀、锁释放与后续迁移；未声称测试宿主断电 |
| U10 | 通过 | 代理收到真实服务端 COMMIT 后丢弃确认并断开；客户端报错但 018 history / DDL 已提交，同工件重复无新增事实 |
| U11 | 通过 | 从真实 016 前缀运行现有链，017 提交后 018 故障；017 合法前缀保留，只继续未提交条目，无演示 migration |
| U12 | 通过 | 故障解除后显式续迁、完整后再重复；已有 history 不改写，目标 DDL 与完整 history 一致 |
| U13 | 通过 | 正式 017→018 初始化角色 revision / 有效邀请任期；按旧列对比 manifest 全部权威表（迁移表除外）hash 不变；原 owner 交接 integration 继续核验不可变合同 |
| U14 | 通过 | schema 018 完整时旧 app readiness 拒绝、缺失 Web root 导致非零退出；期间 Caddy 保持停止，修正整套工件后重新验收 |
| U15 | 通过 | 维护依赖实际停止 Caddy / app 与无其他客户端，SSE 断开；另证 live 与 ready 可不同，未用探针变绿直接认定开放 |
| U16 | 部分覆盖 | 新工具拒绝旧 manifest、非空恢复目标不覆盖；真实 pg_restore wrapper 报 16 major 被拒绝，现有 manifest major 单测通过。未启动另一 major 的 PostgreSQL 服务端 |
| U17 | 通过 | 匹配旧工具恢复独立空库、权威表和 Activity hash 匹配；只启动旧 017 app / Web，history 保持 17，原源停止且快照不变 |
| U18 | 通过 | U17 完成后独立停止恢复副本，显式迁移到 018，再启动匹配工件并通过 HTTP / UI；未把恢复旧服务默认解释为自动升级 |
| U19 | 通过 | 实际 pg_restore 的 CREATE TABLE 阶段触发 SQL 错误或终止 backend；CLI 非零、无 completed，核对事务回滚，另建空库完整重试 |
| U20 | 通过 | 权威表恢复完成后 Activity INSERT 触发器报错；权威 hash 匹配但 Activity 无残留，CLI 不报完成；原目标拒绝覆盖，换空库完整重试 |
| U21 | 通过 | 整体权威快照、关系、Ticket 状态、Document 两版、Audit / receipt 与重建 Activity 一致；正式受限查询对无权成员返回 404 |
| U22 | 部分覆盖 | 本轮 owner 交接发生在备份后，恢复带回原 owner，receipt 为 not_observed 而不重放；旧 Session / 邀请 / OIDC 事务为空、setup 关闭、UI 重新登录。密码 verifier 快照一致与恢复测试通过，但 Compose 本轮未另做备份后改密码 |
| U23 | 通过 | 停写备份先往返比较；另生成一条消息和一次 owner 交接并明确舍弃，恢复后均不重放；只对合成演练数据适用。最终副本 UI 验收写入单独记录 |
| U24 | 通过 | 同 origin 切换独立实例，始终先停源再开副本；普通升级保留 Session，恢复后刷新 / 后退均要求登录；再次升级恢复副本时新 Session 保持 |
| U25 | 部分覆盖 | UI 登录、Project→Ticket→Document、历史、来源、桌面/手机读写通过；最终副本 Document 2→3、Ticket in-progress/2→done/3；无权登录与后退清空内容。精确重试、SSE 与列表权限由正式 HTTPS 脚本验证；未通过 UI 重做完整 Message→Decision 创建链、原生 IME 或独立真实成员持续使用 |
| U26 | 通过 | 报告记录工件、阶段和错误边界；HTTP 404 的 body/header request ID 一致（最终复跑）；扫描本轮密码、DB Secret、Cookie、API 正文及 verifier 无泄漏；不输出原始 SQL / DSN / dump。浏览器合成正文截图不代表可收集真实业务正文 |
| U27 | 部分覆盖 / 未执行 | 已记录实际规模及分阶段时间；Activity 与 restore 合并计时。另一操作者按手册判断停止 / 续迁 / 恢复的走查未执行，不能由本 Agent 自验或脚本替代 |
| U28 | 通过 | 本轮专属容器 / network / volume / image tag、临时 Secret / dump / CA 删除，Chrome 关闭；保留脱敏报告、合成截图与共享构建缓存。失败清理状态独立记录，不操作用户实例、系统信任或远端 |

## 规模、耗时与数据取舍

浏览器演练的停写前备份：数据库 10,876,595 字节，dump 229,921 字节，36 张权威表；2 条 Message、1 条 Decision、1 条 Ticket、1 个 Document、2 个文档版本、11 条 domain event。

| 阶段 | 实测秒数 |
| --- | ---: |
| 停入口 / app 并等待 SSE / 连接收敛 | 15.630 |
| 备份 CLI | 0.336 |
| 独立旧空目标 restore（含 Activity 重建） | 0.389 |
| CLI 锁等待故障与清理整段 | 121.345 |
| 017→018 migrate CLI | 0.302 |
| 匹配新服务启动 / ready | 11.045 |
| 旧恢复服务启动 / ready | 10.959 |
| 恢复副本再次 migrate | 0.272 |

这些时间包含容器命令开销，Activity 未单独计时，不是生产 RTO / RPO。API 快照核验和浏览器等待也不能从单个 CLI 数值中推导。构建缓存命中仅约 3 秒，首次依赖下载另耗约 302 秒，不纳入停写窗口承诺。

明确舍弃的备份后事实为一条合成消息和一次 owner 交接；恢复时原源保持隔离，目标恢复旧 owner、交接 receipt 为 not_observed。最终目标验收另写 Document 第三版和 Ticket done / revision 3，只存在恢复副本，随后按本轮授权清理；不把这些写入计入恢复前快照一致性，也不自动合并回源。

## 剩余验收与接续

另一操作者需依手册走查 017 未提交、017 合法前缀、018 已提交但确认丢失、权威恢复成功但 Activity 失败四种状态，并能独立选择停止、续迁、空目标恢复或申请新的数据取舍授权。目标试用环境还要以包含后续依赖修复的实际候选核对工件、正常证书信任、外部 writer、容量、材料保管与责任分工。

当前阶段仍是 M0.5 / M1；真实 Jenkins B 与 macOS 原生中文 IME 继续暂缓。支持说明和评估授权选择仍待确认，未开放外部试用、签发授权、部署生产或完成完整 Golden Path。
