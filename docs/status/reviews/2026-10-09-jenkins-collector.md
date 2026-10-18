# 2026-10-09 可信 Jenkins 采集代码与离线验证

## 范围与授权

所有者要求“提交工作区更改，继续推进下一步”。前一轮 Ticket / Component 浏览器补验、ADR-0035 A 与文档已精确提交为 `47f2761`（`feat: 实现 Jenkins 终态快照持久发送与恢复`），本地 `dev` 相对跟踪引用领先一笔；未 push。提交时工作区干净，本记录描述该提交之后的独立 B 采集代码。

真实联调预检发现本机 Docker 将宿主目录以 `fuse` 挂载，不满足已冻结的原生 Linux 文件系统边界。说明后，所有者明确选择“先交付代码，暂缓真实联调”。本轮没有启动 Jenkins controller / agent、迁移实验数据、创建 named volume、放宽文件系统限制、安装依赖或修改旧实验。

## 实现

- `experiments/jenkins-collector` 独立可信 controller 脚本：finalized 快速路径、初始化完成后及每 60 秒对账、每批 100 个编号、低编号缺口保留和循环复查。
- manifest、文件锁、连续前缀 / 扫描位置、历史缺失 / 不可读 / 运行中状态、不可变快照、原子同步写入和严格文件安全检查。
- source / build / digest 核对后的持久生产者交接记录；目前保留全部文件，删除和双边清理未开放。
- 单 listener / timer 的注册与显式卸载、来源暂停、只读状态和 180 秒心跳过期识别。worker 状态中的 collector 标记改为 `external_status_required`，指向独立采集状态，不把队列为空当作没有漏采。
- 复用固定 Jenkins 镜像中的 Groovy/JDK，无新业务端点、migration、公共 v1 字段或依赖。操作约束见[采集器说明](../../../experiments/jenkins-collector/README.md)。

## 验证

| 项目 | 实际证据 |
| --- | --- |
| 固定镜像兼容性 | 在缓存的 Jenkins 2.568.3 / JDK 21 镜像内，使用 WAR 原有 Groovy、core 与 servlet API 编译核心及 Jenkins adapter；无网络，无 Jenkins 服务 |
| 对账状态 | 合成历史证明回调缺失、逆序完成、低编号运行中 / 缺失 / 不可读、205 个编号分批处理、重开后继续游标，以及上限持续增长时仍回头恢复旧缺口 |
| 文件 / 故障 | write、file sync、rename、directory sync 故障；快照发布后 checkpoint 失败恢复；异字节回调与文件篡改拒绝；source 重绑 / 编号倒退、symlink / hardlink / 权限、容量与损坏拒绝 |
| 生命周期 | 同 JVM 锁竞争、独立 Groovy 进程突然退出后的 OS 锁释放及重开；不等同于 Jenkins controller 重启 |
| 状态 | 持锁期间只读检查不修改 checkpoint；stopped 与过期心跳非零；输出不包含 payload 或凭据 |
| Go 回归 | `go test -race ./internal/jenkins ./cmd/jenkins-worker` 与对应 `go vet` 通过；无业务数据库或 Web 变动，本轮未重跑 PostgreSQL / Web |
| 仓库检查 | `./scripts/check-repo.sh`（448 文件）、`git diff --check`、shell 语法检查通过 |
| Go 交接 | `check.sh --export` 的五份真实 Groovy 序列化合成快照进入 `TestCollectorWorkerContract`；三态 sent / delivered 与确认摘要一致，UNSTABLE / NOT_BUILT 为 blocked |

第一次离线编译只带 `WEB-INF/lib/*`，因缺 servlet API 失败；补上同一 WAR 中的 `executable/winstone.jar` 后通过，没有下载依赖或绕过类型。首次 Go 契约测试被沙箱阻止写编译缓存，经批准复跑通过。

## 尚未证明与下一步

ADR-0035 B **未完成退出验收**。离线测试中的历史是合成数据，API 编译不能证明 Jenkins 初始化时序、真实 Run 生命周期和插件兼容性。真实 controller / worker 分别重启、漏回调后补采、历史删除、构建倒序完成、agent 无法写 spool / 读 HMAC、同 UID 的跨容器只读挂载、宿主断电和物理磁盘耐久性均未在本轮验收。

生产者确认已持久化，但 worker 紧凑确认索引、双边清理协议与显式删除入口仍待实现及故障验证；当前 `cleanup_enabled: false`。不以本轮代码宣称无人值守长期采集、生产可用或完整 Golden Path。

恢复真实联调时先明确原生 Linux 主机 / 目录、单独 Docker project、固定 controller / agent、无真实仓库的有界 probe、worker / receiver 临时凭据与数据、故障注入范围；运行前另行授权，结束移除该 project 容器 / 网络并保留相对 bind mount 的证据目录。旧三探针数据和配置保持独立。所有者恢复授权前不启动这些资源。
