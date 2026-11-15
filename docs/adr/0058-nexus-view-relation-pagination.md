# ADR-0058：核心 Nexus View 关系分页

状态：已接受

日期：2026-11-15

本 ADR 扩展核心协作对象关系读取。现有双向发现、关系方向和受限证据权限规则继续适用。 [ADR-0022](0022-transactional-activity-and-incoming-relations.md#双向关系与权限)

## 背景

Thread、Decision 和 Ticket 的 Nexus View 在一次响应中返回全部可展示的关系。存储先汇集关系，再为每条关系分别查询目标权限与标题。现有关系规模用例用同一时间创建 100 个派生 Decision，校验全量结果以及同时间下按链接 ID 的稳定顺序。它提供对照入口，尚待用新旧合同做查询测量。 [当前缺口](../status/current.md#已确认缺口) · [既有规模用例](../../server/internal/goldenpath/postgres/activity_update_integration_test.go#L231)

现有 Go DTO 和 Web 解析器还用主响应里的关系检查来源基数。Decision 替代确认需要直接读取两端的证据和替代关系。首批需要保留这些可独立核对的事实，并让可增长的关系从正式页面分页读取。 [协作读取合同](0019-session-scoped-thread-decision-ticket-transport.md) · [替代合同](0049-decision-supersession.md)

## 决定

### 首批对象与关系范围

| 当前对象 | 随详情返回的上下文 | 分页读取的关系 |
| --- | --- | --- |
| Thread | 可读的原 Message，或无来源 Channel 的历史 Thread 的既有详情形态 | 入向 `derived-from` Decision |
| Decision | 来源 Thread 或既有受限占位，以及按当前权限可读的直接前任和后任 | 入向 `implements` Ticket |
| Ticket | 创建时的 `implements` Decision，或沿 `execution_context` 读取的原 Message 与既有受限表示 | 出向 `relates-to` Document 与出向 `affects` Component，按同一顺序合页 |

两处读取均沿现有关系方向和 active 状态。当前对象从路径获得，关系的另一端可跳转到原对象。Decision 替代显示和 Ticket 原消息沿各自的字段、历史与来源合同。 [Decision 替代](0049-decision-supersession.md) · [消息来源](0054-message-to-ticket-and-execution-discussion.md)

本批范围为三个协作对象。Document、Repository、Component、CI Run、Deployment 的主详情各沿现行合同。关联管理、构建和部署追溯继续使用各自的正式入口。Activity Timeline 的读取与分页另立合同。 [组件关联](0034-ticket-component-relations.md) · [交付追溯](0057-thread-delivery-trace.md)

### HTTP 与兼容

前缀为 `/api/v1/workspaces/{workspace_id}`，下列路径分别使用 `threads/{thread_id}`、`decisions/{decision_id}` 或 `tickets/{ticket_id}`。

| GET | 返回 |
| --- | --- |
| `/{collection}/{id}/nexus-view` | 沿既有协议返回完整详情。Ticket 继续接受现有 `include` 组合。 |
| `/{collection}/{id}/nexus-view?relations=summary` | 沿同一 DTO 返回 Current、能力、Timeline 及表中随详情返回的上下文。`relations` 仅包含该上下文，来源基数和严格字段校验按原合同执行。Ticket 可同时使用现有 `include`。`relations` 参数只接受单个 `summary` 值。 |
| `/{collection}/{id}/nexus-view/relations` | 返回 `data.items` 和 `data.next_cursor`。关系行只含既有可读关系 DTO 的 `visibility`、`direction`、`relation_type` 和 `target`，其中 `target` 仅含 ref 与安全标题。 |

分页路由只接受 `limit` 与 `after`，默认 25 条，最多 50 条。三个对象各自有固定的关系列表，客户端根据当前对象选择入口。拒绝未知参数、空值、重复参数和不合法的游标。复用同源 Session、Workspace 准入、`private, no-store` 及既有错误响应格式。缺失、不可读或失去访问权的当前对象统一返回 404，未登录返回 401。

未选择 `relations=summary` 的客户端继续得到既有响应形状、排序、受限占位和 Ticket 原消息协商行为。新 Web 与新 Go 同批交付。旧 Go 拒绝新参数时，Web 显示明确的读取合同错误。

### 授权、排序与游标

- 每一页使用一个 PostgreSQL repeatable-read 准入事务。校验 Session、当前 Workspace membership 和当前对象的读取权限，再复用目标对象各自的读取语义。Decision、Ticket 和 Document 沿其 governing Project 读权，Component 沿当前 Workspace 成员读权，归档对象沿现有只读规则。
- 数据库按 Workspace、关系方向、类型和 active 状态选取候选，再按可见性过滤。不可读目标既不占页，也不产生占位、总数、类型、时间或游标。Decision 的无字段受限 evidence 占位和 Ticket 的原消息占位继续随详情返回。分页条目只针对通过读取权限的目标。
- 保留每种列表在现有详情中的相对顺序，按链接的 `(created_at, id)` 升序做排他 keyset。先过滤，再取 `limit + 1`。最后一条已返回的可读关系产生 `next_cursor`，最后一页返回 `null`。单页读取固定快照，跨页读取当前事实。
- 游标绑定版本、Workspace、操作者、当前对象类型及 ID、关系列表种类、上一页末条的可读目标 ref 及关联代次校验值。用绑定同一作用域和内部链接 ID 的摘要标识代次。服务端在当前快照内从可读边界解析排序键。游标及可见条目均省略原始 EntityLink ID、原始排序时间、内部元数据和被过滤边界。
- 作用域错误或格式错误返回 400。当前对象仍可读而游标边界被移除、被替换或失去可读性时统一返回 409，使用同一错误代码和文案。新 Web 从第一页恢复。游标不授予访问权，同一目标解除后重连也不会让旧游标跳到新代次。

### Web 与查询成本

- 正常详情页和 Decision 替代确认改用精简详情。正式协作页保留来源、直接替代关系和原动作区域，并在“来源与后续结果”内渲染当前关系页。分页操作只更新该列表。空列表、读取失败和重试沿页面现有表达，计数标注为当前页条目数。
- 切换 Workspace、对象或游标时使用 AbortSignal 取消相应请求，忽略迟到响应。显式重读、页面重新进入及现有焦点或可见性刷新从第一页核对。404 清理失权详情与关系，401 交给 Session 流程。普通网络或服务错误保留可用的详情和动作，关系区域单独提供重试。
- PostgreSQL 查询在 `LIMIT` 前用集合查询完成目标存在、Project 可见性和标题读取。逐页 SQL 调用次数应独立于页宽、匹配行数和无权目标的数量。将首批类型白名单和权限等价性用于新旧读取的对照，避免两套可见规则漂移。精简详情在查询阶段仅选择固定上下文。
- 使用既有 EntityLink 和业务行。先用代表数据核对已有索引。若入向查询仍随 Workspace 的无关关系量扩大扫描范围，记录 `EXPLAIN (ANALYZE, BUFFERS)`，提出对应索引和部署影响并另行确认迁移范围。

## 替代方案

- 直接把旧 `nexus-view` 的 `relations` 改成第一页。旧客户端会把局部数据解释为完整结果，来源校验和 Decision 替代确认也可能受分页边界影响。
- 先全量读取后由 HTTP 或 Web 截取。网络响应变小，数据库仍承担逐条授权和读取成本。
- 给通用关系 API 开放任意实体、方向和关系类型过滤。首批页面只需要上表的组合，其余关系的权限与展示需要逐一确定后再开放。

## 实施顺序与验收

| 顺序 | 可复验条件 |
| --- | --- |
| 1. 读取合同和 PostgreSQL | 三类对象分页与既有全量结果逐项对照。覆盖时间相同、边界更替、空页、权限混排、归档、跨 Workspace 及撤权。测量单页 SQL 调用及查询计划，分别扩大本条关系、无权结果和同 Workspace 无关关系的数量。 |
| 2. HTTP 和 Web | 验证原无参接口及 Ticket 的现有 include 组合。新参数请求解析、严格字段矩阵、游标作用域及 400、401、404、409 通过合成 Session 回环 HTTPS。正式页面检查切页、键盘、窄屏、迟到响应、失权、重试和 Decision 替代确认。 |
| 3. 历史与恢复 | 以 schema 029 验证已有数据和整库恢复后的顺序、来源与可达权限。用 v9 在独立空目标取回可携带的协作关系并重新读取。运行与环境相关的关联继续按各自备份范围检查。 |

## schema 030 入向索引

schema 029 的实测显示，同一 Workspace 的无关关系会扩大入向查询的扫描范围。所有者确认 migration 030 为 `entity_links` 增加一个 B-tree 部分索引，键为 `(workspace_id, to_type, to_id, created_at, id)`，范围为 active 的 `Decision → Thread` 和 `Ticket → Decision`。关系事实、权限、公开 DTO、Activity 17 和上下文包 v9 沿现行合同。

普通 `CREATE INDEX` 在单一 migration 事务中运行，与 history 030 同时提交。部署安排停写、匹配的 029 工件备份、显式迁移和匹配的 Go / Web 验证。回退通过 029 工件将升级前备份恢复到独立空目标。具体验证范围见技术验收，真人观察、真人实测与真实 Jenkins 联调沿近期执行顺序安排。 [技术验收](../status/reviews/2026-11-15-nexus-view-relation-pagination.md#schema-030-升级与恢复) · [近期执行顺序](../status/current.md#近期执行顺序)
