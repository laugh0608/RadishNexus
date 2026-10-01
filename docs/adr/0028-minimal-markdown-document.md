# ADR-0028：最小 Markdown Document 与不可变版本

状态：已接受

日期：2026-09-14

关联：[ADR-0021](0021-document-editor-and-collaboration-foundation.md) 的非 CRDT 接受路径。项目所有者于 2026-09-14 确认本合同及 goldmark 固定版本依赖范围，要求今晚仅完成提交与文档收尾，正式实施安排到下一工作日。

## 背景

首次访问初始化、基础对象配置和 Message → Thread → Decision → Ticket 已形成可操作切片。Golden Path 接下来要求成员从 Ticket 找到设计 Document，保存后仍能追溯来源，并在并发编辑或实例恢复后保留内容和版本。

当前注册表、数据库、Nexus View resolver 和公共 HTTP 尚无 Document。仅增加 textarea 不能完成该缺口；必须一起冻结对象、当前权限、不可变版本、EntityLink、事件、渲染和恢复边界。阶段 A 编辑器实验只支持后续结构化编辑选型，不证明正式 Markdown 渲染安全。

## 决定

以下为已确认的实施范围；合同接受不等于代码已实现。同步领域模型与核心合同，实施时再更新对应自动化；具体执行顺序见当前状态。

### 1. 用户路径与范围

1. 有权成员在 Ticket 页面选择“创建设计文档”，填写标题和 Markdown，显式创建；Document、首个版本和 Ticket 关系同事务提交。
2. Ticket 出向 Relations 与 Document 入向 Relations 可双向跳转；Project 提供权限过滤、分页的文档列表，刷新或重新登录后可以独立发现文档。
3. Document 页面提供阅读、Markdown 编辑、显式预览、显式保存、版本列表、历史版本阅读和“恢复为新版本”。
4. 两个标签页基于同一版本保存时只有一个成功；另一页保留草稿并展示冲突，由用户选择放弃或基于最新版本手动重新应用。

首期创建入口只从 Ticket 发起；一个 Ticket 可有多个 Document。独立创建、关联既有文档、解除关系、删除文档、移动 Project、文档级私密权限、附件、评论、搜索、自动保存、实时协同和浏览器持久化不在本切片。后续解除关系必须保留来源和不可逆的 removed 记录，不能直接删边。

Document 不替代 Decision。新文档关系不改变已接受 Decision 的证据，也不把可变正文自动升级为审批、确认或交付事实。

### 2. 对象与版本

| 字段 | 合同 |
| --- | --- |
| `id` / EntityRef | `document` 类型、`doc_` 前缀，沿用实体 ID 生成与校验；引用为 `entity://document/<id>` |
| `workspace_id` | 创建后不可变；与 governing Project、来源 Ticket 相同 |
| `governing_project_id` | 创建后不可变，仅承担导航和授权作用域，不能替代 EntityLink |
| `visibility` | 首期固定 `project`；创建请求不能自行指定更宽或更窄权限 |
| `current_revision` | 正整数，从 1 开始，按文档连续递增，上限 2147483647；耗尽明确拒绝 |
| `created_by` / `created_at` | 由服务端当前主体与 UTC 时间产生，不由客户端填写 |

每个不可变 revision 包含 `(workspace_id, document_id, revision)`、`title`、`body_markdown`、`format_version`、`created_by`、`created_at`、`source_event_id` 和可空 `restored_from_revision`。版本号只在文档内有效，不成为独立 EntityRef，不扩展实体 URI 的 query / fragment。

- 标题 trim 后为 1..200 个 Unicode code point，禁止换行、NUL 和控制字符；标题与正文属于同一个版本，不能单独绕过并发控制修改。
- 正文允许为空；必须有效 UTF-8，最多 262144 bytes，禁止 NUL。仅把 CRLF / CR 规范化为 LF，不 trim 正文、不 Unicode normalize、不经过 parser 重写源文本，保留 Markdown 换行所需尾随空格。
- `format_version` 固定为 `nexus-markdown-v1`，与实验 corpus 的 `radishnexus-markdown-v1` 明确区分。
- 当前对象通过版本指针读取标题和正文，不维护第二份可独立修改的当前正文。数据库约束保证指针、版本、作者和事件在同一 Workspace；revision 拒绝 UPDATE / DELETE。
- 创建首版为 revision 1；每个新的有效保存命令产生下一版。相同内容的新命令仍是一次显式版本记录，UI 在草稿未改变时禁用普通保存，不能据此省略服务端幂等控制。
- 恢复旧版读取权威历史的标题、正文和格式，追加新版本并记录来源版本；不覆盖旧版本、不重置版本号、不回滚关系或权限。

### 3. 权限与并发撤权

Document 首期全部继承当前 Project 读取边界：`workspace` Project 的活跃 Workspace 成员可读，`restricted` Project 仅显式 Project 成员可读。这意味着来源 Ticket 中手动填写的新文档将对整个可读 Project 开放，页面创建前须明确显示该范围，不自动复制私密 Thread 正文。

| 能力 | 所需当前权限 |
| --- | --- |
| 列表、当前正文、历史列表、历史正文、Relations、Activity | 活跃账户及 Workspace membership，且能读取 governing Project |
| 从 Ticket 创建 | 当前可读 Ticket，同一活跃 Project 的 `contributor / decider / admin` |
| 保存、恢复、草稿预览 | 同一活跃 Project 的 `contributor / decider / admin` |

作者没有独占编辑权；Workspace owner 没有额外 Project 越权能力。Project 归档后只读，包括精确写重试也重新检查当前状态。未知对象与不可发现对象统一 not-found；可读但不可写返回 forbidden。历史版本没有冻结旧授权，所有入口复用当前权限。

事务沿用已有 Project 与 membership 锁顺序，在授权校验及提交期间阻止并发撤权穿越；再锁 Document 串行推进版本。多个资源按固定顺序取锁，不能先锁 Document 再反向取得 Project。数据库集成测试必须验证撤权、归档与保存之间的合法提交顺序。

客户端在重新读取发现 401、403、404、Workspace 切换或退出时清除当前正文、历史、预览与未保存草稿；网络错误不伪装撤权。无实时撤权推送，本切片不承诺立即抹除用户已看到或自行复制的内容。HTTP 响应使用 `no-store`，不落 localStorage / IndexedDB / Service Worker cache。

### 4. 显式保存、冲突与幂等

创建、保存和恢复复用 [ADR-0019](0019-session-scoped-thread-decision-ticket-transport.md) 的 `client_operation_id` 校验和 `(workspace, actor, command, target, operation)` 去重范围，扩展既有 collaboration receipt 合同，不另建第二套通用幂等系统。

- 新命令分别为 `document.create`（target Ticket）、`document.save`、`document.restore`（target Document）。receipt 增加结果 revision，旧命令保持该字段为空，并保持 immutable 及事件外键。
- canonical digest 包含动作的全部业务输入：创建包含规范化标题 / 正文 / 格式；保存还包含 `base_revision`；恢复包含 `base_revision` 和 `restore_revision`。输入顺序不影响摘要，变化重放返回 409，不泄露摘要或原输入。
- 当前授权通过后，先查精确 receipt，再对新命令核对 `base_revision`。响应丢失后即使已有更新版本，精确重试也返回原 `applied_revision`，不能再追加版本。
- 首次创建返回 201，精确重试返回 200；保存和恢复成功返回 200。写响应只返回结果 Document ref、`applied_revision`，不附带可能过时的正文。客户端再读取最新权威对象，不能把旧 receipt 的版本当作当前版本。
- 新保存 / 恢复的 base 过期返回 409 与当前 revision，不在错误中夹带当前正文。客户端通过正常授权 GET 加载最新内容，对比本地草稿；不静默替换 base、自动合并或自动重试写入。
- 冲突后用户确认重新应用，才基于最新 revision 和新 operation ID 提交；旧草稿仍可编辑。结果不明时保留原 operation ID 和原输入，提供精确重试，不把重新创建当作恢复方式。
- 恢复必须先展示所选历史版本和当前版本，再由用户明确点击确认；请求携带 `confirmed: true`，该交互字段不成为业务事实。旧格式若已不被当前安全实现支持，明确拒绝恢复，不能删内容后保存成功。

Web 在等待请求时禁止重复操作，忽略过期读取和预览响应；输入组合期间不把 Enter 当作保存快捷键。离开脏草稿前提示，刷新退出后的草稿丢失风险明确呈现，不承诺离线恢复。

### 5. Markdown 与安全展示

`nexus-markdown-v1` 采用 CommonMark 0.31.2 基础解析，不开启 GFM 或自定义属性扩展。支持段落、1..6 级标题、强调 / 加粗、引用、有序 / 无序及嵌套列表、分隔线、围栏 / 缩进代码块、行内代码、链接、软换行和硬换行。软换行输出普通文本换行，硬换行输出受控 `br`；不做富文本往返序列化。

- 原始 HTML block / inline、图片节点和非白名单 URL 明确拒绝保存与预览，返回有界诊断及源码位置，不静默删除；代码块、行内代码或转义文本中的 HTML / URL 作为普通文本保留。
- 链接允许有效的绝对 `http` / `https` URL，要求 host，拒绝 userinfo、控制字符、反斜杠及解析歧义；在 CommonMark 实体与转义处理后验证实际目标。不接受相对 URL、协议相对 URL、`javascript`、`data`、`file`、`mailto` 或 `entity` scheme。
- 普通文本中的 EntityRef 只作为文字，不能自动生成 EntityLink、抓取目标或推导权限。正式对象跳转通过权限过滤的 Relations 提供。
- 表格、任务列表、数学公式、Mermaid、脚注等扩展未开启，按基础 Markdown 的普通文本 / 基础节点解释；帮助文案明确无扩展语义。未知 AST 节点则 fail closed，不能直接跳过。
- 不自动请求远程图片、链接预览、字体或 embed；外链只由用户点击打开，固定 `noopener noreferrer` 和无 referrer。代码纯文本展示，不加载高亮语言或执行代码。

采用单一服务端 parser，把 AST 转为项目定义的只读 `nexus-markdown-view-v1` 展示节点。节点仅允许上述固定语义、文本、受检 URL、标题等级和列表属性；React 按白名单创建元素，禁止 `dangerouslySetInnerHTML`、动态 tag / style / event handler。客户端验证节点形状和 URL，未知版本 / 节点显示明确失败。该结构是可重建的读取投影，不接受客户端提交，不保存成权威正文，不暴露 goldmark AST 或编辑器内部 JSON。

预览与保存、历史读取使用同一解析及安全策略。预览有当前 Project 写权限和 CSRF 检查，只处理请求内草稿，不写版本、事件或日志正文。HTTP JSON body 上限 2 MiB，以容纳转义后的 256 KiB 文本，解码后仍检查字段限额；解析投影最多 20000 个节点、嵌套深度 32，超限明确拒绝。节点上限不是 parser 本身的 CPU 限制，实施必须对最大输入、深嵌套和恶意分隔符做耗时 / 内存测试，不把 HTTP timeout 当作 CPU 抢占保证。

历史若因安全修复不能继续渲染，原始 Markdown 保留；有权用户可以查看安全转义的源码及失败说明，不返回部分成功的渲染结果。此源码读取是显式模式，不是隐藏渲染降级。

### 6. 依赖选择及成本

已确认新增唯一正式依赖 `github.com/yuin/goldmark v1.8.6`，用于解析 CommonMark AST；不加入正式 Tiptap / Lexical / Yjs，也不另加 Web Markdown parser 或 HTML sanitizer。安全边界由受限节点投影与 URL 校验承担，不能把 parser 的默认 HTML renderer 当作安全证明。

2026-09-14 官方来源预检：

- [固定版本发布页](https://github.com/yuin/goldmark/releases/tag/v1.8.6)存在，并记录近期修复；选择固定 v1 API 基线，不宣称它是整个项目最新版本或有长期支持承诺。
- [固定版本 go.mod](https://raw.githubusercontent.com/yuin/goldmark/v1.8.6/go.mod)要求 Go 1.22 且没有第三方 require，与当前 Go 1.25 基线兼容；[版本说明](https://github.com/yuin/goldmark/tree/v1.8.6)描述 CommonMark 0.31.2、AST 和默认禁用不安全 HTML 输出。
- [上游许可证](https://raw.githubusercontent.com/yuin/goldmark/master/LICENSE)为 MIT；本轮固定 tag 的 LICENSE 网页抓取失败，因此不能声称已核对下载包许可证。授权添加后必须检查精确版本包 LICENSE、module checksum 和依赖图，更新第三方声明与已有依赖基线；不符时停止该依赖接入并报告。

不采用自写正则解析器，避免自行重现 CommonMark；不采用前后端两个 parser，避免预览与存储语义漂移；不以完整富文本依赖替代当前版本合同。代价是需要维护有限展示节点 adapter 和安全 corpus，草稿预览需要一次显式请求。后续升级需重跑规范与攻击样例，不能随意改变已保存格式的含义。本轮未安装依赖、未改变 go.mod / go.sum，未执行该版本漏洞扫描。

### 7. 来源、EntityLink、Activity

创建时原子建立 `ticket --relates-to--> document`，首期只注册该方向与类型组合，不开放通用任意连边 API。两端必须同 Workspace、同 governing Project；`assertion=asserted`、`origin=user`，创建者为当前主体，来源事件为本次 `document.created`，`origin_ref` 为空、`metadata={}`。关系指向文档身份及其当前版本，不表示来源 Ticket 曾确认某个版本。

创建同时写 revision 1、`document.created` v1、receipt、EntityLink、Outbox 和 Activity；保存 / 恢复写 `document.revised` v1。恢复事件只增加 `restored_from_revision`；事件使用既有 envelope、Document primary ref、Project scope、actor、服务端时间和 request correlation。payload 仅含必要 ref、revision 及恢复来源，不复制标题、Markdown、展示树、operation ID 或 digest。

Timeline 继续遵守事件主要对象语义：Document 展示创建 / 修订 / 恢复；不会自动向 Ticket 或 Thread 复制全部 Activity。正常事务与全量重建使用同一 projector，并同步投影版本及兼容检查。历史事件主体显示沿用既有权限规则，不引入额外账户目录泄漏。

Ticket 出向和 Document 入向 resolver 增加该精确关系白名单，读取两端当前权限后才取标题。出向不可读目标返回纯 restricted 占位，入向不可读来源隐藏；直接访问仍 not-found。引用、来源事件和 receipt 都不能成为授权凭证。

### 8. 公共读取与命令面

所有路径以 `/api/v1/workspaces/{workspace_id}` 为前缀，继承既有 Session、当前 Workspace、同源、CSRF、严格 JSON 和错误映射。下列为已确认、尚未实现的全部新增入口：

| 方法与后缀 | 用途 / 关键输入 |
| --- | --- |
| `GET /projects/{project_id}/documents` | ID 游标稳定分页；默认 20、最大 50；仅 ref、标题、当前 revision、创建 / 更新时间，不返回正文 |
| `POST /tickets/{ticket_id}/documents` | `client_operation_id`、`title`、`body_markdown`、`format_version`；显式创建并建立关系 |
| `GET /documents/{document_id}/nexus-view` | 当前版本、受限展示投影、Relations、Timeline；复用现有 Nexus View 分区与失败语义 |
| `POST /documents/{document_id}/revisions` | `client_operation_id`、`base_revision`、`title`、`body_markdown`、`format_version` |
| `GET /documents/{document_id}/revisions` | 按 revision 降序游标分页；默认 20、最大 50；只返回版本元数据和该版标题，不批量返回正文 |
| `GET /documents/{document_id}/revisions/{revision}` | 当前权限下的单个历史快照及展示投影 |
| `POST /documents/{document_id}/restorations` | `client_operation_id`、`base_revision`、`restore_revision`、`confirmed` |
| `POST /projects/{project_id}/document-preview` | `body_markdown`、`format_version`，只读解析草稿并返回展示投影 |

分页游标拒绝跨作用域或非法值，列表不返回不可见对象计数。历史列表只随新版本在头部增长，后续页使用严格小于游标；结果不承诺跨请求数据库快照。DTO 不返回内部表、receipt、事件原文、Outbox、角色或成员列表；权威 Markdown 只在已授权当前 / 单历史读取中返回。未知格式、超限、非法 Markdown 给出明确 validation 错误，不在错误中回显正文。

### 9. 备份、恢复与导出

下一连续 forward-only migration 新增 Document / revision，扩展 entity / relation 注册表与既有 receipt 约束；不改写 001..009。schema readiness 继续精确匹配，旧二进制不能带着未知 Document schema 启动服务；回退需要配套旧工件及变更前备份，不提供降级 SQL 或删版本回退。

Document、全部 revisions、来源 EntityLink、receipt、领域事件和必要 Outbox 属权威备份数据；展示树不持久化，Activity 仍从事件重建。新增表必须进入备份分类、manifest 及空目标恢复验证，不能以未知表 fallback 绕过检查。

恢复后验证当前指针、全部正文与标题、连续版本、恢复来源、关系 provenance 和精确重试结果；服务端按当前权限读取历史。Session、草稿和浏览器状态不因 Document 而进入备份。

未来 `.nexus` 导出须保留稳定文档身份、格式版本、可读 Markdown revision、当前指针、操作者映射和有权导出的关系来源；导出每个历史版本仍按当前权限过滤，不能用旧成员关系恢复权限。receipt、登录态、凭据、未提交草稿和内部展示树不属于可移植正文合同。本切片不建立 `.nexus` 文件格式、打包下载或导入 API，不把 PostgreSQL 恢复写成已完成产品导出。

## 后果

该切片让普通成员能从执行事项进入设计文档，并能理解谁保存了哪个版本、为何发生冲突和如何恢复内容。Markdown 是可读权威数据，后续结构化编辑可复用稳定文档身份与版本边界。

首期没有文档级私密分享，Project 可读成员都能读取历史；历史正文不能通过普通编辑擦除，受控脱敏与保留策略需独立设计。版本全量存储与不可变 receipt 会持续占用空间，256 KiB 单版上限不是工作区总容量承诺，团队试用前需按代表数据测量。当前不承诺 CRDT、离线草稿恢复或跨实例可移植导入完成。

## 迁移与验证门槛

实施验收必须完成：

1. 领域 / Markdown corpus：中文、emoji、空文档、嵌套列表、软硬换行、规范化和源码保留；HTML、图片、协议混淆、实体编码、控制字符、未知节点、最大输入与深度限制。核验 React 不生成非白名单元素、属性或远程请求。
2. 真实 PostgreSQL：正式命令创建 Ticket → Document 关系；并发保存单赢家、同键精确重试及变化重放、响应丢失后新版本与旧 receipt、归档 / 撤权竞态、失败原子回滚、恢复追加与 revision 不可变。
3. HTTP / Web：全部角色、跨 Workspace、不可发现历史、分页、当前 / 历史读取、受控预览、409 草稿保留、确认重新应用、未知提交结果、退出 / 切换清理、响应乱序和恢复确认。
4. 正常 Timeline 与反向关系从正式写入直接读取，另测重建一致性；完整备份到全新 PostgreSQL 17 目标，比较权威快照、重建 Activity 和恢复后 retry，不能用预置最终关系或手工重建补正常路径。
5. Go 定向测试 / race / vet、Web 类型 / Lint / 构建 / 测试、依赖许可证与安全检查、`./scripts/check-repo.sh`；HTTPS 浏览器验证两个标签页冲突、中文 IME、桌面 / 手机布局、刷新后发现、历史恢复及撤权。浏览器与真实数据库运行范围须在启动前说明；不能从 jsdom 推导人工验收通过。

本轮只完成源码 / 文档核对及合同确认；既有编辑器实验 `npm run check` 的 28 项测试、构建和 115 包依赖基线检查通过，仍有原有实验 bundle 超过 500 kB 的构建提示。它没有验证拟新增 goldmark 或正式 Document。实际实施和真实浏览器证据在实施后独立记录，不提前勾选。
