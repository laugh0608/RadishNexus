# ADR-0021：Document 正文、编辑器与协同分层

状态：提议

日期：2026-09-03

## 背景

Golden Path 只要求一个简单 Markdown Document，并明确排除多人协作文档、CRDT 和离线编辑；M5 才要求结构化编辑、多人协作、评论、版本历史和协同故障恢复，M7 才进入浏览器离线缓存与跨端同步。若现在直接选择完整协同栈，容易把编辑器内部 JSON、某个 CRDT binary、商业服务或未经授权的 WebSocket 行为变成长期公共合同。

Document 同时受以下既有边界约束：

- Document 是一等对象，不能代替 Decision、Ticket 或 Deployment；对象关系继续使用 EntityLink；
- 引用不授予权限，权限撤销必须同时约束正文、评论、版本、搜索、Activity、导出和客户端缓存；
- Web 采用 React + TypeScript，但后续 Flutter 不应被迫解释 Web 编辑器的私有节点类型；
- 自部署核心路径不依赖官方云、付费协同服务或持续联网许可证检查；
- `.nexus` 导出需要可读、版本化和可迁移，不能只保存某个运行时才能解释的 opaque binary；
- 新依赖必须验证许可证、维护状态、供应链、浏览器行为和真实维护成本。

本 ADR 先固定评估和分层边界，不冻结 Document ID、PostgreSQL schema、HTTP route、实时 transport 或最终依赖。只有下述实验门槛全部通过后，才将状态改为“已接受”并开始正式纵向切片。

## 评估结论

### M0.5 正文基线

M0.5 使用服务端权威、版本化的 UTF-8 Markdown 正文，不引入富文本编辑器或 CRDT 依赖。首个 Web 交互可以使用受控的 Markdown textarea、预览和显式保存，写入必须带当前 base revision 或等价条件；并发覆盖不得静默 last-write-wins，冲突应返回当前 revision 并要求用户明确重新应用或放弃草稿。

首版 Markdown 必须声明受支持的语义子集和版本。至少覆盖段落、标题、强调、链接、引用、列表、代码块、行内代码和受控换行；原始 HTML、脚本、iframe、data URL 与未经治理的 embed 不进入默认子集。具体 parser、规范化和渲染安全仍由后续实验决定，不在本提议中把任一库输出当成已接受事实。

Document 当前版本、不可变 revision、操作者、服务端时间和来源是业务事实。编辑器 selection、菜单状态、presence、cursor 和未提交 undo stack 不是业务事实，不进入 Activity、EntityLink、备份或导出。

### 可移植表示与内部表示

版本化 Markdown 是 M0.5 的权威正文，也是长期必须保留的可读导出表示。未来结构化编辑器可以维护版本化内部 document tree，CRDT 可以维护可合并更新，但二者都属于实现表示：

- 不把 Tiptap / ProseMirror JSON、Lexical EditorState 或 Yjs / Automerge binary 直接公开为稳定业务 DTO；
- 每个已提交 revision 必须能生成确定、经过 sanitizer 和权限过滤的 Markdown snapshot；
- unsupported node 不得静默丢失；schema upgrade 必须显式迁移、拒绝或保留受控 opaque payload，并有往返 fixture；
- 备份若包含内部表示，还必须包含 schema / codec version、校验和与可读 snapshot；恢复不能只证明 binary 可加载，还要证明正文、revision 和关系一致；
- Flutter 与外部工具只依赖后续冻结的公共 Document contract，不依赖 Web 编辑器包。

Markdown 不承诺对所有排版做 byte-for-byte 往返。实验必须区分语义等价、规范化变化和不可接受的数据丢失，并把可接受的规范化规则版本化。

### 编辑器候选

第一实验候选为 Tiptap 3 / ProseMirror，Lexical 作为同一 corpus 的对照候选：

| 候选 | 保留理由 | 当前风险与限制 | 结论 |
| --- | --- | --- | --- |
| Tiptap 3 / ProseMirror | MIT；React 19 peer range；ProseMirror 提供 schema、transaction、step 与成熟的结构化编辑基础；Tiptap 提供较薄的 React/headless 扩展层，并有官方 Yjs binding | `@tiptap/markdown` 官方仍标记 Beta；headless UI 的 toolbar、焦点和 ARIA 仍由本项目负责；Comments、Snapshots 等能力不能默认依赖付费 Tiptap 产品 | 阶段 A 已选定进入阶段 B；不等于已批准生产依赖 |
| Lexical | MIT；React-first；EditorState 为不可变、可序列化 snapshot；提供 Markdown、Yjs 与 React 包 | 节点注册、导入导出、协同初始化和 schema 演进需要自行装配；当前协同实现同时存在 legacy 与 experimental v2 路径，迁移风险需实测 | 阶段 A 对照已完成；保留退出证据，不进入阶段 B |
| 直接 ProseMirror | schema、transaction 和 `prosemirror-markdown` 边界最直接，MIT | 需要自行承担 React 生命周期、命令、toolbar、可访问性和扩展装配；与 Tiptap 比较前没有证据证明额外控制能抵消维护成本 | 作为 Tiptap 底层与故障定位参照，不单独建立第三套 UI |

截至 2026-09-03，npm 官方 registry 显示 Tiptap core / React / starter-kit / Markdown 为 `3.31.2`，Lexical core / React / Markdown / Yjs 为 `0.50.0`，均声明 MIT。版本只用于可复验实验输入，不是仓库依赖决定；正式引入仍需精确锁定、许可证与 lifecycle script 检查。

### 协同内核候选

Yjs 是后续协同实验的第一候选，但当前不接入正式 server 或 Web：

- Yjs shared type、document update、network provider、awareness 和 persistence provider 分离；update 可乱序、重复应用，适合独立验证收敛和持久化边界；
- Tiptap 和 Lexical 均有 Yjs integration，可在不更换协同内核的前提下比较编辑器；
- awareness 不属于持久正文，不能进入 revision、Activity 或备份；
- `y-websocket-server` 官方明确定位为开发 server 或自建 backend 起点，不能直接作为 RadishNexus 生产服务；
- `y-indexeddb` 会把正文更新持久化到浏览器，只能在 M7 离线合同冻结并具备撤权清理、用户切换隔离、容量和恢复测试后评估。本轮不读取或写入 Web Storage。

Automerge 保留为未来离线 / 跨端候选，不进入第一实验。它的 network-agnostic、不可变 snapshot、历史和 Rust / WASM 能力与 M7 方向相符，但当前 rich-text API 仍要求应用自行解释 marks / block markers，官方 ProseMirror binding 在 npm 为 `0.2.0`。在尚无离线和跨端真实需求时，同时验证第二个 CRDT 会扩大实验而不改变 M0.5 选择。

ProseMirror 自带的 central-authority collab steps 可作为无 CRDT 对照，但不能与 Yjs update 混成一套协议；是否采用要在 M5 根据在线协同、离线和运维需求单独决定。

### 自部署与商业边界

RadishNexus 可以使用 MIT 的 Tiptap editor core、ProseMirror、Lexical 或 Yjs，但核心 Document 功能不得依赖 Tiptap Cloud、付费 Comments / Snapshot / Conversion、其它官方云或非开放协同服务。评论、版本、权限、备份和导出必须拥有 RadishNexus 自己的业务合同；第三方服务以后只能作为可选 connector。

编辑器 UI 仍须由本项目验证键盘、焦点、toolbar 语义、screen reader、中文 IME、复制粘贴和 390px 布局。候选官方声明或 demo 不能替代真实浏览器验收。

## 协同安全边界

后续实时协同协议必须先于实现冻结，并至少明确：

1. Session、Workspace 与 Document 当前权限在连接、初始同步、每批 update、snapshot、comment 和恢复时如何重新验证；
2. Document room 名称、CRDT client ID、state vector、update、awareness 和 cursor 都不是授权能力；
3. 被撤权客户端如何停止收发、清空正文 / 草稿 / presence，并防止离线 update 在重新获权前上传；
4. 单连接、单用户、单 Document、单 update、累计未压缩更新、内存和持久化的硬上限；
5. update 的幂等、乱序、重放、损坏、压缩、snapshot、compaction、schema migration 与服务重启恢复；
6. 多副本 fan-out、连接 draining、备份期间一致性和恢复后旧连接失效；
7. 评论锚点、EntityLink 和 revision 如何在正文变更后保持可解释，不能靠裸字符 offset 冒充稳定引用；
8. 日志、事件、Activity、metrics 和错误响应不得携带正文、selection、update binary 或受限对象身份。

Message SSE 是单向、可回到 canonical history 的提示通道，不承担上述双向 Document update。Document 合同冻结前不复用或扩展该 SSE，也不建立 WebSocket endpoint。

## 可丢弃实验合同

### 阶段 A：单人编辑与 Markdown 往返

在隔离的 `experiments/document-editor` 中比较 Tiptap 与 Lexical，不改动 `web/package.json` 或正式 Web bundle。依赖安装需要项目所有者对精确 package、版本、lockfile 和清理范围另行授权。

首轮 headless corpus 建议只安装以下直接依赖，并使用 Node 24 内建 test runner，不额外引入测试框架：

- Tiptap：`@tiptap/core@3.31.2`、`@tiptap/pm@3.31.2`、`@tiptap/starter-kit@3.31.2`、`@tiptap/markdown@3.31.2`；
- Lexical：`lexical@0.50.0`、`@lexical/headless@0.50.0`、`@lexical/markdown@0.50.0`、`@lexical/rich-text@0.50.0`、`@lexical/list@0.50.0`、`@lexical/link@0.50.0`、`@lexical/code@0.50.0`。

这些 package 只进入实验自己的 `package.json` / `package-lock.json`；`node_modules` 保持忽略并可直接删除。安装后必须检查 registry 来源、SHA-512 integrity、完整 transitive license 和 `preinstall / install / postinstall`，出现未审阅许可证或 lifecycle script 就停止，不运行包内命令。React UI 与 Yjs 不属于首轮安装范围。

两套候选必须消费同一版本化 corpus：

- 空文档、段落、ATX 标题、强调、链接、引用、有序 / 无序 / 嵌套列表；
- fenced code、行内 code、软换行、两个空格或反斜杠 hard break；
- 中文、emoji、组合字符、RTL 片段和超长单词 / URL；
- paste 的 plain text / Markdown、未知 HTML、危险链接和不支持节点；
- 大文档、schema version N → N+1、重复 parse / serialize 和 malformed input。

每个 case 记录输入 Markdown、内部 JSON、规范化 Markdown、第二次往返结果、结构摘要和丢失诊断。通过条件是第二次往返稳定、支持语义不丢失、不支持输入显式失败或受控保留、输出可由服务端独立 sanitizer 处理。不得通过忽略 fixture、隐藏 warning 或为候选分别降低 corpus 来获得通过。

浏览器阶段必须复核 keyboard-only、composition / IME、undo / redo、focus restore、copy / paste、screen reader 基本语义、390px 和长文档；jsdom 不能替代这些结果。

#### 阶段 A headless 实测（2026-09-03）

项目所有者已授权上述精确版本和独立 lockfile。`experiments/document-editor` 现已用 Node 24 内建 test runner 对两套候选运行同一 `radishnexus-markdown-v1` corpus；12 个 headless case 均满足重复 parse 内部 JSON 一致、第二次 Markdown 往返稳定、声明支持的文本和结构不丢失。初次 headless lockfile 共 74 个 transitive package，只来自 npm 官方 registry，全部使用 SHA-512 integrity，transitive license 只有 MIT / BSD-2-Clause，lifecycle install script 为 0，npm high-level audit 为 0 漏洞。

实测同时暴露了不能隐藏的差异：Tiptap 将反斜杠 hard break 规范化为两个空格、转义未知 raw HTML，但原生 MarkdownManager 会把未注册节点静默序列化为空字符串，因此 RadishNexus 适配层必须在 serialize 前按版本化 node / mark schema fail closed；Lexical 原生拒绝未知节点，但会原样保留 raw HTML。两者都会保留 `javascript:` link，证明 editor import / export 不能承担 sanitizer，正式服务端必须独立拒绝危险协议和 unsupported HTML。

完整 case、内部 JSON、SHA-256、规范化结果、第二次往返、结构摘要与诊断由 `npm run report` 可重复输出，汇总结论记录在 [`experiments/document-editor/RESULTS.md`](../../experiments/document-editor/RESULTS.md)。

#### 阶段 A 浏览器实测（2026-09-03）

项目所有者随后授权 dev-only `vite@8.2.2` 与 `@lexical/history@0.50.0`，实验以原生 DOM 装配两套候选，不引入 React 或正式 Web bundle。扩展后的 lockfile 共 115 个 transitive package，仍全部来自 npm 官方 registry 并使用 SHA-512；license 为 Apache-2.0、BSD-2-Clause、BSD-3-Clause、ISC、MIT、MPL-2.0。唯一 lifecycle manifest 是精确审阅的 optional `fsevents@2.3.3`，安装固定 `ignore-scripts=true`，high-level audit 为 0 漏洞。

内置浏览器已经证明两套候选的 keyboard formatting、正常编辑 undo / redo、toolbar focus restore、plain-text paste / copy、自定义 `text/markdown` MIME paste、ARIA 基本语义、Unicode 直接输入、390px 和 400-section 长文档交互。390px 下 document / body / viewport width 均为 390，无可见元素横向越界，两套 editor 的 `clientWidth` 与 `scrollWidth` 都为 340；长文档末尾内容存在且只在 editor 内纵向滚动。最终干净标签的 console 无 warning / error。

实测发现并修复了 Lexical harness 的两个集成问题：Markdown MIME 被自定义 handler 消费后，默认 plain fallback 仍继续执行并造成重复内容；Clear 后 selection format 未重置并把 bold 泄漏到新输入。两项都已在真实浏览器复验。对照页 production build 为 758.85 kB JavaScript / 238.81 kB gzip，包含两套候选与共享 corpus，只作为实验成本上限，不是单候选 production bundle。

内置浏览器控制层明确不支持 `Input.imeSetComposition`；自动化可以直接输入 `你好`，但 composition start / update / end 计数均为 0，因此该结果没有被冒充为 IME 证据。项目所有者随后在同一隔离页面用 macOS 中文输入法分别向两个 editor 输入 `萝卜输入测试`，确认无双写、吞字或 selection 跳动；输入完第二个 editor 后，第一次 `⌘Z` 按当前焦点只撤销第二个 editor，重新聚焦第一个 editor 后一次 `⌘Z` 也完整撤销对应 composition。两套候选的 OS IME 与焦点隔离 history 因此均通过。

阶段 A 据此选择 Tiptap 3 / ProseMirror 进入阶段 B。Lexical 没有在 React、IME 或可访问性装配上表现出足以替代第一候选的优势，而 ProseMirror 的 schema / transaction 基础和 Tiptap 官方 Yjs binding 与后续实验边界更直接。选择仍保留全部已知风险：Tiptap Markdown Beta、未知节点原生静默丢失、危险链接保留和 headless UI 责任必须由版本化适配层、服务端校验和正式 UI 门禁控制。阶段 B 依赖尚未授权或安装，本 ADR 继续保持“提议”。

### 阶段 B：内存协同收敛

只有阶段 A 选出编辑器后，才在实验内加入 Yjs，不启动 WebSocket 或浏览器持久化。用两个内存 Y.Doc 和可控 update 传递验证：

- 并发不相交与重叠编辑最终收敛；
- update 重复、乱序、延迟和断开后恢复；
- 初始内容只建立一次，不产生重复空段落；
- 本地 undo 不撤销他人的修改；
- schema 不一致、未知 node、损坏 update 和超限输入 fail closed；
- 从 compact snapshot + 后续 updates 恢复，与可读 Markdown snapshot 一致。

阶段 B 只证明数据结构和 editor binding，不证明授权、transport、服务持久化或生产可用。

### 阶段 C：正式协议前置条件

若 M5 确认多人在线协作有真实需求，再单独提出 ADR，冻结 Go server / sidecar 边界、认证授权、update envelope、持久化、compaction、资源上限、代理、graceful shutdown 和浏览器撤权清理。该 ADR 接受后才能安装或启动正式协同 transport。

## 未采用的方案

### 现在把 Tiptap JSON 当作数据库和公共格式

这样最快看到富文本 UI，但把 schema upgrade、Flutter、导出和工具兼容绑定到 Web 包。Tiptap Markdown 仍为 Beta，也不能证明任意扩展都能无损导出，因此不采用。

### 现在把 Yjs update 当作唯一权威事实

opaque update 适合合并，不自动提供业务 revision、审计、可读导出、权限过滤或长期 schema 迁移。没有 snapshot / compaction / restore 合同就直接持久化，会把实验 binary 变成无法退出的数据库格式，因此不采用。

### 直接使用 Tiptap Collaboration 或其它托管服务

这会让核心自部署路径依赖外部产品合同，并把 Comments、Snapshots、provider token 和运维边界带入当前阶段。即使有 on-premises 选项，也不能替代 RadishNexus 自己的权限、备份和恢复合同，因此不采用。

### 当前同时实现 Yjs 与 Automerge

两者的数据模型、sync 和 persistence 不兼容。并行实现会让 editor binding、恢复和跨端评估翻倍，而当前没有离线真实用例能决定取舍，因此 Automerge 延后。

## 后果

正面影响：

- Golden Path 可以用最小 Markdown Document 完成关系与恢复验证，不被 M5 协同复杂度阻塞；
- 编辑器、协同内核、transport、持久化和可移植格式各自有清晰退出边界；
- Tiptap 与 Lexical 共享同一 corpus，Yjs 只在选定编辑器后验证，实验规模可控；
- Web、Flutter、导出和备份不会被某个编辑器私有 JSON 或 CRDT binary 静默绑定；
- 商业扩展不会成为自部署核心能力的隐式依赖。

成本与限制：

- M0.5 先交付 Markdown textarea 而不是完整所见即所得；
- Markdown snapshot 不能表达所有未来富文本能力，支持子集和规范化必须严格版本化；
- 若阶段 A 证明两套候选都不能满足往返或 IME，需要重新收窄 schema 或评估其它编辑器；
- M5 仍需独立解决 CRDT 持久化、compaction、多副本、权限撤销和评论锚点；
- M7 离线能力不能仅靠添加 IndexedDB provider，仍需完整数据清理和用户切换合同。

## 接受与验证门槛

本 ADR 只有同时满足以下条件才可改为“已接受”：

1. 项目所有者明确授权实验依赖与独立 lockfile，仓库未改变正式 Web 依赖；
2. 阶段 A 的共享 corpus、round-trip 报告、bundle / dependency 清单和浏览器证据完成；
3. 明确选择 Tiptap、Lexical 或拒绝两者，并记录具体失败样例，而不是凭偏好选型；
4. 阶段 B 的确定性内存协同测试完成，或明确决定 M0.5 / M1 不引入 CRDT；
5. Document 最小字段、revision、权限、EntityLink、事件、备份和导出边界另行审查；
6. `./scripts/check-repo.sh` 与实验自己的无网络门禁通过；
7. `docs/status/current.md` 记录真实通过项和剩余停止线。

在接受前，不新增正式 migration、Document HTTP route、production dependency、WebSocket、浏览器持久化或 public schema。

## 规范与候选依据

- [ProseMirror Guide](https://prosemirror.net/docs/guide/)：schema、transaction、step 与 central-authority collaboration；
- [ProseMirror Markdown](https://github.com/ProseMirror/prosemirror-markdown)：CommonMark 对应 schema、parser 与 serializer；
- [Tiptap editor overview](https://tiptap.dev/docs/editor/getting-started/overview)：ProseMirror 基础、headless extension 与开源核心边界；
- [Tiptap Markdown](https://tiptap.dev/docs/editor/markdown)：双向 Markdown 能力及 Beta 状态；
- [Tiptap accessibility guide](https://tiptap.dev/docs/guides/accessibility)：headless UI 的 keyboard、ARIA 与 VoiceOver 责任；
- [Tiptap collaboration extension](https://tiptap.dev/docs/editor/extensions/functionality/collaboration)：Y.Doc binding、协同 undo 与初始化注意事项；
- [Lexical Editor State](https://lexical.dev/docs/concepts/editor-state) 与 [Lexical React plugins](https://lexical.dev/docs/react/plugins)：immutable snapshot、serialization、node / plugin 装配与 collaboration 初始化；
- [Lexical MIT license](https://github.com/facebook/lexical/blob/main/LICENSE)；
- [Yjs](https://github.com/yjs/yjs)：shared types、idempotent / commutative updates、provider 分层、snapshot 与 undo；
- [Yjs WebSocket server](https://github.com/yjs/y-websocket-server)：开发 server、认证和 persistence 起点的官方限制；
- [Yjs IndexedDB provider](https://github.com/yjs/y-indexeddb)：浏览器离线持久化行为与清理 API；
- [Automerge design](https://automerge.org/docs/hello/)、[repository adapters](https://automerge.org/docs/reference/repositories/) 与 [rich text](https://automerge.org/docs/reference/documents/rich-text/)：local-first、network / storage 分层和 marks / block markers。
