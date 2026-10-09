# ADR-0035：Jenkins 终态快照的持久交付与漏采恢复

状态：A 已接受并实现；B 采集代码及离线验证已完成，真实联调经所有者要求暂缓，清理与退出验收未完成

日期：2026-10-09

关联：[ADR-0030](0030-authenticated-jenkins-delivery-adapter.md)、[Golden Path](../golden-path.md)。A 部分替代 ADR-0030 中仅一次性发送的范围限制，提供独立持久发送 worker；B 的可信采集脚本已实现，但真实持续运行尚待验收。现有 v1 delivery、来源身份、权限、receipt 与 CI Run / Deployment 分离合同保持不变。

## 背景与源码判断

基线为 `c81d162`。Repository / Component 与 Ticket / Component 人工关联已经完成，仍不能证明某项工作进入某次构建。今天先解决可靠收集与送达终态，不同时扩展业务来源关系。

现有实现提供了可复用的边界，也留下明确缺口：

- `server/internal/jenkins/sender.go` 每次最多 4 次请求、单次 10 秒、总计 60 秒；进程退出后没有持久待办。接收端 receipt 已能处理提交成功但响应丢失，不需要另一套业务去重表。
- sender 已有严格成功响应解析，但 `responseError` 会把未知永久响应与暂时失败都归入 `ErrDelivery`。持久 worker 不能凭错误字符串决定是否无限补送，应先保留内部类型化的 HTTP 状态、白名单机器码和重试分类；不把响应正文暴露给操作者。
- `experiments/jenkins-lab/bootstrap.groovy` 只为一次性三个 probe 注册 finalized listener；旧目录存在即拒绝启动。快照使用临时文件与原子移动，但没有文件 / 目录同步、重启对账、队列状态或容量策略。不能直接作为长期部署脚本。
- 当前来源配置是部署者维护的文件，source ID 跨重启不重绑依靠变更管理。本提案不把本地 manifest 描述成核心数据库中的持久 Integration 注册表。

## 推荐范围与分段交付

推荐固定单 Jenkins job、单 source、单 worker 的本地磁盘交接。沿用现有 Go 模块与标准库，不新增 PostgreSQL 表、业务 migration、HTTP 端点、payload 字段、消息中间件或第三方依赖。首轮支持受控 Linux 本地文件系统；NFS、对象存储、多副本、高可用和任意 Jenkins 插件组合不在承诺范围。

| 切片 | 具体交付 | 可以作出的结论 |
| --- | --- | --- |
| A：持久发送 | Go worker、单实例锁、不可变快照导入、持久状态、分类重试、状态读取与显式恢复；保留现有一次性 CLI | 已落盘终态在约定故障范围内可重启恢复送达；不宣称 Jenkins 漏采恢复完成 |
| B：可信采集 | 独立于 probe 引导的 controller 受控采集脚本、finalized 快速路径、启动与周期对账、缺口报告及真实重启联调 | 在声明的 job、历史保留与磁盘条件下持续收集终态；不是生产插件或普通管理员自助管理 |
| 后续独立切片 | Repository / commit / CI Run 来源、Ticket 具体交付关系、完整浏览器交付链 | 必须另行冻结证据、权限和关系语义，不由 A / B 自动推导 |

A 已获所有者确认并实施，操作入口见[worker 说明](../../server/jenkins-worker.md)。B 的可信采集代码已按后续推进要求实现，入口见[采集器说明](../../experiments/jenkins-collector/README.md)。真实 Jenkins 运行、部署目录、凭据和资源需要执行前具体授权；所有者现已明确暂缓真实联调。现有实验三探针保留为历史验证入口，不将其自动变成常驻服务。

## 运行与信任边界

controller 是快照生产者；独立 Go worker 是签名发送者。worker 不加载构建脚本、不读取核心业务表、不控制构建结果。controller / agent 不持有 Nexus HMAC；agent 与仓库代码不可写快照、worker 状态或配置。采集脚本具有 controller 管理员级能力，只由可信部署者配置，不能从被构建仓库动态加载。

每个 source 使用独立 spool，明确区分：controller 写的只读输入目录、worker 私有状态目录、worker 写而 controller 只读的确认目录。目录权限与挂载在启动前检查；不挂载 Docker socket、整个 Jenkins HOME 或宿主源码给 worker。worker 不修改或删除 controller 输入，controller 只根据匹配确认记录执行后续保留策略。

输入、状态和确认目录只允许预期所有者写入。A 严格要求与 worker 相同 UID、目录 0700、文件 0600，并拒绝硬链接；跨容器只读 / 读写挂载和 UID 装配需在 B 实测，当前不支持放宽组权限。拒绝符号链接、非普通文件、越界路径和超限内容，文件名从经验证的 build number 生成，不拼接任意 job 名或外部 URL。

worker 启动时持有操作系统释放的独占锁，第二实例拒绝启动；不靠 PID 文件超时猜测旧进程死亡。锁和文件同步的实现必须在受支持平台验证，不支持的文件系统启动失败，不静默降低持久性。

## 来源绑定与不可变输入

本地 spool manifest 版本为 1，冻结 receiver 实例的本地标识、HTTPS origin、source ID、Jenkins 实例本地标识、job full name、Workspace / Component 绑定及首个采集 build number。实例标识由部署者明确分配，不把 URL 当稳定身份。manifest 是运维约束，不证明远端绑定或 Jenkins 内容真实性；操作者仍须核对 ADR-0030 接收配置。

已有 spool 与配置不符时停止，不把积压转发到新实例或新 Component。实例、job、Workspace / Component 改变需新 source 与新 spool；job 改名、复制、重建或 build number 重用不能继续旧 source。仅允许明确密钥轮换沿用 spool；HTTPS origin 迁移本轮采用停止并人工迁移程序，不自动跟随重定向。

每条身份固定为 `(source_id, build_number)`，delivery ID 仍为 `build-{number}`。本地记录保存受限终态 payload、原始字节摘要和版本化状态；不会持久化 HMAC、签名、Cookie 或完整 Jenkins Run。相同身份和相同字节可重复导入；不同字节立即报告冲突并停止来源发送，保留原状态和输入，即使仅空白不同也不自动覆盖。接收端仍按既有 canonical digest 判断业务等价，两个摘要职责分开。

采集端只提取 ADR-0030 白名单字段，保留真实 result；不通过改写 `UNSTABLE` / `NOT_BUILT` 让输入通过。worker 对这些结果持久记录受控的不支持状态，不发送伪造三态，也不阻塞其他构建。非法输入保留受限故障记录，不在日志复制原始内容。

## 持久交接、重启与未知结果

1. controller 在同一文件系统的临时文件写入完整快照，完成文件同步，再原子发布并同步父目录；已存在同身份文件须比对，不覆盖。监听回调只做有界本地工作，不等待 Nexus 网络响应。
2. worker 对输入校验后先持久导入自己状态中的不可变 payload，再安排发送。调用一次 sender 前先持久扣除一轮预算并记录 `in_flight`；一轮包含最多 4 次网络请求，崩溃也计作已消耗一轮，写失败则不发送。无需用另一套 HTTP 循环复制现有签名与发送逻辑。
3. 每次状态更新使用同目录临时文件、文件同步、原子替换、目录同步。损坏、未知版本、同步失败或磁盘不可写均报告并停止受影响来源，不当作空队列。
4. 启动时 `in_flight` 视为结果未知，以同一 delivery / payload 恢复；不能标成成功，也不能生成新 ID。每次发送使用当前可用 key 和新时间戳重新签名。
5. 只有收到并严格解析既有 200 / 201 成功响应后，才持久写入 `delivered` 与 CI Run ID；随后发布仅含 source / build、payload digest 和确认版本的本地确认。若成功后本地写失败，保留未知结果；重启重放由 receiver receipt 返回原 CI Run。
6. worker 状态与确认之间崩溃时，启动重新生成缺失确认。任何一侧重复处理不会改变原 payload；本地确认只是交接凭据，不能成为新的业务事实来源。

首轮区分 `pending`、`in_flight`、`retry_wait`、`delivered`、`blocked`。`blocked` 必须有受控原因，不把“重试耗尽”“认证失败”“冲突”“本地损坏”混为成功或统一继续重试。未知成功响应形状按结果未知进入有界重试，耗尽后保留为 blocked。

## 重试预算、故障可见性与恢复

每轮复用现有 sender 的 4 次 / 60 秒预算；自动轮次最多 12 次，从首次尝试起最多 24 小时。轮间退避建议从 1 分钟开始倍增、上限 1 小时，并加有界抖动；轮次、首次时间和下次时间均持久化，重启不重置预算。达到任一上限转 blocked。只运行一个网络请求，轮转处理到期待办，单条失败不阻塞后续可处理事实。

| 情形 | 建议行为 |
| --- | --- |
| 网络中断、408 / 429 / 500 / 502 / 503 / 504 | 既有轮内重试后进入持久退避；遵守既有 Retry-After 合同 |
| 200 / 201 响应丢失、截断或形状无效 | 结果未知，同身份有界重放，不确认交接 |
| 401、来源绑定错误、TLS 信任错误 | 暂停该来源网络发送并报告，修复配置后显式恢复；不暴露 key 或原始响应 |
| 409、非法输入、不支持 result、目标不可用或其他永久 4xx / 重定向 | 对应条目 blocked，保留原因；冲突不靠新 delivery ID 绕过 |
| 本地磁盘满、同步失败、manifest 漂移、状态损坏 | 停止受影响来源的导入 / 发送，保留输入，状态命令非零退出 |

需为 sender 增加内部类型化错误分类，保留旧 CLI 安全输出和成功合同；未知 4xx 不得因落入 `ErrDelivery` 获得自动重试。TLS 分类依赖可判定错误类型，不比较底层字符串。轮内仍有界等待，取消时保留已发请求的未知结果。

状态命令给出各状态数量、最老待办时间、最近成功时间、剩余容量；支持按配置中的 source 和精确 build 查询受控原因。运行日志只记录受控 ID、状态、次数、时间、容量与机器码，不打印 payload、摘要、路径、job、Header、签名或底层错误。worker 输出 `collector: external_status_required`，不声称从队列推导采集心跳或覆盖完整性。B 已增加独立 checkpoint 与只读状态，报告心跳、扫描位置和覆盖缺口；stopped、paused 或心跳超过 180 秒时非零退出，避免把“没有构建”和“采集器死亡”混为一谈。

管理员修复后可对精确 source / build 显式重试，保持 payload 与 delivery ID；追加本地操作记录，重新建立有界预算。冲突和损坏不允许自动重试，需先查明来源；不能编辑状态文件清零当作恢复步骤。本地运行记录不宣称防篡改安全 Audit。

## controller 漏采恢复与完整性边界

`onFinalized` 提供快速生成路径，但回调异常不会终止 Jenkins 构建。因此只靠监听器和发送队列仍可能漏采。推荐独立 controller 脚本在启动完成后及每 60 秒执行一次有界对账，复用同一纯快照构造与发布逻辑。

对账从 manifest 显式 `first_build_number` 开始，按编号检查至本轮捕获的 `nextBuildNumber - 1`，每批至多 100 个编号；每轮扫描的上限跨批次固定，按持久游标循环复查已观察区间，避免持续增长的构建使旧缺口一直无法复查。持久保存连续已处理前缀与未完成 / 缺失编号；较大编号先结束不能使较小运行中编号被跳过。回调与对账按同身份串行化发布，不用两个独立游标竞争覆盖。

运行中 Run 保留待查；已 finalized 且历史存在的 Run 可重新生成缺失快照。只有快照已可靠发布，或已有匹配交接确认，才能推进对应采集状态。无法读取、已删除、编号缺口或历史截断记录为未解决缺口；不得自行解释为从未执行或自动跳过。大跨度积压分批恢复并暴露进度，不在启动时无限遍历历史。

脚本重载必须明确卸载旧监听器与定时任务，确保只有一个实例；身份和版本不明时拒绝重复注册。启动不创建用户 / job、不调度 probe、不改安全策略；与现有一次性 bootstrap 隔离。首轮以既有固定 controller 镜像验证，不能用当前在线 Javadoc 代替该镜像的兼容性证据。

保障条件是：采集范围内的 Jenkins 历史在可靠发布前仍可读取，spool 未丢失，受支持文件系统能提供验证过的同步语义。若历史与 spool 同时丢失，报告证据缺失，不能合成构建或承诺无损。构建删除 / 保留策略必须与该边界匹配；本切片不自动修改 Jenkins 全局保留策略。

## 容量、停用与生命周期

首轮每个 source 最多 10,000 个未清理身份与临时文件、输入 / 状态 / 确认目录合计最多 512 MiB；同一 build 的三个文件只计一个身份，残留临时文件各占一个名额。单 payload 最多 16 KiB，单状态记录最多 32 KiB。计数包含 blocked 和待清理成功项；目录分批读取、每轮最多导入 100 个新快照，状态扫描受总容量上限约束。容量超限停止该来源并报告，写前还保留原子写入空间；不丢弃最老待办。B 的 callback 不能因此改变 Jenkins 构建结果，后续由历史对账恢复缺口。当前 collector 另有最多 4 MiB checkpoint、8 MiB 状态目录预算；该元数据目录不计入 A 的三个交接目录。未开放清理前，collector 从 first build 到已观察最大编号的区间跨度也限制为 10,000，历史缺口占范围，不自动跳过。

默认不自动删除未交付、blocked 或未解决缺口。A 的 `cleanup-plan` 只报告交付超过 7 天的候选数量与 `cleanup_enabled: false`，保留全部输入与状态；B 当前已持久化生产者确认，但双边清理与 worker 紧凑索引尚未实现，仍禁止删除。后续清理设计为：controller 读取匹配 worker 确认后，先持久保存同身份与摘要的交接记录，才允许清理超过 7 天的对应输入；worker 清理前必须核对该交接记录，并先保留紧凑 source / build / digest / CI Run 确认索引，再删除大 payload。任一侧缺少确认则不清理，不能仅因成功时间已过期删除。确认索引在 source 退役前保留，也计入容量；触顶需人工归档 / 停用，不承诺无限运行。

停用先停止采集与 worker，保留 spool，再按 ADR-0030 撤销 receiver 来源 / key 并重启；停止 worker 本身不撤销外部写权限。已提交 CI Run / receipt 保留，已经在途请求可能已成功，重启必须按未知结果核对。重新启用同一身份可继续原积压，不自动重置采集起点。

轮换沿用 receiver 新旧 key 重叠 → 停止 worker → 切换文件引用 → 重启核验 → 移除旧 key；payload 与 delivery 不变。spool 备份不包含 Secret，按停止采集与 worker 后的一致快照备份。恢复默认离线，核对目标实例与 manifest，重新供应凭据后显式启用；旧备份重放由原 receiver receipt 幂等处理。Nexus 业务备份与 collector spool 不是同一恢复事务，须分别记录恢复点及可能缺口。

## 具体交付来源与整链后续

本提案不增加 commit 字段，也不建立 Ticket 已交付关系。下一提案必须分别回答：

1. Repository 外部稳定身份如何与实际 checkout 匹配；完整 commit ID 来自何处，如何处理多仓库、merge commit、重建、构建期间变更与无法确认的来源。
2. 哪些元数据由可信 controller / 受控构建配置提供，哪些只是用户或仓库声明；签名不使未经验证的声明变成证据。
3. Ticket 与具体 CI Run 的人工关联或受控证据引用表达什么，谁能写、谁能读、如何解除、如何保留历史；Component 共属不生成此关系。
4. Deployment 继续引用明确 CI Run，只有持有当前 Environment 显式授权的成员确认外部 staging 终态；不能因 CI Run 成功自动记录部署。

真实整链验收使用正式入口新建 Team / Project / Component / Environment 与授权，实际 Jenkins 构建经采集进入 CI Run，成员在浏览器读取并记录外部已完成的 staging 结果；撤销记录权后新命令被拒绝，历史按当前读取权限可见。若当次没有实际外部 staging 行为，只验收到 CI Run，不预置或声称 Deployment 成功。记录 staging 的外部动作、目标与清理另行明确授权。

Repository 来源与 Ticket 具体交付关系完成后，再验收原 Thread / Ticket 的双向发现和正常 Timeline、私密目标过滤及备份恢复。仅 A / B 完成不满足完整 Golden Path 退出条件。

## 替代方案与影响

| 方案 | 取舍 |
| --- | --- |
| 反复手动运行现有 sender | 不新增运行模型，但没有持久预算、漏采发现和无人值守恢复，不满足本次目标 |
| 给现有 sender 套无限循环 | 不能区分永久失败、状态损坏或源配置改变；容易静默丢失或无限请求，不采用 |
| 将队列放入 Nexus 核心 PostgreSQL | receiver 不可用时仍需外部缓冲，扩大业务 schema 和插件数据库访问，不解决源端漏采 |
| Go worker 直接轮询 Jenkins Remote API | 新增 Jenkins 网络凭据和 API / job 差异；需另证 finalized 与来源边界，当前先复用已验证 controller 路径 |
| 新 Jenkins 插件或独立消息中间件 | 可能改善正式生命周期，但引入构建、分发、依赖和升级成本；受控单 job 阶段暂不采用 |

新增的是独立进程、本地持久格式、运维配置与受控 controller 脚本的维护责任。没有业务 schema 或公共 v1 协议变化，不扩大用户 / 插件权限。回退时停用新 worker / 采集脚本并保留 spool；可对核验过的单条原始快照使用旧 sender 恢复，不让旧程序读取新状态格式。核心已记录事实不可随工具回退删除。

## 验证与退出条件

| 层级 | 必须验证 |
| --- | --- |
| A：文件与状态 | 首次 / 重复 / 冲突导入、symlink / 非普通文件、超限、未知版本、锁竞争、临时写 / sync / rename 各故障点、成功但本地未记账后重启 |
| A：协议与失败 | 既有严格 v1、类型化失败、未知 4xx、坏成功响应、TLS、重定向、轮内与轮间预算、重启不清预算、手工恢复、配额与公平处理 |
| A：真实数据库 | HTTPS delivery 首次 / 重复 / 提交后响应丢失，CI Run / receipt / event / Activity 唯一，不自动创建 Deployment；真实进程终止与重启恢复 |
| B：真实 Jenkins | 三态、不可支持状态、回调缺失后的对账、完成顺序倒置、controller / worker 分别重启、历史缺口、磁盘故障、重复注册拒绝、source 重绑拒绝、agent 不能写 spool 或读取 HMAC |
| 运维 | 停用、密钥轮换、只读状态、精确重试、dry-run / 显式清理、离线恢复与再次启用，日志 / 截图无敏感材料 |
| 回归 | Go 定向与相关 PostgreSQL 测试、现有 sender CLI 合同、仓库检查；没有 Web 修改不机械重跑全部 Web |

A 完成须证明已落盘输入可恢复；B 完成还须证明真实漏采可在历史保留条件下恢复。两段都必须清楚报告未恢复缺口，不用测试数量或一次成功发送替代完整性结论。A 已完成 Go race / vet、真实 PostgreSQL 提交后杀进程与重放、Linux CLI / SIGTERM、故障注入和离线副本检查，详见[实施记录](../status/reviews/2026-10-09-delivery-continuation.md)。后续 B 采集代码、固定镜像 API 编译、合成历史故障矩阵和 Groovy → Go 字节交接已通过，见[采集记录](../status/reviews/2026-10-09-jenkins-collector.md)。本机 Docker bind mount 为 FUSE，超出支持范围；所有者已选择先交付代码、暂缓真实联调。B 的真实 Jenkins / 挂载 / 重启与双边清理退出条件未满足，没有实测宿主断电或物理磁盘丢失。

## 外部核验

2026-10-09 核对 Jenkins 官方 [RunListener.onFinalized](https://javadoc.jenkins.io/hudson/model/listeners/RunListener.html#onFinalized(R))：回调发生于 COMPLETED 且构建记录已落盘后，异常会被吞掉以保护构建。由此推导本提案需要独立失败可见性与对账恢复，不能依靠回调异常阻止漏采。在线文档对应当前版本；既有实验固定版本仍需单独实测。
