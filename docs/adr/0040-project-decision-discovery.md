# ADR-0040：Project 下的 Decision 只读发现

状态：已接受（2026-10-18 所有者确认实施；实际完成线见当前状态）

日期：2026-10-18

关联：[ADR-0004](0004-project-scoped-collaboration-permissions.md)、[ADR-0019](0019-session-scoped-thread-decision-ticket-transport.md)、[ADR-0025](0025-project-and-channel-discovery.md)、[ADR-0038](0038-project-ticket-discovery.md)、[Golden Path](../golden-path.md)。本提案增量扩展只读发现，不改变 Decision 状态写入、来源或权限语义。

## 背景与源码核对

核对基线为 `fab5561`。Project 工单发现与 Ticket 最小状态流转已经完成；Decision 已有从 Thread 提案、人工接受、canonical 详情及来源 / 后续 Ticket 双向发现，但没有 Project 下的独立列表。普通成员重新登录后仍需沿原讨论或 ID 工具找回结论。

本轮只做源码与文档核对，以下不是新接口已实现或行为已重新测试的声明：

| 核对点 | 实际依据 | 对本提案的约束 |
| --- | --- | --- |
| Decision 归属与读取 | [entityAccess](../../server/internal/goldenpath/postgres/relations.go) 的 Decision 分支读取 `governing_project_id`，再调用 [readProjectAccess](../../server/internal/goldenpath/postgres/access.go) | 列表按自身 governing Project 发现，不按 evidence 可读性过滤，不通过 EntityLink 推导归属 |
| Project 读取 | `readProjectAccess` 复核 active Workspace membership、Project visibility 与显式 membership；归档不禁止读取 | workspace 可见项目提供普通成员只读基线；restricted 项目无 owner 特权 |
| 人工接受 | [AcceptDecision](../../server/internal/goldenpath/postgres/store.go) 检查 Project 可读、active、接受角色及全部现有 evidence，然后处理 receipt / 状态 | 看见列表或详情不能替代接受权限；保持现有事务和精确重试合同 |
| 来源权限 | [readableThread / requireReadableDecisionEvidence](../../server/internal/goldenpath/postgres/access.go) 复核 Thread、origin Channel 与相应 membership | Project admin 不穿透受限 Thread / Channel；列表不读取或展开来源 |
| 状态支持 | [migration 001](../../server/db/migrations/001_golden_path_foundation.sql) 预留四状态；[publicDecision](../../server/internal/platform/httptransport/collaboration.go) 与 [Web adapter](../../web/src/collaboration/api.ts) 只接受 `proposed / accepted` | 首片只支持现有两个公共状态；不能把数据库预留当作已交付功能 |
| 列表与分页 | [discovery service](../../server/internal/goldenpath/discovery.go)、[PostgreSQL discovery](../../server/internal/goldenpath/postgres/discovery.go)、[HTTP discovery](../../server/internal/platform/httptransport/discovery.go) 已实现 Project Ticket 稳定 ID 分页 | 扩展同一发现入口；显式增加 Decision kind、ID 前缀及状态校验，不能落入 Channel 默认分支 |
| Web 生命周期 | [ProjectTicketsPage](../../web/src/workspace/ProjectTicketsPage.tsx) 与 [adapter](../../web/src/workspace/project-tickets-api.ts) 已有双请求共同清理、分页、取消和恢复重读 | 复用现有行为与样式惯例，不新造对象中心或持久列表缓存 |
| 页面与装配 | [ProjectBrowser](../../web/src/workspace/ProjectBrowser.tsx)、[App](../../web/src/App.tsx)、[Web Shell 白名单](../../server/internal/platform/httptransport/web_app.go)、[服务装配](../../server/cmd/radishnexus/main.go) | 新列表须接通导航、直接地址、登录恢复与正式 HTTP 路由，不能只完成组件或 service |
| 查询基础 | migration 001 已有 Decision `id` 主键与 `(workspace_id, id)` 唯一约束；现有 migration 未提供 Decision 的 `(workspace_id, governing_project_id, id)` 专用索引 | 不能直接照搬 Ticket 查询性能结论；实施时独立测量，再决定是否需要索引 |

目标是让成员从 Project 独立找回已有待确认草案与已确认结论，继续使用已有上下文。近期顺序仍按[当前状态](../status/current.md)；真实 Jenkins B 与原生中文 IME 人工验收继续暂缓。

## 决定

### 1. 归属、读取与接受权限

列表只包含路径 Workspace / Project 下 `governing_project_id` 匹配的 Decision。创建者、旧角色、来源引用、cursor 和列表缓存均不授予权限。

每次请求沿用同源 HTTPS、Host / trusted proxy、Session 和服务端 Principal；在一次 PostgreSQL repeatable-read 查询事务中，复核当前 Workspace / Project 权限后读取该页：

| 情况 | 列表与详情读取预期 | 既有接受动作边界 |
| --- | --- | --- |
| active Workspace 成员，workspace 可见 Project，无 Project 角色 | 可读 | 不具备接受角色，拒绝 |
| restricted Project 的 viewer / contributor | 可读 | 不具备接受角色，拒绝 |
| Project decider / admin | 可读 | 仍须 active Project、全部 evidence 可读、合法人工确认请求，并符合既有状态 / receipt 合同 |
| restricted Project 无显式 membership，含 Workspace owner | `404` | 不可访问；不能借列表或旧 receipt 绕过 |
| 暂停 / 移除 Workspace 成员、跨 Workspace 或不存在 Project | `404` | 不可访问 |
| Session 失效或账户禁用使 Session 无效 | 认证层返回 `401` | 返回登录流程 |
| 已归档但仍可读的 Project | 可读，标记项目已归档 | 保持既有 `409 conflict`，不允许新接受操作 |
| Decision 可读、来源 Thread 或其 origin Channel 不可读 | Decision 仍可发现；详情来源继续按目标权限投影 | 即使 decider / admin 也不能接受 evidence 不可读的草案 |

对 workspace 可见 Project 移除显式角色，只撤回相应写能力，不撤回 active Workspace 成员的只读基线。验收须区分角色移除与实际失去读取权。

Decision 的 `question` 是自身已按 Project 可读的业务字段；本列表不自动从 Thread 抽取文字，也不复制 evidence 摘录。既有人工写入的 Decision 问题不会因为来源后来受限而自动脱敏；本切片不新增这种内容派生或权限联动规则，也不能宣称能识别用户手工复制的敏感文本。

### 2. 最小公共读取合同

拟新增一个业务端点：

`GET /api/v1/workspaces/{workspace_id}/projects/{project_id}/decisions`

成功响应示例（拟议合同，当前不可作为已实现接口调用）：

```json
{
  "data": {
    "items": [
      {
        "ref": { "type": "decision", "id": "dec_example" },
        "title": "登录失败是否应使用统一错误提示？",
        "status": "proposed"
      }
    ],
    "next_cursor": null
  }
}
```

- 条目仅有 `ref / title / status`。`title` 是现有 discovery 展示字段，精确承载 Decision 的 `question`，不新增数据库 title、不生成摘要、不改写原问题；详情继续使用 `question`。不同时返回两个同义字段。
- `status` 仅接受 `proposed / accepted`；页面分别标记“待确认 / 已确认”。“待确认”不是“待我决策”，不表示当前用户有接受权限。
- 不返回 outcome、rationale、evidence、来源标题 / 摘录、提议人、决策人、时间、角色、capabilities、关联 Ticket 数量或隐藏对象数量。结论与理由继续在当前授权的详情中阅读。
- SQL 不使用状态过滤隐藏 rejected、superseded 或未知状态。待返回页出现不支持状态、错误 ref、空白 / 无效 UTF-8 / NUL 问题、重复或非递增 ID 时，服务端拒绝整页并返回通用 `500 internal`，不返回部分结果或伪造状态；Web 严格校验合同。该约束只针对已读取的页，不扫描全部历史以预检未到达的记录。
- 有权限且没有条目为 `200` 与空数组；不可读 Project 为 `404`，不能当作空列表。支持 `GET`，其他方法为 `405` 且 `Allow: GET`；未知子路径为 `404`。沿用当前认证 / 输入验证顺序，不承诺畸形请求一律收敛为 `404`。
- 成功与错误均沿用 `Cache-Control: private, no-store`、`Vary: Cookie`、request ID 与既有错误 envelope；GET 不新增 CSRF 要求，不开放 CORS、新认证方式或写操作。

### 3. 分页、并发与状态变化

- 仅接受 `limit / after`；默认 25，limit 为 1～50 的规范十进制整数。沿用 raw query 2048 字节、cursor 1024 字符限制；重复、未知、空值、超界及非法编码返回 `400 invalid`。首片不加状态筛选、搜索、offset 或可选排序。
- 以稳定 Decision ID 升序 keyset 分页：`id > after_id`，读取 `limit + 1` 判定下一页。不按创建 / 更新时间排序，UI 不称为“最新决策”。
- cursor 复用现有 v1 opaque base64url 结构，绑定 Workspace、Project、`kind = decision` 与 `dec_` 边界；严格拒绝未知 / 重复字段、非规范编码、错误版本或前缀、跨 Project / Workspace / 列表复用。客户端不解析 cursor；实现须显式扩展 `discoveryPrefix`，不依赖默认值。
- cursor 不是签名授权或数据快照。结构合法的边界可以对应不存在的对象，不额外查其存在性或标题；每一页重新授权。仅有更多结果时返回 next_cursor，取本页最后一条已返回记录的 ID，不返回总数、隐藏计数或占位。
- 单页使用一致快照；跨页不冻结数据集。并发插入且 ID 落在已翻过的边界前，需要回首页刷新才能看到；固定集合要求完整且无重复。Decision 从 proposed 被接受后，后续请求读取当前状态，ID 顺序不变，不冻结旧状态或自动更新已显示页面。
- 复核权限与查询同处事务，但两个独立 HTTP 请求及跨页没有共同快照。权限在成功请求后撤回时，不承诺所有已打开内容即时消失；下一次请求和下述恢复触发点重新授权。点击详情也必须独立检查当前权限。

### 4. 页面入口与恢复行为

拟新增 canonical 页面：

`/workspaces/{workspace_id}/projects/{project_id}/decisions`

用户路径为“工作台选择 Workspace → 选择 Project → 查看项目决策 → 现有 Decision 详情”。[ProjectBrowser](../../web/src/workspace/ProjectBrowser.tsx) 的入口与项目工单 / 文档并列，不重做工作台。

- 列表显示项目名称、项目归档状态、问题、Decision 状态、上一页 / 下一页和刷新；不提供接受、拒绝、批量处理或独立创建动作。
- 项目上下文复用 `GET /api/v1/workspaces/{workspace_id}/projects/{project_id}/configuration`，读取不要求 admin；页面只消费名称与状态，不据此暴露配置权限。该请求与列表请求共享加载代次、取消和清理规则，不声称服务端跨请求原子性。
- 条目进入既有 `/workspaces/{workspace_id}/decisions/{decision_id}`。详情增加按当前对象 `project.id` 生成的“查看项目决策”链接，回到列表首页；不接受任意 return URL，不依赖旧列表提供项目归属。
- 同步 Web Shell 白名单、App 路由、Workspace 上下文及登录后恢复。直接 URL 可加载 Shell 不代表有权读取数据；未知嵌套路径不能误落到详情或配置页面。
- cursor 栈仅在当前页面内存维护；不新增 URL query、localStorage 或持久数据缓存。刷新、重新进入、焦点恢复、页面重新可见、浏览器历史恢复（包括 BFCache）重新读取首页。分页按钮按当前 cursor 栈重新请求对应页。
- 翻页、刷新和恢复重读先清空旧项目标题 / 条目；切换 Workspace / Project 同时清理分页栈。离开页面中止请求并清理可恢复旧数据。任一上下文 / 列表请求失败，二者共同清空；迟到成功响应不得覆盖新作用域或重新显示已清空数据。
- 明确 loading、empty、error / retry、不可读与 Session 失效。空态为“当前项目没有可显示的决策”；`404` 提示项目不可访问，`401` 回登录，其余失败可重试，不能显示预置数据或空态冒充成功。
- 问题以纯文本显示，不解释为 HTML / Markdown；长问题换行，不把渲染截断内容当作新的存储字段。使用语义链接、可见焦点和文字状态，支持桌面、手机与键盘。

### 5. 实现、索引与兼容边界

首片拟包含 Go discovery service / PostgreSQL 查询 / HTTP adapter、正式 React 列表与入口、必要测试和文档同步。不修改 Decision 创建 / acceptance / Ticket 创建、事件 / Activity、receipt 或权限实现；现有动作只做关键回归。只读发现不产生新的业务写入、领域事件、Activity、Audit 或 command receipt。

默认不新增业务表、字段、依赖或 migration，不回填 Decision；不为两类列表建立新的通用框架。复用已有组件和工具时，仅抽取确有重复且不改变既有行为的部分，避免夹带 Ticket / Document 重构。

索引决策在实施时用正式命令建立的多 Project、混合状态数据测量：记录目标 / 非目标数量、首尾页和小占比 Project 的 `EXPLAIN (ANALYZE, BUFFERS)`、SQL 次数与耗时。列表不应逐条查询 evidence 或详情，SQL 次数不随页大小线性增加。现有 Ticket 的规模证据不能代替 Decision 测量，也不能把一次实验当作生产容量承诺。

若证据表明需增加 `(workspace_id, governing_project_id, id)` 索引，应先补充查询证据、migration 和升级 / 恢复影响并取得范围确认，再新增 forward-only migration；不修改既有 migration，也不放宽 readiness。当前最新 schema 是 016，若新增 migration 则须验证从 016 升级、readiness 精确匹配与空目标恢复。

这是增量 GET 和页面合同：旧 Web 继续使用原入口，新 Web 需要配套服务端。端点缺失 / 响应异常必须显式报错，不 fallback 到枚举 ID 或 fixture。无 migration 时按配套旧应用工件回退，不改业务数据；若后续批准索引 migration，旧二进制受精确 schema readiness 限制，需按恢复 / forward repair 方案处理，不能删 migration history 伪造兼容。

实现时同步 server / Web README、公共合同和当前状态；验证证据另写实施记录，不把此提案的验收要求改写成已通过结果。

## 验收矩阵

以下全部为待执行要求，本轮没有运行业务测试、数据库或浏览器验收。失败场景应验证响应与可见 DOM 不残留旧数据，不能只断言状态码或 mock 被调用。

| 编号 | 场景与输入 | 必须成立的结果 | 证据层 |
| --- | --- | --- | --- |
| P01 | workspace 可见 Project，active 普通成员且无 Project 角色 | 可列出并打开 Decision；不能接受草案 | PostgreSQL + HTTP；浏览器普通成员主链 |
| P02 | restricted Project 的 viewer / contributor / decider / admin | 四角色读取一致；前两者接受被拒绝，后两者仍受 evidence 与 active 边界约束 | PostgreSQL + 既有 acceptance 回归 |
| P03 | restricted Project 无 membership，含 Workspace owner；不存在 / 跨 Workspace Project | 列表与详情均不可发现，公共接口同形 `404`，无标题、ref 或计数泄漏 | PostgreSQL + HTTP |
| P04 | 暂停 / 移除 Workspace 成员；账户禁用、Session 过期 / 撤销 | membership 不可访问为 `404`；失效身份为 `401`，Web 清理并返回登录 | PostgreSQL + HTTP + Web |
| P05 | Project 已归档，普通成员仍可读 | 列表、详情可读并有归档标记；有接受角色者仍不能新接受，保持 `409` | PostgreSQL + Web + 浏览器 |
| P06 | 可读 Decision 的 evidence Thread 受限，或其 origin Channel 受限 | 仍发现 Decision；列表不含来源内容；详情只保留既有安全投影；decider / admin 不穿透，接受拒绝 | PostgreSQL + HTTP + 浏览器 |
| P07 | workspace Project 撤回显式角色；restricted Project 撤回 membership | 前者保留读取且撤回对应写权；后者下次读取 `404`；不混淆两个结论 | PostgreSQL + 浏览器刷新 / 后退 |
| D01 | 0、25、26、27 与多页对象，混合 proposed / accepted，limit 1 / 50 | 空页为成功；固定集合按 ID 完整无重复；next_cursor 仅在有后页时存在 | PostgreSQL + HTTP |
| D02 | 多 Workspace / Project 对象及相同问题文本 | 只按稳定 ref 和 governing Project 归属返回，不按标题去重或按关系扩张 | PostgreSQL |
| D03 | 不存在的合法边界、并发插入、跨页接受某 Decision | 不查询边界存在性；遵守 ID 边界；重读反映 accepted，刷新可找到边界前新增对象 | PostgreSQL |
| D04 | 下一页前撤权，之后重新授权 | 撤权请求拒绝；恢复后重新授权读取首页，不直接复用旧业务快照 | PostgreSQL + Web |
| D05 | 长问题、中文、HTML / Markdown 样式文本 | title 与 question 原值一致；页面纯文本安全展示、换行可读；不提取 evidence | HTTP + Web + 浏览器 |
| D06 | 损坏投影或非首片状态，包括 rejected / superseded | 待返回页整页失败；不过滤记录、映射状态或部分返回；Web 拒绝异常响应 | HTTP stub + Web；数据库异常数据专项 |
| H01 | 缺 Session、错误 Host / proxy、非法路径 ID；未知路径和 GET 以外方法 | 沿用认证 / 错误合同；方法与 Allow 精确，未知子路径不进入其他业务路由 | HTTP + 正式装配测试 |
| H02 | 重复 / 未知 / 空 query、非法 limit、超长或非规范 cursor | `400 invalid`，不扩大请求范围 | HTTP |
| H03 | cursor 跨 Workspace / Project / Ticket / Channel 等列表复用；版本、字段、ID 前缀篡改 | 全部拒绝；合法 cursor 仍重新授权 | HTTP + PostgreSQL |
| H04 | DTO 多余字段、非法状态 / ref / title、重复 / 乱序 ID、错误下一页边界 | 服务端拒绝无效投影；Web 拒绝无效公共响应，不把结果当空态 | HTTP + Web |
| H05 | 成功、空列表、认证失败、无权限、输入失败及服务端异常 | 缓存头与错误 envelope 一致；无正文、来源、角色、receipt 或隐藏计数 | HTTP |
| W01 | Project 入口 → 列表 → 详情 → 项目列表 | 路径正确；详情按自身 project 生成返回链接；回首页重新请求 | Web + 浏览器 |
| W02 | 直接地址、退出再登录、刷新、Workspace / Project 切换 | 正确恢复路径与作用域；不显示上个项目标题 / 条目 / cursor | Web + 浏览器 |
| W03 | 翻页、刷新、焦点 / 可见性恢复、popstate、pagehide / BFCache pageshow | 按合同清理旧数据并重新读取；浏览器历史不复活撤权前内容 | Web 事件测试 + 真实浏览器恢复 |
| W04 | 上下文成功而列表失败，反向组合，取消无效的迟到响应、连续切换 | 任一失败清理二者；旧代次响应不能覆盖新页或错误状态 | Web 可控异步测试 |
| W05 | loading / empty / error / retry / 404 / 401 | 状态可区分；错误不伪装空列表；重试发起新读取，401 清理 Session UI | Web |
| W06 | 桌面、手机、键盘、长问题、多页与状态文字 | 链接 / 按钮可达，焦点可见，无横向溢出；状态不依赖颜色 | 浏览器 |
| R01 | 正式创建消息 → Thread → Proposed Decision；人工接受其中一部分 | 普通成员重新登录后无需 ID 从 Project 找回两种状态，来源按权限可见，既有 Ticket 关系可继续访问 | 隔离 HTTPS 浏览器 |
| R02 | 现有 Project / Channel / Document / Ticket 发现和 Decision 详情 / acceptance / Ticket 创建 | 新路由不抢占旧路由；读取权限、写入 / 精确重试及现有 DTO 不变 | Go + PostgreSQL + Web 定向回归 |
| R03 | 多 Project 代表性数据，首尾页、小占比项目，页大小变化 | 记录计划、SQL 次数与耗时；无逐条 evidence 查询；基于证据决定是否增索引 | PostgreSQL 查询测量 |
| R04 | 反复 GET / 翻页 / 刷新，隔离其它写入 | Decision、EntityLink、业务事件、Outbox、Activity、Audit、receipt 不因发现请求新增或改变 | PostgreSQL；不把认证 Session 维护视为业务写入 |
| R05 | 仅当批准索引 migration | 016 升级、readiness、权威备份 / 空目标恢复后列表与原事实一致；旧工件不伪造 schema 兼容 | 升级 + 备份恢复 |

主场景须从正式基础配置、账户邀请与成员授权入口建立，业务 Decision 经正式命令产生；不能手写 SQL 预置最终结论替代用户链。分页 / 规模夹具可重复调用同一正式业务命令；损坏记录和数据库预留状态只用于异常专项，不能作为功能已支持的证据。源 Thread 的敏感测试标记应与 Decision 问题不同，以检出自动展开来源的泄漏。

实现阶段计划使用已有 `./scripts/check-server.sh`、`./scripts/check-server-postgres.sh`、`./scripts/check-web.sh` 与 `./scripts/check-repo.sh`；索引分支另执行 `./scripts/check-server-backup-restore.sh` 并补升级测试。数据库 / 容器 / 浏览器服务的目标、资源和清理需按当次实施授权明确，以上计划不授权本轮启动。实际执行后记录命令、结果、失败修正、截图及未覆盖范围；未执行项不得标为通过。

退出结论限定为“成员通过正式 Project 入口找回已有 Decision，当前权限、分页与页面恢复通过验证”。这不等于 Decision 完整生命周期、待我决策、真实团队持续使用、完整 Golden Path 或生产容量通过。

## 替代方案与取舍

| 方案 | 判断 |
| --- | --- |
| 继续沿 Thread / Ticket 关系或 ID 工具找回 | 保留既有路径，但无法满足独立项目发现 |
| 列表加入来源摘录、完整结论和接受按钮 | 扩大数据与写权限面；首片通过详情承担阅读与确认 |
| 按 evidence 权限过滤 Decision | 改变现有对象读取边界，使列表与详情不一致；不采用 |
| 同时支持 rejected / superseded 或仅筛选两种状态 | 前者缺完整公共读写合同，后者静默隐藏漂移；异常页明确失败 |
| 新增 question 字段或 title / question 双字段 | Decision 详情继续用 question，列表沿用既有 title 投影，避免平行发现结构 |
| 首片加状态筛选 / 最近更新排序 / 待我决策 | 增加过滤、可变排序或能力语义；先验证最小发现，按实际使用另立切片 |
| 预先增加索引或建立通用对象中心 | 缺查询或重复证据；先复用并测量 |

代价是增加一个公共 GET 合同、一个页面及其恢复生命周期；成员仍需进入详情阅读完整结论，列表不实时刷新状态。收益是补齐讨论与执行之间的独立决策入口，同时保持权限、来源和状态合同稳定。

## 所有者确认范围与实施分工

2026-10-18 所有者先授权提案、源码核对及验收矩阵，随后确认按本 ADR 进入实现，并要求先提交现有提案文档。实现与验收改动暂不提交或 push；启动隔离验收资源前须说明目标、影响及清理方式。

已确认按上述最小 GET / 页面合同推进，只展示问题与两种现有状态，复用当前权限与稳定 ID 分页，不加入筛选、写动作或默认 migration。若需索引，应先补证据与影响再确认；Decision 拒绝 / 替代 / 复核、Ticket 看板、Jenkins B、Flutter、账号恢复和管理员交接均不合并进此片。

实现由主会话负责接口冻结、实际 diff 审查和关键行为独立验收。边界明确的实现按本任务约定交给 `gpt-6.1-sol / high`：后端发现链与测试、前端列表与入口分开委派；每次列出精确允许修改文件、固定接口及上表验收编号，公共文件由主会话协调。子代理只返回简洁改动说明、实际测试结果和未解决问题；架构问题或连续返工由主会话接管，很小的改动可直接完成。
