# 受控 Jenkins controller 采集器

状态：2026-10-09 代码与离线验证完成；所有者已明确暂缓真实联调。[ADR-0035 B](../../docs/adr/0035-durable-jenkins-terminal-delivery.md) 的真实 Jenkins、挂载与重启退出条件尚未满足；后续已实现显式双边清理并按离线范围验证。实现独立于旧 [三探针实验](../jenkins-lab/README.md)，不改动旧实验脚本或数据。

## 能力与边界

`JenkinsCollector.groovy` 提供单 job 的 `RunListener.onFinalized` 快速路径，在 Jenkins `COMPLETED` 后开始对账，此后每 60 秒最多检查 100 个编号。`Collector.groovy` 负责不可变文件、manifest、操作系统锁、checkpoint 与确认摘要核对；不访问网络、Nexus 数据库或 Secret。核心不创建用户、job、agent，不排队构建、不修改安全策略。

回调与对账共用同一锁和发布逻辑。输入先写临时文件、同步文件、原子移动并同步父目录，再保存 checkpoint。已存在输入只接受完全一致的字节；同一构建的新回调若给出不同内容，暂停来源并保留原始文件。原始 result 保持 `SUCCESS`、`FAILURE`、`ABORTED`、`UNSTABLE` 或 `NOT_BUILT`，后两者由 worker 明确阻塞，不伪装成三态。

对账维护连续发布前缀、最大已观察编号、下次扫描编号及每项 `published` / `running` / `missing` / `unreadable` 状态。循环扫描历史区间，较大编号完成不会跳过低编号缺口；每轮扫描捕获固定上限，跨批次持久保留；新构建留到下一轮，避免持续增长的编号让低位缺口长期得不到复查。已发布文件仍存在且摘要一致时，不要求 Jenkins 永久保留 Run；若已发布文件丢失，只能从字节相符的历史恢复，否则暂停并报告。checkpoint 丢失或损坏不能通过重新初始化接管已有目录。

`ack` 仅在 source、build number 与原始 payload digest 均匹配时形成持久 `handoff_at`。只有精确、显式的双边清理才允许删除输入和压缩 worker payload；正常采集不自动删除，`cleanup_enabled: false` 与 `cleanup_mode: explicit_build_only` 明确这一点。不要按年龄或成功数量手动删文件以绕过确认或容量限制。

## 运行前提

- 固定验证镜像为 `jenkins/jenkins:2.568.3-jdk21@sha256:c1e4c349365f6d16d88595b2c5f7e8ff39b8ae1d061f62420bac193b4b9616d0`，复用其中的 Groovy 2.4.21 与 JDK 21，无新增插件、下载、Maven 工程或依赖。
- 首轮只接受 Linux 本地 `ext4`、`xfs`、`btrfs`、`overlay`、`tmpfs` 文件系统标识。tmpfs 仅用于易失测试；容器 overlay 的寿命也必须独立管理，不能承诺容器删除后数据仍在。白名单不等于物理断电认证。
- 2026-10-09 本机 Docker 的宿主 bind mount 实测为 `fuse`，明确拒绝。即使目录在 Linux 容器中，也不能当作 Linux 原生持久磁盘。真实联调等待另行指定原生 Linux 文件系统环境，不放宽检查或改用未授权的 volume。
- controller / worker 需要同一数值 UID；各目录 0700、普通文件 0600，拒绝符号链接、硬链接及所有者不符。父路径必须由可信部署者控制。
- 部署副本必须来自受控管理目录，不能从待构建仓库加载。Jenkins 管理员对 controller 有完全控制权，本脚本不能隔离恶意管理员。
- agent 不挂载任何 spool、配置或 HMAC；worker 不挂载 Jenkins HOME 或构建工作区；controller 不挂载 worker 状态或 HMAC。跨容器权限与只读挂载尚待真实验收。

## 配置与目录

配置必须为绝对路径、私有普通文件，JSON 拒绝重复 / 多余 / 缺失字段。示例中的 ID 和 origin 是占位部署绑定，不会连接该域名：

```json
{
  "version": 1,
  "input_dir": "/spool/input",
  "state_dir": "/spool/collector",
  "ack_dir": "/spool/ack",
  "binding": {
    "version": 1,
    "receiver_id": "nexus_instance",
    "jenkins_id": "jenkins_instance",
    "origin": "https://nexus.example.test",
    "source_id": "ci_source",
    "workspace_id": "wrk_example",
    "component_id": "cmp_example",
    "job_full_name": "example/build",
    "first_build_number": 1
  }
}
```

| 目录 | controller | worker | 内容 |
| --- | --- | --- | --- |
| `/spool/input` | 读写 | 只读 | `build-N.json` 不可变终态快照 |
| `/spool/collector` | 读写 | 普通 run 不挂载；离线 cleanup 只读 | `.lock`、`manifest.json`、`checkpoint.json` |
| `/spool/ack` | 只读 | 读写 | worker 发布的确认 |
| worker 私有状态目录 | 不挂载 | 读写 | worker manifest、重试预算、不可变 payload 副本 |

三个 collector 路径必须互不包含，配置在其外部。controller 和 worker 各自 manifest 的 `binding` 字段必须一致；worker 自身的配置仍按 [worker 说明](../../server/jenkins-worker.md) 提供，其 `state_dir` 不能指向 collector 状态目录。两个进程不共享同一锁。

首次初始化前由部署者创建空目录并正确设置所有权。collector `init` 只接受空 input / ack 和仅含锁的 collector 状态目录，不创建或接管业务历史。随后初始化 worker 的私有状态。不同 job、实例、Component、来源或采集起点不得复用旧目录；job 改名、复制、删除重建、编号复用仍属于人工部署约束，manifest 不能证明 Jenkins job 的不可变身份。观察到 `nextBuildNumber` 倒退会暂停，但不能发现所有人为伪造的重建。

## 装配与生命周期

以下为待授权部署步骤，本次未执行：

1. 将 `StrictJson.groovy`、`Collector.groovy`、`JenkinsCollector.groovy` 和 `offline.groovy` 放到只读 `/opt/radishnexus/collector/`；配置放到 `/opt/radishnexus/collector-config.json`。
2. 在固定镜像提供的 Groovy/JDK classpath 中执行 `offline.groovy init /opt/radishnexus/collector-config.json`，创建并同步 manifest 与 checkpoint。无需启动 Jenkins 或供应 HMAC。编译 / classpath 的可复验命令见 `check.sh`；WAR 的 `executable/winstone.jar` 提供 servlet API，不能只使用 `WEB-INF/lib/*`。
3. 仅将 `start.groovy` 作为 Jenkins `init.groovy.d` 入口；它通过受控目录加载实现，注册一个 listener 和一个 daemon timer。初始化尚未完成时不扫描 job，最多在完成后的下一个 60 秒 tick 开始补采。
4. worker 按自己的配置独立运行。Jenkins 回调只做有界本地操作，不等待 HTTPS sender。
5. 显式停用 / 重载时，由可信管理员执行 `stop.groovy`，等待同步方法结束、取消 timer、保存 stopped 状态、释放锁并移除 listener。随后才能重新执行 `start.groovy`。重复注册或身份未知会被拒绝，不覆盖旧实例。

JVM 退出由操作系统释放文件锁；再次启动沿已有 checkpoint 补采，不自动清空错误、历史或输入。发生写盘故障、输入冲突、确认冲突或来源漂移后，当前实例保持 paused，修复后必须显式停用并重启；不能通过修改 checkpoint 重置范围。原有 receiver 撤销与密钥轮换仍按 worker 说明操作；停止 collector 不撤销机器写权限。

## 状态与容量

`offline.groovy status /absolute/collector-config.json` 是只读命令，可以在 collector 持锁时执行，不读取 HMAC 或打印 payload / digest / job / 路径。它核对 manifest 和 checkpoint，输出 source ID、最后观察时间、最后完整扫描时间、连续前缀、扫描位置及逐项状态。`stopped`、`paused`、心跳超过 180 秒或时钟明显倒退时非零退出；活跃进程存在不等于历史缺口已经补齐。

正在运行的 listener `status()` 提供数量汇总，包括未扫描、运行中、缺失、不可读、已发布和已确认数量。checkpoint 中的状态表示最近观察，不是 Jenkins 的实时查询。每轮最多 100 个编号，大积压的完整复查可能跨多个周期。worker 仅输出 `collector: external_status_required`，其成功发送计数不能代替上述采集状态。

本版保留所有身份索引，采集区间最大跨度 10,000 个编号，首次编号也在其中；缺失编号和 blocked 结果占用范围，不允许用较大游标跳过缺口。每个输入最多 16 KiB，checkpoint 最多 4 MiB，collector 状态文件总计最多 8 MiB，目录枚举有数量和字节上限。collector 元数据独立于 A 的 input / worker state / ack 合计 512 MiB 上限，worker 继续执行该共享交接容量限制。残留临时文件也计容量，超限停止，不自动丢弃。

## 精确预览与清理

先停止双方并取得四个目录的一致备份。下面是 `offline.groovy` 的参数，仍需固定镜像原有 Groovy/JDK classpath；本轮只在测试 tmpfs 中执行，未清理真实实验或业务数据。

```text
offline.groovy cleanup-plan /opt/radishnexus/collector-config.json 42
offline.groovy cleanup /opt/radishnexus/collector-config.json 42 --confirmed
```

预览不持锁、不写文件，输出 eligible 和受控原因。没有持久 handoff、未满 7 天、ack 缺失的输入不合格；确认或输入摘要不同则报错。执行必须指定一个构建并带 `--confirmed`，没有全量、范围或自动模式。命令取得 collector 锁并重新核对，先将 checkpoint 升为 v2，在该条写入 `retired`、原 digest、handoff_at 和 retired_at，再删除输入并同步父目录。其余 published / running / missing / unreadable 条目保持原状。

retired 与 published 都计入连续采集前缀。对账不会重建 retired 输入；重复 finalized 回调仍会核对原始摘要，不接受同编号的新内容。删除失败后保留 retired 意图，修复存储后可重复同一命令；不能为完成删除而改写摘要。输入恢复或删除后目录同步失败时也沿原记录重试，不重新确认交付事实。

随后按[worker 清理步骤](../../server/jenkins-worker.md#显式双边安全清理)核对 collector 的只读状态并压缩 payload。collector 本身不访问或删除 worker 文件，不持有 Nexus HMAC。首次清理以前 checkpoint 仍为 v1，新程序读取 v1 / v2；旧程序拒绝 v2，回退必须恢复一致旧备份，不能仅替换二进制。两侧时钟必须正确，未来时间或保留期不足不会通过清理校验。

## 离线验证

```sh
experiments/jenkins-collector/check.sh
```

只使用本地已缓存镜像，`--network none`、只读根文件系统和源码、受限 tmpfs；不启动 Jenkins HTTP、agent 或 probe，不自动拉取镜像。镜像内真实 API 编译通过与真实 controller 运行是两类证据。

需要核对 Groovy 到 Go 的实际字节时，先创建一个空的临时导出目录，再执行：

```sh
experiments/jenkins-collector/check.sh --export /absolute/empty/test-directory
cd server
RADISHNEXUS_COLLECTOR_CONTRACT=/absolute/empty/test-directory \
  go test ./internal/jenkins -run TestCollectorWorkerContract -count=1
```

导出包含五个合成结果，不含凭据；导出目录仅传递测试数据，不是 durable spool，可以经过宿主 FUSE。导出还包含 collector manifest 和实际 v2 retired checkpoint。Go 用例确认三态导入 / 送达 / digest ack、另外两态 blocked，以及 Groovy 清理证明驱动的 Go payload 压缩；没有访问真实 receiver。未设置环境变量时该用例跳过，不伪造跨语言证据。

已验证与未验证清单见[实施记录](../../docs/status/reviews/2026-10-09-jenkins-collector.md)；显式清理的后续证据见[清理记录](../../docs/status/reviews/2026-10-09-jenkins-cleanup.md)。只有原生 Linux 持久目录上的真实 Jenkins 分别重启、回调丢失补采和挂载隔离完成验收后，才能将 B 标记完成。
