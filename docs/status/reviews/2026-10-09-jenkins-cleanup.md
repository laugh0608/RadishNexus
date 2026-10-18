# 2026-10-09 Jenkins 双边显式安全清理

## 范围

所有者确认继续推进上一轮提出的双边安全清理。起始为干净 `dev`、HEAD `8a0634b`；本地相对 `origin/dev` 跟踪引用领先两笔，未 fetch 或 push。此前明确的“先交付代码，暂缓真实联调”继续生效。

本轮只实现与验证精确清理入口、紧凑确认索引和恢复边界，不执行真实业务或旧实验数据清理，不启动 Jenkins / agent。使用既有标准库、固定镜像及测试目录，无依赖安装、业务 migration、公共 HTTP v1 或来源权限变化。

## 交付

| 层次 | 最终行为 |
| --- | --- |
| collector 预览 | `cleanup-plan CONFIG BUILD` 只读核对 handoff、当前 ack、原始摘要和 7×24 小时保留期，返回 eligible / 受控原因 |
| collector 执行 | `cleanup CONFIG BUILD --confirmed` 持本侧锁，先同步 v2 retired 记录，再删除该输入并同步父目录；保留身份，普通对账不再生成该输入 |
| worker 预览 | `cleanup-plan -config CONFIG -collector-state DIR -build BUILD` 核对相同 binding、retired 摘要、时间顺序、保留期、collector stopped / 无错误、input 已移除与当前 ack；不读 Secret |
| worker 执行 | `cleanup ... -confirmed` 在 Linux 上持本侧锁重做核对，将完整 v1 记录原子替换为 v2 compacted 索引；保留 source / build / digest / CI Run / delivered / compacted 时间，不发请求 |
| 恢复 | 清理中断可对同一 build 重复执行；旧输入同字节不重发、异字节报冲突，ack 可从索引重建；retired / compacted 身份不会从配额移除 |

运行与查询均不自动清理。`cleanup_enabled: false` 保持自动行为关闭，`cleanup_mode: explicit_build_only` 表明只有显式精确执行入口。年龄候选汇总不是已核对的清理清单。压缩后的重试详情不再保留，精确 status 以 `history_compacted: true` 表达，不能把未知历史显示成零。

操作、挂载、备份和回退步骤以 [worker 说明](../../../server/jenkins-worker.md#显式双边安全清理)和 [collector 说明](../../../experiments/jenkins-collector/README.md#精确预览与清理)为准。第一次显式清理产生本地 v2；新代码继续读取 v1，旧代码拒绝 v2。索引不能还原 payload，回退需恢复一份双方停止时取得的一致旧备份，并核对原 receiver receipt；不支持仅降级二进制或混用恢复点。

## 验证证据

| 验证 | 结果与范围 |
| --- | --- |
| collector 固定镜像 | `experiments/jenkins-collector/check.sh` 通过；复用缓存的 Jenkins 2.568.3 / JDK 21、无网络、只读源码与临时 tmpfs，不启动 Jenkins 服务 |
| 保留与冲突 | 7 天边界前 / 边界时、无 ack、未交付 / blocked、输入或证明摘要不匹配、来源重绑、损坏 / 重复 JSON 字段、未来时间、collector running / paused 均按合同阻止清理 |
| 故障注入 | collector 的 write / file sync / rename / directory sync / unlink / unlink directory sync；worker 原子替换的四个阶段；恢复保留原输入或已持久身份，不假报完成 |
| 真实进程中断 | Groovy 子进程在 unlink 后、目录同步前退出 24；Go 子进程在紧凑索引 rename 后、目录同步前退出 25。两侧锁由 OS 释放，重开后可重复完成，未重新发送 |
| 离线备份 | 停止后的 collector 副本保留 retired、缺失 Jenkins 历史也不再生成；worker 三目录与 collector 证明副本恢复后仍识别 compacted，旧输入再出现也不重发 |
| 跨语言字节契约 | `check.sh --export` 的五份合成结果、manifest 与实际 v2 retired checkpoint，经 `RADISHNEXUS_COLLECTOR_CONTRACT` 导入 Go；三态交付、两态 blocked、相同摘要证明驱动压缩且保留原 CI Run |
| Go 回归 | `./scripts/check-server.sh` 全量 race / vet / 模块一致性通过；后续定向 race / vet 覆盖最终清理与状态输出调整 |
| Linux CLI | 缓存 `golang:1.26.7-alpine3.23`、无网络、源码及模块缓存只读，测试临时目录位于容器内；缺确认拒绝、预览不变、精确压缩、重复执行与 receipt 保留通过 |
| 仓库检查 | `./scripts/check-repo.sh`、`git diff --check` 与相关 shell 语法检查通过 |

首次 Go 测试受沙箱构建缓存与临时监听限制，经批准复跑。新增证明目录最初未显式设置 0700，严格安全校验使测试失败；修正测试准备后通过，没有放宽产品校验。所有删除只涉及本轮创建的合成测试文件。

## 剩余边界与接续

- 没有执行本轮真实 Jenkins、跨容器同 UID / 只读挂载或原生持久磁盘重启联调。当前宿主 bind mount 的 FUSE 限制不变，真实联调按所有者要求暂缓。
- tmpfs / 容器层上的进程故障测试不能证明宿主断电、物理磁盘或生产容量耐久性。双方时钟、历史保留和一致备份仍是运行前提。
- 清理只回收大 payload，永久身份索引仍占 10,000 数量上限；source 归档 / 退役属于后续独立操作，不能删索引来制造无限运行能力。
- 本轮未重跑真实 PostgreSQL 和 Web：未改变核心业务或 Web 路径，网络交付幂等的既有 A 证据保持独立；本轮断言清理本身不调用发送者。
- ADR-0035 B 仍未满足真实运行退出条件。下一段可独立推进 Repository / commit / CI Run 来源证据提案，先冻结可信来源与多仓库 / 重建边界，再讨论 Ticket 具体交付关系。

本次改动尚未提交；没有 push、部署、系统配置变更或真实数据删除。
