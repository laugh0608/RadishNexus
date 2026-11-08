# M1 受控试用准入核对

日期：2026-11-08

候选源码：`4f2b208ae0c2ffd2e05d4647dc963b2faba53661`（`4f2b208`）。配套 schema 025、Activity 13，默认导出 `project-collaboration-v6`。本机预演工件已登记，正式试用的目标环境、工件交接和参与主体待确认。当前结论为准入准备中。 [候选实施记录](2026-11-08-decision-manual-review.md) · [支持范围与准入判据](../../trial-support.md#准入与退出判据)

按首批建议范围核对单宿主、单 Go app、单 PostgreSQL 17、Caddy 同源 HTTPS 和一个非生产 Workspace，使用合成或充分脱敏的可重建材料。下表将候选验证、历史隔离证据和目标环境待验证项分开记录。责任人先按角色分配，项目所有者负责确认具体人选。参与者、实际联系方式和授权材料在受控位置留存。

## T01～T08 准入表

| 编号 | 当前状态 | 已有证据 | 缺口与通过条件 | 责任人 |
| --- | --- | --- | --- | --- |
| T01 范围、参与主体与数据边界 | 待确认 | 支持草案已提出受邀主体内部、非生产、可重建数据及一个 Workspace 的首批范围。候选 Chrome 验收使用合成账号和数据。 [推荐首批范围](../../trial-support.md#推荐首批范围) · [候选验收](2026-11-08-decision-manual-review.md) | 确认具体参与主体、部署负责人、数据责任人及可用材料。私下记录用途、可使用的数据类别、脱敏要求、参与安排和停止条件，经项目所有者确认 | 项目所有者确认范围；试用部署负责人和数据责任人共同核对数据与参与安排 |
| T02 软件使用权 | 待确认 | E01～E12 已整理为评估授权待决事项，包含候选版本、用途、运行、恢复副本和数据取回的待确认范围。 [评估授权待决事项](../../evaluation-authorization-decisions.md#待决矩阵) | 完成所有者选择和专业复核，确认具体主体、适用工件及用途范围，取得有效书面授权并在受控位置留存 | 项目所有者负责决定与签发；专业复核人负责复核 |
| T03 环境与工件 | 本机 Compose 预演通过，正式试用环境待验证 | 已登记候选 app、operations 与独立 CLI 的实际身份。本机 Compose 通过全新 schema 025 安装、正式 Web 入口、CA 校验、仅 Caddy 发布回环端口及 Go/PostgreSQL 端口隔离。候选另有回环 HTTPS Chrome 证据。 [本机预演结果](#本机-compose-预演结果) · [候选 Chrome 验收](2026-11-08-decision-manual-review.md#正式-https-chrome-验收) | 选定并记录宿主 OS / CPU、Docker / Compose、PostgreSQL、Caddy 和浏览器版本。登记同一候选的 app（含 Web）、operations、独立 `nexus-context` CLI、配置与旧工件留存的实际镜像 ID 或校验值。在目标宿主复验安装、显式 migration、schema readiness、Caddy CA 信任、HTTPS 和端口隔离 | 维护者核对工件和版本；试用部署负责人记录目标环境并负责复验 |
| T04 正式用户路径 | 部分有证据；独立成员真人复验本轮跳过，保留待验证 | 候选在普通成员身份下由 Agent 验证正式导航、复核、替代、撤权后 BFCache 重读、Ticket 和 Document 来源及后续写入。账号恢复、Workspace owner 交接和成员生命周期另有历史隔离证据。 [候选 Chrome 验收](2026-11-08-decision-manual-review.md#正式-https-chrome-验收) · [账号恢复](2026-10-18-controlled-account-recovery.md) · [owner 交接](2026-10-18-workspace-owner-handoff.md) · [成员生命周期](2026-10-24-membership-lifecycle.md) | 目标环境需覆盖登录、发现与协作、拒绝越权、账号恢复和主动交接，并告知已知限制。ADR-0050 R08 独立成员真人操作留待单独取证 | 维护者准备场景、记录结果与已知限制；Workspace owner 配置权限与交接，试用部署负责人负责账号恢复，参与成员负责独立操作 |
| T05 支持与报告 | 待确认 | 仓库已有公开缺陷模板与私密安全报告流程。支持草案已定义脱敏报告字段、普通问题与安全问题的分流。 [缺陷模板](../../../.github/ISSUE_TEMPLATE/bug-report.yml) · [私密安全报告](../../../SECURITY.md#私下报告) · [责任与故障报告](../../trial-support.md#责任与故障报告) | 确认支持联系人、联系时段、首次回复安排、私密运维渠道和升级责任。用合成故障走查分流，并准备一份可用的脱敏报告示例 | 项目所有者确认支持安排；维护者准备示例并核对报告入口 |
| T06 可恢复性 | 本机 025 空目标恢复通过，正式试用环境待验证 | 本机候选 Compose 停写后完成正式备份和独立空库恢复，40 张权威表及 Activity 一致，恢复后原复核 receipt 可重试。候选另有匹配 023 / 024 旧工件的恢复后迁移证据。ADR-0043 的故障诊断与续迁另有历史隔离 Compose 证据，U27 未执行。 [本机预演结果](#本机-compose-预演结果) · [候选分层验证](2026-11-08-decision-manual-review.md#分层结果) · [ADR-0043 演练](2026-10-24-upgrade-failure-recovery.md) | 在目标宿主用匹配工件执行实际备份和独立空目标恢复，核对权威事实、成员准入和 Activity 重建。按 ADR-0043 补齐目标范围的停写、失败诊断、续迁或恢复证据与操作者复验。恢复前后保留明确的版本和数据取舍记录 | 试用部署负责人执行；维护者复核迁移历史和恢复结果，数据责任人确认数据取舍 |
| T07 数据取舍与测量 | 已有本机小样本实测，正式环境与阈值待确认 | 候选本机 Compose 已分别记录停写、备份、恢复 CLI 和独立 Activity 重建耗时。历史单 Project 样本另有 1,000 条消息的上下文包导出与导入测量。 [本机小样本实测](#本机小样本实测) · [上下文包补验实测](2026-10-28-context-package-supplement.md#实测规模与耗时) | 先明确目标试用数据量、并发连接及多标签页场景。在候选目标环境分开记录停写、备份、恢复和 Activity 重建的实测耗时，以及对象数量、数据量和连接规模。由数据责任人及部署负责人确认可接受停机与数据损失 | 试用部署负责人测量；数据责任人确认可接受数据损失，维护者与试用部署负责人共同核对停机窗口 |
| T08 暂停和退出 | 本机停写和清理已验证，正式退出安排待确认 | 本机候选 Compose 验证了停写、SSE 关闭及清理专属资源。候选另有整库恢复与 v6 单 Project 上下文包往返证据。 [本机预演结果](#本机-compose-预演结果) · [数据、备份与退出](../../trial-support.md#数据备份与退出) · [上下文迁移手册](../../../deploy/context-package.md) | 确认谁决定停写与恢复、谁保管证据、谁负责终止联络。约定备份访问、保留或删除、停止访问及到期取回方式，将适用工件、恢复权限和数据取回条件与 T02 的实际授权核对 | 项目所有者确认暂停和退出安排；试用部署负责人执行，数据责任人确认留存和取回范围 |

## 本机预检证据

以下实测来自维护者本机，拟用于首轮隔离预演。正式试用目标宿主仍待选定。

| 核对项 | 实际结果 |
| --- | --- |
| 宿主与客户端 | macOS 27.0.1 arm64，Docker client / server 29.8.2，Compose 5.5.0，Docker engine `linux/aarch64`，Chrome 156.0.8078.80 |
| 固定基础镜像 | 候选引用的 PostgreSQL 17.10、Caddy 2.11.4、Go 1.26.7 builder、Node 24.16.0 builder 和 Alpine 3.23.5 runtime 均可按完整 digest 在本机检查，平台均为 `linux/arm64`。 [Compose 配置](../../../deploy/compose.yaml) · [Dockerfile](../../../deploy/Dockerfile) |
| Compose 静态解析 | 从 `4f2b208` 的 Git 快照分别解析基础配置和首次初始化 overlay，均通过 `docker compose config --quiet`。基础配置的端口映射未限定宿主地址。另验证临时 overlay 的解析结果仅由 Caddy 发布 `127.0.0.1` HTTPS 端口，app 和 PostgreSQL 保持无宿主端口 |
| app / operations | 当前两个 `:local` 镜像缺少候选提交来源标记，工件身份待核验。预演从候选 Git 快照构建专属标签，记录 app（含 Web）与 operations 的实际 image ID，以及生效的 Compose 和 Caddy 配置摘要 |
| 独立 `nexus-context` CLI | 从候选 Git 快照在本机 Go 1.26.3 下使用已缓存依赖离线构建成功。临时二进制 SHA-256 为 `6b522c53af4c060bdc7dadbf0ce92e476e7b3776a0066203eb614e34745f8f93`，已清理。正式交接时按实际运行平台单独构建并登记校验值。 [CLI 构建与权限](../../../deploy/context-package.md#准备与权限) |

## 首轮预演范围

项目所有者已确认下表中的本机隔离预演范围，使用合成数据、回环 HTTPS 和固定基础镜像。离线构建发现缓存缺项后，另行确认构建阶段可联网获取现有锁定依赖。正式试用的主体、宿主、支持与退出安排仍待确认。

| 待确认项 | 建议安排 |
| --- | --- |
| T01 范围与数据 | 先在维护者本机演练一个合成 Workspace 和一个 Project，用合成身份和可重建材料验证讨论 → Decision → Ticket → Document 的来源链。正式试用主体、部署负责人和数据责任人由项目所有者确认后私下登记 |
| T03 环境与工件 | 固定 `4f2b208`，基础镜像使用本机已有的固定 digest，构建阶段使用锁定依赖。为预演源实例和空恢复目标使用专属 Compose project、标签、网络、volume 与临时 Secret。Caddy HTTPS 限定回环地址，CA 信任限定在验收客户端 |
| T05 支持与报告 | 建议由维护者接收普通问题，项目所有者确认联系人、联系时段和首次回复安排，部署负责人上报停写、备份和恢复情况。公开缺陷沿用仓库模板，敏感运维问题走确认后的私密渠道，安全问题沿用仓库安全报告流程。 [责任与故障报告](../../trial-support.md#责任与故障报告) · [私密安全报告](../../../SECURITY.md#私下报告) |
| T08 暂停和退出 | 建议由部署负责人在权限异常、备份不可验证或升级状态不明时先停写，维护者复核证据，项目所有者确认是否继续。退出先停止访问，由数据责任人核对备份与可读上下文包的取回范围，再由部署负责人按约定清理。正式试用的保留期、访问人和退出联系人待确认。 [数据、备份与退出](../../trial-support.md#数据备份与退出) |

**T05 脱敏报告样例（合成场景）。** 候选为 `4f2b208`。升级后 readiness 返回 `503`，预期 schema 025，观察到 schema 024。影响限于专属测试实例，公共入口保持停写，升级前备份仍保留。报告附工件身份、迁移历史核对结论、备份时点及其后是否有写入。将此问题转给部署负责人和维护者，由维护者先核对工件与 history，再决定续迁或空目标恢复。 [失败定位](../../../deploy/README.md#失败定位) · [升级恢复手册](../../../deploy/upgrade-recovery.md)

## 首轮验证步骤

1. 从固定 Git 快照构建并登记候选工件，显式迁移至 schema 025，检查 readiness、Caddy CA 校验、回环绑定和端口隔离。沿正式入口创建最小合成链路，验证可读来源与授权拒绝。
2. 停写并完成整库备份，在独立空目标使用同一候选恢复。核对迁移历史、协作来源、成员准入和 Activity 13 重建，分别记录停写、备份、恢复和重建的耗时，以及本轮数据量与连接规模。正式试用的容许停机、容许数据损失及代表性负载另按 T07 确认。
3. 本机预演结束后停止两套实例，只清理本次创建的 Compose project、网络、volume、专属镜像标签、临时目录和会话，保留脱敏结果与构建缓存。T04 的真人复验继续待验证。T02 和正式试用目标的准入各自确认。

首轮用候选 025 匹配工件验收空目标恢复。既有 017→018 故障演练脚本的版本断言固定在历史基线；目标环境的 024→025 故障验证另按 ADR-0043 的手册和矩阵实施。 [部署演练](../../../deploy/README.md#仓库演练) · [历史演练入口](../../../scripts/README.md#单宿主升级失败与恢复演练) · [当前执行顺序](../current.md#近期执行顺序)

## 首轮构建结果与接续

已归档 `4f2b208` 的 Git 快照，并以固定基础镜像、`--pull=false` 和 `--network=none` 构建专属 app 镜像。构建停在 Dockerfile 的 `go mod download && go mod verify`，隔离 builder 的缓存缺少锁定的 `github.com/jackc/pgpassfile@v1.0.0` 所需文件。进一步核对本机 Go 缓存后发现，`golang.org/x/sys@v0.43.0` 的模块元数据缺失。Linux ARM64 musl 所需的 109 个 npm 锁定包中，本机内容缓存命中 106 个，缺少 `@oxlint/binding-linux-arm64-musl`、`@rolldown/binding-linux-arm64-musl` 和 `lightningcss-linux-arm64-musl` 的锁定包内容。npm 构建阶段尚未完成，缓存核对结果与镜像构建结果分别记录。 [候选 Dockerfile](../../../deploy/Dockerfile)

该次离线尝试在镜像构建阶段结束，专属资源已清理。项目所有者随后确认，构建阶段可联网获取 `server/go.sum` 和 `web/package-lock.json` 中的锁定依赖，基础镜像仍使用本机已有的固定 digest。运行阶段沿用回环 HTTPS 和专属资源范围。

## 本机 Compose 预演结果

最终复跑通过。两套实例均来自候选 Git 快照。候选 Dockerfile、Compose、Caddyfile 和锁文件与 `4f2b208` 一致。首次联网构建成功后，演练脚本在成员管理预览阶段读取了错误的摘要字段。修正为读取正式响应的 `digest` 后，使用清理后的专属实例复跑。

| 实际工件 | 身份 |
| --- | --- |
| app（含 Web） | `sha256:e6c59e4cbed2081eecae534ee633f60318cc34918dcfb6f22dcc63d4ea945efc` |
| operations | `sha256:3bd6d535258c749cf15f863e12811abdb64ce3bcd41e161cfb325b76b67eca7b` |
| 独立 `nexus-context` CLI | darwin/arm64，Go 1.26.3，`CGO_ENABLED=0`，SHA-256 `1f1ad148501ffa25da68636494e27f1802dd6153661b5ae0c22c3e07e8fa80ef` |
| 生效的 Compose 配置 | 源 SHA-256 `0a2ec839d028452c048a67b03492c2c1df1614bf1dc32721d64385da90ac734a`，恢复目标 SHA-256 `e86d6d5d0defa6f59e6534b7fa3f1250a0b4369e0903e4c3a9a39bd18da4e63f` |
| 两实例共用的 Caddyfile | SHA-256 `857724fc7bc2f498548fe43a2eb07049f04769ecc09cf1d0333872d499d36045` |

| 核对项 | 实际结果 |
| --- | --- |
| 安装与网络 | 全新实例显式迁移至 schema 025，迁移历史 25 项。源和恢复目标均可通过实例 CA 校验 readiness 与正式 Web 入口，未指定受信任 CA 时连接被拒。仅 Caddy 发布 IPv4 回环端口，app 与 PostgreSQL 无宿主端口，Go 只信任 Caddy 的固定 `/32` |
| 正式协作与权限 | 通过 HTTPS API 完成合成邀请、Project 成员管理、Message → Thread → Decision → Ticket → Document，并保留两版文档及一条 Decision 人工复核。普通 contributor 可读复核，越权记录复核返回 `403`，未获 Project 权限的成员读对象或复核返回 `404` |
| 停写与备份 | 停止 Caddy 和 app 后，活跃 SSE 关闭，确认源库没有其他客户端。正式 operations CLI 完成备份，manifest 标记 PostgreSQL 17、pg_dump 17 与 25 项迁移 |
| 空目标恢复 | 使用相同 operations 工件在空库恢复。源与目标的容器、volume 及 PostgreSQL system identifier 各自独立。40 张权威表的快照摘要逐表一致，恢复后的 9 条 Activity 均为版本 13，投影快照与源一致 |
| 恢复后访问与重试 | 目标的 Session、邀请与 OIDC 事务表为空，旧 Session 请求被拒。重新登录后验证可读来源、原 Ticket 状态、两版 Document 和访问拒绝。精确重试原 Decision 复核返回 `200` 及原事件 ID，事件仍只有一条。源库权威表在恢复复验后仍与备份时一致 |
| 清理 | 已退出恢复目标的临时 Session，核对本轮专属容器、网络、volume 和镜像标签均已清除。保留构建缓存和本节脱敏结果 |

### 本机小样本实测

本轮使用 1 个 Workspace、1 个 Project、3 个合成账户、2 条消息、1 条 Thread、1 条 Decision、1 个 Ticket、1 份两版 Document 和 1 个活跃 SSE 连接。数据库占 11,359,923 bytes，归档占 329,634 bytes。

| 测量项 | 单次实测 | 测量边界 |
| --- | --- | --- |
| 停写 | 15.593 秒 | 停止 Caddy 与 app 并确认无其他数据库客户端，SSE 关闭另作断言 |
| 备份 CLI | 0.370 秒 | 停写源上的正式备份命令，含临时运维容器开销 |
| 恢复 CLI | 0.459 秒 | 已启动的独立空库上的正式恢复命令，含命令内部 Activity 重建和临时运维容器开销 |
| 独立 Activity 重建 | 5.828 毫秒 | 在官方恢复和第一次快照比对完成后，从同一候选编译的临时测量程序计时调用重建函数。再次比对投影一致，计时排除临时容器启动和数据库连接 |

以上测量用于本机合成样本。正式试用的目标宿主、负载、可接受停机及数据损失仍待确认。目标环境的 024→025 故障验证、U27 和 T04 真人复验仍按各自范围取证。 [当前执行顺序](../current.md#近期执行顺序)
