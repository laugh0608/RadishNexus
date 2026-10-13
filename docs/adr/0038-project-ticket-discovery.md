# ADR-0038：Project 下的 Ticket 只读发现

状态：提议（待项目所有者确认；未实施）

日期：2026-10-13

关联：[ADR-0004](0004-project-scoped-collaboration-permissions.md)、[ADR-0019](0019-session-scoped-thread-decision-ticket-transport.md)、[ADR-0025](0025-project-and-channel-discovery.md)、[Golden Path](../golden-path.md)。本提案增量扩展只读发现，不替代既有权限与 Ticket 创建合同。

## 背景与源码基线

基线为 `490e93b`。Ticket 已有正式创建、canonical 详情、Decision 来源、Component 人工关联和 Document 入口；首页 Project 导航已有 Channel 与 Document 发现，但没有 Project 工单列表。成员重新进入工作仍需沿原讨论及关系逐层查找，或使用次级 ID 工具。

源码依据：

- [ProjectBrowser](../../web/src/workspace/ProjectBrowser.tsx) 承载选中 Project 后的导航；[WorkspaceHome](../../web/src/workspace/WorkspaceHome.tsx) 保留按 ID 打开协作对象的工具。
- [发现 service](../../server/internal/goldenpath/discovery.go)、[发现查询](../../server/internal/goldenpath/postgres/discovery.go)与 [HTTP adapter](../../server/internal/platform/httptransport/discovery.go) 已有稳定 ID 分页、作用域 cursor 和严格公共 DTO。
- [对象权限查询](../../server/internal/goldenpath/postgres/relations.go)按 Ticket 的 `governing_project_id` 判断读取；[公共协作接口](../../server/internal/platform/httptransport/collaboration.go)与 [Web 协作 adapter](../../web/src/collaboration/api.ts)当前只接受 Ticket `open` 状态。

目标是让普通成员从 Project 找回已创建的 Ticket，并继续使用既有上下文；不依赖 Jenkins、外部 provider 或维护者提供对象 ID。真实 Jenkins 的 ADR-0035 / ADR-0037 B 继续暂缓，具体 Ticket ↔ CI Run 关系仍按 ADR-0037 的后续前提推进。

## 提议决定

### 1. 对象归属与权限

列表只包含路径 Workspace、Project 下 `governing_project_id` 匹配的 Ticket；不通过 Component 共属、Repository 或 EntityLink 反推 Project 归属。

每次请求复用现有同源 HTTPS Host、可信代理、Session 与服务端 Principal，并在一次 PostgreSQL repeatable-read 查询事务中复核当前权限：

| 情况 | 结果 |
| --- | --- |
| active Workspace 成员，Project 为 `workspace` 可见 | 可以发现工单，不要求 Project 写角色 |
| active Workspace 成员，Project 为 `restricted` 且有显式 Project membership | 可以发现工单，viewer / contributor / decider / admin 的读取边界一致 |
| restricted Project 无显式 membership | 不可发现，Workspace owner 不例外 |
| 暂停或已移除 Workspace 成员、跨 Workspace、Project 不存在 | 与不可访问 Project 一样返回 `404` |
| Session 失效或账户被禁用导致 Session 不再有效 | 沿用认证入口的 `401`，客户端回到登录 |
| Project 已归档但仍可读 | 可以发现与打开历史 Ticket，页面明确标记项目已归档 |

Ticket 本身没有独立 restricted membership；此处复用既有 Ticket 详情的授权语义，不新建权限继承链。创建者身份、旧角色、历史列表、cursor 或来源引用都不是读取凭证。

可读 Ticket 的来源 Thread 不可读时，Ticket 仍按自身 Project 权限发现；列表不展开来源，详情中的来源与关系继续分别复核目标权限。公开 Project 移除显式角色只撤回对应写能力，不能被描述为撤回其 Workspace 成员的只读基线；撤权验收必须区分角色移除与实际失去读取权。

### 2. 公共读取合同

新增唯一业务端点：

`GET /api/v1/workspaces/{workspace_id}/projects/{project_id}/tickets`

仅接受 `limit` 与 `after`。成功响应示例仅说明形状，不代表已有实现：

```json
{
  "data": {
    "items": [
      {
        "ref": { "type": "ticket", "id": "tkt_example" },
        "title": "为登录接口增加速率限制",
        "status": "open"
      }
    ],
    "next_cursor": null
  }
}
```

- 条目只含 `ref / title / status`，沿用最小 discovery 形状；不返回正文、来源摘要、作者、时间、角色、receipt、关系数量或后台来源标识。
- 首片 `status` 只接受现有正式 Ticket 读取合同中的 `open`。数据库与 ADR-0004 预留的 `in-progress / done / canceled` 不等于对应公共读写能力已实现；状态流转切片须一起扩展列表、详情和事件合同。
- 查询不通过 `status = 'open'` 隐藏其它记录。若待返回数据出现尚不支持的状态、非法 ref 或无效标题，服务端以通用 `500 internal` 拒绝整页，保留内部诊断，不伪造 open、不部分返回；Web 同样严格校验响应。
- 有权限但没有条目返回 `200` 与空数组；不可读 Project 返回 `404`，不能降级为空列表。未知子路径返回 `404`；不支持的方法返回 `405` 与精确 `Allow`，不开放写入口。
- GET 不增加 CSRF 要求；成功与错误均沿用 `Cache-Control: private, no-store`、`Vary: Cookie` 和既有错误 envelope。不引入 CORS、离线缓存或新身份入口。

### 3. 分页与并发可见性

- `limit` 默认 25，取值 1～50；重复、未知、空值、超界及非法 query 返回 `400 invalid`。沿用既有查询长度和解析限制，不增加 `offset`、搜索、筛选或可选排序参数。
- 使用稳定 Ticket ID 升序 keyset：排他边界为 `id > after_id`，先判定当前权限和作用域，再取 `limit + 1` 判断是否有下一页。不是按创建时间或“最近更新”排序，UI 不作相反暗示。
- `after` 使用既有版本化 opaque base64url cursor，绑定 Workspace、Project、Ticket 列表种类和边界 ID。未知版本、字段或类型、非法 `tkt_` 边界、跨作用域复用均拒绝；不与 Component 反向 Ticket 列表互通。
- cursor 不是签名授权或结果快照；客户端不得依赖其编码。结构合法的边界不要求指向仍存在的对象，服务端不额外查询该边界对象的标题或存在性；下一页重新授权，不信任 cursor 中的任何权限暗示。
- 仅在还有结果时返回下一 cursor，边界取本页最后一个已返回条目；不返回隐藏对象占位、总数、隐藏数量或跳过数量。
- 单页使用一致读取快照，跨页没有固定快照保证。并发新增且 ID 小于或等于已翻过边界的对象可能要回到首页刷新才出现；权限变化后结果可减少或整个 Project 不再可读。固定数据下分页不得重复或遗漏，不宣称跨页冻结数据集。

### 4. 页面入口与状态生命周期

新增列表 canonical 页面：

`/workspaces/{workspace_id}/projects/{project_id}/tickets`

用户路径为“首页选择 Workspace → 选择 Project → 查看项目工单 → 打开 Ticket”。工单入口与现有项目文档入口并列，保留 Channel 导航和次级 ID 工具，不重做整个工作台。

- 列表显示已授权读取的 Project 名称与归档状态、工单标题、状态和分页控件；项目上下文复用现有 `GET /api/v1/workspaces/{workspace_id}/projects/{project_id}/configuration`，该读取允许可读 Project 的普通成员，不要求 admin。只使用名称与状态，不因此显示管理入口或信任旧首页对象，不增加第二套详情接口。
- Ticket 链接继续进入 `/workspaces/{workspace_id}/tickets/{ticket_id}`。详情提供依据当前对象 governing Project 的“查看项目工单”链接，回到列表首页；不接受任意外部 return URL。
- 列表页支持直接地址、刷新和登录后恢复原路径；服务端静态页面白名单、App 路由、当前 Workspace 上下文同步接入。路径可加载 Web Shell 不表示已获数据读取权。
- 页码与 cursor 栈只保存在当前页面内存，不新增 URL query 或本地持久缓存。页面刷新、重新进入、焦点恢复和从浏览器后退恢复列表时重新授权并读取首页；前进、后退不直接恢复旧业务数据快照。
- 切换 Workspace / Project、翻页、刷新、页面重新可见、Session 失效或请求错误时清理旧条目；作用域变化同时清理旧 Project 标题与分页状态。Project 上下文与列表按同一加载代次管理，任一读取返回不可读、失效或错误时均清理两者，不保留旧 Project 名称配合错误列表。请求可取消，迟到响应不能覆盖新作用域或重新显示已清空的数据。
- 明确 loading、empty、error / retry、不可读和失效 Session 状态；empty 文案为“当前项目没有可显示的工单”，不推断其它 Project 或隐藏对象。上一页 / 下一页按有效边界启用，刷新回到首页。
- 没有权限实时推送；只承诺下一次请求与上述重读触发点重新判断权限，不能宣称撤权后所有已打开页面即时消失。点击工单后详情必须再独立授权。
- 使用语义链接、可见焦点与清晰状态文本，支持键盘、窄屏及长标题换行，不依赖颜色表达状态。

### 5. 首片范围与实现约束

首片包括 Go 只读 service / PostgreSQL 查询 / HTTP adapter、正式 React 列表与入口、必要测试及文档同步。复用既有 discovery、错误处理与分页惯例，避免新造通用对象中心或平行授权 helper。

不包括独立创建 Ticket、状态修改、负责人、优先级、看板、全文搜索、Decision 列表、通知、通用关系分页、交付关联、外部调用或 Jenkins B。现有 Ticket 创建仍从 Accepted Decision 进入；只读列表不产生领域事件、Activity、Audit 写入或 command receipt。

不新增业务表、业务字段或依赖，不回填或修改存量 Ticket。实施时用代表性多 Project 数据检查查询计划、SQL 次数与耗时；如现有索引不足，只通过独立 forward-only migration 增加 `(workspace_id, governing_project_id, id)` 查询索引，并记录实际依据，不修改已发布 migration。不为本切片扩大 schema 兼容窗口。

## 替代方案与取舍

| 方案 | 判断 |
| --- | --- |
| 继续只通过原 Thread / Decision 或 ID 找回工单 | 已有路径保留，但无法提供 Project 执行对象的独立入口 |
| 同时建立 Ticket / Decision / Thread 通用列表、搜索与看板 | 扩大对象合同和状态管理，首片只解决工单发现 |
| 按更新时间排序并加入状态筛选 | 会增加可变排序边界与筛选合同；当前仅 open 的正式流程没有相应收益 |
| 依赖前端角色过滤或从关联对象继承权限 | 不能保护 API、分页和直接地址，违反当前权限语义 |
| 列表先支持四状态，详情以后再补 | 会产生列表可见但无法打开的对象，本提案保持现有公共合同一致 |

代价是增加一个 GET 合同和列表页面，并继续明确非 open 状态尚未开放。收益是成员可以独立找回执行对象，不要求提前扩充工作流或恢复真实 Jenkins 联调。

## 兼容、交付与回退

这是增量读取能力，现有创建、详情、关系、事件与 receipt 合同不变。Go 与 Web 成套交付；旧 Web 可继续使用旧入口，新 Web 需要含新端点的服务端。新端点缺失或异常必须显示错误，不回退到预置数据或枚举 ID。

默认无需业务数据迁移、Activity 重建或备份格式调整。若只增加查询索引，须验证从当前 migration 015 升级、readiness 版本匹配及新实例恢复后查询；索引 migration 一旦应用，旧二进制会按既有精确 schema readiness 拒绝服务，回退遵循已记录的恢复 / forward repair 方案，不删除 migration history 伪造兼容。

接受并实施后同步 server / Web 说明、必要公共合同、当前状态和带证据边界的实施记录。本提议阶段只新增本 ADR、索引条目并调整当前顺序，不提前改写已接受的领域状态或实现说明。

## 验收范围与退出判据

以下均为未来实施要求，本轮文档不构成执行证据：

1. **真实 PostgreSQL 权限**：覆盖 workspace / restricted Project、普通无写角色成员、viewer / contributor / decider / admin、无 membership 的 owner、暂停 / 移除成员、跨 Workspace、归档及不存在 Project；列表与逐对象详情的可读性一致。区分公开 Project 角色移除后仍可读，与 restricted membership / Workspace 访问撤回后不可读。
2. **分页与最小数据**：覆盖 0、25、26 和多页，limit 1 / 50、边界不存在、并发插入、页间撤权与恢复读取；固定集合完整无重复。确认响应、cursor 和错误不含来源摘要、隐藏数量及权限内部信息。使用既有正式命令创建业务测试对象；专门的损坏数据测试验证非 open 等漂移拒绝，不当作支持新状态的证据。
3. **HTTP 合同**：匿名 / 失效 Session、Host / proxy、合法与非法 ID、严格 query / cursor、跨列表 / Project / Workspace 重用、非法版本与重复字段、GET 之外的方法、缓存头、错误收敛与 DTO 拒绝部分结果。
4. **Web 自动化**：从 Project 入口到列表与详情、详情返回列表、直接地址和登录恢复、分页与刷新、空态与错误重试、归档标记、Scope 切换、焦点 / 可见性恢复、浏览器历史恢复触发重读、迟到响应、Session 失效及撤权清空。私密来源不在列表或错误中展开。
5. **隔离 HTTPS 浏览器**：从正式基础配置和成员入口建立场景，通过 Message → Thread → Proposed Decision → 人工 Accepted → Ticket 创建业务对象；普通成员退出再登录，无需输入 ID 即可从 Project 找回工单，进入既有来源 / Component / Document 路径。补验分页、刷新和后退撤权，以及桌面、手机、键盘与长标题；不得用手写 SQL 或预置最终工单替代主流程，分页夹具可调用同一正式业务命令。
6. **回归与工程检查**：执行定向 Go、真实 PostgreSQL、Web 检查和 `./scripts/check-repo.sh`，回归既有 Project / Channel / Document 发现及 Ticket 详情。记录代表性查询计划、数据规模、SQL 次数和耗时，不从单次测试推断生产容量。如增加索引 migration，额外完成 015 升级与备份恢复验证。

退出结论限定为“成员通过正式 Project 入口独立找回已有 Ticket，当前权限、分页和页面恢复通过验证”。不表示工单状态流转、完整 Golden Path、真实团队持续使用或 Jenkins SCM 采集完成。实施验收资源与清理仍按当次授权准备。

## 待所有者确认

建议确认此只读切片的权限不变量、稳定 ID 分页与 open 状态边界、Project 工单入口及验收范围后，再实施 Go / Web。后续顺位为 Ticket 最小状态流转、Decision 只读发现、试用所需身份与运维能力；各项单独冻结范围，不因本提案获得实施授权。真实 Jenkins B 继续暂缓。
