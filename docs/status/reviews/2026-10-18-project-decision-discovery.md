# Project 决策只读发现实施与验证

日期：2026-10-18

## 范围与结论

[ADR-0040](../../adr/0040-project-decision-discovery.md) 已确认并实施。成员可从首页 Project 入口找回已有 proposed / accepted Decision，打开原详情并返回列表；当前权限、分页、来源隔离与页面恢复已验证。此结论不代表 Decision 完整生命周期、待我决策、真实团队持续使用、完整 Golden Path 或生产容量通过。

起点为 `dev` 的 `fab5561`，提案和确认范围先提交为 `19290dc`。按所有者要求，由主会话主持接口冻结和独立审查，三个 `gpt-6.1-sol / high` 子代理分别承担后端、Web 与真实数据库测试。主会话检查实际 diff，独立运行门禁、查询测量与浏览器验收；连续出现的数据库异常夹具问题由主会话接管修正。实现验收结束时依要求保持未提交；所有者随后授权提交本轮实现、测试和文档，本次收尾纳入提交，未 push、PR、发布或部署。

## 实际改动

- `DiscoveryService`、PostgreSQL Store 与 HTTP discovery handler 增加 Project decisions GET。授权与查询处于同一 repeatable-read 事务，沿用 governing Project 当前权限；没有逐条读取 evidence。
- DTO 只有 `ref / title / status`，title 是 question 原文；两个状态严格校验，非法投影整页失败。默认 25、最大 50，v1 cursor 绑定 Workspace / Project / decision kind，稳定 ID 排他边界。
- `ProjectDecisionsPage` 和严格 API adapter、App canonical 路由、Project 入口、Decision 详情返回链接已接通。Project 元数据和列表使用同一请求代次；失败、撤权、作用域切换与页面离开清理旧数据，迟到响应不能恢复内容。
- 没有增加 migration、索引、依赖、写入动作、权限规则、筛选、排序或来源展开；既有 acceptance / Ticket 创建继续使用原合同。
- server / Web README、ADR 索引及当前状态已同步。ADR 验收矩阵保留要求，实际结果集中于本记录。

## 自动化验证

| 实际命令 | 结果与覆盖 |
| --- | --- |
| `./scripts/check-server.sh` | 通过；Go race 测试、vet、模块一致性及既有 HTTP / 授权 / acceptance 回归 |
| `GOFLAGS=-v ./scripts/check-server-postgres.sh` | 最终完整运行通过；正式命令数据、权限、分页、异常投影、业务只读不变量及查询测量 |
| `./scripts/check-web.sh` | 通过；33 个测试文件、216 个测试，TypeScript / 构建及脚本中的其他检查通过 |
| `./scripts/check-repo.sh` | 通过；根入口一致性、正式文档链接与文本卫生 |
| `git diff --check` | 通过；无空白错误 |

HTTP / 服务测试覆盖错误 Host / proxy、Session、方法和精确路由、未知或重复 query、cursor 作用域 / kind / 版本 / 字段 / 编码篡改、最小 DTO、无部分成功、缓存头及错误 envelope。生产装配路由和已有列表回归同时运行。

真实 PostgreSQL 测试经正式基础配置、邀请、成员授权和业务命令建数据，覆盖 active Workspace 无 Project 角色、restricted 四角色、owner 不穿透、跨 Workspace / Project、账户禁用、Session 过期 / 撤销、成员暂停 / 移除、归档、私密 Thread / Channel evidence、角色移除与读取撤回的区别。分页覆盖空集合、25 / 26 / 27、limit 1 / 50、不存在的合法边界、插入 / 接受造成的页间变化与重新授权。

异常数据专项将已正式建立的记录变为 rejected / superseded 或非法问题，确认未到达的页不被全局预扫描、到达异常页返回通用 500；这些仅是数据库异常夹具，不代表开放拒绝或替代命令。反复 GET / 翻页前后比较 Decision、EntityLink、domain event、Outbox、Activity、identity / configuration Audit 与 receipt 的完整序列化行集，确认没有业务写入。

Web 可控异步测试覆盖元数据 / 列表任一失败、同时失败、迟到结果、请求取消、作用域切换、401 / 404、empty / loading / retry、刷新 / focus / visibility / popstate、pagehide 清理与 persisted pageshow 重读首页。直接地址、重新登录、详情返回及安全纯文本展示有独立断言。

本地原始日志位于 `/private/tmp/radishnexus-decision-server-check.log`、`/private/tmp/radishnexus-decision-web-check.log` 和 `/private/tmp/radishnexus-decision-postgres-check-r3.log`；这些是本地临时证据，不作为仓库长期资产。

## 查询测量与索引决定

独立构造 3 个 Project、1227 条 Decision，其中目标项目 27 条、其他项目各 600 条；均由正式命令产生，混合 proposed / accepted，问题文本相同以验证不按标题合并。运行 `ANALYZE` 后记录正式列表 SQL 的 `EXPLAIN (ANALYZE, BUFFERS)` 与服务调用计数。

| 探针 | 返回条数 | SQL 调用数 | 服务耗时 | EXPLAIN 执行耗时 |
| --- | ---: | ---: | ---: | ---: |
| 首部，limit 1 | 1 | 6 | 2.297 ms | 0.010 ms |
| 首部，limit 25 | 25 | 6 | 1.106 ms | 0.135 ms |
| 尾页，limit 25 | 2 | 6 | 1.049 ms | 0.107 ms |
| 首部，limit 50 | 27 | 6 | 1.118 ms | 0.106 ms |
| 空尾页，limit 50 | 0 | 6 | 1.030 ms | 0.098 ms |

limit 1 使用主键索引；其余探针在该规模选择顺序扫描并排序。SQL 调用数不随页大小增长，没有 evidence N+1。本片没有证据支持为此规模立即新增 migration，保留现有索引；这不是生产容量结论，也不承诺更大数据集继续使用同一计划。未触发索引分支，因此未单独执行 016 升级或备份恢复门禁。

## 隔离 HTTPS 浏览器

使用现有固定 PostgreSQL / Caddy 镜像、正式 Go handler 和当前 Web 构建、独立 Chrome 会话；没有 mock API、手写 SQL 预置最终 Decision、真实 Jenkins 或业务数据库。第一轮从 foundation bootstrap 的空业务 Workspace 出发，以正式 UI 创建 Team、restricted Project、Channel、Message、restricted Thread、Proposed Decision 并明确人工接受；通过正式 API 重复创建分页样本、邀请并授权普通 viewer。普通成员正式登录后，已确认 Decision 可读而私密来源不可见。

第一轮到达夹具内部 30 分钟运行时限，在剩余列表检查前自动停止，不能记为一次完整通过。重新启动同范围的一次性环境，使用正式配置、消息、Thread、Decision、人工确认 acceptance、Ticket、邀请与成员 API 重建 27 条合成记录后完成检查；没有修改产品或放宽环境门禁。

第二轮实际通过：

1. 普通成员在正式登录页面登录，从首页选中项目；聚焦“查看项目决策”后按 Enter 进入列表，未使用 ID 工具寻找对象。
2. 首页 25 条、尾页 2 条、上一页返回；待确认 / 已确认文字正确。列表不包含来源 Thread 标题、Message 正文、accepted outcome 或 rationale 测试标记。
3. 长中文问题及字面 `<script>` 作为纯文本展示；1440×1000 桌面与 390×844 手机截图已人工检查，手机无横向溢出，分页按钮可达。
4. 从列表进入已确认 Decision，来源只展示“受限 evidence”占位；可打开既有后续 Ticket，再返回详情和项目列表首页。
5. 列表退出后重新登录保留原 canonical 路径并读取首页。受限项目撤回 viewer 后刷新，项目标题和旧条目均消失；浏览器后退不恢复旧详情。正式重新授权后重新读取成功。
6. 切换到正式创建的 workspace 可见 Project；撤回 decider 角色后刷新 / 后退仍能读列表和详情，新的 acceptance 返回 403。与 restricted 项目撤回成员导致 404 分别验证。
7. 为归档专项，仅用 SQL 将正式创建的测试 Project 标记为 archived；浏览器列表显示归档标记，历史详情可读；拥有 admin 且可读 evidence 的主体新接受返回 409。当前没有公共归档命令，此夹具操作不代表该命令已交付。

本地截图：`output/playwright/decision-desktop.png`、`decision-mobile.png`、`decision-detail.png`、`decision-revoked.png`、`decision-archived.png`；文件位于被忽略的验收输出目录。Browser history 已实测，未声称浏览器一定命中原生 BFCache；persisted 分支由 Web 事件测试覆盖。Workspace 切换由 Web 测试覆盖，真实浏览器本轮验证 Project 切换，未新建第二个 Workspace。

## 失败修正与清理

- 首次 PostgreSQL 运行试图修改不可变 Session 过期时间，被数据库约束拒绝。修正为复用已正式签发 Session 和 PostgreSQL Store、注入过期后的认证时钟，并验证原时钟请求仍成功。
- 第二次运行发现 superseded 异常夹具缺少数据库要求的 active supersedes 关系。主会话补充显式异常关系，恢复记录后标记关系 removed；保留数据库约束和两种状态专项，最终全量 PostgreSQL 通过。
- 浏览器数据准备曾误读身份响应 envelope，以及给 Project key 使用大写，均由既有合同明确拒绝；修正验收脚本后重新通过正式入口准备，没有改产品合同。
- 第一轮夹具超时导致连接拒绝；第二轮正常创建 stop 文件，fixture 返回 PASS。关闭独立浏览器，确认两轮容器和临时 state / CA 文件已移除。首轮残留两只 Caddy 卷经进程标识和创建时间核对后精确删除；其他历史卷未处理。
- 浏览器仅在独立测试 context 中忽略本次自签名 HTTPS 校验，没有安装系统证书、修改钥匙串或全局配置。该验收不证明生产 TLS 信任链。

未新增依赖、未应用业务实例 migration、未运行真实 Jenkins、未重跑生产 Compose / 备份恢复、未开展真实团队持续使用或原生中文 IME 验收。下一顺位见[当前状态](../current.md)，本次不自动进入身份 / 运维新切片。
