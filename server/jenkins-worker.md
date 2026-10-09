# Jenkins 已落盘快照的持久发送

按 [ADR-0035 A](../docs/adr/0035-durable-jenkins-terminal-delivery.md) 实现。`jenkins-worker` 为单 source、单 job、单实例的独立前台进程，复用[现有 sender 与 receiver](jenkins.md)。它只发送可信生产者已经可靠发布的终态快照，不连接 Jenkins、不恢复遗漏回调、不执行部署。[B collector](../experiments/jenkins-collector/README.md) 已有对账代码和离线证据；真实重启 / 挂载联调经所有者要求暂缓，B 退出条件未完成。

## 部署条件

- `run` 只支持 Linux；macOS 仅用于文件与状态测试、离线检查，不作为运行平台。
- 三个目录必须为绝对规范路径、互不嵌套，无 symlink，归 worker 当前 UID 所有且权限为 0700；目录内文件为 0600、普通文件且只有一个硬链接。父目录也必须由可信部署者控制，不允许运行中替换目录。
- Linux 文件系统白名单为 ext4、XFS、Btrfs、tmpfs、overlay；NFS 和其他类型启动失败。tmpfs / 容器临时层仅可用于进程恢复测试，实际持久部署必须使用可保留的本地磁盘并核验挂载与备份。文件系统类型检查不证明存储硬件的断电耐久性。
- 生产者只发布输入；worker 私有状态目录不能暴露给构建 agent 或仓库代码。未来 controller / worker 分容器时分别装配输入只读、确认只读等最小挂载；A 尚未验证该跨容器拓扑，也不支持放宽组权限。
- 发送配置及 Secret 在三个 spool 目录外；Secret 不进入 spool 备份。沿用 HTTPS 证书验证，无跳过校验选项。私有 CA 由部署者装配信任；不挂载 Docker socket、整个 Jenkins HOME 或宿主源码。

## 配置与输入

以下是结构示例，路径、实例标识、source 与对象 ID 必须由部署者核对并替换，不代表已经建立接收端授权：

```json
{
  "version": 1,
  "sender_config_file": "/run/config/jenkins-sender.json",
  "input_dir": "/var/lib/nexus-delivery/input",
  "state_dir": "/var/lib/nexus-delivery/state",
  "ack_dir": "/var/lib/nexus-delivery/acks",
  "binding": {
    "version": 1,
    "receiver_id": "nexus_example",
    "jenkins_id": "jenkins_example",
    "origin": "https://nexus.example.com",
    "source_id": "jenkins_test_auth",
    "workspace_id": "wrk_example",
    "component_id": "cmp_example",
    "job_full_name": "example/build",
    "first_build_number": 1
  }
}
```

字段全部必需，未知或重复字段拒绝。sender 配置沿用 [Jenkins 接入说明](jenkins.md#发送端配置与输入)，其 origin / source 必须与 binding 相同。`receiver_id` / `jenkins_id` 为部署者分配的稳定本地标识；manifest 不查询或证明远端 Workspace / Component 绑定，部署者仍须核对 receiver 来源文件。首次编号只限制接受范围，不证明此前或之后的编号已完整采集。

输入命名固定为 `build-42.json`，build number 必须与 payload 相同，且不小于首次编号。payload 使用已有 v1 格式，不含 Secret。可信生产者必须先在同目录写临时文件、同步文件、原子发布，再同步父目录；不得覆盖已发布身份，重复发布先比对字节。worker 忽略 `.pending-*` 和合法 `build-N.json.tmp` 临时内容，但将其计入配额。

状态目录中的 `manifest.json` 冻结 binding；`build-N.json` 保存原始 payload 字节、摘要、状态、预算和成功 CI Run ID。确认目录中的同名文件仅含确认版本、source / build 和原始 payload 摘要。不要直接编辑这些文件。

## 命令与退出

从 `server/` 使用现有 Go 工具链构建 `go build -o /受控输出目录/jenkins-worker ./cmd/jenkins-worker`。以下命令展示已安装二进制的用法；`run` 会向配置中的真实 origin 发请求，需先完成该目标的部署与发送授权。当前仓库不自动安装服务或启动 worker。

```text
jenkins-worker init -config /run/config/jenkins-worker.json
jenkins-worker status -config /run/config/jenkins-worker.json
jenkins-worker status -config /run/config/jenkins-worker.json -build 42
jenkins-worker run -config /run/config/jenkins-worker.json
jenkins-worker retry -config /run/config/jenkins-worker.json -build 42 -confirmed
jenkins-worker cleanup-plan -config /run/config/jenkins-worker.json
```

| 命令 | 行为 |
| --- | --- |
| `init` | 离线初始化；input 及父目录须预先创建，自动创建 state / acks。非空且无 manifest 的状态不能被接管；不会加载凭据或发送 |
| `run` | 持有 state 内 OS 独占锁，逐条发送；空闲时每 5 秒重新发现输入，最多每轮导入 100 个新快照。第二实例拒绝运行 |
| `status` | 不持锁、不读 Secret、不修改状态；显示已导入条目的状态数量、最老待办时间、最近成功时间、容量及可选精确 build 的原因 / HTTP 状态。运行中的读取不是跨文件事务快照 |
| `retry` | 先停止 worker，修复原因并核对来源，再对精确 blocked build 执行；重新检查 sender 配置和凭据，保留身份和字节，只重建预算并追加本地时间记录，最多 128 次 |
| `cleanup-plan` | 与只读状态同源，报告交付满 7 天的候选数量，始终 `cleanup_enabled: false`；A 没有删除命令 |

stdout 为受控 JSON，stderr 仅安全机器码；错误非零退出。暂停来源时 status 仍输出可用状态，再以非零退出。普通单条 blocked 不使整个 source 暂停，操作者必须检查计数，不能把进程仍运行理解为所有条目已送达。worker 报告 `collector: external_status_required`，必须独立检查 collector 的心跳、编号覆盖与缺口，不能从发送队列推导采集完整性。

SIGTERM / SIGINT 取消当前等待或请求并释放锁。中断请求视为结果未知，已预扣的轮次不会还原；进程停止不等于 receiver 撤销授权。日志不含 payload、job、路径、签名、摘要或底层响应；本地重试时间记录不是防篡改安全 Audit。

## 失败、预算与恢复

发送前先持久写入 `in_flight` 并扣一轮；每轮沿用 sender 的最多 4 请求 / 60 秒。最多 12 轮、首次尝试起 24 小时；持久轮间退避从 1 分钟倍增并加稳定抖动，上限 1 小时。重启不重置预算；一条暂时失败不占住后续到期条目。

| 情形 | 行为与处置 |
| --- | --- |
| 网络中断、408 / 429 / 500 / 502 / 503 / 504 | 轮内有限重试后 `retry_wait`；预算耗尽为 `retry_exhausted`，保留待人工处理 |
| 成功响应丢失或不符合严格合同 | 同一 delivery 与 payload 有界重放，receiver receipt 返回原 CI Run；不推测成功 |
| 401、已确认来源不匹配、证书信任错误 | blocked 并暂停 source，停止网络发送；认证 / 信任问题修复后可精确 retry，binding 冲突不能清零绕过 |
| 非法 payload、不支持 result、其他永久 HTTP 失败 | 对应条目 blocked，其他条目继续；不把 `UNSTABLE` 改成成功或失败。身份字段未完整解码的格式错误不误判为来源改变 |
| 同身份输入字节改变 | `spool_input_conflict`，停止发送并保留原状态；status 也报错，不允许覆盖原 payload 或换 delivery ID 绕过 |
| manifest 改变、状态损坏、文件不安全、写 / sync 失败、容量超限 | 非零退出并保留输入，不能当作空队列；恢复一致副本或修复存储后重新检查，禁止手改状态伪造成功 |

`invalid_request`、未终态、不支持 result、binding 或内容冲突不能通过 `retry` 清除；需要查明生产者问题，纠正源头并制定独立恢复方案。不要把同一构建改成另一个 build number 重送。

只有严格成功响应之后才保存 `delivered`，随后写 ack。成功但本地状态未保存时重放；delivered 已保存但 ack 缺失时仅补 ack，不再发送。所有状态和 ack 更新采用临时文件 → 文件同步 → rename → 目录同步；任一步失败即停止。

容量上限为 10,000 个不同 build 身份加残留临时文件、三个目录总计 512 MiB；单 payload 16 KiB、单记录 32 KiB。blocked、成功项和临时文件均不自动清理。容量接近上限应先停采集、离线备份并规划 B 的双向确认清理；A 不承诺无限运行，不允许按文件年龄直接删队列。

## 停用、轮换与离线副本

停用顺序为停止可信生产者和 worker、保留 spool，再按接收端说明撤销来源或 key 并重启 receiver。历史 CI Run / receipt 保留。轮换只改变 key 引用：receiver 新旧 key 重叠 → 停 worker → 切换 sender 文件 → 重启核验 → 移除旧 key；binding 和 payload 不变。

离线备份三个目录的同一时点副本，包括 manifest、状态、输入和确认，保持权限，不使用硬链接克隆；记录对应 Nexus 数据库恢复点，Secret 另行供应。恢复到受控新路径后只调整配置路径，先执行 `status`；恢复默认不联网，只有目标实例、source 和接收端 receipt 已核对并获得重新启用授权后才 `run`。旧备份的未知请求由原 receiver receipt 去重；没有覆盖的 Jenkins 历史不能从 spool 合成。

job / Jenkins / 接收实例 / Component / Workspace 变更或 build number 重用必须另建 source 与 spool；不能修改 manifest 冒充迁移。origin 迁移需要独立人工程序，当前不自动跟随重定向。验证范围与未覆盖事项见[本轮记录](../docs/status/reviews/2026-10-09-delivery-continuation.md)。
