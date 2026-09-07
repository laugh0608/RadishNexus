# 项目结构性审阅（2026-09-05）

状态：审阅记录；发现与建议不代表已修复或已经授权实施

审阅代码基线：`dev`，提交 `d60f1ab`。本地工作树干净，与当时本地记录的 `origin/dev` 一致；未 fetch 或重新核验远端设置。项目所有者随后要求据此完善文档，形成当前文档调整；本文保留审阅当时的证据，后续完成线以[当前状态](../current.md)为准。

## 范围与判断

检查产品定义、领域模型、Golden Path、路线图、架构、许可策略、治理、Go / Web 核心实现、migration、Compose 与 CI。属于结构性审阅和定向源码检查，不是逐行安全审计，也不是外部市场或法律评估。

判断：稳定引用、权限不穿透、人工 Decision、命令幂等、业务 / 事件原子性和备份恢复已经形成较强基础；产品可发现性、正常运行时上下文更新、真实外部交付链和团队持续使用证据相对不足。近期应把已有能力接成完整用户场景，再按使用反馈扩展功能。

## 源码发现与完成判据

| 发现 | 证据 | 影响与后续完成判据 |
| --- | --- | --- |
| Activity 缺少正常更新路径 | [activity.go](../../../server/internal/goldenpath/postgres/activity.go) 只有显式全量重建；[nexus_view.go](../../../server/internal/goldenpath/postgres/nexus_view.go) 从 `activity_items` 读；[集成测试](../../../server/internal/goldenpath/postgres/store_integration_test.go) 先重建再断言 Timeline | 普通写入后可能读到空或陈旧 Timeline。选择更新机制与时效后，以正式命令 → 正式读取验证，验收中不手工重建；重试、失败和重建并发另测 |
| 关系只查询出向 | [store.go](../../../server/internal/goldenpath/postgres/store.go) 写 Decision → Thread、Ticket → Decision；[relations.go](../../../server/internal/goldenpath/postgres/relations.go) 只按 `from_type / from_id` 查询 | 原对象不能通过此查询发现后续结果。基于同一关系提供有方向的双向发现，刷新仍成立；复用当前权限，不复制镜像权威关系 |
| readiness 只检查连接 | [server main](../../../server/cmd/radishnexus/main.go) 的 `/health/ready` 仅 `Ping` | 未 migration 或 schema 不兼容时也可能报告 ready。增加只读兼容性判断，覆盖缺失、漂移、不兼容和匹配，不隐式迁移 |
| 对象入口要求已知 ID | [App.tsx](../../../web/src/App.tsx) 首页明确未开放对象列表 | 成员依赖维护者告知 ID。最小列表和必要成员管理形成正式入口，按当前权限发现对象，不依赖直接改库 |
| 集成链仍停在部分内部边界 | [server README](../../../server/README.md)、[server main](../../../server/cmd/radishnexus/main.go) | Jenkins 来源验证、CI Run 公共读取、Deployment 写入和上下文关联需贯通；内部 verified delivery 不能替代真实 Webhook 接入 |

前三项源码缺口未在本轮真实数据库环境重现；判断依据为写入、读取、装配与测试调用路径，不能把历史验收或本次单元测试通过解释为修复完成。

## 后续工程建议

这些建议随对应业务切片处理，不形成额外大重构或安装依赖的前置门禁：

- **业务职责**：当前正式能力集中于 `internal/goldenpath`；后续按 messaging、collaboration、delivery 的实际职责逐步明确模块边界，保留单体内事务优势。
- **Web 维护**：`App.tsx` 混合路由、认证与 prototype；协作页面同时管理多类对象和写入。拆分时按状态生命周期处理，避免丢失撤权清空、模糊失败重试与焦点语义。
- **合同重复**：Channel、collaboration、Deployment adapter 重复路径 ID、时间和 JSON 基础校验。先提取语义相同的部分，以共享合同样例核对 Go DTO 与 Web 校验，不削弱业务字段白名单。
- **查询规模**：Relations / Timeline 当前无分页，逐项授权和标题读取增加查询次数。先测代表性数据与结果上限，再优化，不先引入权限缓存或图数据库。
- **可用动作**：后续可审查由服务端给出安全的可执行动作提示，减少成员提交后才发现无权限；提示不是授权，提交仍重查。
- **数据库权限**：[Compose](../../../deploy/compose.yaml) 的 app 与运维入口共用数据库身份；生产化时评估分离运行、迁移和备份权限。
- **容量**：[realtime hub](../../../server/internal/platform/realtime/hub.go) 默认全进程 256、每用户 4、每 Channel 64 连接。多标签页消耗连接；目标用户人数不等于验证容量，资源上限也不是席位授权。
- **数据与审计**：账号恢复、成员撤销、安全 Audit 查询、消息 / 事件 / receipt 保留及受控脱敏需独立合同；不直接放宽不可变约束或把业务事件当作完整审计。
- **预研与产品**：最小 Markdown Document 先完成 revision、保存冲突、权限和恢复，Yjs 不作为前置阻塞。真实试用关注追溯、重复背景和人工干预；许可申请与退出说明应在外部试用前齐备。

长期验收已归入 [Golden Path](../../golden-path.md)、[工程标准](../../development/engineering-standards.md)与[路线图](../../roadmap.md)；精确业务或公共协议仍由对应实施任务审查。

## 本次审阅实际验证

以下命令在文档修改之前、上述代码基线上执行：

| 验证 | 结果 |
| --- | --- |
| `./scripts/check-repo.sh` | 通过，检查 222 个文件 |
| `python3 -m unittest discover -s scripts/tests -p 'test_*.py'` | 8 项通过 |
| `server/` 下 `go test -race ./...`，使用隔离临时 GOCACHE | 首次因沙箱禁止绑定回环测试端口失败；获准在沙箱外重跑后通过 |
| `server/` 下 `go vet ./...` | 通过 |
| `./scripts/check-web.sh` | 格式、Lint、70 项测试、TypeScript、production build 和 152 个锁定 package 的依赖基线检查通过 |
| `experiments/document-editor/` 下 `npm run check` | 28 项测试、production build 和 115 个锁定 package 的依赖基线检查通过 |

Web 构建 JavaScript 为 275.23 kB / 80.44 kB gzip。编辑器实验为 758.85 kB / 238.81 kB gzip，包含两套候选和 corpus，保留 Vite 大 chunk 警告；不能当作单候选或正式 Web 成本。

未重跑真实 PostgreSQL 集成、双实例备份恢复、Compose、浏览器人工验收或 npm 在线漏洞审计。此前相关结果见[历史状态快照](../history/2026-09-03-status.md)；本次依赖基线检查通过不等于刷新了漏洞数据库。

## 文档调整与追踪

项目所有者要求按审阅建议完善文档后，将近期顺序归入当前状态，完成判据归入 Golden Path / 路线图，维护要求归入工程标准，非 CRDT 接受路径归入仍为“提议”的 ADR-0021。历史状态完整归档，不改写已接受 ADR、许可证、业务代码、数据模型、依赖或远端状态。

后续处理上述发现时，在实施证据中引用本文对应项，并更新当前完成线；不把此历史审阅记录改写成持续任务清单。
